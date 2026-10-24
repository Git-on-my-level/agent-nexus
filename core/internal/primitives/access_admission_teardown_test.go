package primitives

import (
	"context"
	"testing"
)

func TestCandidateCompactTeardownSelection(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public teardown control"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private teardown control"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE resource_access_compact_control SET reader=1,force_legacy=0`, `DROP TABLE resource_access_compact_edges`} {
		if _, err := ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "card", workString(public["id"])) {
		t.Fatal("missing selected capability granted a resource")
	}
	if _, err := ws.DB().Exec(`UPDATE resource_access_compact_control SET reader=0`); err != nil {
		t.Fatal(err)
	}
	if !s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "card", workString(public["id"])) {
		t.Fatal("legacy admission depends on removed optional table")
	}
	if s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "card", workString(private["id"])) {
		t.Fatal("teardown lost canonical denial")
	}
}
