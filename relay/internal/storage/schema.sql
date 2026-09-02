-- Shugur Relay Database Schema
-- PostgreSQL (Aurora) optimized schema for Nostr relay
-- Database: Defined in constants.DatabaseName

-- =============================================================================
-- Events table - stores all Nostr events with optimized indexes
-- =============================================================================
CREATE TABLE IF NOT EXISTS events (
  id CHAR(64) NOT NULL,
  pubkey CHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  kind BIGINT NOT NULL,
  tags JSONB NULL,
  content TEXT NULL,
  sig CHAR(128) NOT NULL,

  -- Primary key
  CONSTRAINT events_pkey PRIMARY KEY (id),

  -- Data validation constraints
  CONSTRAINT valid_id CHECK (id ~ '^[a-f0-9]{64}$'),
  CONSTRAINT valid_pubkey CHECK (pubkey ~ '^[a-f0-9]{64}$'),
  CONSTRAINT valid_sig CHECK (sig ~ '^[a-f0-9]{128}$'),
  CONSTRAINT kind_range CHECK (kind >= 0 AND kind <= 65535)
);

-- Performance-optimized indexes
CREATE INDEX IF NOT EXISTS events_created_at_desc
  ON events (created_at DESC);

CREATE INDEX IF NOT EXISTS events_kind_created_at
  ON events (kind ASC, created_at ASC);

CREATE INDEX IF NOT EXISTS events_pubkey_created_at
  ON events (pubkey ASC, created_at ASC);

-- GIN indexes for JSONB queries
CREATE INDEX IF NOT EXISTS events_tags ON events USING GIN (tags);

-- Unique partial indexes for Nostr protocol compliance (replaceable events)
CREATE UNIQUE INDEX IF NOT EXISTS uq_replaceable
  ON events (pubkey, kind)
  WHERE kind = 0 OR kind = 3 OR kind = 41 OR (kind >= 10000 AND kind < 20000);

-- Helper function to extract "d" tag value for addressable events
CREATE OR REPLACE FUNCTION nostr_d_tag(tags JSONB)
RETURNS TEXT IMMUTABLE LANGUAGE sql AS $$
  SELECT elem->>1 FROM jsonb_array_elements(tags) AS elem
  WHERE elem->>0 = 'd' LIMIT 1
$$;

-- Unique partial index for addressable events (kinds 30000-39999 with "d" tag)
CREATE UNIQUE INDEX IF NOT EXISTS uq_addressable
  ON events (pubkey, kind, nostr_d_tag(tags))
  WHERE kind >= 30000 AND kind < 40000
    AND tags @> '[["d"]]'::jsonb;

-- =============================================================================
-- Performance Notes
-- =============================================================================
-- This schema provides:
-- 1. Standard btree indexes for common query patterns
-- 2. GIN index for efficient JSONB tag queries
-- 3. Partial unique indexes for Nostr replaceable/addressable event semantics
-- 4. Aurora PostgreSQL handles replication, compression, and HA automatically
-- =============================================================================
-- Materialized view: event_kind_stats
-- Pre-aggregated event-kind breakdown for the dashboard.
-- Refreshed in the background via REFRESH MATERIALIZED VIEW CONCURRENTLY
-- so reads never block writes to the events table.
-- See https://github.com/psam21/ns/issues/100
-- =============================================================================
CREATE MATERIALIZED VIEW IF NOT EXISTS event_kind_stats AS
SELECT
  kind::int                                                    AS kind,
  COUNT(*)                                                     AS event_count,
  COUNT(*) FILTER (WHERE created_at >= EXTRACT(EPOCH FROM date_trunc('year', now()))) AS ytd_count,
  MAX(created_at)                                              AS last_seen_at
FROM events
GROUP BY kind;

-- Required for REFRESH MATERIALIZED VIEW CONCURRENTLY.
CREATE UNIQUE INDEX IF NOT EXISTS uq_event_kind_stats_kind
  ON event_kind_stats (kind);

-- Optional: index for the "top kinds" sort.
CREATE INDEX IF NOT EXISTS idx_event_kind_stats_count_desc
  ON event_kind_stats (event_count DESC);
