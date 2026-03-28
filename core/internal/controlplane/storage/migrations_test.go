package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestMigrationVersion6BackfillsUniqueListenPorts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "control-plane.sqlite")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	if err := applyMigrationsUpTo(ctx, db, 5); err != nil {
		t.Fatalf("apply migrations up to v5: %v", err)
	}

	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `INSERT INTO organizations(id, slug, display_name, plan_tier, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"org_1",
		"acme",
		"Acme",
		"team",
		"active",
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert organization: %v", err)
	}

	workspaces := []struct {
		id             string
		slug           string
		deploymentRoot string
		createdAt      time.Time
	}{
		{
			id:             "ws_1",
			slug:           "ops",
			deploymentRoot: "/srv/oar/deployments/org_1/ws_1",
			createdAt:      now,
		},
		{
			id:             "ws_2",
			slug:           "eng",
			deploymentRoot: "/srv/oar/deployments/org_1/ws_2",
			createdAt:      now.Add(time.Minute),
		},
		{
			id:             "ws_3",
			slug:           "sales",
			deploymentRoot: "/srv/oar/deployments/org_1/ws_3",
			createdAt:      now.Add(2 * time.Minute),
		},
	}

	for _, workspace := range workspaces {
		if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(
			id, organization_id, slug, display_name, status, region, workspace_tier, workspace_path, base_url,
			public_origin, core_origin, deployment_root, instance_id, desired_state, quota_config_ref, quota_envelope_ref,
			deployed_version, routing_manifest_path, routing_manifest_json, service_identity_id, service_identity_public_key,
			desired_version, last_heartbeat_at, heartbeat_version, heartbeat_build, heartbeat_health_summary_json,
			heartbeat_projection_maintenance_summary_json, heartbeat_usage_summary_json, last_successful_backup_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			workspace.id,
			"org_1",
			workspace.slug,
			workspace.slug,
			"ready",
			"us-central1",
			"standard",
			"/"+workspace.slug,
			"https://app.example.test/"+workspace.slug,
			"",
			"",
			workspace.deploymentRoot,
			workspace.id,
			"ready",
			"plan:team",
			"organization:org_1:quota",
			"",
			"",
			"{}",
			"",
			"",
			"",
			nil,
			"",
			"",
			"{}",
			"{}",
			"{}",
			nil,
			workspace.createdAt.Format(time.RFC3339Nano),
			workspace.createdAt.Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("insert workspace %s: %v", workspace.id, err)
		}
	}

	if err := applyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, host_id, host_label, workspace_root, listen_port
		FROM workspaces
		ORDER BY created_at ASC, id ASC`)
	if err != nil {
		t.Fatalf("query migrated workspaces: %v", err)
	}
	defer rows.Close()

	expectedPorts := []int{8000, 8010, 8020}
	index := 0
	for rows.Next() {
		var id string
		var hostID string
		var hostLabel string
		var workspaceRoot string
		var listenPort int
		if err := rows.Scan(&id, &hostID, &hostLabel, &workspaceRoot, &listenPort); err != nil {
			t.Fatalf("scan migrated workspace: %v", err)
		}
		if hostID != "host_local" {
			t.Fatalf("workspace %s host_id = %q, want host_local", id, hostID)
		}
		if hostLabel != "Local packed host" {
			t.Fatalf("workspace %s host_label = %q, want Local packed host", id, hostLabel)
		}
		if want := workspaces[index].deploymentRoot + "/workspace"; workspaceRoot != want {
			t.Fatalf("workspace %s workspace_root = %q, want %q", id, workspaceRoot, want)
		}
		if listenPort != expectedPorts[index] {
			t.Fatalf("workspace %s listen_port = %d, want %d", id, listenPort, expectedPorts[index])
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate migrated workspaces: %v", err)
	}
	if index != len(workspaces) {
		t.Fatalf("migrated %d workspaces, want %d", index, len(workspaces))
	}
}

func TestMigrationVersion8BackfillsOrganizationBilling(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "control-plane.sqlite")
	db, err := sql.Open("sqlite", sqliteDSN(dbPath))
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer db.Close()

	if err := applyMigrationsUpTo(ctx, db, 7); err != nil {
		t.Fatalf("apply migrations up to v7: %v", err)
	}

	now := time.Date(2026, 3, 27, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `INSERT INTO organizations(id, slug, display_name, plan_tier, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"org_billing",
		"billing-org",
		"Billing Org",
		"starter",
		"active",
		now,
		now,
	); err != nil {
		t.Fatalf("insert organization: %v", err)
	}

	if err := applyMigrations(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	var provider string
	var billingStatus string
	var stripeSubscriptionStatus string
	if err := db.QueryRowContext(ctx, `SELECT provider, billing_status, stripe_subscription_status
		FROM organization_billing WHERE organization_id = ?`, "org_billing").Scan(&provider, &billingStatus, &stripeSubscriptionStatus); err != nil {
		t.Fatalf("load organization billing: %v", err)
	}
	if provider != "stripe" {
		t.Fatalf("provider = %q, want stripe", provider)
	}
	if billingStatus != "free" {
		t.Fatalf("billing_status = %q, want free", billingStatus)
	}
	if stripeSubscriptionStatus != "not_started" {
		t.Fatalf("stripe_subscription_status = %q, want not_started", stripeSubscriptionStatus)
	}
}

func applyMigrationsUpTo(ctx context.Context, db *sql.DB, maxVersion int) error {
	if _, err := db.ExecContext(ctx, createMigrationsTableSQL); err != nil {
		return err
	}

	appliedAt := time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, migration := range migrations {
		if migration.Version > maxVersion {
			break
		}
		for _, statement := range migration.Statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, migration.Version, appliedAt); err != nil {
			return err
		}
	}
	return nil
}
