package primitives

import (
	"context"
	"testing"
)

// Migration 150 makes the compact ledger a required capability. Dropping it is
// a schema error, not a supported teardown; admission must fail closed rather
// than silently granting access through a missing table.
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
	if _, err := ws.DB().Exec(`DROP TABLE resource_access_compact_edges`); err != nil {
		t.Fatal(err)
	}
	// A public resource needs no ledger probe and remains visible. A private
	// resource must never become visible because the required table is missing.
	if !s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "card", workString(public["id"])) {
		t.Fatal("missing compact ledger denied a public resource")
	}
	if s.CanAccessResource(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "card", workString(private["id"])) {
		t.Fatal("missing required compact ledger granted a private resource")
	}
}
