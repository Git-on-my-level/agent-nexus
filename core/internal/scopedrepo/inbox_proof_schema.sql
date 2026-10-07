-- Disabled, unregistered maintenance checkpoint. These are BASE ROW comparison
-- receipts, never serving selection proofs or evidence of HTTP enrichment parity.
CREATE TABLE scope_inbox_verification_jobs (
 id TEXT PRIMARY KEY, principal TEXT NOT NULL, pm_actor_id TEXT NOT NULL,
 source_revision INTEGER NOT NULL, authority_revision INTEGER NOT NULL,
 directory_revision INTEGER NOT NULL, shadow_revision INTEGER NOT NULL,
 legacy_epoch INTEGER NOT NULL, registry_hash TEXT NOT NULL,
 phase TEXT NOT NULL, checkpoint TEXT NOT NULL DEFAULT '{}',
 examined INTEGER NOT NULL DEFAULT 0, canonical_rows INTEGER NOT NULL DEFAULT 0,
 eligible INTEGER NOT NULL DEFAULT 0,
 failure TEXT NOT NULL DEFAULT '',
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_token INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_token)='integer' AND lease_token>=0),
 lease_until INTEGER NOT NULL DEFAULT 0 CHECK(typeof(lease_until)='integer' AND lease_until>=0),
 created_at INTEGER NOT NULL DEFAULT 0,
 finished_at INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
CREATE INDEX scope_inbox_verification_retention ON scope_inbox_verification_jobs(finished_at,id)
 WHERE finished_at>0;
CREATE TABLE scope_inbox_verification_scopes (
 job_id TEXT NOT NULL, scope_id TEXT NOT NULL, generation INTEGER NOT NULL,
 role TEXT NOT NULL, membership_generation INTEGER NOT NULL,
 PRIMARY KEY(job_id,scope_id)
) WITHOUT ROWID;
CREATE TABLE scope_inbox_verification_streams (
 job_id TEXT NOT NULL, scope_id TEXT NOT NULL, generation INTEGER NOT NULL,
 audience_key TEXT NOT NULL, binding_generation INTEGER NOT NULL,
 PRIMARY KEY(job_id,scope_id,audience_key)
) WITHOUT ROWID;
CREATE TABLE scope_inbox_verification_seen (
 job_id TEXT NOT NULL, rid INTEGER NOT NULL, scope_id TEXT NOT NULL,
 audience_key TEXT NOT NULL, version INTEGER NOT NULL,
 PRIMARY KEY(job_id,rid)
) WITHOUT ROWID;
CREATE TABLE scope_inbox_verification_counts (
 job_id TEXT NOT NULL, scope_id TEXT NOT NULL, audience_key TEXT NOT NULL,
 bucket TEXT NOT NULL, expected INTEGER NOT NULL DEFAULT 0,
 matched INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(job_id,scope_id,audience_key,bucket)
) WITHOUT ROWID;
CREATE TABLE scope_inbox_comparison_receipts (
 job_id TEXT PRIMARY KEY, principal TEXT NOT NULL, pm_actor_id TEXT NOT NULL,
 source_revision INTEGER NOT NULL, authority_revision INTEGER NOT NULL,
 directory_revision INTEGER NOT NULL, shadow_revision INTEGER NOT NULL,
 legacy_epoch INTEGER NOT NULL, registry_hash TEXT NOT NULL,
 policy_version INTEGER NOT NULL, projector_version INTEGER NOT NULL,
 canonical_rows INTEGER NOT NULL, eligible_rows INTEGER NOT NULL,
 -- Fixed meaning: complete canonical BASE relation and own directory only.
 comparison_kind TEXT NOT NULL CHECK(comparison_kind='canonical_base_v1'),
 serving_eligible INTEGER NOT NULL CHECK(serving_eligible=0)
) WITHOUT ROWID;
