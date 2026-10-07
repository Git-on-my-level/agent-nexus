-- Disabled, empty admission table. No production writer or schema migration.
-- A base-row comparison receipt NEVER substitutes for this serving proof.
CREATE TABLE scope_inbox_serving_receipts (
 snapshot_binding TEXT PRIMARY KEY CHECK(length(snapshot_binding)=64),
 source_revision INTEGER NOT NULL CHECK(typeof(source_revision)='integer' AND source_revision>0),
 authority_revision INTEGER NOT NULL CHECK(typeof(authority_revision)='integer' AND authority_revision>0),
 directory_revision INTEGER NOT NULL CHECK(typeof(directory_revision)='integer' AND directory_revision>0),
 registry_hash TEXT NOT NULL CHECK(length(registry_hash)=64),
 format_version INTEGER NOT NULL DEFAULT 0,
 policy_version INTEGER NOT NULL DEFAULT 0,
 projector_version INTEGER NOT NULL DEFAULT 0,
 discovery_version INTEGER NOT NULL DEFAULT 0,
 enrichment_version INTEGER NOT NULL DEFAULT 0,
 derivation_version INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
