package storage

import (
	"context"
	"database/sql"
	"fmt"
)

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);`

type migration struct {
	Version    int
	Statements []string
}

var migrations = []migration{
	{
		Version: 1,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS events (
				id TEXT PRIMARY KEY,
				type TEXT NOT NULL,
				ts TEXT NOT NULL,
				actor_id TEXT NOT NULL,
				thread_id TEXT,
				refs_json TEXT NOT NULL DEFAULT '[]',
				payload_json TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);`,
			`CREATE TABLE IF NOT EXISTS snapshots (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				thread_id TEXT,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				body_json TEXT NOT NULL,
				provenance_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE TABLE IF NOT EXISTS artifacts (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				content_path TEXT NOT NULL,
				refs_json TEXT NOT NULL DEFAULT '[]',
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE TABLE IF NOT EXISTS actors (
				id TEXT PRIMARY KEY,
				display_name TEXT NOT NULL,
				tags_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE TABLE IF NOT EXISTS derived_views (
				id TEXT PRIMARY KEY,
				view_type TEXT NOT NULL,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL,
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_events_thread_ts ON events (thread_id, ts);`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_kind ON snapshots (kind);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_created_at ON artifacts (kind, created_at);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_views_type ON derived_views (view_type);`,
		},
	},
	{
		Version: 2,
		Statements: []string{
			`ALTER TABLE events ADD COLUMN body_json TEXT NOT NULL DEFAULT '{}'`,
			`ALTER TABLE artifacts ADD COLUMN content_type TEXT NOT NULL DEFAULT 'application/octet-stream'`,
		},
	},
	{
		Version: 3,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS agents (
				id TEXT PRIMARY KEY,
				username TEXT NOT NULL UNIQUE,
				actor_id TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				revoked_at TEXT,
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE TABLE IF NOT EXISTS agent_keys (
				id TEXT PRIMARY KEY,
				agent_id TEXT NOT NULL,
				public_key TEXT NOT NULL,
				algorithm TEXT NOT NULL,
				created_at TEXT NOT NULL,
				revoked_at TEXT,
				FOREIGN KEY(agent_id) REFERENCES agents(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_keys_agent_id ON agent_keys (agent_id);`,
			`CREATE TABLE IF NOT EXISTS auth_refresh_sessions (
				id TEXT PRIMARY KEY,
				agent_id TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				revoked_at TEXT,
				replaced_by_session_id TEXT,
				FOREIGN KEY(agent_id) REFERENCES agents(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_refresh_sessions_agent_id ON auth_refresh_sessions (agent_id);`,
			`CREATE TABLE IF NOT EXISTS auth_access_tokens (
				id TEXT PRIMARY KEY,
				agent_id TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				revoked_at TEXT,
				FOREIGN KEY(agent_id) REFERENCES agents(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_access_tokens_agent_id ON auth_access_tokens (agent_id);`,
		},
	},
	{
		Version: 4,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS auth_used_assertions (
				assertion_hash TEXT PRIMARY KEY,
				used_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_used_assertions_used_at ON auth_used_assertions (used_at);`,
		},
	},
	{
		Version: 5,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS documents (
				id TEXT PRIMARY KEY,
				thread_id TEXT,
				title TEXT,
				slug TEXT,
				status TEXT,
				labels_json TEXT NOT NULL DEFAULT '[]',
				supersedes_json TEXT NOT NULL DEFAULT '[]',
				head_revision_id TEXT NOT NULL,
				head_revision_number INTEGER NOT NULL,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS document_revisions (
				revision_id TEXT PRIMARY KEY,
				document_id TEXT NOT NULL,
				revision_number INTEGER NOT NULL,
				prev_revision_id TEXT,
				artifact_id TEXT NOT NULL,
				thread_id TEXT,
				refs_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				UNIQUE(document_id, revision_number)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_head_revision_id ON documents (head_revision_id);`,
			`CREATE INDEX IF NOT EXISTS idx_document_revisions_document_id_revision_number ON document_revisions (document_id, revision_number);`,
			`CREATE INDEX IF NOT EXISTS idx_document_revisions_document_id_revision_id ON document_revisions (document_id, revision_id);`,
		},
	},
	{
		Version: 6,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS passkey_credentials (
				credential_id TEXT PRIMARY KEY,
				agent_id TEXT NOT NULL,
				user_handle BLOB NOT NULL,
				public_key BLOB NOT NULL,
				attestation_type TEXT NOT NULL,
				transport TEXT NOT NULL DEFAULT '',
				sign_count INTEGER NOT NULL DEFAULT 0,
				backup_eligible INTEGER NOT NULL DEFAULT 0,
				backup_state INTEGER NOT NULL DEFAULT 0,
				aaguid BLOB NOT NULL DEFAULT X'',
				attachment TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				FOREIGN KEY(agent_id) REFERENCES agents(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_credentials_agent_id ON passkey_credentials (agent_id);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_credentials_user_handle ON passkey_credentials (user_handle);`,
		},
	},
	{
		Version: 7,
		Statements: []string{
			`ALTER TABLE artifacts ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE document_revisions ADD COLUMN revision_hash TEXT NOT NULL DEFAULT ''`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_content_hash ON artifacts (content_hash)`,
		},
	},
	{
		Version: 8,
		Statements: []string{
			`ALTER TABLE artifacts ADD COLUMN tombstoned_at TEXT`,
			`ALTER TABLE artifacts ADD COLUMN tombstoned_by TEXT`,
			`ALTER TABLE artifacts ADD COLUMN tombstone_reason TEXT`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_tombstoned_at ON artifacts (tombstoned_at)`,
			`ALTER TABLE documents ADD COLUMN tombstoned_at TEXT`,
			`ALTER TABLE documents ADD COLUMN tombstoned_by TEXT`,
			`ALTER TABLE documents ADD COLUMN tombstone_reason TEXT`,
			`CREATE INDEX IF NOT EXISTS idx_documents_tombstoned_at ON documents (tombstoned_at)`,
		},
	},
	{
		Version: 9,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS idempotency_replays (
				scope TEXT NOT NULL,
				actor_id TEXT NOT NULL,
				request_key TEXT NOT NULL,
				request_hash TEXT NOT NULL,
				response_status INTEGER NOT NULL,
				response_json TEXT NOT NULL,
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				PRIMARY KEY(scope, actor_id, request_key)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_idempotency_replays_created_at ON idempotency_replays (created_at)`,
		},
	},
	{
		Version: 10,
		Statements: []string{
			`ALTER TABLE snapshots ADD COLUMN filter_status TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_priority TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_owner TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_due_at TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_cadence TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_cadence_preset TEXT`,
			`ALTER TABLE snapshots ADD COLUMN filter_tags_json TEXT NOT NULL DEFAULT '[]'`,
			`UPDATE snapshots
			 SET filter_status = NULLIF(json_extract(body_json, '$.status'), ''),
			     filter_priority = NULLIF(json_extract(body_json, '$.priority'), ''),
			     filter_owner = NULLIF(json_extract(body_json, '$.owner'), ''),
			     filter_due_at = NULLIF(json_extract(body_json, '$.due_at'), ''),
			     filter_cadence = NULLIF(TRIM(COALESCE(json_extract(body_json, '$.cadence'), '')), ''),
			     filter_cadence_preset = CASE
			         WHEN kind != 'thread' THEN NULL
			         WHEN TRIM(COALESCE(json_extract(body_json, '$.cadence'), '')) = '' THEN 'reactive'
			         WHEN TRIM(json_extract(body_json, '$.cadence')) = 'reactive' THEN 'reactive'
			         WHEN TRIM(json_extract(body_json, '$.cadence')) IN ('daily', '0 9 * * *') THEN 'daily'
			         WHEN TRIM(json_extract(body_json, '$.cadence')) IN ('weekly', '0 9 * * 1') THEN 'weekly'
			         WHEN TRIM(json_extract(body_json, '$.cadence')) IN ('monthly', '0 9 1 * *') THEN 'monthly'
			         ELSE 'custom'
			     END,
			     filter_tags_json = CASE
			         WHEN kind = 'thread' AND json_type(json_extract(body_json, '$.tags')) = 'array' THEN json_extract(body_json, '$.tags')
			         ELSE '[]'
			     END`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_kind_updated_at ON snapshots (kind, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_kind_status_updated_at ON snapshots (kind, filter_status, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_kind_priority_updated_at ON snapshots (kind, filter_priority, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_kind_cadence_preset_updated_at ON snapshots (kind, filter_cadence_preset, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_commitments_thread_status_due_updated_at ON snapshots (kind, thread_id, filter_status, filter_due_at, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_snapshots_commitments_owner_status_due_updated_at ON snapshots (kind, filter_owner, filter_status, filter_due_at, updated_at DESC, id)`,
			`ALTER TABLE artifacts ADD COLUMN thread_id TEXT`,
			`UPDATE artifacts
			 SET thread_id = COALESCE(
			         NULLIF(json_extract(metadata_json, '$.thread_id'), ''),
			         (SELECT substr(value, 8) FROM json_each(artifacts.refs_json) WHERE value LIKE 'thread:%' LIMIT 1)
			     )`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_tombstoned_created_at ON artifacts (kind, tombstoned_at, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_tombstoned_created_at ON artifacts (thread_id, tombstoned_at, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_kind_tombstoned_created_at ON artifacts (thread_id, kind, tombstoned_at, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_documents_tombstoned_updated_at ON documents (tombstoned_at, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_documents_thread_tombstoned_updated_at ON documents (thread_id, tombstoned_at, updated_at DESC, id)`,
			`CREATE INDEX IF NOT EXISTS idx_documents_status_tombstoned_updated_at ON documents (status, tombstoned_at, updated_at DESC, id)`,
		},
	},
	{
		Version: 11,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS derived_inbox_items (
				id TEXT PRIMARY KEY,
				thread_id TEXT NOT NULL,
				category TEXT NOT NULL,
				trigger_at TEXT NOT NULL,
				due_at TEXT,
				has_due_at INTEGER NOT NULL DEFAULT 0,
				source_event_id TEXT,
				source_commitment_id TEXT,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL,
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_thread_trigger ON derived_inbox_items (thread_id, trigger_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_category_trigger ON derived_inbox_items (category, trigger_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_due_at ON derived_inbox_items (has_due_at, due_at, id);`,
			`CREATE TABLE IF NOT EXISTS derived_thread_views (
				thread_id TEXT PRIMARY KEY,
				stale INTEGER NOT NULL DEFAULT 0,
				last_activity_at TEXT,
				latest_stale_exception_at TEXT,
				inbox_count INTEGER NOT NULL DEFAULT 0,
				pending_decision_count INTEGER NOT NULL DEFAULT 0,
				recommendation_count INTEGER NOT NULL DEFAULT 0,
				decision_request_count INTEGER NOT NULL DEFAULT 0,
				decision_count INTEGER NOT NULL DEFAULT 0,
				artifact_count INTEGER NOT NULL DEFAULT 0,
				open_commitment_count INTEGER NOT NULL DEFAULT 0,
				document_count INTEGER NOT NULL DEFAULT 0,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL DEFAULT '{}',
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_thread_views_stale_generated_at ON derived_thread_views (stale, generated_at DESC, thread_id);`,
		},
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	appliedVersions, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if appliedVersions[m.Version] {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.Version, err)
		}

		for _, statement := range m.Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %d: %w", m.Version, err)
			}
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`,
			m.Version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.Version, err)
		}
	}

	return nil
}

func loadAppliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan schema migration row: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema migration rows: %w", err)
	}

	return applied, nil
}
