package primitives

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/testsql"
)

func TestInventoryAdmissionUsesOneImmutableIndexedBatch(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	other := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := other.CreateWork(ctx, "owner", "", map[string]any{"title": "Public inventory"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := other.CreateWork(ctx, "owner", "", map[string]any{"title": "Private inventory"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	request := WithInventoryAdmission(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}))
	pin, closeRead, err := s.BeginResourceRead(request)
	if err != nil {
		t.Fatal(err)
	}
	read := func(c context.Context, want int) {
		t.Helper()
		var got int
		if err := s.db.QueryRowContext(c, `SELECT count(*) FROM cards`).Scan(&got); err != nil || got != want {
			t.Fatalf("count=%d want=%d err=%v", got, want, err)
		}
	}
	read(pin, 1)
	read(pin, 1)
	batches := 0
	for _, stmt := range counter.Statements() {
		if strings.Contains(stmt.SQL, "json_group_array(json_array(kind,id))") && strings.Contains(stmt.SQL, "resource_access_admission_owners") {
			batches++
		}
	}
	if batches != 1 {
		closeRead()
		t.Fatalf("indexed batches=%d want=1", batches)
	}
	if _, err := other.PatchThread(ctx, "owner", workString(public["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		closeRead()
		t.Fatal(err)
	}
	read(pin, 1)
	closeRead()
	next, finish, err := s.BeginResourceRead(WithInventoryAdmission(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	read(next, 0)
}
