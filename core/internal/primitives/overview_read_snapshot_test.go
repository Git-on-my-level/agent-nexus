package primitives

import (
	"context"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestOverviewReadSnapshotAdmissionAndIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "snapshot control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := doc["id"].(string)
	thread := doc["thread_id"].(string)
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	assert := func(c context.Context, want bool) {
		t.Helper()
		for _, read := range []context.Context{c, context.WithValue(c, summaryPointReadKey{}, true)} {
			var n int
			if err := s.db.QueryRowContext(read, `SELECT count(*) FROM documents WHERE id=?`, id).Scan(&n); err != nil || (n == 1) != want {
				t.Fatalf("visible=%d want=%v err=%v", n, want, err)
			}
		}
	}
	assert(request, true) // Capture an empty closure before revocation.
	setOwner := func(owner string) {
		t.Helper()
		if _, err := s.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
	}
	setOwner("owner")
	pin, close, err := s.BeginOverviewRead(request)
	if err != nil {
		t.Fatal(err)
	}
	assert(pin, false) // Admission repairs the stale empty closure.
	setOwner("")       // WAL writer proceeds while the read snapshot is held.
	assert(pin, false) // Same snapshot remains internally consistent.
	close()
	pin, close, err = s.BeginOverviewRead(request)
	if err != nil {
		t.Fatal(err)
	}
	assert(pin, true) // Admission observes the grant despite stale request state.
	close()
	setOwner("owner")
	privileged := WithRequestAccessScope(ctx, AccessScope{ActorID: "owner"})
	pin, close, err = s.BeginOverviewRead(privileged)
	if err != nil {
		t.Fatal(err)
	}
	assert(pin, true)
	assert(WithRequestAccessScope(pin, AccessScope{ActorID: "stranger"}), false)
	assert(WithAccessScope(pin, AccessScope{ActorID: "stranger"}), false)
	setOwner("other")
	// The proof belongs to the transaction handle, not merely the context.
	pin = context.WithValue(pin, summaryPointReadKey{}, true)
	policy, _ := resourceaccess.PolicyFrom(pin)
	query, args := policy.ReadOnDB(pin, ws.DB(), `SELECT count(*) FROM documents WHERE id=?`, []any{id})
	var outside int
	if err := ws.DB().QueryRowContext(ctx, query, args...).Scan(&outside); err != nil || outside != 0 {
		t.Fatalf("pinned authority escaped its transaction: count=%d err=%v", outside, err)
	}
	close()
	// A missing epoch disables the optimization and retains canonical fallback.
	if _, err := ws.DB().Exec(`DELETE FROM resource_access_epoch`); err != nil {
		t.Fatal(err)
	}
	pin, close, err = s.BeginOverviewRead(request)
	if err != nil {
		t.Fatal(err)
	}
	assert(pin, false)
	close()
}

func TestOverviewReadSnapshotSingleConnectionAndCancellation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.DB().SetMaxOpenConns(1)
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	pin, close, err := s.BeginOverviewRead(request)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(pin, `SELECT count(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	close()
	canceled, cancel := context.WithCancel(request)
	cancel()
	if _, close, err := s.BeginOverviewRead(canceled); err == nil {
		close()
		t.Fatal("canceled admission succeeded")
	}
	if err := s.db.QueryRowContext(request, `SELECT count(*) FROM cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}
