package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"testing"
)

func TestResourceAccessRevisionBeforeParent(t *testing.T) {
	for _, kind := range []string{"document", "card"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			artifact, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{}}, "public", "text")
			if err != nil {
				t.Fatal(err)
			}
			// Import the revision and its citation before the canonical parent exists.
			_, err = ws.DB().Exec(`INSERT INTO `+kind+`_revisions(revision_id,`+kind+`_id,revision_number,artifact_id,created_at,created_by) VALUES('import-r7','late-parent',7,?,'now','owner')`, artifact["id"])
			if err != nil {
				t.Fatal(err)
			}
			copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "copy"}, "See **"+kind+"_revision:late-parent-r7**", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
			if !s.CanAccessResource(scope, "document", copy["id"].(string)) {
				t.Fatal("unresolved citation hidden")
			}
			var thread string
			if kind == "document" {
				parent, _, e := s.CreateDocument(ctx, "owner", map[string]any{"id": "late-parent", "handle": "late-parent", "title": "private"}, "source", "text", nil)
				if e != nil {
					t.Fatal(e)
				}
				thread = parent["thread_id"].(string)
			} else {
				parent, e := s.CreateWork(ctx, "owner", "", map[string]any{"id": "work-source", "title": "private"})
				if e != nil {
					t.Fatal(e)
				}
				thread = parent["thread_id"].(string)
				if _, err = ws.DB().Exec(`INSERT INTO cards(id,title,created_at,created_by,updated_at,updated_by,thread_id,handle,board_id,head_revision_id) SELECT 'late-parent',title,created_at,created_by,updated_at,updated_by,thread_id,'late-parent',board_id,head_revision_id FROM cards WHERE id=?`, parent["id"]); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			if s.CanAccessResource(scope, "document", copy["id"].(string)) {
				t.Fatal("late private parent missed older revision prose")
			}
			if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "document", copy["id"].(string)) {
				t.Fatal("owner lost inherited access")
			}
			if _, err = ws.DB().Exec(`UPDATE ` + kind + `_revisions SET revision_number=8 WHERE revision_id='import-r7'`); err != nil {
				t.Fatal(err)
			}
			var count int
			if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM resource_access_identities WHERE origin=? AND origin_id='import-r7' AND ref='late-parent-r8'`, kind+"_revisions_handle").Scan(&count); err != nil || count != 1 {
				t.Fatalf("revision refresh count=%d err=%v", count, err)
			}
		})
	}
}

func TestResourceAccessInboxImportedParentAliases(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{private["ref"].(string)}, "payload": map[string]any{"text": "private"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ kind, column, id string }{{"thread", "thread_id", private["thread_id"].(string)}, {"card", "source_card_id", private["id"].(string)}, {"event", "source_event_id", event["id"].(string)}} {
		alias := "late-inbox-" + entry.kind
		q := `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json,` + entry.column + `) VALUES(?,'unrelated-thread','ask','now','now','{}',?)`
		if entry.column == "thread_id" {
			q = `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES(?,?,'ask','now','now','{}')`
		}
		if _, err = ws.DB().Exec(q, alias, alias); err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES(?,?,?,'fixture','now')`, entry.kind, alias, entry.id); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{"stranger", "owner"} {
			var count int
			if err = s.db.QueryRowContext(WithRequestAccessScope(ctx, AccessScope{ActorID: actor}), `SELECT COUNT(*) FROM derived_inbox_items WHERE id=?`, alias).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if actor == "owner" {
				want = 1
			}
			if count != want {
				t.Fatalf("%s inbox alias %s count=%d want=%d", actor, entry.kind, count, want)
			}
		}
	}
}
