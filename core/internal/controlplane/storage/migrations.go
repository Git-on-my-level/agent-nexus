package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
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
			`CREATE TABLE IF NOT EXISTS accounts (
				id TEXT PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				display_name TEXT NOT NULL,
				status TEXT NOT NULL,
				created_at TEXT NOT NULL,
				last_login_at TEXT,
				passkey_registered_at TEXT
			);`,
			`CREATE TABLE IF NOT EXISTS passkey_credentials (
				credential_id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				user_handle BLOB NOT NULL,
				public_key BLOB NOT NULL DEFAULT X'',
				attestation_type TEXT NOT NULL,
				transport TEXT NOT NULL DEFAULT '',
				sign_count INTEGER NOT NULL DEFAULT 0,
				backup_eligible INTEGER NOT NULL DEFAULT 0,
				backup_state INTEGER NOT NULL DEFAULT 0,
				aaguid BLOB NOT NULL DEFAULT X'',
				attachment TEXT NOT NULL DEFAULT '',
				credential_json TEXT NOT NULL DEFAULT '{}',
				created_at TEXT NOT NULL,
				last_used_at TEXT,
				FOREIGN KEY(account_id) REFERENCES accounts(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_credentials_account_id ON passkey_credentials (account_id);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_credentials_user_handle ON passkey_credentials (user_handle);`,
			`CREATE TABLE IF NOT EXISTS passkey_ceremonies (
				id TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				account_id TEXT,
				email TEXT NOT NULL,
				display_name TEXT NOT NULL,
				user_handle BLOB NOT NULL,
				challenge TEXT NOT NULL,
				rp_id TEXT NOT NULL,
				origin TEXT NOT NULL,
				credential_id_hint TEXT NOT NULL DEFAULT '',
				expires_at TEXT NOT NULL,
				created_at TEXT NOT NULL,
				consumed_at TEXT,
				FOREIGN KEY(account_id) REFERENCES accounts(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_ceremonies_account_id ON passkey_ceremonies (account_id);`,
			`CREATE INDEX IF NOT EXISTS idx_passkey_ceremonies_expires_at ON passkey_ceremonies (expires_at);`,
			`CREATE TABLE IF NOT EXISTS account_sessions (
				id TEXT PRIMARY KEY,
				account_id TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				issued_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				revoked_at TEXT,
				FOREIGN KEY(account_id) REFERENCES accounts(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_account_sessions_account_id ON account_sessions (account_id);`,
			`CREATE TABLE IF NOT EXISTS organizations (
				id TEXT PRIMARY KEY,
				slug TEXT NOT NULL UNIQUE,
				display_name TEXT NOT NULL,
				plan_tier TEXT NOT NULL,
				status TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
			`CREATE TABLE IF NOT EXISTS organization_memberships (
				id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				account_id TEXT NOT NULL,
				role TEXT NOT NULL,
				status TEXT NOT NULL,
				created_at TEXT NOT NULL,
				UNIQUE(organization_id, account_id),
				FOREIGN KEY(organization_id) REFERENCES organizations(id),
				FOREIGN KEY(account_id) REFERENCES accounts(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_organization_memberships_account_id ON organization_memberships (account_id);`,
			`CREATE TABLE IF NOT EXISTS organization_invites (
				id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				email TEXT NOT NULL,
				role TEXT NOT NULL,
				status TEXT NOT NULL,
				token_hash TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				accepted_at TEXT,
				accepted_by_account_id TEXT,
				revoked_at TEXT,
				revoked_by_account_id TEXT,
				FOREIGN KEY(organization_id) REFERENCES organizations(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_organization_invites_org_created ON organization_invites (organization_id, created_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_organization_invites_email ON organization_invites (email);`,
			`CREATE TABLE IF NOT EXISTS workspaces (
				id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				slug TEXT NOT NULL,
				display_name TEXT NOT NULL,
				status TEXT NOT NULL,
				region TEXT NOT NULL,
				workspace_tier TEXT NOT NULL,
				workspace_path TEXT NOT NULL,
				base_url TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				UNIQUE(organization_id, slug),
				FOREIGN KEY(organization_id) REFERENCES organizations(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_workspaces_org_created ON workspaces (organization_id, created_at, id);`,
			`CREATE TABLE IF NOT EXISTS provisioning_jobs (
				id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				workspace_id TEXT NOT NULL,
				kind TEXT NOT NULL,
				status TEXT NOT NULL,
				requested_at TEXT NOT NULL,
				started_at TEXT,
				finished_at TEXT,
				failure_reason TEXT,
				FOREIGN KEY(organization_id) REFERENCES organizations(id),
				FOREIGN KEY(workspace_id) REFERENCES workspaces(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_provisioning_jobs_org_requested ON provisioning_jobs (organization_id, requested_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_provisioning_jobs_workspace_requested ON provisioning_jobs (workspace_id, requested_at, id);`,
			`CREATE TABLE IF NOT EXISTS launch_sessions (
				id TEXT PRIMARY KEY,
				workspace_id TEXT NOT NULL,
				account_id TEXT NOT NULL,
				return_path TEXT,
				token_hash TEXT NOT NULL UNIQUE,
				created_at TEXT NOT NULL,
				expires_at TEXT NOT NULL,
				consumed_at TEXT,
				FOREIGN KEY(workspace_id) REFERENCES workspaces(id),
				FOREIGN KEY(account_id) REFERENCES accounts(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_launch_sessions_workspace_created ON launch_sessions (workspace_id, created_at, id);`,
			`CREATE TABLE IF NOT EXISTS audit_events (
				id TEXT PRIMARY KEY,
				event_type TEXT NOT NULL,
				actor_account_id TEXT,
				organization_id TEXT,
				workspace_id TEXT,
				target_type TEXT NOT NULL,
				target_id TEXT NOT NULL,
				metadata_json TEXT NOT NULL DEFAULT '{}',
				occurred_at TEXT NOT NULL
			);`,
			`CREATE INDEX IF NOT EXISTS idx_audit_events_org_occurred ON audit_events (organization_id, occurred_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_audit_events_workspace_occurred ON audit_events (workspace_id, occurred_at, id);`,
			`CREATE INDEX IF NOT EXISTS idx_audit_events_actor_occurred ON audit_events (actor_account_id, occurred_at, id);`,
		},
	},
	{
		Version: 2,
		Statements: []string{
			`ALTER TABLE workspaces ADD COLUMN public_origin TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN core_origin TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN deployment_root TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN desired_state TEXT NOT NULL DEFAULT 'ready';`,
			`ALTER TABLE workspaces ADD COLUMN quota_config_ref TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN quota_envelope_ref TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN deployed_version TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN routing_manifest_path TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN routing_manifest_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE provisioning_jobs ADD COLUMN progress_message TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE provisioning_jobs ADD COLUMN stdout_tail TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE provisioning_jobs ADD COLUMN stderr_tail TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE provisioning_jobs ADD COLUMN retryable INTEGER NOT NULL DEFAULT 1;`,
			`ALTER TABLE provisioning_jobs ADD COLUMN parameters_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE provisioning_jobs ADD COLUMN result_json TEXT NOT NULL DEFAULT '{}';`,
		},
	},
	{
		Version: 3,
		Statements: []string{
			`ALTER TABLE workspaces ADD COLUMN service_identity_id TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN service_identity_public_key TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN desired_version TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN last_heartbeat_at TEXT;`,
			`ALTER TABLE workspaces ADD COLUMN heartbeat_version TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN heartbeat_build TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN heartbeat_health_summary_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE workspaces ADD COLUMN heartbeat_projection_maintenance_summary_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE workspaces ADD COLUMN heartbeat_usage_summary_json TEXT NOT NULL DEFAULT '{}';`,
			`ALTER TABLE workspaces ADD COLUMN last_successful_backup_at TEXT;`,
		},
	},
	{
		Version: 4,
		Statements: []string{
			`CREATE TABLE IF NOT EXISTS workspace_backup_schedules (
				workspace_id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				schedule_name TEXT NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1,
				interval_seconds INTEGER NOT NULL DEFAULT 86400,
				retention_days INTEGER NOT NULL DEFAULT 30,
				next_run_at TEXT NOT NULL,
				last_run_at TEXT,
				last_status TEXT NOT NULL DEFAULT '',
				last_failure_reason TEXT,
				last_job_id TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				FOREIGN KEY(workspace_id) REFERENCES workspaces(id),
				FOREIGN KEY(organization_id) REFERENCES organizations(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_workspace_backup_schedules_due ON workspace_backup_schedules (enabled, next_run_at, workspace_id);`,
			`CREATE INDEX IF NOT EXISTS idx_workspace_backup_schedules_org ON workspace_backup_schedules (organization_id, enabled, next_run_at);`,
			`CREATE TABLE IF NOT EXISTS workspace_backup_runs (
				id TEXT PRIMARY KEY,
				organization_id TEXT NOT NULL,
				workspace_id TEXT NOT NULL,
				provisioning_job_id TEXT NOT NULL,
				schedule_name TEXT NOT NULL,
				backup_dir TEXT NOT NULL,
				retention_days INTEGER NOT NULL,
				status TEXT NOT NULL,
				requested_at TEXT NOT NULL,
				started_at TEXT,
				finished_at TEXT,
				failure_reason TEXT,
				retention_expires_at TEXT,
				pruned_at TEXT,
				prune_failure_reason TEXT,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				FOREIGN KEY(organization_id) REFERENCES organizations(id),
				FOREIGN KEY(workspace_id) REFERENCES workspaces(id),
				FOREIGN KEY(provisioning_job_id) REFERENCES provisioning_jobs(id)
			);`,
			`CREATE INDEX IF NOT EXISTS idx_workspace_backup_runs_workspace_requested ON workspace_backup_runs (workspace_id, requested_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_workspace_backup_runs_org_requested ON workspace_backup_runs (organization_id, requested_at DESC, id DESC);`,
			`CREATE INDEX IF NOT EXISTS idx_workspace_backup_runs_retention ON workspace_backup_runs (retention_expires_at, pruned_at, status);`,
		},
	},
	{
		Version: 5,
		Statements: []string{
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_organization_invites_pending_unique ON organization_invites (organization_id, email) WHERE status = 'pending';`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_provisioning_jobs_workspace_backup_running ON provisioning_jobs (workspace_id) WHERE kind = 'workspace_backup' AND status = 'running';`,
		},
	},
	{
		Version: 6,
		Statements: []string{
			`ALTER TABLE workspaces ADD COLUMN host_id TEXT NOT NULL DEFAULT 'host_local';`,
			`ALTER TABLE workspaces ADD COLUMN host_label TEXT NOT NULL DEFAULT 'Local packed host';`,
			`ALTER TABLE workspaces ADD COLUMN workspace_root TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE workspaces ADD COLUMN listen_port INTEGER NOT NULL DEFAULT 8000;`,
			`WITH ranked_workspaces AS (
				SELECT id,
					8000 + ((ROW_NUMBER() OVER (
						PARTITION BY CASE WHEN TRIM(host_id) = '' THEN 'host_local' ELSE host_id END
						ORDER BY created_at ASC, id ASC
					) - 1) * 10) AS derived_listen_port
				FROM workspaces
			)
			UPDATE workspaces
				SET host_id = CASE WHEN TRIM(host_id) = '' THEN 'host_local' ELSE host_id END,
					host_label = CASE WHEN TRIM(host_label) = '' THEN 'Local packed host' ELSE host_label END,
					workspace_root = CASE
						WHEN TRIM(workspace_root) != '' THEN workspace_root
						WHEN TRIM(deployment_root) != '' THEN deployment_root || '/workspace'
						ELSE ''
					END,
					listen_port = CASE
						WHEN listen_port > 0 AND listen_port != 8000 THEN listen_port
						ELSE COALESCE(
							(SELECT derived_listen_port FROM ranked_workspaces WHERE ranked_workspaces.id = workspaces.id),
							8000
						)
					END;`,
		},
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied := map[int]bool{}
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version ASC`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return fmt.Errorf("scan applied migration version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	for _, migration := range migrations {
		if applied[migration.Version] {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		for _, stmt := range migration.Statements {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %d: %w", migration.Version, err)
			}
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			migration.Version,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", migration.Version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", migration.Version, err)
		}
	}

	return nil
}
