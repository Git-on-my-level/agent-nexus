package primitives

import (
	"context"
	"encoding/json"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

func TestResourceAccessExactValueCheckMatchesFullSpellingGraph(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, revision, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Private", "handle": "private-document"}, "text", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(doc["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(card["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET authority='external',metadata_json='{"source":{"url":"https://private.invalid/item"}}' WHERE card_id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','historical-private',?,'private-document','now')`, doc["id"]); err != nil {
		t.Fatal(err)
	}
	for _, root := range []struct{ kind, id, ref string }{{"document", "legacy document", "legacy alias"}, {"Document", "mixed-case-id", "mixed-case-alias"}, {"card", "https://private.invalid/deleted", "https://private.invalid/alias"}, {"plan", "private-plan", "private-plan"}, {"external_key", "opaque:key", "opaque:key"}, {"external_key", "alternate-key-id", "alternate:ref"}, {"external_key", "plain-key-id", "plain-key-alias"}, {"filter/actors", "private-profile", "private-profile"}} {
		if _, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES(?,?,?,'owner')`, root.kind, root.id, root.ref); err != nil {
			t.Fatal(err)
		}
	}
	old := `SELECT EXISTS (SELECT 1 FROM json_each(?) j JOIN _anx_denied_atoms d ON j.value=d.ref COLLATE NOCASE)`
	scope := AccessScope{ActorID: "stranger"}
	for _, atom := range []string{anyStringValue(doc["id"]), "document:" + anyStringValue(doc["id"]), "document:PRIVATE-DOCUMENT", "doc:historical-private", anyStringValue(revision["revision_id"]), "document_revision:private-document-r1", "document:legacy document", "document:legacy alias", "legacy document", "legacy alias", "https://private.invalid/item", "card:https://private.invalid/item", "https://private.invalid/deleted", "card:https://private.invalid/deleted", "https://private.invalid/alias", "card:https://private.invalid/alias", "plan:private-plan", "private-plan", "opaque:key", "external_key:opaque:key", "alternate-key-id", "alternate:ref", "ALTERNATE:REF", "plain-key-id", "plain-key-alias", "PLAIN-KEY-ALIAS", "external_key:alternate:ref", "filter/actors:private-profile", "private-profile", "document:public", "document:mixed-case-id", "document:mixed-case-alias"} {
		t.Run(atom, func(t *testing.T) {
			raw, _ := json.Marshal([]string{atom})
			atoms := string(resourceaccess.ContentReferenceAtomsJSON(string(raw), "structured"))
			var want, got bool
			if err := ws.DB().QueryRow(`WITH RECURSIVE `+accessCTEs(scope, old)+` `+old, atoms).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if (atom == "alternate:ref" || atom == "plain-key-alias") && !want {
				t.Fatal("alternate tombstone ref must be denied")
			}
			query := exactValueCheckSQL()
			if err := ws.DB().QueryRow(`WITH RECURSIVE `+accessCTEs(scope, query)+` `+query, atoms, atoms, atoms, atoms, atoms, atoms).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("exact check=%v full spelling check=%v", got, want)
			}
			request := WithRequestAccessScope(ctx, scope)
			if err := s.db.QueryRowContext(request, query, atoms, atoms, atoms, atoms, atoms, atoms).Scan(&got); err != nil || got != want {
				t.Fatalf("request exact check=%v want=%v err=%v", got, want, err)
			}
			pin, close, err := s.BeginOverviewRead(request)
			if err != nil {
				t.Fatal(err)
			}
			defer close()
			if _, ok := pin.Value(pinnedDenialKey{}).(pinnedDenial); !ok {
				t.Fatal("read was not admitted")
			}
			if err := s.db.QueryRowContext(pin, query, atoms, atoms, atoms, atoms, atoms, atoms).Scan(&got); err != nil || got != want {
				t.Fatalf("admitted exact check=%v want=%v err=%v", got, want, err)
			}
		})
	}
}
