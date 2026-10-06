package primitives

import (
	"context"
	"errors"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestResourceAccessLegacyProseDocumentIDs(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, id := range []string{"[]", "{}", `"quoted"`, "(parentheses)", "two words"} {
		// The prose precedes its referenced resource: indexing must not depend
		// on which resource IDs existed when the content was written.
		copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "CopiedEvidence"}, "CopiedEvidence copied from **document:"+id+"**", "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		source, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": id, "title": "source"}, "source", "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "owner", anyStringValue(source["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{"owner", "stranger", "unauthorized-agent"} {
			scope := WithAccessScope(ctx, AccessScope{ActorID: actor})
			if got := s.CanAccessResource(scope, "document", anyStringValue(copy["id"])); got != (actor == "owner") {
				t.Fatalf("%s sees prose for %q: %v", actor, id, got)
			}
			if err := s.CheckResourceValues(scope, map[string]any{"text": "copied from document:" + id}); (actor != "owner") != errors.Is(err, ErrNotFound) {
				t.Fatalf("prose mutation %s/%q: %v", actor, id, err)
			}
			results, _, err := s.SearchDocuments(scope, DocumentSearchFilter{Query: "CopiedEvidence"})
			if err != nil {
				t.Fatal(err)
			}
			if actor != "owner" && len(results) != 0 {
				t.Fatalf("prose search leaked: %#v", results)
			}
		}
	}
}

func TestResourceAccessProseGraphIdentityForms(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "[]", "title": "private"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id, handle := anyStringValue(doc["id"]), anyStringValue(doc["handle"])
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(doc["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','historical-doc',?,?,'now')`, id, handle); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,'retained-doc','owner')`, id); err != nil {
		t.Fatal(err)
	}
	var revisionID string
	if err = ws.DB().QueryRowContext(ctx, `SELECT revision_id FROM document_revisions WHERE document_id=? LIMIT 1`, id).Scan(&revisionID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"document:" + id, "document:" + handle, "document:historical-doc", "document:retained-doc", "document_revision:" + revisionID, "document_revision:" + handle + "-r1", "document:public-unrelated"} {
		t.Run(ref, func(t *testing.T) {
			artifact, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{}}, "See **"+ref+"**", "text")
			if err != nil {
				t.Fatal(err)
			}
			artifactID, private := anyStringValue(artifact["id"]), ref != "document:public-unrelated"
			for _, actor := range []string{"stranger", "unauthorized-agent", "owner"} {
				got := s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: actor}), "artifact", artifactID)
				if want := actor == "owner" || !private; got != want {
					t.Fatalf("%s access=%v, want %v", actor, got, want)
				}
			}
			var count int
			if err = ws.DB().QueryRowContext(ctx, `WITH RECURSIVE `+privateOwnershipGraph()+` SELECT COUNT(*) FROM _anx_private WHERE kind='artifact' AND id=? AND owner='owner'`, artifactID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count > 0) != private {
				t.Fatalf("owner attribution count=%d, private=%v", count, private)
			}
		})
	}
}
