package storage_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestAttentionMigrationPreservesLegacyVisitsAndReapplies(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE overview_visits`,
		`CREATE TABLE overview_visits(principal_id TEXT PRIMARY KEY,visited_at TEXT NOT NULL,snapshot_json TEXT NOT NULL)`,
		`INSERT INTO overview_visits VALUES('human:alice','2026-10-20T12:00:00.000000000Z','{"card:old":{"health":"moving","steps":{}}}')`,
		`DELETE FROM schema_migrations WHERE version=114`,
	} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var scope, at, raw string
	if err = ws.DB().QueryRow(`SELECT scope_ref,visited_at,snapshot_json FROM overview_visits WHERE principal_id='human:alice'`).Scan(&scope, &at, &raw); err != nil {
		t.Fatal(err)
	}
	if scope != "builtin:overview" || at != "2026-10-20T12:00:00.000000000Z" || raw != `{"card:old":{"health":"moving","steps":{}}}` {
		t.Fatal(scope, at, raw)
	}
	if _, err = ws.DB().Exec(`INSERT INTO overview_visits VALUES('human:alice','document:first','2026-10-21T12:00:00.000000000Z','{}')`); err != nil {
		t.Fatal(err)
	}
}
