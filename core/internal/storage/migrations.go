package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"organization-autorunner-core/internal/authaudit"
)

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL
);`

type migration struct {
	Version    int
	Statements []string
	Apply      func(context.Context, *sql.Tx) error
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
	{
		Version: 12,
		Statements: []string{
			`UPDATE documents
			 SET thread_id = COALESCE(
			         NULLIF(thread_id, ''),
			         (
			             SELECT substr(value, 8)
			               FROM document_revisions dr, json_each(dr.refs_json)
			              WHERE dr.revision_id = documents.head_revision_id
			                AND value LIKE 'thread:%'
			              LIMIT 1
			         )
			     )
			WHERE documents.thread_id IS NULL OR documents.thread_id = ''`,
			`UPDATE document_revisions
			 SET thread_id = COALESCE(
			         NULLIF(thread_id, ''),
			         (
			             SELECT substr(value, 8)
			               FROM json_each(document_revisions.refs_json)
			              WHERE value LIKE 'thread:%'
			              LIMIT 1
			         )
			     )
			WHERE thread_id IS NULL OR thread_id = ''`,
		},
	},
	{
		Version: 13,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS derived_thread_dirty_queue (
				thread_id TEXT PRIMARY KEY,
				dirty_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_thread_dirty_queue_dirty_at ON derived_thread_dirty_queue (dirty_at ASC, thread_id ASC);`,
		},
	},
	{
		Version: 14,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS auth_bootstrap_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				consumed_token_hash TEXT NOT NULL,
				consumed_at TEXT NOT NULL,
				consumed_by_agent_id TEXT NOT NULL,
				consumed_by_actor_id TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS auth_invites (
				id TEXT PRIMARY KEY,
				token_hash TEXT NOT NULL UNIQUE,
				kind TEXT NOT NULL,
				created_by_agent_id TEXT NOT NULL,
				created_by_actor_id TEXT NOT NULL,
				note TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				expires_at TEXT,
				consumed_at TEXT,
				revoked_at TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_created_at ON auth_invites (created_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_consumed_at ON auth_invites (consumed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_revoked_at ON auth_invites (revoked_at);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_expires_at ON auth_invites (expires_at);`,
		},
	},
	{
		Version: 15,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS boards (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL,
				status TEXT NOT NULL,
				labels_json TEXT NOT NULL DEFAULT '[]',
				owners_json TEXT NOT NULL DEFAULT '[]',
				primary_thread_id TEXT NOT NULL,
				primary_document_id TEXT,
				column_schema_json TEXT NOT NULL,
				pinned_refs_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_status_updated_at ON boards (status, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_primary_thread_id ON boards (primary_thread_id);`,
			`CREATE TABLE IF NOT EXISTS board_cards (
				board_id TEXT NOT NULL,
				thread_id TEXT NOT NULL,
				column_key TEXT NOT NULL,
				rank TEXT NOT NULL,
				pinned_document_id TEXT,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				PRIMARY KEY (board_id, thread_id),
				FOREIGN KEY(board_id) REFERENCES boards(id) ON DELETE CASCADE
			);`,
			`CREATE INDEX IF NOT EXISTS idx_board_cards_board_column_rank ON board_cards (board_id, column_key, rank, thread_id);`,
			`CREATE INDEX IF NOT EXISTS idx_board_cards_thread_id ON board_cards (thread_id, board_id);`,
		},
	},
	{
		Version: 16,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS thread_projection_refresh_status (
				thread_id TEXT PRIMARY KEY,
				is_dirty INTEGER NOT NULL DEFAULT 0,
				in_progress INTEGER NOT NULL DEFAULT 0,
				queued_at TEXT,
				started_at TEXT,
				completed_at TEXT,
				last_error_at TEXT,
				last_error TEXT,
				updated_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_thread_projection_refresh_status_dirty ON thread_projection_refresh_status (is_dirty, in_progress, queued_at, thread_id);`,
		},
	},
	{
		Version: 17,
		Statements: []string{
			`ALTER TABLE artifacts RENAME TO artifacts_legacy`,
			`CREATE TABLE artifacts (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				thread_id TEXT,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				content_type TEXT NOT NULL,
				content_hash TEXT NOT NULL,
				refs_json TEXT NOT NULL DEFAULT '[]',
				metadata_json TEXT NOT NULL DEFAULT '{}',
				tombstoned_at TEXT,
				tombstoned_by TEXT,
				tombstone_reason TEXT
			);`,
			`INSERT INTO artifacts(
				id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json,
				tombstoned_at, tombstoned_by, tombstone_reason
			)
			SELECT
				id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json,
				tombstoned_at, tombstoned_by, tombstone_reason
			FROM artifacts_legacy`,
			`DROP TABLE artifacts_legacy`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_created_at ON artifacts (kind, created_at)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_content_hash ON artifacts (content_hash)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_tombstoned_at ON artifacts (tombstoned_at)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_tombstoned_created_at ON artifacts (kind, tombstoned_at, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_tombstoned_created_at ON artifacts (thread_id, tombstoned_at, created_at, id)`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_kind_tombstoned_created_at ON artifacts (thread_id, kind, tombstoned_at, created_at, id)`,
		},
		Apply: scrubLegacyArtifactMetadata,
	},
	{
		Version: 18,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS auth_audit_events (
				id TEXT PRIMARY KEY,
				event_type TEXT NOT NULL,
				occurred_at TEXT NOT NULL,
				actor_agent_id TEXT,
				actor_actor_id TEXT,
				subject_agent_id TEXT,
				subject_actor_id TEXT,
				invite_id TEXT,
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_occurred_at ON auth_audit_events (occurred_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_event_type ON auth_audit_events (event_type, occurred_at DESC, id DESC)`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_invite_id ON auth_audit_events (invite_id, occurred_at DESC, id DESC)`,
		},
		Apply: applyAuthAuditMigration,
	},
	{
		Version: 19,
		Statements: []string{
			`ALTER TABLE auth_audit_events ADD COLUMN occurred_at_sort_key TEXT`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_sort_key ON auth_audit_events (occurred_at_sort_key DESC, id DESC)`,
		},
		Apply: applyAuthAuditSortKeyMigration,
	},
	{
		Version:    20,
		Statements: nil,
		Apply:      applyThreadProjectionGenerationMigration,
	},
	{
		Version: 21,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS blob_usage_ledger (
				content_hash TEXT PRIMARY KEY,
				size_bytes INTEGER NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS blob_usage_totals (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				blob_bytes INTEGER NOT NULL DEFAULT 0,
				blob_objects INTEGER NOT NULL DEFAULT 0,
				rebuilt_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
		},
	},
	{
		Version:    22,
		Statements: nil,
		Apply:      applyV22ArchiveAndTombstoneColumnsMigration,
	},
	{
		Version:    23,
		Statements: nil,
		Apply:      applyV23EventArchiveAndTombstoneColumnsMigration,
	},
}

func applyV22ArchiveAndTombstoneColumnsMigration(ctx context.Context, tx *sql.Tx) error {
	addCol := func(table, column, ddl string) error {
		ok, err := tableExistsTx(ctx, tx, table)
		if err != nil || !ok {
			return err
		}
		exists, err := columnExistsTx(ctx, tx, table, column)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("migration 22 %s.%s: %w", table, column, err)
		}
		return nil
	}
	if err := addCol("artifacts", "archived_at", `ALTER TABLE artifacts ADD COLUMN archived_at TEXT`); err != nil {
		return err
	}
	if err := addCol("artifacts", "archived_by", `ALTER TABLE artifacts ADD COLUMN archived_by TEXT`); err != nil {
		return err
	}
	if ok, err := tableExistsTx(ctx, tx, "artifacts"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_artifacts_archived_at ON artifacts (archived_at)`); err != nil {
			return fmt.Errorf("migration 22 artifacts archived index: %w", err)
		}
	}

	if err := addCol("documents", "archived_at", `ALTER TABLE documents ADD COLUMN archived_at TEXT`); err != nil {
		return err
	}
	if err := addCol("documents", "archived_by", `ALTER TABLE documents ADD COLUMN archived_by TEXT`); err != nil {
		return err
	}
	if ok, err := tableExistsTx(ctx, tx, "documents"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_documents_archived_at ON documents (archived_at)`); err != nil {
			return fmt.Errorf("migration 22 documents archived index: %w", err)
		}
	}

	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"archived_at", `ALTER TABLE snapshots ADD COLUMN archived_at TEXT`},
		{"archived_by", `ALTER TABLE snapshots ADD COLUMN archived_by TEXT`},
		{"tombstoned_at", `ALTER TABLE snapshots ADD COLUMN tombstoned_at TEXT`},
		{"tombstoned_by", `ALTER TABLE snapshots ADD COLUMN tombstoned_by TEXT`},
		{"tombstone_reason", `ALTER TABLE snapshots ADD COLUMN tombstone_reason TEXT`},
	} {
		if err := addCol("snapshots", col.name, col.ddl); err != nil {
			return err
		}
	}
	if ok, err := tableExistsTx(ctx, tx, "snapshots"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_snapshots_archived_at ON snapshots (archived_at)`); err != nil {
			return fmt.Errorf("migration 22 snapshots archived index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_snapshots_tombstoned_at ON snapshots (tombstoned_at)`); err != nil {
			return fmt.Errorf("migration 22 snapshots tombstoned index: %w", err)
		}
	}

	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"archived_at", `ALTER TABLE boards ADD COLUMN archived_at TEXT`},
		{"archived_by", `ALTER TABLE boards ADD COLUMN archived_by TEXT`},
		{"tombstoned_at", `ALTER TABLE boards ADD COLUMN tombstoned_at TEXT`},
		{"tombstoned_by", `ALTER TABLE boards ADD COLUMN tombstoned_by TEXT`},
		{"tombstone_reason", `ALTER TABLE boards ADD COLUMN tombstone_reason TEXT`},
	} {
		if err := addCol("boards", col.name, col.ddl); err != nil {
			return err
		}
	}
	if ok, err := tableExistsTx(ctx, tx, "boards"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_boards_archived_at ON boards (archived_at)`); err != nil {
			return fmt.Errorf("migration 22 boards archived index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_boards_tombstoned_at ON boards (tombstoned_at)`); err != nil {
			return fmt.Errorf("migration 22 boards tombstoned index: %w", err)
		}
	}

	return nil
}

func applyV23EventArchiveAndTombstoneColumnsMigration(ctx context.Context, tx *sql.Tx) error {
	addCol := func(table, column, ddl string) error {
		ok, err := tableExistsTx(ctx, tx, table)
		if err != nil || !ok {
			return err
		}
		exists, err := columnExistsTx(ctx, tx, table, column)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("migration 23 %s.%s: %w", table, column, err)
		}
		return nil
	}
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"archived_at", `ALTER TABLE events ADD COLUMN archived_at TEXT`},
		{"archived_by", `ALTER TABLE events ADD COLUMN archived_by TEXT`},
		{"tombstoned_at", `ALTER TABLE events ADD COLUMN tombstoned_at TEXT`},
		{"tombstoned_by", `ALTER TABLE events ADD COLUMN tombstoned_by TEXT`},
		{"tombstone_reason", `ALTER TABLE events ADD COLUMN tombstone_reason TEXT`},
	} {
		if err := addCol("events", col.name, col.ddl); err != nil {
			return err
		}
	}
	if ok, err := tableExistsTx(ctx, tx, "events"); err != nil {
		return err
	} else if ok {
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_archived_at ON events (archived_at)`); err != nil {
			return fmt.Errorf("migration 23 events archived_at index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_tombstoned_at ON events (tombstoned_at)`); err != nil {
			return fmt.Errorf("migration 23 events tombstoned_at index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_thread_archived ON events (thread_id, archived_at)`); err != nil {
			return fmt.Errorf("migration 23 events thread_archived index: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_thread_tombstoned ON events (thread_id, tombstoned_at)`); err != nil {
			return fmt.Errorf("migration 23 events thread_tombstoned index: %w", err)
		}
	}
	return nil
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
		if m.Apply != nil {
			if err := m.Apply(ctx, tx); err != nil {
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

func scrubLegacyArtifactMetadata(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, metadata_json FROM artifacts`)
	if err != nil {
		return fmt.Errorf("query artifact metadata for scrub: %w", err)
	}
	defer rows.Close()

	type artifactMetadataRow struct {
		id           string
		metadataJSON string
	}

	pending := make([]artifactMetadataRow, 0)
	for rows.Next() {
		var row artifactMetadataRow
		if err := rows.Scan(&row.id, &row.metadataJSON); err != nil {
			return fmt.Errorf("scan artifact metadata for scrub: %w", err)
		}

		scrubbedJSON, changed, err := scrubLegacyArtifactMetadataJSON(row.metadataJSON)
		if err != nil {
			return fmt.Errorf("scrub artifact %s metadata_json: %w", row.id, err)
		}
		if changed {
			row.metadataJSON = scrubbedJSON
			pending = append(pending, row)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate artifact metadata for scrub: %w", err)
	}

	for _, row := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE artifacts SET metadata_json = ? WHERE id = ?`, row.metadataJSON, row.id); err != nil {
			return fmt.Errorf("update scrubbed artifact %s metadata_json: %w", row.id, err)
		}
	}

	return nil
}

func applyAuthAuditMigration(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExistsTx(ctx, tx, "auth_invites")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	columns := []struct {
		name       string
		definition string
	}{
		{name: "consumed_by_agent_id", definition: "TEXT"},
		{name: "consumed_by_actor_id", definition: "TEXT"},
		{name: "revoked_by_agent_id", definition: "TEXT"},
		{name: "revoked_by_actor_id", definition: "TEXT"},
	}
	for _, column := range columns {
		present, err := columnExistsTx(ctx, tx, "auth_invites", column.name)
		if err != nil {
			return err
		}
		if present {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE auth_invites ADD COLUMN %s %s", column.name, column.definition)); err != nil {
			return err
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_auth_invites_consumed_by_agent_id ON auth_invites (consumed_by_agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_invites_revoked_by_agent_id ON auth_invites (revoked_by_agent_id)`,
	}
	for _, statement := range indexes {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}

func applyAuthAuditSortKeyMigration(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExistsTx(ctx, tx, "auth_audit_events")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, occurred_at FROM auth_audit_events WHERE COALESCE(occurred_at_sort_key, '') = ''`)
	if err != nil {
		return fmt.Errorf("query auth audit events for sort key backfill: %w", err)
	}
	defer rows.Close()

	type authAuditRow struct {
		id         string
		occurredAt string
		sortKey    string
	}

	pending := make([]authAuditRow, 0)
	for rows.Next() {
		var row authAuditRow
		if err := rows.Scan(&row.id, &row.occurredAt); err != nil {
			return fmt.Errorf("scan auth audit row for sort key backfill: %w", err)
		}
		occurredAt, err := authaudit.ParseOccurredAt(row.occurredAt)
		if err != nil {
			return fmt.Errorf("parse auth audit occurred_at for %s: %w", row.id, err)
		}
		row.sortKey = authaudit.FormatOccurredAtSortKey(occurredAt)
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate auth audit rows for sort key backfill: %w", err)
	}

	for _, row := range pending {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_audit_events SET occurred_at_sort_key = ? WHERE id = ?`, row.sortKey, row.id); err != nil {
			return fmt.Errorf("update auth audit sort key for %s: %w", row.id, err)
		}
	}

	return nil
}

func applyThreadProjectionGenerationMigration(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExistsTx(ctx, tx, "thread_projection_refresh_status")
	if err != nil {
		return err
	}
	if !exists {
		if err := createThreadProjectionGenerationTable(ctx, tx); err != nil {
			return err
		}
		return createThreadProjectionGenerationIndex(ctx, tx)
	}

	alreadyMigrated, err := columnExistsTx(ctx, tx, "thread_projection_refresh_status", "desired_generation")
	if err != nil {
		return err
	}
	if alreadyMigrated {
		return createThreadProjectionGenerationIndex(ctx, tx)
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE thread_projection_refresh_status RENAME TO thread_projection_refresh_status_legacy`); err != nil {
		return fmt.Errorf("rename legacy thread projection refresh status table: %w", err)
	}
	if err := createThreadProjectionGenerationTable(ctx, tx); err != nil {
		return err
	}
	if err := createThreadProjectionGenerationIndex(ctx, tx); err != nil {
		return err
	}

	rows, err := tx.QueryContext(
		ctx,
		`SELECT
			s.thread_id,
			s.is_dirty,
			s.in_progress,
			s.queued_at,
			s.started_at,
			s.completed_at,
			s.last_error_at,
			s.last_error,
			s.updated_at,
			EXISTS(SELECT 1 FROM derived_thread_views v WHERE v.thread_id = s.thread_id) AS has_projection
		FROM thread_projection_refresh_status_legacy s`,
	)
	if err != nil {
		return fmt.Errorf("query legacy thread projection refresh status rows: %w", err)
	}
	defer rows.Close()

	type projectionRow struct {
		threadID             string
		isDirty              int
		inProgress           int
		queuedAt             sql.NullString
		startedAt            sql.NullString
		completedAt          sql.NullString
		lastErrorAt          sql.NullString
		lastError            sql.NullString
		updatedAt            string
		hasProjection        bool
		desiredGeneration    int64
		materializedGen      int64
		inProgressGeneration *int64
	}

	pending := make([]projectionRow, 0)
	for rows.Next() {
		var row projectionRow
		if err := rows.Scan(
			&row.threadID,
			&row.isDirty,
			&row.inProgress,
			&row.queuedAt,
			&row.startedAt,
			&row.completedAt,
			&row.lastErrorAt,
			&row.lastError,
			&row.updatedAt,
			&row.hasProjection,
		); err != nil {
			return fmt.Errorf("scan legacy thread projection refresh status row: %w", err)
		}

		switch {
		case row.isDirty != 0 || row.inProgress != 0:
			row.desiredGeneration = 1
			if row.inProgress != 0 {
				value := int64(1)
				row.inProgressGeneration = &value
			}
		case row.hasProjection:
			row.desiredGeneration = 1
			row.materializedGen = 1
		}
		pending = append(pending, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy thread projection refresh status rows: %w", err)
	}

	for _, row := range pending {
		var inProgressGeneration any
		if row.inProgressGeneration != nil {
			inProgressGeneration = *row.inProgressGeneration
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO thread_projection_refresh_status(
				thread_id,
				desired_generation,
				materialized_generation,
				in_progress_generation,
				queued_at,
				started_at,
				completed_at,
				last_error_at,
				last_error,
				updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.threadID,
			row.desiredGeneration,
			row.materializedGen,
			inProgressGeneration,
			nullStringValue(row.queuedAt),
			nullStringValue(row.startedAt),
			nullStringValue(row.completedAt),
			nullStringValue(row.lastErrorAt),
			nullStringValue(row.lastError),
			row.updatedAt,
		); err != nil {
			return fmt.Errorf("insert migrated thread projection refresh status for %s: %w", row.threadID, err)
		}

		if row.desiredGeneration > row.materializedGen {
			dirtyAt := firstNonEmptyNullString(row.queuedAt, row.startedAt, row.lastErrorAt)
			if dirtyAt == "" {
				dirtyAt = row.updatedAt
			}
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO derived_thread_dirty_queue(thread_id, dirty_at)
				 VALUES (?, ?)
				ON CONFLICT(thread_id) DO UPDATE SET
					dirty_at = CASE
						WHEN derived_thread_dirty_queue.dirty_at <= excluded.dirty_at THEN derived_thread_dirty_queue.dirty_at
						ELSE excluded.dirty_at
					END`,
				row.threadID,
				dirtyAt,
			); err != nil {
				return fmt.Errorf("requeue migrated thread projection refresh for %s: %w", row.threadID, err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE thread_projection_refresh_status_legacy`); err != nil {
		return fmt.Errorf("drop legacy thread projection refresh status table: %w", err)
	}
	return nil
}

func createThreadProjectionGenerationTable(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(
		ctx,
		`CREATE TABLE IF NOT EXISTS thread_projection_refresh_status (
			thread_id TEXT PRIMARY KEY,
			desired_generation INTEGER NOT NULL DEFAULT 0,
			materialized_generation INTEGER NOT NULL DEFAULT 0,
			in_progress_generation INTEGER,
			queued_at TEXT,
			started_at TEXT,
			completed_at TEXT,
			last_error_at TEXT,
			last_error TEXT,
			updated_at TEXT NOT NULL
		);`,
	); err != nil {
		return fmt.Errorf("create thread projection generation table: %w", err)
	}
	return nil
}

func createThreadProjectionGenerationIndex(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(
		ctx,
		`CREATE INDEX IF NOT EXISTS idx_thread_projection_refresh_status_generations ON thread_projection_refresh_status (desired_generation, materialized_generation, in_progress_generation, queued_at, thread_id);`,
	); err != nil {
		return fmt.Errorf("create thread projection generation index: %w", err)
	}
	return nil
}

func tableExistsTx(ctx context.Context, tx *sql.Tx, tableName string) (bool, error) {
	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`,
		tableName,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check table %s existence: %w", tableName, err)
	}
	return exists, nil
}

func columnExistsTx(ctx context.Context, tx *sql.Tx, tableName string, columnName string) (bool, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		return false, fmt.Errorf("describe table %s: %w", tableName, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			dataType   string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultVal, &pk); err != nil {
			return false, fmt.Errorf("scan table info %s: %w", tableName, err)
		}
		if name == columnName {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table info %s: %w", tableName, err)
	}
	return false, nil
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func firstNonEmptyNullString(values ...sql.NullString) string {
	for _, value := range values {
		if value.Valid && value.String != "" {
			return value.String
		}
	}
	return ""
}

func scrubLegacyArtifactMetadataJSON(metadataJSON string) (string, bool, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return "", false, err
	}
	if _, ok := metadata["content_path"]; !ok {
		return metadataJSON, false, nil
	}
	delete(metadata, "content_path")
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", false, err
	}
	return string(encoded), true, nil
}
