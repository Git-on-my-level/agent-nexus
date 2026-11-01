// The ordered migration registry and the runner that applies it. The
// migrations slice is the single source of migration order and version
// numbers, so it stays in one piece here; migration bodies live in the
// migrations_*.go files grouped by subject.

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
	// ApplyChunk executes one resumable chunk in the runner's transaction.
	// The runner alone verifies completion, records Version and updates its ledger.
	ApplyChunk func(ctx context.Context, tx *sql.Tx) (done bool, err error)
	Verify     func(ctx context.Context, tx *sql.Tx) error
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
				head_revision_id TEXT,
				head_revision_number INTEGER NOT NULL DEFAULT 1,
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

			`CREATE TABLE IF NOT EXISTS card_revisions (
				revision_id TEXT PRIMARY KEY,
				card_id TEXT NOT NULL,
				revision_number INTEGER NOT NULL,
				prev_revision_id TEXT,
				artifact_id TEXT NOT NULL,
				thread_id TEXT,
				refs_json TEXT NOT NULL DEFAULT '[]',
				revision_hash TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL,
				UNIQUE(card_id, revision_number)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_number ON card_revisions (card_id, revision_number);`,
			`CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_id ON card_revisions (card_id, revision_id);`,

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
			`CREATE TABLE IF NOT EXISTS agent_wakeups (
				wakeup_id TEXT PRIMARY KEY,
				status TEXT NOT NULL,
				notification_status TEXT NOT NULL DEFAULT 'unread',
				target_handle TEXT NOT NULL,
				target_actor_id TEXT NOT NULL,
				workspace_id TEXT NOT NULL DEFAULT '',
				workspace_name TEXT NOT NULL DEFAULT '',
				thread_id TEXT NOT NULL DEFAULT '',
				thread_title TEXT NOT NULL DEFAULT '',
				trigger_event_id TEXT NOT NULL DEFAULT '',
				trigger_created_at TEXT NOT NULL DEFAULT '',
				trigger_text TEXT NOT NULL DEFAULT '',
				refs_json TEXT NOT NULL DEFAULT '[]',
				bridge_instance_id TEXT NOT NULL DEFAULT '',
				failure_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				claimed_at TEXT,
				completed_at TEXT,
				failed_at TEXT,
				read_at TEXT,
				dismissed_at TEXT,
				updated_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_target_status_created ON agent_wakeups (target_actor_id, notification_status, created_at DESC, wakeup_id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_status_created ON agent_wakeups (status, created_at DESC, wakeup_id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_trigger_created ON agent_wakeups (thread_id, created_at ASC, wakeup_id ASC);`,

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
	{
		Version: 7,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS secrets (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL UNIQUE,
			description TEXT,
			ciphertext  BLOB NOT NULL,
			nonce       BLOB NOT NULL,
			key_id      TEXT NOT NULL DEFAULT 'v1',
			actor_id    TEXT NOT NULL,
			updated_by  TEXT,
			created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);`,
		},
	},
	{
		Version: 8,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS consumed_grant_jtis (
				jti TEXT PRIMARY KEY,
				consumed_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_consumed_grant_jtis_consumed_at ON consumed_grant_jtis (consumed_at);`,
		},
	},
	{
		Version: 9,
		Statements: []string{
			`DROP INDEX IF EXISTS idx_topics_status_updated_at;`,
			`DROP INDEX IF EXISTS idx_threads_status_updated_at;`,
			`DROP INDEX IF EXISTS idx_boards_status_updated_at;`,
		},
		AfterApply: applyMigration9LegacyStatusCleanup,
	},
	{
		Version:    10,
		Statements: []string{},
		AfterApply: applyMigration10DropThreadFilterColumns,
	},
	{
		Version:    11,
		Statements: []string{},
		AfterApply: applyMigration11CardsSummaryAndDropPriority,
	},
	{
		Version:    12,
		Statements: []string{},
		AfterApply: applyMigration12TopicsSummaryExtensionsJSON,
	},
	{
		Version:    13,
		Statements: []string{},
		AfterApply: applyMigration13DropEventsBodyJSON,
	},
	{
		Version:    14,
		Statements: []string{},
		AfterApply: applyMigration14ClearDerivedInboxForCanonicalCategories,
	},
	{
		Version:    15,
		Statements: []string{},
		AfterApply: applyMigration15DropBoardsDocumentsLabelsJSON,
	},
	{
		Version:    16,
		Statements: []string{},
		AfterApply: applyMigration16DropTopicsTypeColumn,
	},
	{
		Version:    17,
		Statements: []string{},
		AfterApply: applyMigration17BoardsDocumentsSummary,
	},
	{
		Version:    18,
		Statements: []string{},
		AfterApply: func(ctx context.Context, tx *sql.Tx) error {
			ok, err := sqliteTableExists(ctx, tx, "events")
			if err != nil || !ok {
				return err
			}
			_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_type_ts_id ON events (type, ts DESC, id DESC);`)
			return err
		},
	},
	{
		Version:    19,
		Statements: []string{},
		AfterApply: applyMigration19CardRevisions,
	},
	{
		Version: 20,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS home_topic_read_cursors (
				reader_id TEXT NOT NULL,
				topic_id TEXT NOT NULL,
				last_read_event_ts TEXT NOT NULL,
				last_read_event_id TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (reader_id, topic_id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_home_topic_read_cursors_reader ON home_topic_read_cursors (reader_id, topic_id);`,
			`CREATE INDEX IF NOT EXISTS idx_home_topic_read_cursors_topic ON home_topic_read_cursors (topic_id, reader_id);`,
		},
		AfterApply: func(ctx context.Context, tx *sql.Tx) error {
			ok, err := sqliteTableExists(ctx, tx, "events")
			if err != nil || !ok {
				return err
			}
			if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_thread_ts_id ON events (thread_id, ts DESC, id DESC);`); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_actor_ts_id ON events (actor_id, ts DESC, id DESC);`)
			return err
		},
	},
	{
		Version: 21,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS agent_wakeups (
				wakeup_id TEXT PRIMARY KEY,
				status TEXT NOT NULL,
				notification_status TEXT NOT NULL DEFAULT 'unread',
				target_handle TEXT NOT NULL,
				target_actor_id TEXT NOT NULL,
				workspace_id TEXT NOT NULL DEFAULT '',
				workspace_name TEXT NOT NULL DEFAULT '',
				thread_id TEXT NOT NULL DEFAULT '',
				thread_title TEXT NOT NULL DEFAULT '',
				trigger_event_id TEXT NOT NULL DEFAULT '',
				trigger_created_at TEXT NOT NULL DEFAULT '',
				trigger_text TEXT NOT NULL DEFAULT '',
				refs_json TEXT NOT NULL DEFAULT '[]',
				bridge_instance_id TEXT NOT NULL DEFAULT '',
				failure_reason TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				claimed_at TEXT,
				completed_at TEXT,
				failed_at TEXT,
				read_at TEXT,
				dismissed_at TEXT,
				updated_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_target_status_created ON agent_wakeups (target_actor_id, notification_status, created_at DESC, wakeup_id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_status_created ON agent_wakeups (status, created_at DESC, wakeup_id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_trigger_created ON agent_wakeups (thread_id, created_at ASC, wakeup_id ASC);`,
		},
	},
	{
		Version:    22,
		Statements: []string{},
		AfterApply: applyMigration22TrashCanceledResolutionCards,
	},
	{
		Version: 23,
		Statements: []string{
			`CREATE INDEX IF NOT EXISTS idx_agent_wakeups_thread_trigger_created ON agent_wakeups (thread_id, created_at ASC, wakeup_id ASC);`,
		},
	},
	{
		Version: 24,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS home_read_cursors (
				reader_id TEXT NOT NULL,
				group_ref TEXT NOT NULL,
				last_read_event_ts TEXT NOT NULL,
				last_read_event_id TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY (reader_id, group_ref)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_home_read_cursors_reader ON home_read_cursors (reader_id, group_ref);`,
		},
		AfterApply: applyMigration24HomeGroupCursors,
	},
	{
		Version: 25,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS resource_handle_aliases (
				id TEXT PRIMARY KEY,
				resource_type TEXT NOT NULL,
				alias_handle TEXT NOT NULL,
				resource_id TEXT NOT NULL,
				canonical_handle TEXT NOT NULL,
				created_at TEXT NOT NULL,
				created_by TEXT NOT NULL DEFAULT 'schema_migration',
				reason TEXT NOT NULL DEFAULT '',
				UNIQUE(resource_type, alias_handle)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_resource_handle_aliases_resource ON resource_handle_aliases (resource_type, resource_id);`,
		},
		AfterApply: applyMigration25ResourceHandles,
	},
	{
		Version: 26,
		Statements: []string{
			`CREATE TABLE work_metadata (
				card_id TEXT PRIMARY KEY REFERENCES cards(id) ON DELETE CASCADE,
				authority TEXT NOT NULL DEFAULT 'nexus', connection_id TEXT NOT NULL DEFAULT '', native_id TEXT NOT NULL DEFAULT '',
				metadata_json TEXT NOT NULL DEFAULT '{}', version INTEGER NOT NULL DEFAULT 1,
				latest_observation_id TEXT, latest_attempt_id TEXT,
				refresh_json TEXT NOT NULL DEFAULT '{"state":"idle"}', updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
			);`,
			`CREATE UNIQUE INDEX idx_work_source_identity ON work_metadata(authority,connection_id,native_id) WHERE authority <> 'nexus';`,
			`CREATE TABLE work_observations (
				id TEXT PRIMARY KEY, card_id TEXT NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
				idempotency_key TEXT NOT NULL, digest TEXT NOT NULL, observed_at TEXT NOT NULL, received_at TEXT NOT NULL,
				source_sequence INTEGER, status TEXT NOT NULL, body_json TEXT NOT NULL,
				UNIQUE(card_id,idempotency_key)
			);`,
			`CREATE INDEX idx_work_observations_card_received ON work_observations(card_id,received_at DESC,id DESC);`,
		},
	},
	{
		Version:    27,
		AfterApply: applyMigration27DocumentKnowledge,
	},
	{
		Version:    28,
		AfterApply: applyMigration28DocumentFTS,
	},
	{
		Version: 30,
		Statements: []string{
			`CREATE TABLE runs (
				id TEXT PRIMARY KEY, handle TEXT NOT NULL UNIQUE,
				launcher TEXT NOT NULL, external_id TEXT NOT NULL,
				host_id TEXT NOT NULL, agent_id TEXT NOT NULL, adapter TEXT NOT NULL,
				model TEXT, state TEXT NOT NULL, liveness TEXT NOT NULL,
				result_collected INTEGER NOT NULL, labels_json TEXT NOT NULL,
				card_ref TEXT, repository TEXT, branch TEXT, started_at TEXT,
				ended_at TEXT, last_observed_at TEXT NOT NULL,
				UNIQUE(launcher, host_id, external_id)
			);`,
			`CREATE INDEX idx_runs_agent_observed ON runs(agent_id, last_observed_at DESC);`,
			`CREATE INDEX idx_runs_card_observed ON runs(card_ref, last_observed_at DESC);`,
			`CREATE INDEX idx_runs_state_liveness ON runs(state, liveness);`,
			`CREATE TABLE agent_presence (
				agent_id TEXT PRIMARY KEY, current_card_ref TEXT, note TEXT,
				observed_at TEXT NOT NULL
			);`,
			`CREATE TABLE agent_progress_notes (
				id INTEGER PRIMARY KEY AUTOINCREMENT, agent_id TEXT NOT NULL,
				card_ref TEXT, text TEXT NOT NULL, observed_at TEXT NOT NULL
			);`,
			`CREATE INDEX idx_agent_progress_notes_agent_at ON agent_progress_notes(agent_id, observed_at DESC);`,
		},
	},
	// Command-center host identity uses a separate version band from run/presence work.
	{
		Version: 40,
		Statements: []string{
			`CREATE TABLE hosts (id TEXT PRIMARY KEY, slug TEXT NOT NULL UNIQUE, display_name TEXT NOT NULL, os_user TEXT NOT NULL, hostname TEXT NOT NULL, discovered_adapters_json TEXT NOT NULL, created_at TEXT NOT NULL, revoked_at TEXT, bridge_instance_id TEXT, bridge_checked_in_at TEXT, bridge_expires_at TEXT);`,
			`CREATE TABLE host_keys (id TEXT PRIMARY KEY, host_id TEXT NOT NULL REFERENCES hosts(id), public_key TEXT NOT NULL, created_at TEXT NOT NULL, revoked_at TEXT);`,
			`CREATE UNIQUE INDEX host_active_key ON host_keys(host_id) WHERE revoked_at IS NULL;`,
			`CREATE TABLE host_exclusions (host_id TEXT NOT NULL REFERENCES hosts(id), name TEXT NOT NULL, PRIMARY KEY(host_id,name));`,
			`CREATE TABLE host_agents (host_id TEXT NOT NULL REFERENCES hosts(id), name TEXT NOT NULL, agent_id TEXT NOT NULL UNIQUE REFERENCES agents(id), identity_kind TEXT NOT NULL, PRIMARY KEY(host_id,name));`,
			`CREATE TABLE host_enrollments (id TEXT PRIMARY KEY, user_code TEXT NOT NULL UNIQUE, poll_token_hash TEXT NOT NULL, public_key TEXT NOT NULL, requested_slug TEXT NOT NULL, os_user TEXT NOT NULL, hostname TEXT NOT NULL, discovered_adapters_json TEXT NOT NULL, request_nonce TEXT NOT NULL, adoptions_json TEXT NOT NULL, requesting_ip TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, completed_host_id TEXT);`,
			`CREATE UNIQUE INDEX host_reserved_slug ON host_enrollments(requested_slug) WHERE status='approved';`,
			`CREATE TABLE host_enrollment_tokens (id TEXT PRIMARY KEY, label TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT, revoked_at TEXT, created_by_agent_id TEXT NOT NULL);`,
		},
		AfterApply: applyMigration40HostIdentity,
	},
	{
		Version: 41,
		Statements: []string{
			`CREATE TABLE human_attention_response_claims (
				request_event_id TEXT PRIMARY KEY,
				inbox_item_id TEXT NOT NULL,
				actor_id TEXT NOT NULL,
				request_key TEXT NOT NULL,
				request_hash TEXT NOT NULL,
				response_event_id TEXT NOT NULL UNIQUE,
				response_json TEXT NOT NULL,
				created_at TEXT NOT NULL
			);`,
			`CREATE UNIQUE INDEX human_attention_response_key ON human_attention_response_claims(actor_id,request_key) WHERE request_key<>'';`,
			`CREATE INDEX human_attention_response_inbox ON human_attention_response_claims(inbox_item_id);`,
		},
	},
	{
		Version: 42,
		Statements: []string{
			`CREATE TABLE agent_sessions (
				id TEXT PRIMARY KEY, agent_id TEXT NOT NULL REFERENCES agents(id), actor_id TEXT NOT NULL,
				provider TEXT NOT NULL, host_scope TEXT NOT NULL, native_session_id TEXT NOT NULL,
				native_session_id_kind TEXT NOT NULL, capabilities_json TEXT NOT NULL,
				activity TEXT NOT NULL CHECK(activity IN ('active','idle','closed')),
				sequence INTEGER NOT NULL CHECK(sequence >= 0), request_json TEXT NOT NULL,
				created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL,
				UNIQUE(agent_id, provider, host_scope, native_session_id)
			);`,
			`CREATE TABLE work_participants (
				id TEXT PRIMARY KEY, card_id TEXT NOT NULL REFERENCES cards(id), session_id TEXT NOT NULL REFERENCES agent_sessions(id),
				actor_id TEXT NOT NULL DEFAULT '',
				activity TEXT NOT NULL CHECK(activity IN ('active','idle','left')),
				sequence INTEGER NOT NULL CHECK(sequence >= 0), request_json TEXT NOT NULL,
				created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, expires_at TEXT NOT NULL,
				UNIQUE(card_id,session_id)
			);`,
			`CREATE INDEX idx_work_participants_card_id ON work_participants(card_id,id);`,
			`CREATE INDEX idx_work_participants_actor_card_active ON work_participants(actor_id, card_id, session_id, id) WHERE activity <> 'left' AND actor_id <> '';`,
			`CREATE TRIGGER work_participants_actor_insert AFTER INSERT ON work_participants WHEN NEW.actor_id = '' OR NEW.actor_id LIKE 'actor:%' BEGIN UPDATE work_participants SET actor_id = COALESCE((SELECT CASE WHEN trim(s.actor_id) LIKE 'actor:%' THEN substr(trim(s.actor_id),7) ELSE trim(s.actor_id) END FROM agent_sessions s WHERE s.id = NEW.session_id), '') WHERE id = NEW.id; END;`,
			`CREATE TRIGGER work_participants_actor_update AFTER UPDATE OF session_id ON work_participants BEGIN UPDATE work_participants SET actor_id = COALESCE((SELECT CASE WHEN trim(s.actor_id) LIKE 'actor:%' THEN substr(trim(s.actor_id),7) ELSE trim(s.actor_id) END FROM agent_sessions s WHERE s.id = NEW.session_id), '') WHERE id = NEW.id; END;`,
		},
	},
	{Version: 43, AfterApply: applyMigration43ReportIndexes},
	{Version: 44, AfterApply: applyMigration44CardPlans},
	{Version: 45, Statements: []string{`CREATE TABLE workspace_dashboard (singleton INTEGER PRIMARY KEY CHECK(singleton=1), document_id TEXT, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL);`}},
	{
		Version: 46,
		Statements: []string{
			`CREATE TABLE human_attention_answer_wake_batches (
				target_actor_id TEXT PRIMARY KEY,
				target_handle TEXT NOT NULL,
				workspace_id TEXT NOT NULL,
				batch_id TEXT NOT NULL,
				first_answered_at TEXT NOT NULL,
				last_answered_at TEXT NOT NULL,
				thread_id TEXT NOT NULL DEFAULT '',
				trigger_event_id TEXT NOT NULL,
				trigger_created_at TEXT NOT NULL,
				answer_count INTEGER NOT NULL,
				ask_event_ids_json TEXT NOT NULL DEFAULT '[]',
				answer_event_ids_json TEXT NOT NULL DEFAULT '[]',
				refs_json TEXT NOT NULL DEFAULT '[]',
				updated_at TEXT NOT NULL
			);`,
			`CREATE INDEX idx_human_attention_answer_wake_batches_updated ON human_attention_answer_wake_batches(last_answered_at,target_actor_id);`,
		},
	},
	{
		Version: 47,
		Statements: []string{
			`ALTER TABLE human_attention_answer_wake_batches ADD COLUMN debounce_deadline TEXT NOT NULL DEFAULT '';`,
			`CREATE TABLE human_attention_request_resolutions (
				request_event_id TEXT PRIMARY KEY,
				resolution_event_id TEXT NOT NULL UNIQUE,
				resolution_type TEXT NOT NULL CHECK(resolution_type IN ('answered','withdrawn')),
				actor_id TEXT NOT NULL,
				created_at TEXT NOT NULL
			);`,
			`CREATE INDEX idx_human_attention_request_resolutions_type ON human_attention_request_resolutions(resolution_type,request_event_id);`,
		},
		AfterApply: applyMigration47HumanAttentionResolutions,
	},
	{
		Version: 48,
		Statements: []string{
			`CREATE TABLE human_attention_answer_reads (
				answer_event_id TEXT PRIMARY KEY,
				requester_actor_id TEXT NOT NULL,
				read_at TEXT NOT NULL
			);`,
			`CREATE INDEX idx_human_attention_answer_reads_requester ON human_attention_answer_reads(requester_actor_id,read_at,answer_event_id);`,
		},
		AfterApply: applyMigration48HumanAttentionAnswerReads,
	},
	{Version: 49, AfterApply: applyMigration49SeriesTokenScope, Statements: []string{
		`CREATE TABLE series_adapters (name TEXT PRIMARY KEY, description TEXT NOT NULL, agent_id TEXT NOT NULL REFERENCES agents(id), host_id TEXT NOT NULL REFERENCES hosts(id), expected_interval INTEGER NOT NULL, created_at TEXT NOT NULL, revoked_at TEXT, deleted_at TEXT, last_push TEXT);`,
		`CREATE TABLE series_definitions (name TEXT PRIMARY KEY, adapter TEXT NOT NULL REFERENCES series_adapters(name), unit TEXT NOT NULL, kind TEXT NOT NULL);`,
		`CREATE TABLE series_labels (series TEXT NOT NULL REFERENCES series_definitions(name) ON DELETE CASCADE, labels TEXT NOT NULL, PRIMARY KEY(series,labels));`,
		`CREATE TABLE series_points (series TEXT NOT NULL, labels TEXT NOT NULL, ts INTEGER NOT NULL, value REAL, state TEXT, received_day INTEGER NOT NULL, PRIMARY KEY(series,labels,ts), FOREIGN KEY(series,labels) REFERENCES series_labels(series,labels) ON DELETE CASCADE);`,
		`CREATE INDEX series_points_time ON series_points(ts);`,
		`CREATE TABLE series_daily (series TEXT NOT NULL, labels TEXT NOT NULL, day INTEGER NOT NULL, n INTEGER NOT NULL, total REAL, low REAL, high REAL, last_ts INTEGER NOT NULL, last_value REAL, last_state TEXT, PRIMARY KEY(series,labels,day), FOREIGN KEY(series,labels) REFERENCES series_labels(series,labels) ON DELETE CASCADE);`,
		`CREATE TABLE series_ingestion_days (day INTEGER PRIMARY KEY, n INTEGER NOT NULL);`,
	}},
	{Version: 50, Statements: []string{
		`CREATE TABLE series_request_budgets (scope TEXT NOT NULL, minute INTEGER NOT NULL, n INTEGER NOT NULL, PRIMARY KEY(scope,minute));`,
	}},
	{Version: 51, Statements: []string{
		seriesLiveDailyTableSQL,
		SeriesLiveDailyBackfillSQL,
		`CREATE INDEX series_points_daily_value ON series_points(series,labels,(ts/86400000000000)*86400000000000,value);`,
		seriesLiveDailyInsertSQL,
		seriesLiveDailyUpdateSQL,
	}},
	{Version: 52, Statements: []string{
		// Cover edge aggregates without a table lookup for every raw value.
		`CREATE INDEX series_points_query ON series_points(series,labels,ts,value);`,
	}},
	{Version: 53, Statements: []string{
		`CREATE TABLE overview_visits (principal_id TEXT PRIMARY KEY, visited_at TEXT NOT NULL, snapshot_json TEXT NOT NULL);`,
	}, AfterApply: func(ctx context.Context, tx *sql.Tx) error {
		exists, err := sqliteTableExists(ctx, tx, "events")
		if err != nil || !exists {
			return err
		}
		_, err = tx.ExecContext(ctx, `CREATE INDEX idx_events_overview_answers ON events(type,ts,id)`)
		return err
	}},
	{Version: 54, Statements: []string{
		`CREATE TABLE access_requests (
            id TEXT PRIMARY KEY, principal_id TEXT NOT NULL REFERENCES agents(id),
            actor_id TEXT NOT NULL, username TEXT NOT NULL, grant_name TEXT NOT NULL,
            reason TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
            created_at TEXT NOT NULL, decided_at TEXT NOT NULL DEFAULT '', decided_by TEXT NOT NULL DEFAULT '',
            request_event_id TEXT NOT NULL UNIQUE REFERENCES events(id), inbox_item_id TEXT NOT NULL UNIQUE,
            UNIQUE(principal_id,grant_name), CHECK(status IN ('pending','approved','denied'))
        );`,
		`CREATE INDEX access_requests_pending ON access_requests(status,created_at,id);`,
		`CREATE INDEX host_enrollments_pending_expiry ON host_enrollments(status,expires_at);`,
	}},
	// Privacy follows main's access-request migration. Repeating its schema
	// idempotently also repairs databases from the earlier privacy-54 preview.
	{Version: 55, Statements: []string{
		`CREATE TABLE IF NOT EXISTS access_requests (
            id TEXT PRIMARY KEY, principal_id TEXT NOT NULL REFERENCES agents(id),
            actor_id TEXT NOT NULL, username TEXT NOT NULL, grant_name TEXT NOT NULL,
            reason TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
            created_at TEXT NOT NULL, decided_at TEXT NOT NULL DEFAULT '', decided_by TEXT NOT NULL DEFAULT '',
            request_event_id TEXT NOT NULL UNIQUE REFERENCES events(id), inbox_item_id TEXT NOT NULL UNIQUE,
            UNIQUE(principal_id,grant_name), CHECK(status IN ('pending','approved','denied'))
        );`,
		`CREATE INDEX IF NOT EXISTS access_requests_pending ON access_requests(status,created_at,id);`,
		`CREATE INDEX IF NOT EXISTS host_enrollments_pending_expiry ON host_enrollments(status,expires_at);`,
	}, AfterApply: installResourceAccess},
	// Earlier PR previews already recorded 55. Reconcile them as well: new
	// ownership sources and repaired indexes must not depend on a fresh install.
	{Version: 56, AfterApply: installResourceAccess},
	{Version: 57, AfterApply: installCompleteResourceAccess},
	// Reconcile the short-lived 57 preview's ambiguous scalar/JSON extraction.
	{Version: 58, AfterApply: func(ctx context.Context, tx *sql.Tx) error {
		if err := installResourceAccessEdges(ctx, tx); err != nil {
			return err
		}
		if exists, err := sqliteTableExists(ctx, tx, "artifacts"); err != nil {
			return err
		} else if exists {
			if _, err = tx.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=NULL`); err != nil {
				return err
			}
		}
		return installCompleteResourceAccess(ctx, tx)
	}},
	{Version: 59, AfterApply: repairLegacyReferenceAccess},
	// Rebuild truncated scalar-text edges and replace old TEXT-argument triggers.
	{Version: 60, AfterApply: repairNULReferenceAccess},
	{Version: 61, AfterApply: installResourceAccessMentions},
	{Version: 62, AfterApply: repairResourceAccessReadIndexes},
	{Version: 63, AfterApply: indexResourceAccessProfiles},
	{Version: 64, AfterApply: applyMigration64BoardRole},
	{Version: 65, AfterApply: applyMigration65EvidenceIndex},
	{Version: 66, AfterApply: indexBoundedOverviewReads},
	{Version: 68, AfterApply: indexBoundedEventStreamReads},
	{Version: 69, AfterApply: indexBoundedReceiptStreamReads},
	{Version: 70, AfterApply: indexInboxLifecycle},
	{Version: 71, AfterApply: installScopeInboxLive},
	{Version: 72, AfterApply: installWorkSummaryAttention},
	{Version: 73, AfterApply: indexAgentSummaryCards},
	{Version: 74, AfterApply: installAskSubjects},
	{Version: 75, AfterApply: installAskSubscriptions},
	{Version: 76, AfterApply: installAskEffectiveClosure},
	{Version: 77, Statements: []string{`CREATE TABLE IF NOT EXISTS ask_subject_boards(actor_id TEXT PRIMARY KEY, board_id TEXT NOT NULL)`}},
	{Version: 78, AfterApply: boundIdentityRefreshes},
	{Version: 79, Statements: []string{`CREATE VIEW IF NOT EXISTS pm_card_event_positions AS SELECT id,thread_id,ts FROM events`, `CREATE VIEW IF NOT EXISTS pm_card_ask_positions AS SELECT ask_id,card_id,open FROM ask_subjects`, `CREATE VIEW IF NOT EXISTS pm_card_alias_positions AS SELECT resource_type,alias_handle,resource_id FROM resource_handle_aliases`}},
	{Version: 80, AfterApply: installPMWorkspaceContext},
	{Version: 81, AfterApply: installProjectionRetryBackoff},
	{Version: 82, AfterApply: installCommentSearch},
	{Version: 83, AfterApply: upgradeSearchNormalization},
	{Version: 84, AfterApply: upgradeExternalCommentSearch},
	{Version: 85, AfterApply: upgradeCommentSearchPendingCursor},
	// Agent-inbox asks filter by the requester recorded inside the ask payload.
	// The partial expression index turns that predicate from a scan over every
	// ask event into a seek on one principal's asks; the expression must match
	// the query in primitives/human_attention_inbox.go exactly. The one-time
	// build happens inside this migration; inserts after it only touch the
	// index for human_attention_requested rows. Legacy fixtures may predate
	// the events table, so the index is conditional like the other event
	// indexes (see the overview-answers migration).
	{Version: 86, AfterApply: installAskRequesterIndex},
	// Ask-batch responses replay through one durable claim row per filed batch
	// so a retried submit returns the original combined result. Member answers
	// still land in human_attention_response_claims; their request_key stays
	// empty because the unique (actor_id, request_key) index there is per row.
	{Version: 87, Statements: []string{`CREATE TABLE IF NOT EXISTS human_attention_batch_response_claims (
		batch_id TEXT PRIMARY KEY,
		inbox_item_id TEXT NOT NULL DEFAULT '',
		actor_id TEXT NOT NULL,
		request_key TEXT NOT NULL DEFAULT '',
		request_hash TEXT NOT NULL DEFAULT '',
		response_json TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_human_attention_batch_response_claims_key
		ON human_attention_batch_response_claims(actor_id, request_key) WHERE request_key<>''`,
		`CREATE INDEX IF NOT EXISTS idx_human_attention_batch_response_claims_inbox
		ON human_attention_batch_response_claims(inbox_item_id)`},
	},
	{Version: 88, AfterApply: installTopicWorkspacePageIndexes},
	{Version: 89, AfterApply: upgradeWorkSummaryAskLinks},
	// brief.v1 facts materialized on write events (asks, notes, plan and
	// card changes), with a one-time backfill. See brief_facts.go.
	{Version: 90, AfterApply: installBriefFacts},
	{Version: 91, AfterApply: installBriefAskBlocks},
	// Curation proposals: one row per action+targets key, so the formula and
	// the PM propose a key at most once while open and never after a dismissal.
	{Version: 92, Statements: []string{`CREATE TABLE IF NOT EXISTS curation_proposals (
		key TEXT PRIMARY KEY,
		proposal_event_id TEXT NOT NULL,
		action TEXT NOT NULL,
		source TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		targets_json TEXT NOT NULL DEFAULT '[]',
		created_at TEXT NOT NULL,
		resolved_at TEXT
	)`,
		`CREATE INDEX IF NOT EXISTS idx_curation_proposals_event ON curation_proposals(proposal_event_id)`,
		// Accepted batch proposals: one row per member ask, so brief.v1
		// decisions collapse members with a primary-key seek per page.
		`CREATE TABLE IF NOT EXISTS curation_batch_members (
		ask_id TEXT PRIMARY KEY,
		batch_key TEXT NOT NULL,
		proposal_event_id TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
		`CREATE INDEX IF NOT EXISTS idx_curation_batch_members_key ON curation_batch_members(batch_key)`},
	},
	// One durable delivery queue for wakes and human notifications, plus the
	// indexed open-ask deadline ledger the escalation clock reads.
	{Version: 93, AfterApply: installDeliveryQueue},
	// S20 seeks promised notes by due time instead of scanning brief_notes.
	{Version: 94, AfterApply: installBriefNotesNextAtIndex},
	// Trashing a message removes its brief note; restoring the message puts
	// the note back and dirties the card so the promise fold is recomputed.
	{Version: 95, AfterApply: installBriefNotesTrash},
	// Date-only by and next.at were stored at midnight. End of that UTC day
	// is the resolution brief_asks and ask_deadlines both use.
	{Version: 96, AfterApply: installDateOnlyEndOfDay},
	// Filed-batch respond ids follow the oldest member of the whole batch.
	// brief_asks.batch_id plus idx_brief_asks_batch is that seek.
	{Version: 97, AfterApply: installBriefAskBatchID},
	// A live note promise is the stated next. Existing briefs were folded
	// when an earlier plan-step date could win, so requeue them once.
	{Version: 98, AfterApply: requeueBriefPromiseFold},
	{Version: 99, AfterApply: installRunReceiptIndexes},
	{Version: 100, AfterApply: installBriefLoopFacts},
	{Version: 101, AfterApply: requeueIncumbentLoopFacts},
	{Version: 102, AfterApply: installLoopAdmission},
	{Version: 103, AfterApply: installLoopAdmissionRecency},
	// Handle allocation reserves purged identities with kind + ref, case-insensitively.
	// The primary key (kind, id, ref, owner) cannot seek that pair.
	{Version: 104, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_resource_access_tombstones_kind_ref ON resource_access_tombstones(kind, ref COLLATE NOCASE)`,
	}},
	{Version: 105, AfterApply: installBriefAgentOwnershipIndexes},
	{Version: 106, AfterApply: upgradeAgentSummaryAndBriefAskIndexes},
	{Version: 107, AfterApply: installHostBridge},
	{Version: 108, AfterApply: installBriefPlanRefs},
	// Channel options (addressing, kinds, label) and the one channel a
	// grant-created agent may declare.
	{Version: 109, AfterApply: installChannelDeclarations},
	// The periodic auth purge seeks expired access tokens and expired or
	// revoked refresh sessions, and keeps each principal's newest access
	// token by probing (agent_id, created_at).
	{Version: 110, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_auth_access_tokens_expires_at ON auth_access_tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_access_tokens_agent_created ON auth_access_tokens(agent_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_refresh_sessions_expires_at ON auth_refresh_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_refresh_sessions_revoked_at ON auth_refresh_sessions(revoked_at) WHERE revoked_at IS NOT NULL`,
	}},
	// Key revision and revision-artifact reference edges by their document.
	// Installs triggers only; existing edges move in bounded maintenance.
	{Version: 111, AfterApply: installDocumentAccessHistory},
	// The inbox lifecycle backfill splits one item's refs across chunks:
	// adds the job's ref_offset and the item's lifecycle_partial columns and
	// reinstalls the lifecycle triggers. DDL only; no rows are scanned.
	{Version: 112, AfterApply: indexInboxLifecycle},
	{Version: 113, Statements: []string{
		`CREATE TABLE IF NOT EXISTS dashboard_pins (principal_id TEXT NOT NULL, position INTEGER NOT NULL CHECK(position>=0 AND position<12), document_id TEXT NOT NULL, PRIMARY KEY(principal_id,position), UNIQUE(principal_id,document_id));`,
		`CREATE INDEX IF NOT EXISTS idx_events_report_subject_ts ON events(COALESCE(json_extract(payload_json,'$.subject_ref'),json_extract(payload_json,'$.payload.subject_ref')),ts DESC,id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_agents_collection_roster ON agents(COALESCE(json_extract(metadata_json,'$.principal_kind'),'agent'),username);`,
	}},
	{Version: 114, AfterApply: installAttentionRecords},
	// Exclude plain chatter from the bounded promise fold and admit recent
	// grouping candidates through a lifecycle-filtered ordering index.
	{Version: 115, Statements: []string{
		`CREATE VIEW IF NOT EXISTS work_grouping_card_positions AS SELECT id,created_at,column_key FROM cards WHERE archived_at IS NULL AND trashed_at IS NULL;`,
		`CREATE VIEW IF NOT EXISTS work_grouping_brief_positions AS SELECT card_id,open,promise_at,last_evidence_at,loop_annotated,loop_annotated_at,loop_last_run_at FROM brief_initiatives;`,
		`CREATE VIEW IF NOT EXISTS work_grouping_field_positions AS SELECT c.id,EXISTS(SELECT 1 FROM card_plans p WHERE p.card_id=c.id) AS plan_exists,EXISTS(SELECT 1 FROM work_metadata m WHERE m.card_id=c.id) AS metadata_exists FROM cards c;`,
		`CREATE INDEX IF NOT EXISTS idx_brief_notes_promise_facts ON brief_notes(card_id,ts DESC) WHERE next_at IS NOT NULL OR evidence=1;`,
		`CREATE INDEX IF NOT EXISTS idx_cards_grouping_recent ON cards(trashed_at,archived_at,created_at DESC,id DESC) WHERE column_key NOT IN ('done','cancelled');`,
		`CREATE INDEX IF NOT EXISTS idx_cards_grouping_closed ON cards(trashed_at,archived_at,created_at DESC,id DESC) WHERE column_key IN ('done','cancelled');`,
		`CREATE INDEX IF NOT EXISTS idx_events_promise_facts ON events(thread_id,ts DESC) WHERE type='message_posted' AND (json_extract(payload_json,'$.payload.next.at') IS NOT NULL OR json_array_length(json_extract(payload_json,'$.payload.evidence_refs'))>0);`,
		`INSERT INTO brief_dirty(card_id) SELECT card_id FROM brief_initiatives WHERE true ON CONFLICT DO NOTHING;`,
	}},
	{Version: 116, ApplyChunk: accessAdmissionMigrationChunk, Verify: verifyAdmissionMigration},
	{Version: 117, AfterApply: restrictAdmissionUpdatePrograms, Verify: verifyAdmissionUpdatePrograms},
	// Card work leases: one row per card, CAS-updated inside the write
	// transaction, plus the partial index the lease expiry clock seeks.
	{Version: 118, AfterApply: installCardLeases},
	// Stop delivering legacy curation prompts. Canonical withdrawal events and
	// resolution claims commit together, retaining a reason for each request.
	{Version: 119, AfterApply: quietCurationSuggestions},

	// Indexed derived positions keep background polls independent of history.
	{Version: 120, AfterApply: installIdleMaintenancePositions},
	{Version: 121, AfterApply: installWorkTracking},
	{Version: 122, Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_events_card_done ON events(json_extract(payload_json,'$.payload.card_id'),anx_timestamp_key(ts) DESC,id DESC) WHERE type='card_moved' AND json_extract(payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(payload_json,'$.payload.from_column_key'),'')<>'done' AND COALESCE(trashed_at,'')=''`,
		`CREATE VIEW IF NOT EXISTS work_done_event_positions AS SELECT id,ts,json_extract(payload_json,'$.payload.card_id') AS card_id FROM events INDEXED BY idx_events_card_done WHERE type='card_moved' AND json_extract(payload_json,'$.payload.column_key')='done' AND COALESCE(json_extract(payload_json,'$.payload.from_column_key'),'')<>'done' AND COALESCE(trashed_at,'')=''`,
	}},
	{Version: 123, AfterApply: installAskPresentationHeads},
	{Version: 124, AfterApply: installAskRetirementDeliveryPolicy},
	// Watch trends bound each record family's read through identity/time
	// positions; canonical hydrated rows keep their own authorization.
	{Version: 125, AfterApply: installWatchTrendPositions},
	{Version: 126, AfterApply: installInitiativeBoardAuthority},
	// The work tree stores parentage and the Unsorted tray (see work_tree.go).
	{Version: 127, AfterApply: installWorkTree},
	// Watch's latest-note excerpt read a field notes never set.
	{Version: 128, AfterApply: repairBriefNoteExcerpts},
	// Core's own reminders have only a summary, and showed as blank notes.
	{Version: 129, AfterApply: repairBriefNoteExcerpts},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	appliedVersions, err := loadAppliedVersions(ctx, db)
	if err != nil {
		return err
	}

	// Verify every recorded postcondition against one read snapshot, matching
	// applyWorkspaceMigrations' fenced loader: one BEGIN/COMMIT round trip
	// instead of one per migration, and a consistent view instead of one
	// snapshot per verify.
	if anyRecorded(migrations, appliedVersions) {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return err
		}
		for _, m := range migrations {
			if !appliedVersions[m.Version] || m.Verify == nil {
				continue
			}
			if err := m.Verify(ctx, tx); err != nil {
				tx.Rollback()
				return fmt.Errorf("recorded migration %d: %w", m.Version, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	for _, m := range migrations {
		if appliedVersions[m.Version] {
			continue
		}

		if err := applyMigration(ctx, db, m); err != nil {
			return err
		}
	}

	return nil
}

func anyRecorded(all []migration, applied map[int]bool) bool {
	for _, m := range all {
		if applied[m.Version] {
			return true
		}
	}
	return false
}

func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	return applyMigrationToLedger(ctx, db, m, "schema_migrations")
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

func sqliteTableExists(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func sqliteTableHasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
