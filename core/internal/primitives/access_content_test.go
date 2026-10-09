package primitives

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResourceAccessBlobContentAndDocumentSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private content target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	ref := anyStringValue(private["ref"])
	content := map[string]any{"report": map[string]any{"ref": ref, "title": "BlobSecret"}}
	a, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{}}, content, "structured")
	if err != nil {
		t.Fatal(err)
	}
	pair, _, err := s.CreateArtifactAndEvent(ctx, "owner", map[string]any{"kind": "note", "refs": []string{}}, content, "structured", map[string]any{"type": "artifact_recorded", "refs": []string{}, "payload": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := s.CreateArtifactAttachment(ctx, "owner", map[string]any{"refs": []string{}}, "text/markdown", "note.md", strings.NewReader("BlobSecret [evidence]("+ref+")\xff"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "public report"}, content, "structured", []string{})
	if err != nil {
		t.Fatal(err)
	}
	public, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "public search document"}, "public text", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	comment := "CommentSecret [evidence](" + ref + ")"
	if _, err = s.CreateDocumentComment(ctx, "owner", anyStringValue(public["id"]), comment, ""); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"stranger", "unauthorized-agent", "owner"} {
		scope := WithAccessScope(ctx, AccessScope{ActorID: actor})
		for _, artifact := range []map[string]any{a, pair, attachment} {
			_, _, err := s.GetArtifactContent(scope, anyStringValue(artifact["id"]))
			if actor == "owner" && err != nil {
				t.Errorf("owner content: %v", err)
			}
			if actor != "owner" && !errors.Is(err, ErrNotFound) {
				t.Errorf("%s reads private content: %v", actor, err)
			}
		}
		if got := s.CanAccessResource(scope, "document", anyStringValue(doc["id"])); got != (actor == "owner") {
			t.Errorf("%s report access=%v", actor, got)
		}
		matches, _, err := s.SearchDocuments(scope, DocumentSearchFilter{Query: "CommentSecret"})
		if err != nil {
			t.Fatal(err)
		}
		if (len(matches) > 0) != (actor == "owner") {
			t.Errorf("%s hidden comment search matched %d", actor, len(matches))
		}
	}
	// A preview56 database has no content manifest. It must be hidden even from
	// the owner until the backend-aware backfill publishes the derived edges.
	if _, err = ws.DB().Exec(`UPDATE artifacts SET content_refs_json=NULL WHERE id=?`, a["id"]); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "artifact", anyStringValue(a["id"])) {
		t.Fatal("unindexed blob was exposed")
	}

	var originalHash string
	if err = ws.DB().QueryRow(`SELECT content_hash FROM artifacts WHERE id=?`, a["id"]).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE artifacts SET content_hash=? WHERE id=?`, strings.Repeat("f", 64), a["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE artifacts SET content_refs_json=NULL WHERE id=?`, pair["id"]); err != nil {
		t.Fatal(err)
	}
	if err = s.BackfillArtifactAccess(ctx); err == nil {
		t.Fatal("unavailable blob silently indexed")
	}
	var pending int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM artifacts WHERE id=? AND content_refs_json IS NULL`, pair["id"]).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("unavailable blob starved later backfill: %d %v", pending, err)
	}
	if _, err = ws.DB().Exec(`UPDATE artifacts SET content_hash=? WHERE id=?`, originalHash, a["id"]); err != nil {
		t.Fatal(err)
	}
	if err = s.BackfillArtifactAccess(ctx); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), "artifact", anyStringValue(a["id"])) {
		t.Fatal("backfilled blob lost privacy")
	}
	if !s.CanAccessResource(WithAccessScope(ctx, AccessScope{ActorID: "owner"}), "artifact", anyStringValue(a["id"])) {
		t.Fatal("owner lost backfilled blob")
	}
}
