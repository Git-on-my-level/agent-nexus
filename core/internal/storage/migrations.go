package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
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

func applyMigration19CardRevisions(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil || !ok {
		return err
	}
	for _, col := range []struct {
		name string
		sql  string
	}{
		{"head_revision_id", `ALTER TABLE cards ADD COLUMN head_revision_id TEXT;`},
		{"head_revision_number", `ALTER TABLE cards ADD COLUMN head_revision_number INTEGER NOT NULL DEFAULT 1;`},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "cards", col.name)
		if err != nil {
			return fmt.Errorf("migration 19 pragma cards.%s: %w", col.name, err)
		}
		if !has {
			if _, err := tx.ExecContext(ctx, col.sql); err != nil {
				return fmt.Errorf("migration 19 add cards.%s: %w", col.name, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS card_revisions (
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
	);`); err != nil {
		return fmt.Errorf("migration 19 create card_revisions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_number ON card_revisions (card_id, revision_number);`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_id ON card_revisions (card_id, revision_id);`); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_revisions`).Scan(&existing); err != nil {
		return fmt.Errorf("migration 19 count card_revisions: %w", err)
	}
	if existing > 0 {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS card_versions;`); err != nil {
			return fmt.Errorf("migration 19 drop card_versions: %w", err)
		}
		return nil
	}
	hasCardVersions, err := sqliteTableExists(ctx, tx, "card_versions")
	if err != nil {
		return err
	}
	if hasCardVersions {
		if err := migration19BackfillFromCardVersions(ctx, tx); err != nil {
			return err
		}
	} else if err := migration19BackfillFromCards(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS card_versions;`); err != nil {
		return fmt.Errorf("migration 19 drop card_versions: %w", err)
	}
	return nil
}

func migration19BackfillFromCards(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, thread_id, title, summary, definition_of_done_json, refs_json, created_at, created_by FROM cards`)
	if err != nil {
		return fmt.Errorf("migration 19 select cards: %w", err)
	}
	defer rows.Close()
	type cardRow struct {
		id, title, summary, dod, refs, createdAt, createdBy string
		thread                                              sql.NullString
	}
	var cards []cardRow
	for rows.Next() {
		var r cardRow
		if err := rows.Scan(&r.id, &r.thread, &r.title, &r.summary, &r.dod, &r.refs, &r.createdAt, &r.createdBy); err != nil {
			return fmt.Errorf("migration 19 scan card: %w", err)
		}
		cards = append(cards, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range cards {
		if err := migration19InsertCardRevision(ctx, tx, r.id, 1, "", r.thread.String, r.title, r.summary, r.dod, r.refs, r.createdAt, r.createdBy); err != nil {
			return err
		}
	}
	return nil
}

func migration19BackfillFromCardVersions(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT card_id, version, thread_id, title, summary, definition_of_done_json, refs_json, created_at, created_by FROM card_versions ORDER BY card_id, version`)
	if err != nil {
		return fmt.Errorf("migration 19 select card_versions: %w", err)
	}
	defer rows.Close()
	type versionRow struct {
		cardID, title, summary, dod, refs, createdAt, createdBy string
		version                                                 int
		thread                                                  sql.NullString
	}
	prev := map[string]string{}
	for rows.Next() {
		var r versionRow
		if err := rows.Scan(&r.cardID, &r.version, &r.thread, &r.title, &r.summary, &r.dod, &r.refs, &r.createdAt, &r.createdBy); err != nil {
			return fmt.Errorf("migration 19 scan card_version: %w", err)
		}
		if err := migration19InsertCardRevision(ctx, tx, r.cardID, r.version, prev[r.cardID], r.thread.String, r.title, r.summary, r.dod, r.refs, r.createdAt, r.createdBy); err != nil {
			return err
		}
		prev[r.cardID] = migration19RevisionID(r.cardID, r.version)
	}
	return rows.Err()
}

func migration19RevisionID(cardID string, revisionNumber int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("card-revision:%s:%d", strings.TrimSpace(cardID), revisionNumber))).String()
}

func migration19InsertCardRevision(ctx context.Context, tx *sql.Tx, cardID string, revisionNumber int, prevRevisionID, threadID, title, summary, dodJSON, refsJSON, createdAt, createdBy string) error {
	revisionID := migration19RevisionID(cardID, revisionNumber)
	content := map[string]any{"title": strings.TrimSpace(title), "summary": strings.TrimSpace(summary), "definition_of_done": json.RawMessage(dodJSON)}
	contentBytes, _ := json.Marshal(content)
	sum := sha256.Sum256(contentBytes)
	contentHash := hex.EncodeToString(sum[:])
	metadata := map[string]any{
		"id":              revisionID,
		"kind":            "card",
		"created_at":      createdAt,
		"created_by":      createdBy,
		"content_type":    "structured",
		"content_hash":    contentHash,
		"refs":            json.RawMessage(refsJSON),
		"card_id":         cardID,
		"revision_id":     revisionID,
		"revision_number": revisionNumber,
		"title":           strings.TrimSpace(title),
		"summary":         strings.TrimSpace(summary),
	}
	if strings.TrimSpace(prevRevisionID) != "" {
		metadata["prev_revision_id"] = strings.TrimSpace(prevRevisionID)
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		VALUES (?, 'card', ?, ?, ?, 'structured', ?, ?, ?)`, revisionID, nullableMigrationString(threadID), createdAt, createdBy, contentHash, refsJSON, string(metadataJSON)); err != nil {
		return fmt.Errorf("migration 19 insert artifact: %w", err)
	}
	revisionHash := hex.EncodeToString(sha256.New().Sum([]byte(contentHash + cardID + fmt.Sprint(revisionNumber))))
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, revisionID, cardID, revisionNumber, nullableMigrationString(prevRevisionID), revisionID, nullableMigrationString(threadID), refsJSON, revisionHash, createdAt, createdBy); err != nil {
		return fmt.Errorf("migration 19 insert card_revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET head_revision_id = ?, head_revision_number = ? WHERE id = ? AND COALESCE(head_revision_number, 0) <= ?`, revisionID, revisionNumber, cardID, revisionNumber); err != nil {
		return fmt.Errorf("migration 19 update card head: %w", err)
	}
	return nil
}

func nullableMigrationString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func applyMigration12TopicsSummaryExtensionsJSON(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "topics")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasBody, err := sqliteTableHasColumn(ctx, tx, "topics", "body_json")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.body_json: %w", err)
	}
	hasExt, err := sqliteTableHasColumn(ctx, tx, "topics", "extensions_json")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.extensions_json: %w", err)
	}
	if hasBody && !hasExt {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics RENAME COLUMN body_json TO extensions_json`); err != nil {
			return fmt.Errorf("migration 12 rename body_json: %w", err)
		}
		hasExt = true
	} else if !hasExt {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics ADD COLUMN extensions_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("migration 12 add extensions_json: %w", err)
		}
		hasExt = true
	}
	hasSummary, err := sqliteTableHasColumn(ctx, tx, "topics", "summary")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.summary: %w", err)
	}
	if !hasSummary {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics ADD COLUMN summary TEXT`); err != nil {
			return fmt.Errorf("migration 12 add summary: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, type, title, thread_id, extensions_json FROM topics`)
	if err != nil {
		return fmt.Errorf("migration 12 scan topics: %w", err)
	}
	defer rows.Close()

	type row struct {
		id, ext string
		typ     sql.NullString
		title   sql.NullString
		thread  sql.NullString
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.typ, &r.title, &r.thread, &r.ext); err != nil {
			return fmt.Errorf("migration 12 scan row: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migration 12 rows: %w", err)
	}

	for _, r := range batch {
		extObj := map[string]any{}
		if strings.TrimSpace(r.ext) != "" {
			if err := json.Unmarshal([]byte(r.ext), &extObj); err != nil || extObj == nil {
				extObj = map[string]any{}
			}
		}
		migration12CoerceLegacyTopicExt(extObj)

		nextType := strings.TrimSpace(r.typ.String)
		if nextType == "" {
			nextType = strings.TrimSpace(migration12StringFromAny(extObj["type"]))
		}
		nextTitle := strings.TrimSpace(r.title.String)
		if nextTitle == "" {
			nextTitle = strings.TrimSpace(migration12StringFromAny(extObj["title"]))
		}
		nextThread := strings.TrimSpace(r.thread.String)
		if nextThread == "" {
			nextThread = strings.TrimSpace(migration12StringFromAny(extObj["thread_id"]))
		}

		summary := strings.TrimSpace(migration12StringFromAny(extObj["summary"]))
		if summary == "" && nextTitle != "" {
			summary = nextTitle
		}

		hasRefLists := migration12ExtHasRefLists(extObj)
		if hasRefLists {
			targets := migration12CombineTopicRefTargets(extObj, nextThread)
			if err := migration12ReplaceTopicRefEdges(ctx, tx, r.id, targets); err != nil {
				return err
			}
		} else if err := migration12AnnotateTopicRefMetadata(ctx, tx, r.id, nextThread); err != nil {
			return err
		}

		for _, k := range migration12TopicKeysPromotedFromExt() {
			delete(extObj, k)
		}
		extBytes, err := json.Marshal(extObj)
		if err != nil {
			return fmt.Errorf("migration 12 marshal extensions for %s: %w", r.id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET type = ?, title = ?, thread_id = ?, summary = ?, extensions_json = ? WHERE id = ?`,
			nextType, nextTitle, nextThread, summary, string(extBytes), r.id,
		); err != nil {
			return fmt.Errorf("migration 12 update topic %s: %w", r.id, err)
		}
	}
	return nil
}

func applyMigration13DropEventsBodyJSON(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "events")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasBody, err := sqliteTableHasColumn(ctx, tx, "events", "body_json")
	if err != nil {
		return fmt.Errorf("migration 13 pragma events.body_json: %w", err)
	}
	if !hasBody {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, refs_json, payload_json, body_json FROM events`)
	if err != nil {
		return fmt.Errorf("migration 13 select events: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		id, refsJSON, payloadJSON string
		bodyJSON                  sql.NullString
	}
	var batch []rowData
	for rows.Next() {
		var r rowData
		if err := rows.Scan(&r.id, &r.refsJSON, &r.payloadJSON, &r.bodyJSON); err != nil {
			return fmt.Errorf("migration 13 scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migration 13 rows: %w", err)
	}

	for _, r := range batch {
		newRefs := r.refsJSON
		newPayload := r.payloadJSON

		bodyStr := ""
		if r.bodyJSON.Valid {
			bodyStr = strings.TrimSpace(r.bodyJSON.String)
		}
		if bodyStr != "" && bodyStr != "{}" {
			var body map[string]any
			if err := json.Unmarshal([]byte(bodyStr), &body); err != nil || body == nil {
				body = map[string]any{}
			}
			if raw, ok := body["refs"]; ok {
				if b, err := json.Marshal(raw); err == nil {
					newRefs = string(b)
				}
			}
			wrapper := migration13EventPayloadWrapperFromBodyMap(body)
			b, err := json.Marshal(wrapper)
			if err != nil {
				return fmt.Errorf("migration 13 marshal payload for %s: %w", r.id, err)
			}
			newPayload = string(b)
		} else {
			var pl map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(r.payloadJSON)), &pl); err != nil || pl == nil {
				pl = map[string]any{}
			}
			if _, has := pl["payload"]; !has {
				wrap := map[string]any{"payload": pl}
				b, err := json.Marshal(wrap)
				if err != nil {
					return fmt.Errorf("migration 13 wrap payload for %s: %w", r.id, err)
				}
				newPayload = string(b)
			}
		}

		if newRefs != r.refsJSON || newPayload != r.payloadJSON {
			if _, err := tx.ExecContext(ctx, `UPDATE events SET refs_json = ?, payload_json = ? WHERE id = ?`, newRefs, newPayload, r.id); err != nil {
				return fmt.Errorf("migration 13 update event %s: %w", r.id, err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE events DROP COLUMN body_json`); err != nil {
		return fmt.Errorf("migration 13 drop body_json: %w", err)
	}
	return nil
}

// migration13EventPayloadWrapperFromBodyMap matches primitives.EventPayloadWrapperFromBodyMap (duplicated
// here to avoid an import cycle: this package is imported by internal primitives tests).
func migration13EventPayloadWrapperFromBodyMap(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{"payload": map[string]any{}}
	}
	wrapper := map[string]any{}
	if raw, ok := body["payload"]; ok && raw != nil {
		p, ok := raw.(map[string]any)
		if !ok {
			wrapper["payload"] = map[string]any{}
		} else {
			inner := make(map[string]any, len(p))
			for k, v := range p {
				inner[k] = v
			}
			wrapper["payload"] = inner
		}
	} else {
		wrapper["payload"] = map[string]any{}
	}
	envelope := map[string]struct{}{
		"id": {}, "type": {}, "ts": {}, "actor_id": {}, "thread_id": {}, "refs": {}, "payload": {},
	}
	for k, v := range body {
		if _, skip := envelope[k]; skip {
			continue
		}
		switch k {
		case "archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason":
			continue
		}
		wrapper[k] = v
	}
	return wrapper
}

func migration12TopicKeysPromotedFromExt() []string {
	return []string{
		"id", "type", "title", "summary", "thread_id", "thread_ref", "primary_thread_ref", "primary_thread_id",
		"owner_refs", "document_refs", "board_refs", "related_refs",
		"status", "provenance",
		"created_at", "created_by", "updated_at", "updated_by", "state",
		"archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason",
	}
}

func migration12StringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func migration12CoerceLegacyTopicExt(ext map[string]any) {
	legacyRef := "primary" + "_thread_ref"
	legacyID := "primary" + "_thread_id"
	if v, ok := ext[legacyRef]; ok && v != nil {
		refStr := migration12StringFromAny(v)
		if refStr != "" {
			if prefix, id, okRef := migration12SplitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
					ext["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(ext, legacyRef)
	}
	if v, ok := ext[legacyID]; ok && v != nil {
		id := migration12StringFromAny(v)
		if id != "" {
			if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
				ext["thread_id"] = id
			}
		}
		delete(ext, legacyID)
	}
	if raw, exists := ext["thread_ref"]; exists && raw != nil {
		refStr := migration12StringFromAny(raw)
		if refStr != "" {
			if prefix, id, okRef := migration12SplitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
					ext["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(ext, "thread_ref")
	}
}

func migration12SplitTypedRef(raw string) (prefix, id string, ok bool) {
	raw = strings.TrimSpace(raw)
	i := strings.IndexByte(raw, ':')
	if i <= 0 || i == len(raw)-1 {
		return "", "", false
	}
	return raw[:i], raw[i+1:], true
}

func migration12ExtHasRefLists(ext map[string]any) bool {
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		raw, ok := ext[field]
		if !ok || raw == nil {
			continue
		}
		switch sl := raw.(type) {
		case []any:
			if len(sl) > 0 {
				return true
			}
		case []string:
			if len(sl) > 0 {
				return true
			}
		}
	}
	return false
}

type migration12RefTarget struct {
	TargetType   string
	TargetID     string
	EdgeType     string
	MetadataJSON string
}

func migration12TopicRefMetaJSON(field string) string {
	b, err := json.Marshal(map[string]string{"topic_ref_field": field})
	if err != nil {
		return `{"topic_ref_field":"related_refs"}`
	}
	return string(b)
}

func migration12CombineTopicRefTargets(ext map[string]any, primaryThreadID string) []migration12RefTarget {
	targets := make([]migration12RefTarget, 0, 8)
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		for _, t := range migration12TypedRefEdgeTargets("ref", migration12StringSliceFromAny(ext[field])) {
			t.MetadataJSON = migration12TopicRefMetaJSON(field)
			targets = append(targets, t)
		}
	}
	if strings.TrimSpace(primaryThreadID) != "" {
		targets = append(targets, migration12RefTarget{
			TargetType:   "thread",
			TargetID:     strings.TrimSpace(primaryThreadID),
			EdgeType:     "ref",
			MetadataJSON: `{"topic_ref_field":"_primary_thread"}`,
		})
	}
	return targets
}

func migration12StringSliceFromAny(raw any) []string {
	if raw == nil {
		return nil
	}
	switch sl := raw.(type) {
	case []string:
		out := make([]string, 0, len(sl))
		for _, s := range sl {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(sl))
		for _, v := range sl {
			s := migration12StringFromAny(v)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func migration12TypedRefEdgeTargets(edgeType string, refs []string) []migration12RefTarget {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	targets := make([]migration12RefTarget, 0, len(refs))
	for _, raw := range refs {
		prefix, id, ok := migration12SplitTypedRef(raw)
		if !ok {
			continue
		}
		key := edgeType + "\x00" + prefix + "\x00" + id
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, migration12RefTarget{
			TargetType: prefix,
			TargetID:   id,
			EdgeType:   edgeType,
		})
	}
	return targets
}

func migration12ReplaceTopicRefEdges(ctx context.Context, tx *sql.Tx, topicID string, targets []migration12RefTarget) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("migration 12 ref edges: empty topic id")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "topic", topicID); err != nil {
		return fmt.Errorf("migration 12 clear ref edges for topic %s: %w", topicID, err)
	}
	if len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetType := strings.TrimSpace(target.TargetType)
		targetID := strings.TrimSpace(target.TargetID)
		edgeType := strings.TrimSpace(target.EdgeType)
		if targetType == "" || targetID == "" || edgeType == "" {
			continue
		}
		key := edgeType + "\x00" + targetType + "\x00" + targetID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		meta := strings.TrimSpace(target.MetadataJSON)
		if meta == "" {
			meta = "{}"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), "topic", topicID, targetType, targetID, edgeType, now, meta,
		); err != nil {
			return fmt.Errorf("migration 12 insert ref edge for topic %s: %w", topicID, err)
		}
	}
	return nil
}

func migration12AnnotateTopicRefMetadata(ctx context.Context, tx *sql.Tx, topicID, primaryThreadID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT target_type, target_id, metadata_json FROM ref_edges WHERE source_type = 'topic' AND source_id = ?`,
		topicID,
	)
	if err != nil {
		return fmt.Errorf("migration 12 list ref edges %s: %w", topicID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetType, targetID, meta string
		if err := rows.Scan(&targetType, &targetID, &meta); err != nil {
			return fmt.Errorf("migration 12 scan ref edge: %w", err)
		}
		field := migration12TopicRefFieldFromStoredMeta(meta, targetType, targetID, primaryThreadID)
		if field == "_primary_thread" {
			continue
		}
		newMeta := migration12TopicRefMetaJSON(field)
		if strings.TrimSpace(meta) == newMeta {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE ref_edges SET metadata_json = ? WHERE source_type = 'topic' AND source_id = ? AND target_type = ? AND target_id = ? AND edge_type = 'ref'`,
			newMeta, topicID, targetType, targetID,
		); err != nil {
			return fmt.Errorf("migration 12 annotate ref edge: %w", err)
		}
	}
	return rows.Err()
}

func migration12TopicRefFieldFromStoredMeta(metaJSON, targetType, targetID, primaryThreadID string) string {
	metaJSON = strings.TrimSpace(metaJSON)
	if metaJSON != "" && metaJSON != "{}" {
		var m map[string]any
		if json.Unmarshal([]byte(metaJSON), &m) == nil {
			if s, ok := m["topic_ref_field"].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	tt := strings.TrimSpace(targetType)
	tid := strings.TrimSpace(targetID)
	if tt == "thread" && tid == strings.TrimSpace(primaryThreadID) {
		return "_primary_thread"
	}
	switch tt {
	case "actor":
		return "owner_refs"
	case "document":
		return "document_refs"
	case "board":
		return "board_refs"
	default:
		return "related_refs"
	}
}

func applyMigration11CardsSummaryAndDropPriority(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"cards", "card_versions"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		hasPriority, err := sqliteTableHasColumn(ctx, tx, table, "priority")
		if err != nil {
			return fmt.Errorf("migration 11 pragma %s.priority: %w", table, err)
		}
		if hasPriority {
			q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN priority;", table)
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("migration 11 %s: %w", q, err)
			}
		}
		hasBodyMarkdown, err := sqliteTableHasColumn(ctx, tx, table, "body_markdown")
		if err != nil {
			return fmt.Errorf("migration 11 pragma %s.body_markdown: %w", table, err)
		}
		if hasBodyMarkdown {
			q := fmt.Sprintf("ALTER TABLE %s RENAME COLUMN body_markdown TO summary;", table)
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("migration 11 %s: %w", q, err)
			}
		}
	}
	return nil
}

func applyMigration10DropThreadFilterColumns(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "threads")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	stmts := []string{
		`DROP INDEX IF EXISTS idx_threads_priority_updated_at;`,
		`DROP INDEX IF EXISTS idx_threads_cadence_preset_updated_at;`,
		`ALTER TABLE threads DROP COLUMN filter_priority;`,
		`ALTER TABLE threads DROP COLUMN filter_owner;`,
		`ALTER TABLE threads DROP COLUMN filter_due_at;`,
		`ALTER TABLE threads DROP COLUMN filter_cadence;`,
		`ALTER TABLE threads DROP COLUMN filter_cadence_preset;`,
		`ALTER TABLE threads DROP COLUMN filter_tags_json;`,
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migration 10: %w", err)
		}
	}
	return nil
}

func applyMigration9LegacyStatusCleanup(ctx context.Context, tx *sql.Tx) error {
	execIfTable := func(table, stmt string) error {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil || !ok {
			return err
		}
		_, err = tx.ExecContext(ctx, stmt)
		return err
	}
	stmts := []struct {
		table string
		sql   string
	}{
		{"topics", `UPDATE topics
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at, created_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by, created_by)
			  WHERE status = 'archived'
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"boards", `UPDATE boards
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at, created_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by, created_by)
			  WHERE status IN ('paused', 'closed')
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"threads", `UPDATE threads
			    SET archived_at = COALESCE(NULLIF(archived_at, ''), updated_at),
			        archived_by = COALESCE(NULLIF(archived_by, ''), updated_by)
			  WHERE filter_status = 'archived'
			    AND COALESCE(NULLIF(archived_at, ''), '') = ''
			    AND COALESCE(NULLIF(trashed_at, ''), '') = '';`},
		{"cards", `UPDATE cards
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'done' ELSE resolution END
			  WHERE status = 'done' AND column_key != 'done';`},
		{"cards", `UPDATE cards
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'canceled' ELSE resolution END
			  WHERE status = 'cancelled' AND column_key != 'done';`},
		{"cards", `UPDATE cards SET column_key = 'in_progress' WHERE status = 'in_progress' AND column_key = 'backlog';`},
		{"cards", `UPDATE cards SET column_key = 'ready' WHERE status = 'todo' AND column_key = 'backlog';`},
		{"card_versions", `UPDATE card_versions
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'done' ELSE resolution END
			  WHERE status = 'done' AND column_key != 'done';`},
		{"card_versions", `UPDATE card_versions
			    SET column_key = 'done',
			        resolution = CASE WHEN COALESCE(NULLIF(resolution, ''), '') = '' THEN 'canceled' ELSE resolution END
			  WHERE status = 'cancelled' AND column_key != 'done';`},
		{"card_versions", `UPDATE card_versions SET column_key = 'in_progress' WHERE status = 'in_progress' AND column_key = 'backlog';`},
		{"card_versions", `UPDATE card_versions SET column_key = 'ready' WHERE status = 'todo' AND column_key = 'backlog';`},
	}
	for _, s := range stmts {
		if err := execIfTable(s.table, s.sql); err != nil {
			return fmt.Errorf("migration 9 exec on %s: %w", s.table, err)
		}
	}
	for _, d := range []struct {
		table, column string
	}{
		{"topics", "status"},
		{"boards", "status"},
		{"threads", "filter_status"},
		{"cards", "status"},
		{"card_versions", "status"},
	} {
		ok, err := sqliteTableExists(ctx, tx, d.table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", d.table, d.column)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 9 %s: %w", q, err)
		}
	}
	return nil
}

// applyMigration15DropBoardsDocumentsLabelsJSON removes labels_json from boards and documents
// (launch schema simplification; no indexes referenced this column). actors.tags_json is kept:
// it is used by the actors store and auth for identity/credential metadata, not for board/doc labeling.
func applyMigration15DropBoardsDocumentsLabelsJSON(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"boards", "documents"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		has, err := sqliteTableHasColumn(ctx, tx, table, "labels_json")
		if err != nil {
			return fmt.Errorf("migration 15 pragma %s.labels_json: %w", table, err)
		}
		if !has {
			continue
		}
		q := fmt.Sprintf("ALTER TABLE %s DROP COLUMN labels_json;", table)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 15 %s: %w", q, err)
		}
	}
	return nil
}

// applyMigration16DropTopicsTypeColumn removes topic classification; topics are identified by title/summary/refs only.
func applyMigration16DropTopicsTypeColumn(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "topics")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS idx_topics_type_updated_at`); err != nil {
		return fmt.Errorf("migration 16 drop idx_topics_type_updated_at: %w", err)
	}
	has, err := sqliteTableHasColumn(ctx, tx, "topics", "type")
	if err != nil {
		return fmt.Errorf("migration 16 pragma topics.type: %w", err)
	}
	if !has {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE topics DROP COLUMN type`); err != nil {
		return fmt.Errorf("migration 16 drop topics.type: %w", err)
	}
	return nil
}

func applyMigration17BoardsDocumentsSummary(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"boards", "documents"} {
		ok, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		has, err := sqliteTableHasColumn(ctx, tx, table, "summary")
		if err != nil {
			return fmt.Errorf("migration 17 pragma %s.summary: %w", table, err)
		}
		if has {
			continue
		}
		q := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN summary TEXT NOT NULL DEFAULT ''`, table)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migration 17 add %s.summary: %w", table, err)
		}
	}
	return nil
}

// applyMigration14ClearDerivedInboxForCanonicalCategories removes materialized inbox rows so
// projections can rebuild with enums.inbox_category values and slimmer per-row data_json.
func applyMigration14ClearDerivedInboxForCanonicalCategories(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "derived_inbox_items")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items`); err != nil {
		return fmt.Errorf("migration 14 clear derived_inbox_items: %w", err)
	}
	threadsOK, err := sqliteTableExists(ctx, tx, "threads")
	if err != nil {
		return err
	}
	if !threadsOK {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO derived_topic_dirty_queue (thread_id, dirty_at)
		SELECT id, strftime('%Y-%m-%dT%H:%M:%fZ', 'now') FROM threads`); err != nil {
		return fmt.Errorf("migration 14 queue derived topic rebuild: %w", err)
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
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("migration rollback failed: %v", rbErr)
				}
				return fmt.Errorf("apply migration %d: %w", m.Version, err)
			}
		}
		if m.AfterApply != nil {
			if err := m.AfterApply(ctx, tx); err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("migration rollback failed: %v", rbErr)
				}
				return fmt.Errorf("apply migration %d after hook: %w", m.Version, err)
			}
		}

		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`,
			m.Version,
		); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("migration rollback failed: %v", rbErr)
			}
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
