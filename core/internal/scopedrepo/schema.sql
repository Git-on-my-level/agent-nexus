-- Additive shadow schema. No production initializer invokes this yet; no released
-- migration number is allocated. All identities below are opaque external IDs.
CREATE TABLE IF NOT EXISTS scope_domains (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('active','transitioning')),
 generation INTEGER NOT NULL CHECK(generation > 0)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scope_domains_state ON scope_domains(state,id);
CREATE TABLE IF NOT EXISTS scope_memberships (
 principal TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('reader','writer','owner','admin')),
 generation INTEGER NOT NULL CHECK(generation > 0),
 PRIMARY KEY(principal,scope_id),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_resources (
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 id TEXT NOT NULL,
 canonical_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 PRIMARY KEY(scope_id,kind,id),
 UNIQUE(kind,canonical_id),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_aliases (
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 alias TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 retired INTEGER NOT NULL DEFAULT 0 CHECK(retired IN (0,1)),
 PRIMARY KEY(scope_id,kind,alias),
 FOREIGN KEY(scope_id,kind,resource_id) REFERENCES scope_resources(scope_id,kind,id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_replays (
 principal TEXT NOT NULL,
 replay_key TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 PRIMARY KEY(principal,replay_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_projection_values (
 scope_id TEXT NOT NULL,
 projection_key TEXT NOT NULL,
 value TEXT NOT NULL CHECK(length(CAST(value AS BLOB)) <= 65536),
 PRIMARY KEY(scope_id,projection_key),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
