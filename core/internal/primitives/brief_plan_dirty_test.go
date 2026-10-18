package primitives

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestBriefPlanReferencersIgnoreUnchangedObservation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	url := "https://tracker.example.test/work/42"
	child, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Source", "source": map[string]any{"authority": "tracker", "connection_id": "fixture", "native_id": "42", "url": url}})
	if err != nil {
		t.Fatal(err)
	}
	// Cover canonical card IDs, handles and evidence aliases, with multiple
	// initiatives sharing each spelling.
	ids := []string{}
	for i, ref := range []string{"card:" + anyStringValue(child["id"]), anyStringValue(child["ref"]), url, "tracker:42", "42", url} {
		parent, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": fmt.Sprintf("Initiative %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		id := anyStringValue(parent["id"])
		ids = append(ids, id)
		if err = s.SetCardPlan(ctx, "writer", id, anyStringValue(parent["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", Ref: ref}}}); err != nil {
			t.Fatal(err)
		}
	}
	baseline := time.Now().Add(-time.Hour)
	submit := func(key, status, phase string, seq int) {
		t.Helper()
		if _, err = s.SubmitWorkObservation(ctx, "writer", anyStringValue(child["id"]), map[string]any{"idempotency_key": key, "reader_id": "fixture", "reader_revision": "1", "observed_at": baseline.Add(time.Duration(seq) * time.Minute).UTC().Format(time.RFC3339Nano), "source_sequence": seq, "status": status, "facts": map[string]any{"phase": phase}, "evidence": []any{map[string]any{"authority": "tracker", "connection_id": "fixture", "native_id": "42", "url": url, "phase": phase}}}); err != nil {
			t.Fatal(err)
		}
	}
	queued := func(want int) {
		t.Helper()
		got := 0
		for _, id := range ids {
			var count int
			if err = ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM brief_dirty WHERE card_id=?`, id).Scan(&count); err != nil {
				t.Fatal(err)
			}
			got += count
		}
		if got != want {
			t.Fatalf("queued initiatives=%d want=%d", got, want)
		}
	}
	clear := func() {
		t.Helper()
		if _, err = ws.DB().ExecContext(ctx, `DELETE FROM brief_dirty`); err != nil {
			t.Fatal(err)
		}
	}
	submit("first", "reported", "ready", 1)
	queued(len(ids))
	clear()
	submit("identical", "reported", "ready", 2)
	queued(0)
	submit("error", "error", "blocked", 3)
	queued(0)
	submit("stale", "reported", "blocked", 0)
	queued(0)
	submit("changed", "reported", "done", 4)
	queued(len(ids))
	clear()
	// Linking new source evidence must still invalidate its referencers.
	w, err := s.GetWork(ctx, anyStringValue(child["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchWork(ctx, "writer", anyStringValue(child["id"]), w["version"].(int64), map[string]any{"source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "fixture", "native_id": "other", "url": url + "/new", "status": "open"}}}); err != nil {
		t.Fatal(err)
	}
	queued(len(ids))
}
