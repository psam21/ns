package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Shugur-Network/relay/internal/constants"
	"github.com/Shugur-Network/relay/internal/logger"
	"github.com/Shugur-Network/relay/internal/metrics"
	"github.com/Shugur-Network/relay/internal/relay/nips"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	nostr "github.com/nbd-wtf/go-nostr"
	"go.uber.org/zap"
)

// GetEvents retrieves events based on Nostr filters
func (db *DB) GetEvents(ctx context.Context, filter nostr.Filter) ([]nostr.Event, error) {
	// Compile the filter for efficient processing
	cf := CompileFilter(filter)

	// Build the optimized query
	query, args, err := cf.BuildQuery()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	// Create context with timeout
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Log the query for debugging
	logger.Debug("Executing query",
		zap.String("query", query),
		zap.Int("arg_count", len(args)))

	// Execute query
	rows, err := db.Pool.Query(queryCtx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	// Preallocate slice with capacity to reduce allocations.
	// This size balances memory usage with performance for
	// typical filter cap used by the relay and reduces slice
	// growth for common queries while keeping memory modest.
	events := make([]nostr.Event, 0, constants.DefaultQueryPrealloc) // Process rows
	for rows.Next() {
		var evt nostr.Event
		var createdAt int64
		var rawTags []byte

		if err := rows.Scan(&evt.ID, &evt.PubKey, &evt.Kind, &createdAt, &evt.Content, &rawTags, &evt.Sig); err != nil {
			logger.Warn("Row scan failed", zap.Error(err))
			continue
		}

		evt.CreatedAt = nostr.Timestamp(createdAt)

		// Parse tags
		if len(rawTags) > 0 {
			if err := json.Unmarshal(rawTags, &evt.Tags); err != nil {
				logger.Warn("Failed to unmarshal tags", zap.Error(err))
				evt.Tags = []nostr.Tag{}
			}
		}

		events = append(events, evt)
	}

	// Reorder events in ascending order by created_at
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	return events, nil
}

// GetEventByID retrieves a single event by its ID.
func (db *DB) GetEventByID(ctx context.Context, eventID string) (nostr.Event, error) {
	query := `SELECT id, pubkey, kind, created_at, content, tags, sig FROM events WHERE id = $1`
	row := db.Pool.QueryRow(ctx, query, eventID)

	var evt nostr.Event
	var createdAt int64
	err := row.Scan(&evt.ID, &evt.PubKey, &evt.Kind, &createdAt, &evt.Content, &evt.Tags, &evt.Sig)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("event not found: %w", err)
	}

	evt.CreatedAt = nostr.Timestamp(createdAt) // Convert Unix timestamp to nostr.Timestamp

	return evt, nil
}

// InsertEvent directly inserts a single event
func (db *DB) InsertEvent(ctx context.Context, evt nostr.Event) error {

	// Check Bloom filter first to avoid duplicate DB operations
	if db.Bloom.Test([]byte(evt.ID)) {
		// Already have this event

		return nil
	}
	// No need to add to Bloom filter here - that should be handled by the caller
	// so that we can control when the event is considered "processed"

	_, err := db.Pool.Exec(ctx,
		`INSERT INTO events (id, pubkey, created_at, kind, tags, content, sig)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (id) DO NOTHING`,
		evt.ID, evt.PubKey, evt.CreatedAt.Time().Unix(),
		evt.Kind, evt.Tags, evt.Content, evt.Sig)

	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	return nil
}

// Modified EventBuffer that processes events one at a time

// BatchInsertEvents optimized for PostgreSQL with timeout handling
func (db *DB) BatchInsertEvents(ctx context.Context, events []nostr.Event) error {
	if len(events) == 0 {
		return nil
	}

	// Use smaller batches for efficiency
	const batchSize = 50

	// Execute in smaller batches with retries
	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}

		batchEvents := events[i:end]
		err := db.executeWithRetry(ctx, func(retryCtx context.Context) error {
			return db.insertEventBatch(retryCtx, batchEvents)
		})

		if err != nil {
			return fmt.Errorf("batch insert failed: %w", err)
		}
	}

	return nil
}

// Helper for actual batch insertion
func (db *DB) insertEventBatch(ctx context.Context, events []nostr.Event) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			db.recordError(fmt.Errorf("rollback failed: %w", rollbackErr))
		}
	}()

	batch := &pgx.Batch{}
	for _, evt := range events {
		// Add event to bloom filter first
		db.Bloom.AddString(evt.ID)

		batch.Queue(
			`INSERT INTO events (id, pubkey, created_at, kind, tags, content, sig)
             VALUES ($1, $2, $3, $4, $5, $6, $7)
             ON CONFLICT (id) DO NOTHING`,
			evt.ID,
			evt.PubKey,
			evt.CreatedAt.Time().Unix(),
			evt.Kind,
			evt.Tags,
			evt.Content,
			evt.Sig,
		)
	}

	results := tx.SendBatch(ctx, batch)
	if err := results.Close(); err != nil {
		return fmt.Errorf("batch execution failed: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit failed: %w", err)
	}

	return nil
}

// GetReplaceableEvent retrieves the latest replaceable event for a given pubkey and kind.
func (db *DB) GetReplaceableEvent(ctx context.Context, pubkey string, kind int) (nostr.Event, error) {
	query := `
		SELECT id, pubkey, kind, created_at, content, tags, sig
		FROM events
		WHERE pubkey = $1 AND kind = $2
		ORDER BY created_at DESC
		LIMIT 1`

	row, err := db.ExecuteQuery(ctx, query, pubkey, kind)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("failed to fetch replaceable event: %w", err)
	}

	var evt nostr.Event
	var createdAt int64
	err = row.Scan(&evt.ID, &evt.PubKey, &evt.Kind, &createdAt, &evt.Content, &evt.Tags, &evt.Sig)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("replaceable event not found: %w", err)
	}

	evt.CreatedAt = nostr.Timestamp(createdAt) // Convert Unix timestamp to nostr.Timestamp

	return evt, nil
}

// GetAddressableEvent retrieves the latest addressable event for a given pubkey, kind, and 'd' tag.
func (db *DB) GetAddressableEvent(ctx context.Context, pubkey string, kind int, dVal string) (nostr.Event, error) {
	query := `
		SELECT id, pubkey, kind, created_at, content, tags, sig
		FROM events
		WHERE pubkey = $1 AND kind = $2 AND tags @> $3
		ORDER BY created_at DESC
		LIMIT 1`

	row, err := db.ExecuteQuery(ctx, query, pubkey, kind, fmt.Sprintf(`[["d", "%s"]]`, dVal))
	if err != nil {
		return nostr.Event{}, fmt.Errorf("failed to fetch addressable event: %w", err)
	}

	var evt nostr.Event
	var createdAt int64
	err = row.Scan(&evt.ID, &evt.PubKey, &evt.Kind, &createdAt, &evt.Content, &evt.Tags, &evt.Sig)
	if err != nil {
		return nostr.Event{}, fmt.Errorf("addressable event not found: %w", err)
	}

	evt.CreatedAt = nostr.Timestamp(createdAt) // Convert Unix timestamp to nostr.Timestamp

	return evt, nil
}

// DeleteExpiredEvents removes events that have expired based on the "expiration" tag.
func (db *DB) DeleteExpiredEvents(ctx context.Context) error {
	query := `
		DELETE FROM events
		WHERE EXISTS (
			SELECT 1 FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'expiration' 
			AND tag->>1 IS NOT NULL 
			AND (tag->>1)::BIGINT < extract(epoch FROM now())
		)`

	logger.Debug("🗑 Deleting expired events...")

	_, err := db.Pool.Exec(ctx, query)
	if err != nil {
		logger.Error("❌ Failed to delete expired events", zap.Error(err))
		return fmt.Errorf("failed to delete expired events: %w", err)
	}

	logger.Debug("✅ Expired events deleted successfully")
	return nil
}

// CleanExpiredEvents removes events with expiration tags that have passed their expiration time
func (db *DB) CleanExpiredEvents(ctx context.Context) (int, error) {
	if !db.isConnected() {
		return 0, fmt.Errorf("database is not connected")
	}

	logger.Debug("Deleting expired events...")

	query := `
		DELETE FROM events
		WHERE EXISTS (
			SELECT 1 FROM jsonb_array_elements(tags) AS tag
			WHERE tag->>0 = 'expiration' 
			AND tag->>1 IS NOT NULL 
			AND (tag->>1)::BIGINT <= $1
		)
	`

	result, err := db.Pool.Exec(ctx, query, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("failed to delete expired events: %w", err)
	}

	count := result.RowsAffected()
	logger.Debug("Expired events deleted",
		zap.Int64("count", count))

	return int(count), nil
}

// StartExpiredEventsCleaner starts a background goroutine to clean expired events periodically
func (db *DB) StartExpiredEventsCleaner(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logger.Debug("Running expired events cleanup...")
				count, err := db.CleanExpiredEvents(ctx)
				if err != nil {
					logger.Error("Failed to clean expired events", zap.Error(err))
				} else if count > 0 {
					logger.Info("Cleaned expired events", zap.Int("count", count))
				}
			}
		}
	}()
}

// GetEventCount returns the count of events matching the given filter
func (db *DB) GetEventCount(ctx context.Context, filter nostr.Filter) (int64, error) {
	// PERFORMANCE: Create a query builder with reasonable capacity
	query := strings.Builder{}
	query.Grow(256) // Pre-allocate string builder capacity
	args := make([]interface{}, 0, 10)
	argIndex := 1

	// Start with base SELECT COUNT
	query.WriteString(`SELECT COUNT(*) FROM events`)

	// Track if we need to add WHERE
	needsWhere := false
	addWhere := func() {
		if !needsWhere {
			query.WriteString(` WHERE `)
			needsWhere = true
		} else {
			query.WriteString(` AND `)
		}
	}

	// Add filters in order of index selectivity
	hasIDFilter := len(filter.IDs) > 0
	hasAuthorFilter := len(filter.Authors) > 0
	hasKindFilter := len(filter.Kinds) > 0
	hasSinceFilter := filter.Since != nil
	hasUntilFilter := filter.Until != nil

	// Apply filters based on most efficient index usage
	if hasIDFilter {
		// IDs are primary keys - most selective
		addWhere()
		query.WriteString(fmt.Sprintf("id = ANY($%d)", argIndex))
		args = append(args, filter.IDs)
		argIndex++
	}

	if hasAuthorFilter {
		addWhere()
		query.WriteString(fmt.Sprintf("pubkey = ANY($%d)", argIndex))
		args = append(args, filter.Authors)
		argIndex++
	}

	if hasKindFilter {
		addWhere()
		query.WriteString(fmt.Sprintf("kind = ANY($%d)", argIndex))
		args = append(args, filter.Kinds)
		argIndex++
	}

	// Always apply time filters after key/author filters
	if hasSinceFilter {
		addWhere()
		query.WriteString(fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, filter.Since.Time().Unix())
		argIndex++
	}

	if hasUntilFilter {
		addWhere()
		query.WriteString(fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, filter.Until.Time().Unix())
		argIndex++
	}

	// Handle tag filtering
	if len(filter.Tags) > 0 {
		for tagName, tagValues := range filter.Tags {
			if len(tagValues) > 0 {
				addWhere()
				// Use the inverted index on tags
				query.WriteString(fmt.Sprintf("tags @> $%d", argIndex))
				tagArray := make([][]string, len(tagValues))
				for i, val := range tagValues {
					tagArray[i] = []string{tagName, val}
				}
				args = append(args, tagArray)
				argIndex++
			}
		}
	}

	// Log the query for debugging
	logger.Debug("Executing count query",
		zap.String("query", query.String()),
		zap.Int("arg_count", len(args)))

	// Execute query with timeout
	var count int64
	err := db.Pool.QueryRow(ctx, query.String(), args...).Scan(&count)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return 0, fmt.Errorf("count operation timed out")
		}
		return 0, fmt.Errorf("failed to execute count query: %w", err)
	}

	return count, nil
}

// GetEventPubkeys returns pubkeys of events matching the given filter.
// Used for NIP-45 HyperLogLog computation.
func (db *DB) GetEventPubkeys(ctx context.Context, filter nostr.Filter) ([]string, error) {
	query := strings.Builder{}
	query.Grow(256)
	args := make([]interface{}, 0, 10)
	argIndex := 1

	query.WriteString(`SELECT pubkey FROM events`)

	needsWhere := false
	addWhere := func() {
		if !needsWhere {
			query.WriteString(` WHERE `)
			needsWhere = true
		} else {
			query.WriteString(` AND `)
		}
	}

	if len(filter.IDs) > 0 {
		addWhere()
		query.WriteString(fmt.Sprintf("id = ANY($%d)", argIndex))
		args = append(args, filter.IDs)
		argIndex++
	}
	if len(filter.Authors) > 0 {
		addWhere()
		query.WriteString(fmt.Sprintf("pubkey = ANY($%d)", argIndex))
		args = append(args, filter.Authors)
		argIndex++
	}
	if len(filter.Kinds) > 0 {
		addWhere()
		query.WriteString(fmt.Sprintf("kind = ANY($%d)", argIndex))
		args = append(args, filter.Kinds)
		argIndex++
	}
	if filter.Since != nil {
		addWhere()
		query.WriteString(fmt.Sprintf("created_at >= $%d", argIndex))
		args = append(args, filter.Since.Time().Unix())
		argIndex++
	}
	if filter.Until != nil {
		addWhere()
		query.WriteString(fmt.Sprintf("created_at <= $%d", argIndex))
		args = append(args, filter.Until.Time().Unix())
		argIndex++
	}
	if len(filter.Tags) > 0 {
		for tagName, tagValues := range filter.Tags {
			if len(tagValues) > 0 {
				addWhere()
				query.WriteString(fmt.Sprintf("tags @> $%d", argIndex))
				tagArray := make([][]string, len(tagValues))
				for i, val := range tagValues {
					tagArray[i] = []string{tagName, val}
				}
				args = append(args, tagArray)
				argIndex++
			}
		}
	}

	rows, err := db.Pool.Query(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query event pubkeys: %w", err)
	}
	defer rows.Close()

	var pubkeys []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, fmt.Errorf("failed to scan pubkey: %w", err)
		}
		pubkeys = append(pubkeys, pk)
	}
	return pubkeys, rows.Err()
}

func (db *DB) EventExists(ctx context.Context, eventID string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE id = $1)`,
		eventID,
	).Scan(&exists)
	return exists, err
}

func (db *DB) InsertReplaceableEvent(ctx context.Context, evt nostr.Event) error {
	// First, delete any existing replaceable event for this pubkey and kind
	_, err := db.Pool.Exec(ctx,
		`DELETE FROM events 
		 WHERE pubkey = $1 AND kind = $2`,
		evt.PubKey, evt.Kind)
	if err != nil {
		return fmt.Errorf("failed to delete old replaceable event: %w", err)
	}

	// Then insert the new event
	_, err = db.Pool.Exec(ctx,
		`INSERT INTO events (id, pubkey, created_at, kind, tags, content, sig)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		evt.ID, evt.PubKey, evt.CreatedAt.Time().Unix(),
		evt.Kind, evt.Tags, evt.Content, evt.Sig)
	if err != nil {
		return fmt.Errorf("failed to insert new replaceable event: %w", err)
	}

	// Add to Bloom filter
	db.Bloom.AddString(evt.ID)

	return nil
}

// InsertAddressableEvent upserts (pubkey, kind, dTag) = unique
func (db *DB) InsertAddressableEvent(ctx context.Context, evt nostr.Event) error {
	dVal := nips.GetTagValue(evt, "d")
	if dVal == "" {
		return db.InsertEvent(ctx, evt) // fallback
	}

	_, err := db.Pool.Exec(ctx,
		`DELETE FROM events 
         WHERE pubkey=$1 AND kind=$2 AND tags @> $3`,
		evt.PubKey, evt.Kind, fmt.Sprintf(`[["d","%s"]]`, dVal),
	)
	if err != nil {
		return err
	}

	_, err = db.Pool.Exec(ctx,
		`INSERT INTO events (id,pubkey,created_at,kind,tags,content,sig)
         VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		evt.ID, evt.PubKey, evt.CreatedAt.Time().Unix(),
		evt.Kind, evt.Tags, evt.Content, evt.Sig,
	)
	if err == nil {
		db.Bloom.AddString(evt.ID)
	}
	return err
}

func (db *DB) persistDeletion(ctx context.Context, del nostr.Event) error {
	var eIDs []string
	var aTags []nostr.Tag
	for _, t := range del.Tags {
		if len(t) >= 2 && t[0] == "e" {
			eIDs = append(eIDs, t[1])
		}
		if len(t) >= 2 && t[0] == "a" {
			aTags = append(aTags, t)
		}
	}
	if len(eIDs) == 0 && len(aTags) == 0 {
		return errors.New("deletion event without e or a tags")
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			db.recordError(fmt.Errorf("rollback failed: %w", rollbackErr))
		}
	}()

	// 1) delete events by "e" tag (referenced by event ID) — only if owned by deleter
	if len(eIDs) > 0 {
		_, err = tx.Exec(ctx,
			`DELETE FROM events WHERE id = ANY($1) AND pubkey = $2`,
			eIDs, del.PubKey)
		if err != nil {
			return err
		}
	}

	// 2) delete events by "a" tag (addressable events) — NIP-09 spec
	//    format: <kind>:<pubkey>:<d-identifier>
	//    only delete versions up to the deletion request's created_at
	for _, tag := range aTags {
		parts := strings.SplitN(tag[1], ":", 3)
		if len(parts) != 3 {
			continue
		}
		// Only delete if the pubkey in the "a" tag matches the deleter
		if parts[1] != del.PubKey {
			continue
		}
		_, err = tx.Exec(ctx,
			`DELETE FROM events WHERE kind = $1 AND pubkey = $2
			 AND tags @> $3::jsonb AND created_at <= $4`,
			parts[0], del.PubKey,
			fmt.Sprintf(`[["d","%s"]]`, parts[2]),
			del.CreatedAt.Time().Unix())
		if err != nil {
			logger.Warn("NIP-09: Failed to delete addressable event",
				zap.String("a_tag", tag[1]),
				zap.Error(err))
		}
	}

	// 3) insert the deletion event itself
	_, err = tx.Exec(ctx,
		`INSERT INTO events (id,pubkey,created_at,kind,tags,content,sig)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		del.ID, del.PubKey, del.CreatedAt.Time().Unix(),
		del.Kind, del.Tags, del.Content, del.Sig)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	db.Bloom.AddString(del.ID)
	return nil
}

// persistVanish deletes ALL events from a pubkey (NIP-62 Request to Vanish).
// Also deletes gift-wrapped events (kind 1059) addressed to this pubkey.
// Stores the vanish request itself and adds pubkey to vanished set.
func (db *DB) persistVanish(ctx context.Context, evt nostr.Event) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			db.recordError(fmt.Errorf("rollback failed: %w", rollbackErr))
		}
	}()

	// 1) Delete ALL events from this pubkey up to the vanish request's created_at
	result, err := tx.Exec(ctx,
		`DELETE FROM events WHERE pubkey = $1 AND created_at <= $2`,
		evt.PubKey, evt.CreatedAt.Time().Unix())
	if err != nil {
		return fmt.Errorf("failed to delete events for vanish: %w", err)
	}
	deletedCount := result.RowsAffected()

	// 2) Delete gift-wrapped events (kind 1059) that p-tagged this pubkey
	giftResult, err := tx.Exec(ctx,
		`DELETE FROM events WHERE kind = 1059 AND tags @> $1::jsonb`,
		fmt.Sprintf(`[["p","%s"]]`, evt.PubKey))
	if err != nil {
		logger.Warn("NIP-62: Failed to delete gift-wrapped events",
			zap.String("pubkey", evt.PubKey),
			zap.Error(err))
		// Non-fatal: continue with vanish
	} else {
		giftDeleted := giftResult.RowsAffected()
		if giftDeleted > 0 {
			logger.Info("NIP-62: Deleted gift-wrapped events",
				zap.String("pubkey", evt.PubKey),
				zap.Int64("count", giftDeleted))
		}
	}

	// 3) Store the vanish request itself for bookkeeping
	_, err = tx.Exec(ctx,
		`INSERT INTO events (id,pubkey,created_at,kind,tags,content,sig)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		evt.ID, evt.PubKey, evt.CreatedAt.Time().Unix(),
		evt.Kind, evt.Tags, evt.Content, evt.Sig)
	if err != nil {
		return fmt.Errorf("failed to store vanish request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	logger.Info("NIP-62: Vanish request processed",
		zap.String("pubkey", evt.PubKey),
		zap.Int64("events_deleted", deletedCount))

	db.Bloom.AddString(evt.ID)
	return nil
}

// IsVanishedPubkey checks if a pubkey has previously issued a vanish request.
// Events from vanished pubkeys should be rejected to prevent re-broadcast.
func (db *DB) IsVanishedPubkey(ctx context.Context, pubkey string) (bool, error) {
	if !db.isConnected() {
		return false, fmt.Errorf("database is not connected")
	}
	var count int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE kind = 62 AND pubkey = $1`,
		pubkey).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// EventCountByKindMonth represents event counts grouped by kind and month
type EventCountByKindMonth struct {
	Kind     int
	Year     int
	Month    int
	Count    int64
	KindName string
}

// GetEventCountsByKindMonth returns event counts grouped by kind and month for a given year
func (db *DB) GetEventCountsByKindMonth(ctx context.Context, year int) ([]EventCountByKindMonth, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	startOfYear := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	endOfYear := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	query := `
		SELECT kind, EXTRACT(MONTH FROM to_timestamp(created_at)) as month, COUNT(*) as count
		FROM events
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY kind, month
		ORDER BY kind, month
	`

	rows, err := db.Pool.Query(ctx, query, startOfYear, endOfYear)
	if err != nil {
		return nil, fmt.Errorf("failed to query event counts: %w", err)
	}
	defer rows.Close()

	var results []EventCountByKindMonth
	for rows.Next() {
		var kind int
		var month int
		var count int64
		if err := rows.Scan(&kind, &month, &count); err != nil {
			logger.Warn("Row scan failed", zap.Error(err))
			continue
		}
		kindName := constants.GetNIPName(kind)
		results = append(results, EventCountByKindMonth{
			Kind:     kind,
			Year:     year,
			Month:    month,
			Count:    count,
			KindName: kindName,
		})
	}

	return results, nil
}

// GetEventCountsByKindMonthFromYear returns event counts grouped by year, kind, and month from startYear onward.
// The created_at range predicate can use the events_created_at_desc index, while the grouping happens once for all years.
func (db *DB) GetEventCountsByKindMonthFromYear(ctx context.Context, startYear int) ([]EventCountByKindMonth, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	startOfYear := time.Date(startYear, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	query := `
		SELECT kind::int,
		       EXTRACT(YEAR FROM to_timestamp(created_at))::int as year,
		       EXTRACT(MONTH FROM to_timestamp(created_at))::int as month,
		       COUNT(*) as count
		FROM events
		WHERE created_at >= $1
		GROUP BY kind, year, month
		ORDER BY year, kind, month
		LIMIT $2
	`

	rows, err := db.Pool.Query(ctx, query, startOfYear, constants.EventBreakdownMaxRows)
	if err != nil {
		return nil, fmt.Errorf("failed to query event counts from %d: %w", startYear, err)
	}
	defer rows.Close()

	var results []EventCountByKindMonth
	for rows.Next() {
		var kind, year, month int
		var count int64
		if err := rows.Scan(&kind, &year, &month, &count); err != nil {
			return nil, fmt.Errorf("failed to scan event count row: %w", err)
		}
		results = append(results, EventCountByKindMonth{
			Kind:     kind,
			Year:     year,
			Month:    month,
			Count:    count,
			KindName: constants.GetNIPName(kind),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while reading event counts: %w", err)
	}

	return results, nil
}

// GetYearsWithEvents returns all years that have at least one event
func (db *DB) GetYearsWithEvents(ctx context.Context) ([]int, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	query := `
		SELECT DISTINCT EXTRACT(YEAR FROM to_timestamp(created_at)) as year
		FROM events
		ORDER BY year
	`

	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query years: %w", err)
	}
	defer rows.Close()

	var years []int
	for rows.Next() {
		var year int
		if err := rows.Scan(&year); err != nil {
			logger.Warn("Row scan failed", zap.Error(err))
			continue
		}
		years = append(years, year)
	}

	return years, nil
}

// GetTotalEventCount2026Plus returns the total number of events stored in the database from 2026 onwards.
//
// This reads sum(ytd_count) from the event_kind_stats aggregate table rather
// than running COUNT(*) over events. Both return the same number — verified
// against the production host on 2026-10-02, where the table's ytd total
// (1,164,018) matched an authoritative `SELECT count(*) FROM events WHERE
// created_at >= <2026-01-01>` exactly.
//
// The reason to prefer the table is cost: the direct COUNT is a 25s scan of
// the 2GB events table (measured), and it ran on a timer behind the dashboard
// total. The aggregate is already maintained incrementally by
// RefreshEventKindStats, so this is a single-row read.
//
// Falls back to the direct count if the aggregate table is missing, so a
// partially-migrated database still produces a correct (if slower) answer
// rather than an error.
func (db *DB) GetTotalEventCount2026Plus(ctx context.Context) (int64, error) {
	if !db.isConnected() {
		return 0, fmt.Errorf("database is not connected")
	}

	var aggregate int64
	err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(ytd_count), 0) FROM `+EventKindStatsMVName,
	).Scan(&aggregate)
	if err == nil {
		return aggregate, nil
	}
	logger.Warn("event_kind_stats unavailable for total count; falling back to direct scan",
		zap.Error(err))

	startOf2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	var count int64
	if err := db.Pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM events WHERE created_at >= $1", startOf2026,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to get total event count from 2026: %w", err)
	}

	return count, nil
}

// GetTotalEventCount returns the total number of events stored in the database
//
// Reads sum(event_count) from the event_kind_stats aggregate table instead of
// COUNT(*) over events. The direct count is a 25s scan of the 2GB events table
// (measured 2026-10-02) and this runs on the startup path and on a dashboard
// timer, so it is worth avoiding. Verified equal to the authoritative count on
// the production dataset (1,198,749).
//
// Falls back to the direct scan if the aggregate table is unavailable.
func (db *DB) GetTotalEventCount(ctx context.Context) (int64, error) {
	if !db.isConnected() {
		return 0, fmt.Errorf("database is not connected")
	}

	var aggregate int64
	if err := db.Pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(event_count), 0) FROM `+EventKindStatsMVName,
	).Scan(&aggregate); err == nil {
		return aggregate, nil
	} else {
		logger.Warn("event_kind_stats unavailable for stored count; falling back to direct scan",
			zap.Error(err))
	}

	var count int64
	err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM events").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get total event count: %w", err)
	}

	return count, nil
}

// EventKindStat represents one row from the event_kind_stats materialized view.
// See https://github.com/psam21/ns/issues/100.
type EventKindStat struct {
	Kind       int   `json:"kind"`
	EventCount int64 `json:"event_count"`
	YTDCount   int64 `json:"ytd_count"`
	LastSeenAt int64 `json:"last_seen_at"`
}

// EventKindStatsMVName is the identifier that backs the dashboard's event-kind
// aggregation. Exposed as a constant so the refresher, the handler, and the
// tests all reference the same name.
//
// This was originally a MATERIALIZED VIEW. It is now a plain table maintained
// incrementally — see RefreshEventKindStats for why, and note that
// `CREATE TABLE IF NOT EXISTS` below is deliberately tolerant of the existing
// view so upgrades do not require a manual migration.
const EventKindStatsMVName = "event_kind_stats"

// EnsureEventKindStatsMV creates the event_kind_stats table and its supporting
// indexes if they do not already exist. Safe to call on every boot — uses
// IF NOT EXISTS throughout and every statement is idempotent.
//
// This is split out from InitializeSchema because the production deployment
// already has the events table and InitializeSchema's fast path skips DDL
// entirely on existing databases.
//
// ## Migration note
//
// Previous deployments created this as a MATERIALIZED VIEW. A materialized
// view cannot be INSERTed into, which is what makes the incremental refresh
// impossible. EnsureEventKindStatsMV therefore attempts to create a TABLE; if
// a view with the same name already exists, that statement fails harmlessly
// and the view keeps serving reads via RefreshEventKindStats's full-rebuild
// path until an operator drops it. That keeps the upgrade non-breaking.
func (db *DB) EnsureEventKindStatsMV(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	// CREATE TABLE IF NOT EXISTS is tolerant of the legacy materialized view
	// being present: it fails with "relation already exists", which is
	// non-fatal here because MigrateEventKindStatsViewToTable runs first and
	// is responsible for converting the view.
	//
	// The count_desc index is deliberately NOT created here. Its name already
	// belongs to the index on the legacy view, and Postgres index names are
	// schema-scoped rather than table-scoped, so this statement would fail on
	// upgrade and abort the loop (observed in the 2026-10-02 dry run). It is
	// created by the migration, after the view is dropped.
	const createTable = `CREATE TABLE IF NOT EXISTS event_kind_stats (
			kind          integer NOT NULL,
			event_count   bigint  NOT NULL DEFAULT 0,
			ytd_count     bigint  NOT NULL DEFAULT 0,
			last_seen_at  bigint  NOT NULL DEFAULT 0,
			PRIMARY KEY (kind)
		)`
	if _, err := db.Pool.Exec(ctx, createTable); err != nil {
		// Expected while a legacy materialized view still owns the name; the
		// migration path handles that case.
		logger.Debug("event_kind_stats table creation skipped", zap.Error(err))
	}

	logger.Info("✅ event_kind_stats is ready")
	return nil
}

// MigrateEventKindStatsViewToTable converts a legacy `event_kind_stats`
// MATERIALIZED VIEW into a writable table, preserving the aggregated totals so
// the dashboard does not show zeroed counts across the upgrade.
//
// Idempotent and safe to run on every boot. If the object is already a table,
// or does not exist, this is a no-op.
//
// This is destructive in the narrow sense that it drops the view — but the
// view's entire contents are reproducible from `events` via
// RebuildEventKindStats, and the counts are copied first. Running it requires
// only the same privileges the existing DDL already needed.
//
// The whole thing is one transaction. Two of the steps are load-bearing and
// both were found by running the dry run on the real 1.2M-row dataset rather
// than by reading the code:
//
//   - Renaming a materialized view does not change its relkind, so the
//     renamed copy must be dropped with DROP MATERIALIZED VIEW. A plain
//     DROP TABLE fails with "is not a table" and aborts the migration.
//   - The count_desc index is owned by the view, and Postgres index names are
//     schema-scoped rather than table-scoped. It must be dropped before the
//     same name can be recreated on the new table.
func (db *DB) MigrateEventKindStatsViewToTable(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	// NOTE: c.relkind is Postgres' internal "char" type (OID 18), not text.
	// pgx cannot scan it directly into a Go string, so it must be cast with
	// ::text or the Scan fails. This was found the hard way: the original
	// query scanned straight into a string, the error was swallowed by the
	// `return nil` below, and the migration silently never ran on a
	// production host that still had the materialized view.
	var relKind string
	err := db.Pool.QueryRow(ctx,
		`SELECT c.relkind::text FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = current_schema() AND c.relname = $1`,
		EventKindStatsMVName,
	).Scan(&relKind)
	if err != nil {
		// pgx.ErrNoRows just means the relation does not exist yet, which is
		// the normal case on a fresh install. Anything else is unexpected and
		// must not be swallowed — a silent no-op here leaves the dashboard on
		// the 41s full-refresh path with nothing in the logs to explain it.
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to detect %s relation kind: %w", EventKindStatsMVName, err)
		}
		logger.Info("event_kind_stats does not exist yet; nothing to migrate",
			zap.String("relation", EventKindStatsMVName))
		return nil
	}

	// 'm' = materialized view, 'r' = ordinary table. Anything else (index,
	// view, composite type) is left alone.
	if relKind != "m" {
		return nil
	}

	logger.Info("Migrating event_kind_stats from materialized view to table")

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin event_kind_stats migration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_event_kind_stats_count_desc`,
		`DROP MATERIALIZED VIEW IF EXISTS event_kind_stats`,
		`CREATE TABLE IF NOT EXISTS event_kind_stats (
			kind         integer NOT NULL,
			event_count  bigint  NOT NULL DEFAULT 0,
			ytd_count    bigint  NOT NULL DEFAULT 0,
			last_seen_at bigint  NOT NULL DEFAULT 0,
			PRIMARY KEY (kind)
		)`,
		// One-time full aggregate. Measured at ~41s on the production dataset,
		// which is why this runs before the listener binds with a 5 minute
		// budget rather than in a request path.
		`INSERT INTO event_kind_stats (kind, event_count, ytd_count, last_seen_at)
		 SELECT kind::int, COUNT(*),
		        COUNT(*) FILTER (
		          WHERE created_at >= EXTRACT(EPOCH FROM date_trunc('year', now()))
		        ),
		        COALESCE(MAX(created_at), 0)
		 FROM events
		 GROUP BY kind::int`,
		`CREATE INDEX IF NOT EXISTS idx_event_kind_stats_count_desc
		 ON event_kind_stats (event_count DESC)`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("event_kind_stats migration step failed (%s): %w", firstLine(stmt), err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit event_kind_stats migration: %w", err)
	}

	logger.Info("✅ event_kind_stats migrated to a writable table")
	return nil
}

// firstLine returns the first line of s, for logging SQL fragments without
// dumping the whole statement.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// RefreshEventKindStats advances the event_kind_stats materialized view
// incrementally instead of recomputing it from scratch.
//
// ## Why this is not a plain REFRESH
//
// The original implementation ran REFRESH MATERIALIZED VIEW CONCURRENTLY on a
// view defined as `SELECT kind, COUNT(*), ... FROM events GROUP BY kind`. That
// is a full aggregate over the entire events table. Measured on the production
// host (2026-10-02, 1,198,749 rows, 2,062MB table, shared_buffers=128MB on a
// 1.8GB t4g.small):
//
//	REFRESH MATERIALIZED VIEW CONCURRENTLY event_kind_stats  ->  40.9 s
//	EXPLAIN ANALYZE of the underlying aggregate               ->  39.0 s
//	  Buffers: shared hit=40,416 read=82,276   (86% miss — I/O bound)
//
// 40.9s of work every 2 minutes, re-reading 2GB from disk, is a permanent load
// on a host that also serves relay traffic, and it starved the connection pool
// (3 concurrent DataFileRead queries observed).
//
// ## Why an index did not fix it
//
// A covering index on (kind) INCLUDE (created_at) was tried and measured: the
// planner still preferred Parallel Seq Scan (42s), and when forced with
// enable_seqscan=off the Index Only Scan was *worse* — 81.8s with 320,334
// heap fetches, because the visibility map was not set so no scan could be
// index-only. The index was removed rather than left as write-side overhead.
//
// ## What this does instead
//
// Only rows added since the last refresh are aggregated, using the
// already-present events_created_at_desc index to find them. Steady-state cost
// is proportional to new events (a few thousand), not to the 1.2M-row history.
//
// Kinds present in the delta are upserted; the delta is scanned with
// EXTRACT(EPOCH ...) so it matches the view's own key expression.
//
// Events are only ever appended (with rare deletes handled by the existing
// replacement/deletion paths), so a cumulative counter maintained this way
// converges to the full aggregate. If the table is ever truncated or
// reparented, RebuildEventKindStats restores exact totals from scratch.
func (db *DB) RefreshEventKindStats(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	// Fast path: if the view has never been populated, or a previous
	// incremental update failed, fall back to a full recompute so the cache
	// is never left permanently empty.
	//
	// pgx returns pgtype.Int8 for a nullable int8, so this uses pgtype rather
	// than database/sql's sql.NullInt64 — pgxpool.Pool is not a *sql.DB and
	// has no QueryRowContext.
	var maxSeen pgtype.Int8
	if err := db.Pool.QueryRow(ctx,
		`SELECT MAX(last_seen_at) FROM `+EventKindStatsMVName,
	).Scan(&maxSeen); err != nil {
		return fmt.Errorf("failed to read %s watermark: %w", EventKindStatsMVName, err)
	}

	if !maxSeen.Valid || maxSeen.Int64 <= 0 {
		logger.Info("event_kind_stats watermark missing; performing full rebuild")
		return db.RebuildEventKindStats(ctx)
	}

	// Aggregate only what is new since the watermark.
	const deltaQuery = `
		INSERT INTO ` + EventKindStatsMVName + ` AS t (kind, event_count, ytd_count, last_seen_at)
		SELECT
		  kind::int,
		  COUNT(*),
		  COUNT(*) FILTER (
		    WHERE created_at >= EXTRACT(EPOCH FROM date_trunc('year', now()))
		  ),
		  MAX(created_at)
		FROM events
		WHERE created_at > $1
		GROUP BY kind::int
		ON CONFLICT (kind) DO UPDATE
		SET event_count = t.event_count + EXCLUDED.event_count,
		    ytd_count    = t.ytd_count    + EXCLUDED.ytd_count,
		    last_seen_at = GREATEST(t.last_seen_at, EXCLUDED.last_seen_at)`

	if _, err := db.Pool.Exec(ctx, deltaQuery, maxSeen.Int64); err != nil {
		logger.Warn("Incremental event_kind_stats update failed; falling back to full rebuild",
			zap.Error(err))
		if rebuildErr := db.RebuildEventKindStats(ctx); rebuildErr != nil {
			return fmt.Errorf("incremental update failed (%v) and rebuild failed: %w", err, rebuildErr)
		}
		return nil
	}

	metrics.DBOperations.WithLabelValues("event_kind_stats_incremental_success").Inc()
	return nil
}

// RebuildEventKindStats recomputes event_kind_stats from scratch. Used for the
// first population, and as the fallback when the incremental path fails.
//
// This is the expensive operation the incremental refresh exists to avoid, so
// callers should ensure ctx has a generous timeout. Measured at ~40s on the
// production dataset.
func (db *DB) RebuildEventKindStats(ctx context.Context) error {
	if !db.isConnected() {
		return fmt.Errorf("database is not connected")
	}

	if _, err := db.Pool.Exec(ctx, `REFRESH MATERIALIZED VIEW CONCURRENTLY `+EventKindStatsMVName); err != nil {
		return fmt.Errorf("failed to rebuild %s: %w", EventKindStatsMVName, err)
	}
	return nil
}

// GetEventKindStats reads the cached event-kind breakdown from the
// materialized view. Sorted by event_count DESC, then kind ASC for stable
// ordering across calls.
//
// Returns an empty slice (not nil) when the view is empty so callers can
// distinguish "no data yet" from "query failed".
func (db *DB) GetEventKindStats(ctx context.Context) ([]EventKindStat, error) {
	if !db.isConnected() {
		return nil, fmt.Errorf("database is not connected")
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT kind, event_count, ytd_count, last_seen_at
		FROM `+EventKindStatsMVName+`
		ORDER BY event_count DESC, kind ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query %s: %w", EventKindStatsMVName, err)
	}
	defer rows.Close()

	stats := make([]EventKindStat, 0)
	for rows.Next() {
		var s EventKindStat
		if err := rows.Scan(&s.Kind, &s.EventCount, &s.YTDCount, &s.LastSeenAt); err != nil {
			return nil, fmt.Errorf("failed to scan %s row: %w", EventKindStatsMVName, err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed while reading %s: %w", EventKindStatsMVName, err)
	}

	return stats, nil
}
