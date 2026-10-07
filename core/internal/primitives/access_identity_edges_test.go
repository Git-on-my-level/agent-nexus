package primitives

import (
	"context"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestResourceAccessIndexedIdentityReferenceEdges(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, revision, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Private source", "handle": "private-source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	for _, target := range []struct{ kind, ref string }{
		{"document", anyStringValue(doc["id"])},
		{"document", "private-source"},
		{"document_revision", anyStringValue(revision["revision_id"])},
		{"document_revision", "private-source-r1"},
		{"document", "historical-source"},
		{"document", ""},
	} {
		t.Run(target.kind+":"+target.ref, func(t *testing.T) {
			copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Public until referenced"}, "public", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			if !s.CanAccessResource(request, "document", anyStringValue(copy["id"])) {
				t.Fatal("unlinked copy hidden")
			}
			// Import a navigational edge directly, so the prose mention index
			// cannot mask a missing handle/revision arm of the closure.
			if _, err = ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'document',?, ?,?,'ref','now','{}')`, "edge-"+anyStringValue(copy["id"]), copy["id"], target.kind, target.ref); err != nil {
				t.Fatal(err)
			}
			if target.ref == "historical-source" {
				if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','historical-source',?,'private-source','now')`, doc["id"]); err != nil {
					t.Fatal(err)
				}
			}
			if target.ref == "" {
				if _, err = ws.DB().Exec(`UPDATE documents SET handle='' WHERE id=?`, doc["id"]); err != nil {
					t.Fatal(err)
				}
			}
			if s.CanAccessResource(request, "document", anyStringValue(copy["id"])) {
				t.Fatal("reference edge escaped inherited privacy after epoch change")
			}
			if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "document", anyStringValue(copy["id"])) {
				t.Fatal("owner lost inherited access")
			}
		})
	}
}

func TestResourceAccessEmptyImportedIdentityEdges(t *testing.T) {
	for _, spelling := range []string{"handle", "alias", "tombstone"} {
		for _, edge := range []string{"navigation", "exact"} {
			t.Run(spelling+"/"+edge, func(t *testing.T) {
				ctx := context.Background()
				ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer ws.Close()
				s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
				doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Private", "handle": "private-source"}, "source", "text", nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
					t.Fatal(err)
				}
				copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Imported reference"}, "public", "text", nil)
				if err != nil {
					t.Fatal(err)
				}
				request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
				if !s.CanAccessResource(request, "document", anyStringValue(copy["id"])) {
					t.Fatal("unlinked copy hidden")
				}
				switch spelling {
				case "handle":
					_, err = ws.DB().Exec(`UPDATE documents SET handle='' WHERE id=?`, doc["id"])
				case "alias":
					_, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','',?,'private-source','now')`, doc["id"])
				case "tombstone":
					_, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,'','owner')`, doc["id"])
				}
				if err != nil {
					t.Fatal(err)
				}
				if edge == "navigation" {
					_, err = ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('imported','document',?,'document','','ref','now','{}')`, copy["id"])
				} else {
					_, err = ws.DB().Exec(`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('document',?,'document:')`, copy["id"])
				}
				if err != nil {
					t.Fatal(err)
				}
				if s.CanAccessResource(request, "document", anyStringValue(copy["id"])) {
					t.Fatal("empty imported reference escaped inherited privacy")
				}
				if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "document", anyStringValue(copy["id"])) {
					t.Fatal("owner lost access")
				}
			})
		}
	}
}
