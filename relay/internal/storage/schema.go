package storage

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/Shugur-Network/relay/internal/logger"
	"go.uber.org/zap"
)

//go:embed schema.sql
var schemaDDL string

// CreateDatabaseIfNotExists creates the specified database if it doesn't exist
func (db *DB) CreateDatabaseIfNotExists(ctx context.Context, dbName string) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	logger.Info("Checking if database exists...", zap.String("database", dbName))

	// Check if database exists
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`,
		dbName).Scan(&exists)

	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if !exists {
		// Create database
		logger.Info("Creating database...", zap.String("database", dbName))
		_, err = db.Pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
		if err != nil {
			return fmt.Errorf("failed to create database %s: %w", dbName, err)
		}
		logger.Info("✅ Database created successfully", zap.String("database", dbName))
	} else {
		logger.Info("✅ Database already exists", zap.String("database", dbName))
	}

	return nil
}

// InitializeSchema creates the necessary database and tables if they don't exist
func (db *DB) InitializeSchema(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	logger.Info("Initializing database schema...")

	// Fast path: if the events table already exists, skip DDL entirely.
	// All DDL uses IF NOT EXISTS / CREATE OR REPLACE, so re-running is safe
	// but slow (~2min on 60K+ rows due to index existence checks).
	var tableExists bool
	if err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'events')`,
	).Scan(&tableExists); err != nil {
		logger.Warn("Could not check for existing schema, running full DDL", zap.Error(err))
	} else if tableExists {
		logger.Info("✅ Database schema already exists, skipping DDL")
		return nil
	}

	// Split DDL into individual statements and execute each one.
	// pgx extended query protocol only supports single statements;
	// splitting avoids the need for simple protocol and handles
	// dollar-quoted function bodies correctly.
	statements := splitSQL(schemaDDL)
	for _, stmt := range statements {
		if _, err := db.Pool.Exec(ctx, stmt); err != nil {
			logger.Error("Failed to execute schema statement", zap.Error(err), zap.String("sql", stmt[:min(len(stmt), 80)]))
			return fmt.Errorf("failed to initialize database schema: %w", err)
		}
	}

	logger.Info("✅ Database schema initialized successfully")
	return nil
}

// splitSQL splits a SQL script into individual statements, respecting
// dollar-quoted strings (e.g. $$ ... $$) so function bodies are not split.
func splitSQL(ddl string) []string {
	var stmts []string
	var cur strings.Builder
	inDollarQuote := false
	runes := []rune(ddl)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		// Detect $$ delimiter
		if ch == '$' && i+1 < len(runes) && runes[i+1] == '$' {
			cur.WriteRune(ch)
			cur.WriteRune(runes[i+1])
			i++
			inDollarQuote = !inDollarQuote
			continue
		}
		if ch == ';' && !inDollarQuote {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(ch)
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// VerifySchema checks if all required tables exist
func (db *DB) VerifySchema(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	requiredTables := []string{"events"}

	for _, table := range requiredTables {
		var exists bool
		err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_schema = 'public' 
				AND table_name = $1
			)`, table).Scan(&exists)

		if err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}

		if !exists {
			return fmt.Errorf("required table %s does not exist", table)
		}

		logger.Debug("✅ Table exists", zap.String("table", table))
	}

	logger.Debug("✅ Database schema verification completed")
	return nil
}
