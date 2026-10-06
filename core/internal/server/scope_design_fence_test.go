package server

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

// Experiment only: exercise the unchanged schema-63 initializer, not a mock
// version-check implementation. The proposed new loader would read the renamed
// ledger explicitly. This does not test a full future migration or server rollout.
func TestScopeDesignDowngradeFence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Ordinary unknown migration versions do NOT stop the released initializer.
	if _, err = w.DB().Exec(`INSERT INTO schema_migrations(version,applied_at) VALUES(10000,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	w.Close()
	w, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatalf("baseline changed; unknown-version control failed: %v", err)
	}
	var before int
	if err = w.DB().QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	tx, err := w.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE schema_migrations RENAME TO scope_schema_migrations`,
		`CREATE VIEW schema_migrations AS SELECT version,applied_at FROM scope_schema_migrations WHERE anx_requires_scope_format_v1()`,
	} {
		if _, err = tx.Exec(q); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = w.DB().QueryRow(`SELECT count(*) FROM scope_schema_migrations`).Scan(&n); err != nil || n != before {
		t.Fatalf("canonical ledger lost: n=%d err=%v", n, err)
	}
	w.Close()
	for i := 0; i < 2; i++ {
		opened, err := storage.InitializeWorkspace(ctx, root)
		if opened != nil {
			opened.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "anx_requires_scope_format_v1") {
			t.Fatalf("old initializer must refuse fence on every open: %v", err)
		}
		t.Logf("baseline initializer correctly refused: %v", err)
	}
}
