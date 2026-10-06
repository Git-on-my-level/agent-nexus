-- Unregistered empty shadow DDL. Not a migration, backfill, or certification.
-- Deliberately no IF NOT EXISTS: an incompatible existing schema must fail.
CREATE TABLE scope_feed (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation > 0),
 family TEXT NOT NULL, audience_key TEXT NOT NULL,
 sort_key INTEGER NOT NULL, rid INTEGER NOT NULL CHECK(rid > 0),
 version INTEGER NOT NULL CHECK(version > 0),
 PRIMARY KEY(scope_id,generation,family,audience_key,sort_key,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_feed_resource
 ON scope_feed(scope_id,generation,family,audience_key,rid);
CREATE TABLE scope_counters (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation > 0),
 family TEXT NOT NULL, audience_key TEXT NOT NULL, bucket TEXT NOT NULL,
 value INTEGER NOT NULL CHECK(typeof(value)='integer' AND value >= 0),
 PRIMARY KEY(scope_id,generation,family,audience_key,bucket)
) WITHOUT ROWID;
-- Each certificate is versioned, scoped to this exact immutable generation.
-- All zero by default. Only a future reviewed certification writer may set them.
CREATE TABLE scope_feed_generations (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation > 0),
 projection_version INTEGER NOT NULL DEFAULT 0,
 audience_version INTEGER NOT NULL DEFAULT 0,
 lifecycle_version INTEGER NOT NULL DEFAULT 0,
 legacy_auth_version INTEGER NOT NULL DEFAULT 0,
 legacy_auth_epoch INTEGER NOT NULL DEFAULT -1,
 PRIMARY KEY(scope_id,generation),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
-- Authority data, never caller asserted. Membership generation invalidates old
-- bindings even when the same principal is subsequently granted access again.
CREATE TABLE scope_feed_bindings (
 principal TEXT NOT NULL, scope_id TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation > 0),
 family TEXT NOT NULL, audience_key TEXT NOT NULL,
 membership_generation INTEGER NOT NULL CHECK(membership_generation > 0),
 binding_generation INTEGER NOT NULL CHECK(binding_generation > 0),
 PRIMARY KEY(principal,scope_id,generation,family,audience_key),
 FOREIGN KEY(principal,scope_id) REFERENCES scope_memberships(principal,scope_id) ON DELETE CASCADE
) WITHOUT ROWID;
-- Audience-specific payloads cannot be fetched through RID alone.
CREATE TABLE scope_feed_payloads (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation > 0),
 family TEXT NOT NULL, audience_key TEXT NOT NULL,
 rid INTEGER NOT NULL CHECK(rid > 0), version INTEGER NOT NULL CHECK(version > 0),
 data TEXT NOT NULL CHECK(length(CAST(data AS BLOB)) <= 16384),
 PRIMARY KEY(scope_id,generation,family,audience_key,rid,version)
) WITHOUT ROWID;
