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

-- Performance-optimized indexes (INCLUDE for covering queries, PostgreSQL 11+)
CREATE INDEX IF NOT EXISTS events_created_at_desc
  ON events (created_at DESC)
  INCLUDE (pubkey, kind, tags, content, sig);

CREATE INDEX IF NOT EXISTS events_kind_created_at
  ON events (kind ASC, created_at ASC)
  INCLUDE (pubkey, tags, content, sig);

CREATE INDEX IF NOT EXISTS events_pubkey_created_at
  ON events (pubkey ASC, created_at ASC)
  INCLUDE (kind, tags, content, sig);

-- GIN indexes for JSONB queries
CREATE INDEX IF NOT EXISTS events_tags ON events USING GIN (tags);

-- Unique partial indexes for Nostr protocol compliance (replaceable events)
CREATE UNIQUE INDEX IF NOT EXISTS uq_replaceable
  ON events (pubkey, kind)
  WHERE kind = 0 OR kind = 3 OR kind = 41 OR (kind >= 10000 AND kind < 20000);

-- Unique partial index for addressable events (kinds 30000-39999 with "d" tag)
CREATE UNIQUE INDEX IF NOT EXISTS uq_addressable
  ON events (pubkey, kind, (
    (SELECT elem->>1 FROM jsonb_array_elements(tags) AS elem
     WHERE elem->>0 = 'd' LIMIT 1)
  ))
  WHERE kind >= 30000 AND kind < 40000
    AND tags @> '[["d"]]'::jsonb;

-- =============================================================================
-- Performance Notes
-- =============================================================================
-- This schema provides:
-- 1. Optimized indexes with INCLUDE clauses for covering queries
-- 2. GIN index for efficient JSONB tag queries
-- 3. Partial unique indexes for Nostr replaceable/addressable event semantics
-- 4. Aurora PostgreSQL handles replication, compression, and HA automatically