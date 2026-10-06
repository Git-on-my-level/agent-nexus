package storage_test

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
	"context"
	"encoding/json"
	"testing"
)

func TestResourceAccessReadIndexMigration62(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pm.NewStore(ws.DB()); err != nil {
		t.Fatal(err)
	}
	// Reconstruct v61: bodies exist, but neither PM nor inbox bodies have a
	// write-time ledger and neither parent has its revision INSERT refresh.
	for _, q := range []string{
		`DROP TRIGGER access_pm_records_insert`, `DROP TRIGGER access_pm_records_update`, `DROP TRIGGER access_pm_records_delete`,
		`DROP TRIGGER access_derived_inbox_items_insert`, `DROP TRIGGER access_derived_inbox_items_update`, `DROP TRIGGER access_derived_inbox_items_delete`,
		`DROP TRIGGER mention_documents_revision_handles_insert`, `DROP TRIGGER mention_cards_revision_handles_insert`,
		`DELETE FROM schema_migrations WHERE version>=62`,
	} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(map[string]any{"work_ref": private["ref"]})
	if _, err = ws.DB().Exec(`INSERT INTO pm_records VALUES('decision','migration-pm','ws','owner','',1,?)`, body); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO pm_records VALUES('action','migration-child','ws','owner','',1,'{"decision_id":"migration-pm"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('migration-inbox',?,'ask','now','now',?)`, public["thread_id"], body); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, actor := range []string{"owner", "stranger"} {
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
		for _, q := range []string{`SELECT COUNT(*) FROM pm_records WHERE id='migration-child'`, `SELECT COUNT(*) FROM derived_inbox_items WHERE id='migration-inbox'`} {
			var count int
			if err = resourceaccess.NewDB(ws.DB()).QueryRowContext(scope, q).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if actor == "owner" {
				want = 1
			}
			if count != want {
				t.Fatalf("migration visibility %s count=%d want=%d", actor, count, want)
			}
		}
	}
	var count int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND name IN ('mention_documents_revision_handles_insert','mention_cards_revision_handles_insert')`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("parent repair triggers=%d err=%v", count, err)
	}
}
