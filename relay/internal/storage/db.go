package storage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Shugur-Network/relay/internal/constants"
	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willf/bloom"

	"go.uber.org/zap"
)

// DBState represents the current state of the database connection
type DBState int

const (
	DBStateInitial DBState = iota
	DBStateConnecting
	DBStateConnected
	DBStateDisconnecting
	DBStateClosed
)

// DB represents the PostgreSQL database connection
type DB struct {
	Pool            *pgxpool.Pool
	Bloom           *bloom.BloomFilter
	eventDispatcher *EventDispatcher
	state           DBState
	stateMu         sync.RWMutex
	errors          chan error
	errorCount      int32
	errorCountMu    sync.RWMutex
}

// createPoolBasedOnLoad creates optimized pool configuration based on expected WebSocket load
func createPoolBasedOnLoad(ctx context.Context, dbURI string, maxWSConnections int) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dbURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URI: %w", err)
	}
	
	// Determine appropriate pool size based on WebSocket connection limits
	// This provides a reliable scaling mechanism based on actual configuration
	var maxConns, minConns int32
	var scaleType string
	
	if maxWSConnections <= 200 {
		// Small scale: development, testing, small deployments
		maxConns = int32(constants.DBPoolSmallMaxConns)
		minConns = int32(constants.DBPoolSmallMinConns)
		scaleType = "small"
	} else if maxWSConnections <= 2000 {
		// Medium scale: typical production deployments
		maxConns = int32(constants.DBPoolMediumMaxConns)
		minConns = int32(constants.DBPoolMediumMinConns)
		scaleType = "medium"
	} else {
		// Large scale: high-traffic production deployments
		maxConns = int32(constants.DBPoolLargeMaxConns)
		minConns = int32(constants.DBPoolLargeMinConns)
		scaleType = "large"
	}
	
	// Configure pool with production-optimized settings
	config.MaxConns = maxConns
	config.MinConns = minConns
	config.MaxConnLifetime = constants.DBConnMaxLifetime
	config.MaxConnIdleTime = constants.DBConnMaxIdleTime
	config.ConnConfig.ConnectTimeout = constants.DBConnAcquireTimeout
	config.HealthCheckPeriod = 30 * time.Second // Regular health checks
	
	logger.Info("Database connection pool configured based on load",
		zap.String("scale_type", scaleType),
		zap.Int("max_ws_connections", maxWSConnections),
		zap.Int32("db_max_conns", maxConns),
		zap.Int32("db_min_conns", minConns),
		zap.Duration("max_lifetime", constants.DBConnMaxLifetime),
		zap.Duration("max_idle_time", constants.DBConnMaxIdleTime))
	
	return pgxpool.NewWithConfig(ctx, config)
}

// InitDB initializes the PostgreSQL connection with retries and optimized connection pooling
func InitDB(ctx context.Context, dbURI string, maxWSConnections int) (*DB, error) {
	var pool *pgxpool.Pool
	var err error
	backoff := 2 * time.Second
	attempts := 0

	db := &DB{
		state:  DBStateConnecting,
		errors: make(chan error, 100),
	}

	for i := 0; i < 5; i++ { // Retry up to 5 times
		attempts++
		// Create pool with load-based configuration
		pool, err = createPoolBasedOnLoad(ctx, dbURI, maxWSConnections)
		if err == nil {
			// Test the actual connection
			if err = pool.Ping(ctx); err == nil {
				db.Pool = pool
				// Size the bloom filter from the actual event count so the
				// 1% false-positive rate is preserved as the database grows
				// past 10M events (issue #80). Falls back to 10M if the
				// count query fails.
				count, countErr := db.estimateEventCount(ctx)
				if countErr != nil {
					logger.Warn("Failed to count events for bloom sizing; using default 10M", zap.Error(countErr))
					count = 10_000_000
				}
				db.Bloom = bloom.NewWithEstimates(uint(count+1_000_000), 0.01)
				db.state = DBStateConnected

				// Log pool configuration for verification
				stat := pool.Stat()
				logger.Info("✅ DB Connected Successfully",
					zap.Int("attempts", attempts),
					zap.Int("max_ws_connections", maxWSConnections),
					zap.Int64("event_count_estimate", count),
					zap.Int32("db_max_connections", stat.MaxConns()),
					zap.Int32("db_total_connections", stat.TotalConns()))
				metrics.DBConnections.WithLabelValues("success").Inc()
				return db, nil
			}
			// Connection pool created but ping failed, close it
			pool.Close()
		}

		logger.Warn("Failed to connect to DB, retrying...",
			zap.Error(err),
			zap.Int("attempt", attempts),
			zap.Duration("backoff", backoff))
		metrics.DBConnections.WithLabelValues("failure").Inc()
		time.Sleep(backoff)
		backoff *= 2 // Exponential backoff (2s, 4s, 8s...)
	}

	db.state = DBStateClosed
	metrics.DBErrors.WithLabelValues("connection_failed").Inc()
	return nil, fmt.Errorf("failed to connect to DB after %d attempts: %w", attempts, err)
}

// CloseDB closes the database connection
func (db *DB) CloseDB() error {
	db.stateMu.Lock()
	if db.state == DBStateDisconnecting || db.state == DBStateClosed {
		db.stateMu.Unlock()
		return nil
	}
	db.state = DBStateDisconnecting
	db.stateMu.Unlock()

	if db.Pool != nil {
		db.Pool.Close()
		db.state = DBStateClosed
		logger.Debug("Database connection closed")
		metrics.DBConnections.WithLabelValues("closed").Inc()
		return nil
	}

	return fmt.Errorf("database pool is nil")
}

// ExecuteQuery handles single-row queries (SELECT)
func (db *DB) ExecuteQuery(ctx context.Context, query string, args ...interface{}) (pgx.Row, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	logger.Debug("Executing query",
		zap.String("query", query),
		zap.Any("args", args))

	row := db.Pool.QueryRow(ctx, query, args...)
	return row, nil
}

// ExecuteBatch handles batch inserts or updates
func (db *DB) ExecuteBatch(ctx context.Context, batch *pgx.Batch) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		db.recordError(fmt.Errorf("failed to start transaction: %w", err))
		metrics.DBErrors.WithLabelValues("transaction_start_failed").Inc()
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			// Only log rollback errors if the transaction hasn't been committed
			db.recordError(fmt.Errorf("rollback failed: %w", rollbackErr))
		}
	}()

	br := tx.SendBatch(ctx, batch)
	if err := br.Close(); err != nil {
		db.recordError(fmt.Errorf("batch execution failed: %w", err))
		metrics.DBErrors.WithLabelValues("batch_execution_failed").Inc()
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		db.recordError(fmt.Errorf("transaction commit failed: %w", err))
		metrics.DBErrors.WithLabelValues("transaction_commit_failed").Inc()
		return err
	}

	logger.Debug("Batch operation completed")
	metrics.DBOperations.WithLabelValues("batch_success").Inc()
	return nil
}

// ExecuteCommand handles INSERT, UPDATE, DELETE commands
func (db *DB) ExecuteCommand(ctx context.Context, query string, args ...interface{}) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	logger.Debug("Executing command",
		zap.String("query", query),
		zap.Any("args", args))

	_, err := db.Pool.Exec(ctx, query, args...)
	if err != nil {
		db.recordError(fmt.Errorf("command execution failed: %w", err))
		logger.Error("Command execution failed",
			zap.Error(err),
			zap.String("query", query))
		metrics.DBErrors.WithLabelValues("command_execution_failed").Inc()
	}
	return err
}

// RebuildBloomFilter fetches all event IDs from PostgreSQL and updates the Bloom filter.
func (db *DB) RebuildBloomFilter(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	logger.Info("Rebuilding Bloom filter from database...")

	query := `SELECT id FROM events`
	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		db.recordError(fmt.Errorf("failed to fetch event IDs: %w", err))
		metrics.DBErrors.WithLabelValues("bloom_filter_fetch_failed").Inc()
		return err
	}
	defer rows.Close()

	count := 0
	db.Bloom.ClearAll()

	for rows.Next() {
		// Allow context cancellation to interrupt the long scan (issue #79).
		if err := ctx.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("bloom rebuild canceled: %w", err)
		}
		var eventID string
		if err := rows.Scan(&eventID); err != nil {
			db.recordError(fmt.Errorf("failed to scan event ID: %w", err))
			logger.Debug("Failed to scan event ID",
				zap.Error(err))
			continue
		}

		db.Bloom.AddString(eventID)
		count++

		if count%100000 == 0 {
			logger.Debug("Bloom filter progress",
				zap.Int("events", count))
		}
	}

	if err := rows.Err(); err != nil {
		db.recordError(fmt.Errorf("error scanning rows: %w", err))
		metrics.DBErrors.WithLabelValues("bloom_filter_scan_failed").Inc()
		return err
	}

	logger.Info("Bloom filter rebuilt successfully",
		zap.Int("total_events", count))
	metrics.DBOperations.WithLabelValues("bloom_filter_rebuild_success").Inc()
	return nil
}

// isConnected checks if the database is in a connected state
func (db *DB) isConnected() bool {
	db.stateMu.RLock()
	defer db.stateMu.RUnlock()
	return db.state == DBStateConnected
}

// recordError records an error in the database service
func (db *DB) recordError(err error) {
	db.errorCountMu.Lock()
	db.errorCount++
	count := db.errorCount
	db.errorCountMu.Unlock()

	select {
	case db.errors <- err:
	default:
		// Channel is full, log directly
		logger.Error("Database error (channel full)",
			zap.Error(err),
			zap.Int32("error_count", count))
	}
}

// Add this helper function to your DB struct
func (db *DB) executeWithRetry(ctx context.Context, f func(context.Context) error) error {
	retries := 3
	var lastErr error

	for i := 0; i < retries; i++ {
		err := f(ctx)
		if err == nil {
			return nil
		}

		// Check if error is a timeout or deadlock (retryable)
		if strings.Contains(err.Error(), "statement timeout") ||
			strings.Contains(err.Error(), "deadlock") {
			lastErr = err
			// Exponential backoff
			time.Sleep(time.Duration(1<<i) * 100 * time.Millisecond)
			continue
		}

		// Not a retryable error
		return err
	}

	return fmt.Errorf("operation failed after %d retries: %w", retries, lastErr)
}

// SetEventDispatcher sets the event dispatcher reference for immediate local broadcasting
func (db *DB) SetEventDispatcher(ed *EventDispatcher) {
	db.eventDispatcher = ed
}

// Ping checks database connectivity
func (db *DB) Ping() error {
	if db.Pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return db.Pool.Ping(ctx)
}

// Stats returns database connection pool statistics
func (db *DB) Stats() DatabaseStats {
	if db.Pool == nil {
		return DatabaseStats{}
	}
	
	stat := db.Pool.Stat()
	return DatabaseStats{
		OpenConnections:     int(stat.TotalConns()),
		InUse:               int(stat.AcquiredConns()),
		Idle:                int(stat.IdleConns()),
		MaxOpenConnections:  int(stat.MaxConns()),
		MaxIdleConnections:  int(stat.MaxConns()), // pgxpool doesn't separate max idle
	}
}

// DatabaseStats represents database connection pool statistics
type DatabaseStats struct {
	OpenConnections    int
	InUse             int  
	Idle              int
	MaxOpenConnections int
	MaxIdleConnections int
}

// estimateEventCount returns a fast row-count estimate for sizing the
// Bloom filter. It is a count-only query with a short statement
// timeout, intended for startup use only (issue #80).
func (db *DB) estimateEventCount(ctx context.Context) (int64, error) {
	if !db.isConnected() {
		return 0, fmt.Errorf("database is not connected")
	}
	// Use a small statement timeout so a stuck table scan cannot
	// delay startup indefinitely. On a healthy production database
	// pg_class.reltuples is O(1) and instant.
	var count int64
	err := db.Pool.QueryRow(ctx, `SELECT reltuples::bigint FROM pg_class WHERE relname = 'events'`).Scan(&count)
	if err != nil || count < 0 {
		// Fallback to a precise count if the reltuples estimate is missing
		// (e.g. table not yet analyzed).
		countCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		err = db.Pool.QueryRow(countCtx, `SELECT COUNT(*) FROM events`).Scan(&count)
	}
	return count, err
}
