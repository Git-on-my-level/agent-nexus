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
	// AfterApply runs in the same transaction after Statements (optional).
	AfterApply func(ctx context.Context, tx *sql.Tx) error
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
				body_json TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				archived_at TEXT,
				archived_by TEXT,
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_events_thread_ts ON events (thread_id, ts);`,
			`CREATE INDEX IF NOT EXISTS idx_events_archived_at ON events (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_events_trashed_at ON events (trashed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_events_thread_archived ON events (thread_id, archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_events_thread_trashed ON events (thread_id, trashed_at);`,

			`CREATE TABLE IF NOT EXISTS threads (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL DEFAULT 'thread',
				thread_id TEXT,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				body_json TEXT NOT NULL,
				provenance_json TEXT NOT NULL DEFAULT '{}',
				filter_status TEXT,
				filter_priority TEXT,
				filter_owner TEXT,
				filter_due_at TEXT,
				filter_cadence TEXT,
				filter_cadence_preset TEXT,
				filter_tags_json TEXT NOT NULL DEFAULT '[]',
				archived_at TEXT,
				archived_by TEXT,
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_updated_at ON threads (updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_status_updated_at ON threads (filter_status, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_priority_updated_at ON threads (filter_priority, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_cadence_preset_updated_at ON threads (filter_cadence_preset, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_archived_at ON threads (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_threads_trashed_at ON threads (trashed_at);`,

			`CREATE TABLE IF NOT EXISTS topics (
				id TEXT PRIMARY KEY,
				title TEXT,
				status TEXT,
				type TEXT,
				thread_id TEXT,
				body_json TEXT NOT NULL DEFAULT '{}',
				provenance_json TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				archived_at TEXT,
				archived_by TEXT,
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_status_updated_at ON topics (status, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_type_updated_at ON topics (type, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_thread_id ON topics (thread_id);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_updated_at ON topics (updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_archived_at ON topics (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_topics_trashed_at ON topics (trashed_at);`,

			`CREATE TABLE IF NOT EXISTS ref_edges (
				id TEXT PRIMARY KEY,
				source_type TEXT NOT NULL,
				source_id TEXT NOT NULL,
				target_type TEXT NOT NULL,
				target_id TEXT NOT NULL,
				edge_type TEXT NOT NULL,
				created_at TEXT NOT NULL,
				metadata_json TEXT NOT NULL DEFAULT '{}',
				UNIQUE(source_type, source_id, target_type, target_id, edge_type)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_ref_edges_source ON ref_edges (source_type, source_id);`,
			`CREATE INDEX IF NOT EXISTS idx_ref_edges_target ON ref_edges (target_type, target_id);`,
			`CREATE INDEX IF NOT EXISTS idx_ref_edges_edge_type ON ref_edges (edge_type);`,

			`CREATE TABLE IF NOT EXISTS artifacts (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				thread_id TEXT,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				content_type TEXT NOT NULL,
				content_hash TEXT NOT NULL,
				refs_json TEXT NOT NULL DEFAULT '[]',
				metadata_json TEXT NOT NULL DEFAULT '{}',
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT,
				archived_at TEXT,
				archived_by TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_created_at ON artifacts (kind, created_at);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_content_hash ON artifacts (content_hash);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_trashed_at ON artifacts (trashed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_archived_at ON artifacts (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_trashed_created_at ON artifacts (kind, trashed_at, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_trashed_created_at ON artifacts (thread_id, trashed_at, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_kind_trashed_created_at ON artifacts (thread_id, kind, trashed_at, created_at, id);`,

			`CREATE TABLE IF NOT EXISTS actors (
				id TEXT PRIMARY KEY,
				display_name TEXT NOT NULL,
				tags_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,

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
				updated_by TEXT NOT NULL,
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT,
				archived_at TEXT,
				archived_by TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_head_revision_id ON documents (head_revision_id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_trashed_at ON documents (trashed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_archived_at ON documents (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_trashed_updated_at ON documents (trashed_at, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_thread_trashed_updated_at ON documents (thread_id, trashed_at, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_status_trashed_updated_at ON documents (status, trashed_at, updated_at DESC, id);`,

			`CREATE TABLE IF NOT EXISTS document_revisions (
				revision_id TEXT PRIMARY KEY,
				document_id TEXT NOT NULL,
				revision_number INTEGER NOT NULL,
				prev_revision_id TEXT,
				artifact_id TEXT NOT NULL,
				thread_id TEXT,
				refs_json TEXT NOT NULL DEFAULT '[]',
				revision_hash TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				UNIQUE(document_id, revision_number)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_document_revisions_document_id_revision_number ON document_revisions (document_id, revision_number);`,
			`CREATE INDEX IF NOT EXISTS idx_document_revisions_document_id_revision_id ON document_revisions (document_id, revision_id);`,

			`CREATE TABLE IF NOT EXISTS boards (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL,
				status TEXT NOT NULL,
				labels_json TEXT NOT NULL DEFAULT '[]',
				owners_json TEXT NOT NULL DEFAULT '[]',
				thread_id TEXT NOT NULL,
				refs_json TEXT NOT NULL DEFAULT '[]',
				column_schema_json TEXT NOT NULL,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				archived_at TEXT,
				archived_by TEXT,
				trashed_at TEXT,
				trashed_by TEXT,
				trash_reason TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_status_updated_at ON boards (status, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_thread_id ON boards (thread_id);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_archived_at ON boards (archived_at);`,
			`CREATE INDEX IF NOT EXISTS idx_boards_trashed_at ON boards (trashed_at);`,

			`CREATE TABLE IF NOT EXISTS cards (
				id TEXT PRIMARY KEY,
				board_id TEXT,
				thread_id TEXT,
				title TEXT NOT NULL,
				body_markdown TEXT NOT NULL DEFAULT '',
				due_at TEXT,
				definition_of_done_json TEXT NOT NULL DEFAULT '[]',
				column_key TEXT NOT NULL DEFAULT 'backlog',
				rank TEXT NOT NULL DEFAULT '',
				version INTEGER NOT NULL DEFAULT 1,
				parent_thread_id TEXT,
				pinned_document_id TEXT,
				assignee TEXT,
				priority TEXT,
				status TEXT NOT NULL,
				resolution TEXT,
				resolution_refs_json TEXT NOT NULL DEFAULT '[]',
				refs_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				updated_by TEXT NOT NULL,
				provenance_json TEXT NOT NULL DEFAULT '{}',
				archived_at TEXT,
				archived_by TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_cards_parent_thread_id ON cards (parent_thread_id);`,
			`CREATE INDEX IF NOT EXISTS idx_cards_archived_at ON cards (archived_at);`,

			`CREATE TABLE IF NOT EXISTS card_versions (
				card_id TEXT NOT NULL,
				version INTEGER NOT NULL,
				board_id TEXT,
				thread_id TEXT,
				title TEXT NOT NULL,
				body_markdown TEXT NOT NULL DEFAULT '',
				due_at TEXT,
				definition_of_done_json TEXT NOT NULL DEFAULT '[]',
				column_key TEXT NOT NULL DEFAULT 'backlog',
				rank TEXT NOT NULL DEFAULT '',
				parent_thread_id TEXT,
				pinned_document_id TEXT,
				assignee TEXT,
				priority TEXT,
				status TEXT NOT NULL,
				resolution TEXT,
				resolution_refs_json TEXT NOT NULL DEFAULT '[]',
				refs_json TEXT NOT NULL DEFAULT '[]',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				provenance_json TEXT NOT NULL DEFAULT '{}',
				PRIMARY KEY (card_id, version),
				FOREIGN KEY(card_id) REFERENCES cards(id) ON DELETE CASCADE
			);`,
			`CREATE INDEX IF NOT EXISTS idx_card_versions_card_id_version ON card_versions (card_id, version);`,

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

			`CREATE TABLE IF NOT EXISTS auth_used_assertions (
				assertion_hash TEXT PRIMARY KEY,
				used_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_used_assertions_used_at ON auth_used_assertions (used_at);`,

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
				revoked_at TEXT,
				consumed_by_agent_id TEXT,
				consumed_by_actor_id TEXT,
				revoked_by_agent_id TEXT,
				revoked_by_actor_id TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_created_at ON auth_invites (created_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_consumed_at ON auth_invites (consumed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_revoked_at ON auth_invites (revoked_at);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_expires_at ON auth_invites (expires_at);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_consumed_by_agent_id ON auth_invites (consumed_by_agent_id);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_invites_revoked_by_agent_id ON auth_invites (revoked_by_agent_id);`,

			`CREATE TABLE IF NOT EXISTS auth_audit_events (
				id TEXT PRIMARY KEY,
				event_type TEXT NOT NULL,
				occurred_at TEXT NOT NULL,
				occurred_at_sort_key TEXT,
				actor_agent_id TEXT,
				actor_actor_id TEXT,
				subject_agent_id TEXT,
				subject_actor_id TEXT,
				invite_id TEXT,
				metadata_json TEXT NOT NULL DEFAULT '{}'
			);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_occurred_at ON auth_audit_events (occurred_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_event_type ON auth_audit_events (event_type, occurred_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_invite_id ON auth_audit_events (invite_id, occurred_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_auth_audit_events_sort_key ON auth_audit_events (occurred_at_sort_key DESC, id DESC);`,

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

			`CREATE TABLE IF NOT EXISTS derived_inbox_items (
				id TEXT PRIMARY KEY,
				thread_id TEXT NOT NULL,
				category TEXT NOT NULL,
				trigger_at TEXT NOT NULL,
				due_at TEXT,
				has_due_at INTEGER NOT NULL DEFAULT 0,
				source_event_id TEXT,
				source_card_id TEXT,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL,
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_thread_trigger ON derived_inbox_items (thread_id, trigger_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_category_trigger ON derived_inbox_items (category, trigger_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_inbox_items_due_at ON derived_inbox_items (has_due_at, due_at, id);`,

			`CREATE TABLE IF NOT EXISTS derived_topic_views (
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
				open_card_count INTEGER NOT NULL DEFAULT 0,
				document_count INTEGER NOT NULL DEFAULT 0,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL DEFAULT '{}',
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_topic_views_stale_generated_at ON derived_topic_views (stale, generated_at DESC, thread_id);`,

			`CREATE TABLE IF NOT EXISTS derived_topic_dirty_queue (
				thread_id TEXT PRIMARY KEY,
				dirty_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_topic_dirty_queue_dirty_at ON derived_topic_dirty_queue (dirty_at ASC, thread_id ASC);`,

			`CREATE TABLE IF NOT EXISTS topic_projection_refresh_status (
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
			`CREATE INDEX IF NOT EXISTS idx_topic_projection_refresh_status_generations ON topic_projection_refresh_status (desired_generation, materialized_generation, in_progress_generation, queued_at, thread_id);`,
		},
	},
	{
		Version: 2,
		Statements: []string{
			`ALTER TABLE cards ADD COLUMN risk TEXT NOT NULL DEFAULT 'low';`,
			`ALTER TABLE card_versions ADD COLUMN risk TEXT NOT NULL DEFAULT 'low';`,
		},
	},
	{
		Version: 3,
		Statements: []string{
			`ALTER TABLE documents ADD COLUMN refs_json TEXT NOT NULL DEFAULT '[]';`,
			`ALTER TABLE documents ADD COLUMN provenance_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE cards ADD COLUMN trashed_at TEXT;`,
			`ALTER TABLE cards ADD COLUMN trashed_by TEXT;`,
			`ALTER TABLE cards ADD COLUMN trash_reason TEXT;`,
			`CREATE INDEX IF NOT EXISTS idx_cards_trashed_at ON cards (trashed_at);`,
			`CREATE TABLE IF NOT EXISTS derived_board_views (
				board_id TEXT PRIMARY KEY,
				stale INTEGER NOT NULL DEFAULT 0,
				generated_at TEXT NOT NULL,
				data_json TEXT NOT NULL DEFAULT '{}',
				source_hash TEXT
			);`,
			`CREATE INDEX IF NOT EXISTS idx_derived_board_views_stale_generated_at ON derived_board_views (stale, generated_at DESC, board_id);`,
		},
	},
	{
		Version: 4,
		Statements: []string{
			`ALTER TABLE events RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE events RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE events RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE threads RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE threads RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE threads RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE topics RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE topics RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE topics RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE artifacts RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE artifacts RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE artifacts RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE documents RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE documents RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE documents RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE boards RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE boards RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE boards RENAME COLUMN trash_reason TO trash_reason;`,
			`ALTER TABLE cards RENAME COLUMN trashed_at TO trashed_at;`,
			`ALTER TABLE cards RENAME COLUMN trashed_by TO trashed_by;`,
			`ALTER TABLE cards RENAME COLUMN trash_reason TO trash_reason;`,
		},
	},
	{
		Version: 5,
		Statements: []string{
			`DROP INDEX IF EXISTS idx_events_thread_tombstoned;`,
			`DROP INDEX IF EXISTS idx_artifacts_kind_tombstoned_created_at;`,
			`DROP INDEX IF EXISTS idx_artifacts_thread_tombstoned_created_at;`,
			`DROP INDEX IF EXISTS idx_artifacts_thread_kind_tombstoned_created_at;`,
			`DROP INDEX IF EXISTS idx_documents_tombstoned_updated_at;`,
			`DROP INDEX IF EXISTS idx_documents_thread_tombstoned_updated_at;`,
			`DROP INDEX IF EXISTS idx_documents_status_tombstoned_updated_at;`,
			`CREATE INDEX IF NOT EXISTS idx_events_thread_trashed ON events (thread_id, trashed_at);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_kind_trashed_created_at ON artifacts (kind, trashed_at, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_trashed_created_at ON artifacts (thread_id, trashed_at, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_artifacts_thread_kind_trashed_created_at ON artifacts (thread_id, kind, trashed_at, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_trashed_updated_at ON documents (trashed_at, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_thread_trashed_updated_at ON documents (thread_id, trashed_at, updated_at DESC, id);`,
			`CREATE INDEX IF NOT EXISTS idx_documents_status_trashed_updated_at ON documents (status, trashed_at, updated_at DESC, id);`,
		},
	},
	{
		Version: 6,
		Statements: []string{
			`DROP INDEX IF EXISTS idx_documents_status_trashed_updated_at;`,
			`UPDATE documents
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at, created_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by, created_by)
			  WHERE status = 'archived'
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`,
			`UPDATE documents
			    SET trashed_at = COALESCE(NULLIF(trashed_at, ''), updated_at, created_at),
			        trashed_by = COALESCE(NULLIF(trashed_by, ''), updated_by, created_by),
			        trash_reason = COALESCE(NULLIF(trash_reason, ''), 'legacy status migration')
			  WHERE status = 'trashed'
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`,
			`ALTER TABLE documents DROP COLUMN status;`,
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
		if m.AfterApply != nil {
			if err := m.AfterApply(ctx, tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %d after hook: %w", m.Version, err)
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
