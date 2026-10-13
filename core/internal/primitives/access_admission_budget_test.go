package primitives

import (
	"context"
	"testing"

	"agent-nexus-core/internal/testsql"
)

// Epoch validation and stale closure capture must share one SQL admission.
// A cheap standalone probe adds a second round trip after workspace writes.
func TestAccessAdmissionQueryBudgetAcrossEpochChanges(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Admission budget control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := func() context.Context { return WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}) }
	read := func(c context.Context, want int) {
		t.Helper()
		var visible int
		if err := s.db.QueryRowContext(c, `SELECT count(*) FROM documents WHERE id=?`, doc["id"]).Scan(&visible); err != nil || visible != want {
			t.Fatalf("visible=%d want=%d error=%v", visible, want, err)
		}
	}
	for _, phase := range []string{"cold", "warm", "stale"} {
		if phase == "stale" {
			if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
		}
		counter.Reset()
		want := 1
		if phase == "stale" {
			want = 0
		}
		read(request(), want)
		if got := counter.Count(); got != 2 {
			t.Fatalf("%s admission+read=%d queries; want 2", phase, got)
		}
	}
	// Pinning an already captured request also costs one admission statement,
	// including after a concurrent authorization change. The pinned consuming
	// read must use the current visibility proven by that same statement.
	c := request()
	read(c, 0)
	for _, phase := range []string{"current", "invalidated"} {
		want := 0
		if phase == "invalidated" {
			if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": ""}, nil); err != nil {
				t.Fatal(err)
			}
			want = 1
		}
		counter.Reset()
		pin, finish, err := s.BeginResourceRead(c)
		if err != nil {
			t.Fatal(err)
		}
		admission := counter.Count()
		read(pin, want)
		finish()
		if admission != 1 || counter.Count() != 2 {
			t.Fatalf("%s pin admission=%d total=%d; want 1+1", phase, admission, counter.Count())
		}
	}
}
