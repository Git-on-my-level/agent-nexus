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
