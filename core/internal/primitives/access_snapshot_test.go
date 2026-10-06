package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"testing"
)

func TestResourceAccessReadCacheSeparatesDatabaseAndPMScope(t *testing.T) {
	ctx := context.Background()
	var captured *denialSnapshot
	for i := 0; i < 2; i++ {
		ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
		doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "cache-control", "title": "control"}, "body", "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
		if s.CanAccessResource(request, "document", "cache-control") {
			t.Fatal("private document visible")
		}
		snapshot := denialSnapshotFrom(request)
		if snapshot == nil || snapshot == captured {
			t.Fatal("database identities shared a closure")
		}
		captured = snapshot
		pmRequest := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader", PMActorID: "reader"})
		if !s.CanAccessResource(pmRequest, "document", "cache-control") || denialSnapshotFrom(pmRequest) == snapshot {
			t.Fatal("PM authority shared another scope's closure")
		}
	}
}

func TestResourceAccessRequestSnapshotInvalidatesInsideStatement(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "[]", "title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	assertVisible := func(want bool) {
		t.Helper()
		_, _, err := s.GetDocument(request, "[]")
		if (err == nil) != want {
			t.Fatalf("visible=%v want=%v err=%v", err == nil, want, err)
		}
	}
	assertVisible(true)
	snapshot := denialSnapshotFrom(request)
	if snapshot == nil {
		t.Fatal("request did not capture snapshot")
	}
	otherRequest := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(otherRequest, "document", "[]") || denialSnapshotFrom(otherRequest) != snapshot {
		t.Fatal("unchanged database, principal and epoch did not reuse the bounded read closure")
	}
	for _, owner := range []string{"owner", "", "owner"} {
		if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		assertVisible(owner == "")
		freshRequest := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
		if s.CanAccessResource(freshRequest, "document", "[]") != (owner == "") || denialSnapshotFrom(freshRequest) == snapshot {
			t.Fatal("new request reused a closure from an earlier authorization epoch")
		}
		if denialSnapshotFrom(request) != snapshot {
			t.Fatal("expected statement epoch fallback, not replacement of immutable snapshot")
		}
	}
	for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger", PMActorID: "stranger"}} {
		nested := WithAccessScope(request, scope)
		if denialSnapshotFrom(nested) != nil {
			t.Fatal("rebound scope retained another principal's snapshot")
		}
		if !s.CanAccessResource(nested, "document", "[]") {
			t.Fatalf("nested privileged scope lost access: %+v", scope)
		}
	}
	privileged := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger", PMActorID: "stranger"})
	if !s.CanAccessResource(privileged, "document", "[]") {
		t.Fatal("selected PM lost access")
	}
	if s.CanAccessResource(WithAccessScope(privileged, AccessScope{ActorID: "stranger"}), "document", "[]") {
		t.Fatal("PM scope downgrade reused privileged cache")
	}
	// Empty encoded reference arrays must not become the scalar document ID [].
	if _, err := s.CreateArtifact(request, "stranger", map[string]any{"kind": "note", "refs": []string{}}, "public artifact", "text"); err != nil {
		t.Fatalf("public write with empty JSON refs=%v", err)
	}
	if _, err := s.CreateArtifact(request, "stranger", map[string]any{"kind": "note", "refs": []string{}}, "", "text"); err != nil {
		t.Fatalf("public empty internal manifest=%v", err)
	}
	// Transactions do not consume request caches. Their own writes must be seen by
	// their following reads, and write argument checks cannot reuse stale answers.
	tx, err := s.db.BeginTx(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(request, `SELECT count(*) FROM documents WHERE id='[]'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("transaction visibility=%d err=%v", count, err)
	}
	if _, err := tx.ExecContext(request, `INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,refs_json,content_refs_json) VALUES('forbidden','note','now','stranger','text','none',json_array(?),'[]')`, "[]"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("JSON scalar builder hid a private ID: %v", err)
	}
	if _, err := tx.ExecContext(request, `UPDATE documents SET title=? WHERE id=?`, "changed", "[]"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("transaction write guard=%v", err)
	}
}

func TestResourceAccessRequestSnapshotMissingEpochFallsBack(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(request, "document", doc["id"].(string)) {
		t.Fatal("public source missing")
	}
	if _, err := ws.DB().ExecContext(ctx, `DELETE FROM resource_access_epoch`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(request, "document", doc["id"].(string)) {
		t.Fatal("missing cache sentinel exposed private resource")
	}
	freshRequest := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if s.CanAccessResource(freshRequest, "document", doc["id"].(string)) || denialSnapshotFrom(freshRequest) != nil {
		t.Fatal("missing epoch allowed a shared cache hit on a new request")
	}
}

func TestResourceAccessRequestSnapshotSeesLateAliasAndProse(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "copy"}, "See _document:late_alias_", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(request, "document", copy["id"].(string)) {
		t.Fatal("unresolved reference should remain visible")
	}
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','late_alias',?,?,'now')`, doc["id"], doc["handle"]); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(request, "document", copy["id"].(string)) {
		t.Fatal("late alias escaped cached visibility")
	}
}
