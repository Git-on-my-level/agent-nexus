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

func TestResourceAccessProfileMigration63(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "profile source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// Reconstruct deployed v62: profile rows exist without derived provenance.
	for _, source := range resourceaccess.FilterOwnershipSources() {
		for _, op := range []string{"insert", "update", "delete"} {
			if _, err = ws.DB().Exec("DROP TRIGGER access_" + source.Table + "_" + op); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, q := range []string{`DELETE FROM resource_access_edges WHERE source_kind LIKE 'filter/%'`, `DELETE FROM schema_migrations WHERE version>=63`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = ws.DB().Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('migration-host','migration-host',?,'user','machine','[]','now')`, "See **"+private["ref"].(string)+"**"); err != nil {
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
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	visible := func(want int) {
		t.Helper()
		var count int
		if err = resourceaccess.NewDB(ws.DB()).QueryRowContext(scope, `SELECT COUNT(*) FROM hosts WHERE id='migration-host'`).Scan(&count); err != nil || count != want {
			t.Fatalf("profile count=%d want=%d err=%v", count, want, err)
		}
	}
	visible(0)
	// Restore privacy, revoke it again and delete the profile; the same request
	// must see each trigger change through the statement epoch fallback.
	for _, value := range []string{"public", private["ref"].(string), "public"} {
		if _, err = ws.DB().Exec(`UPDATE hosts SET display_name=? WHERE id='migration-host'`, value); err != nil {
			t.Fatal(err)
		}
		want := 0
		if value == "public" {
			want = 1
		}
		visible(want)
	}
	if _, err = ws.DB().Exec(`DELETE FROM hosts WHERE id='migration-host'`); err != nil {
		t.Fatal(err)
	}
	var edges int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='filter/hosts' AND source_id='migration-host'`).Scan(&edges); err != nil || edges != 0 {
		t.Fatalf("deleted profile edges=%d err=%v", edges, err)
	}
	// A profile can precede its private target/alias. Existing mention-index
	// identity triggers must resolve the already-written ancillary source.
	if _, err = ws.DB().Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('migration-host','migration-host','See **card:late-profile-alias**','user','machine','[]','now')`); err != nil {
		t.Fatal(err)
	}
	visible(1)
	if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','late-profile-alias',?,?,'now')`, private["id"], private["handle"]); err != nil {
		t.Fatal(err)
	}
	visible(0)
}
