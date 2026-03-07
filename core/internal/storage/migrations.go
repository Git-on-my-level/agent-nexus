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
