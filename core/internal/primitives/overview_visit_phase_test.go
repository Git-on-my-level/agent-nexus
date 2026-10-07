package primitives_test

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

/*
The visit snapshot records each card's phase, so the next visit can tell a
card that moved into blocked from one that was already blocked and has since
been edited.

Without this, "newly blocked" was derived from current phase plus a fresh
updated_at, which announced a brand new blocker every time anyone touched an
already-blocked card. This is the half of that fix the ledger owns: the
phases have to survive the write and come back keyed by card id. Grouping
them is covered in the server package.
*/
func TestOverviewVisitSnapshotCarriesPhaseForTransitionDetection(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Work"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	work := []map[string]any{}
	for title, phase := range map[string]string{"Restore the replica": "blocked", "Ship the importer": "in_progress"} {
		w, err := s.CreateWork(ctx, "actor", board["id"].(string), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		w["phase"] = phase
		work = append(work, w)
	}
	if err = s.RecordOverviewVisit(ctx, "alice", work, now); err != nil {
		t.Fatal(err)
	}

	digest, err := s.OverviewChanges(ctx, "alice", work, false, nil, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if digest.Since == nil {
		t.Fatal("no baseline was recorded")
	}
	if len(digest.PriorPhases) != 2 {
		t.Fatalf("phases did not survive the snapshot: %v", digest.PriorPhases)
	}
	for _, w := range work {
		id := w["id"].(string)
		if digest.PriorPhases[id] != w["phase"] {
			t.Fatalf("card %s came back as %q, want %q", id, digest.PriorPhases[id], w["phase"])
		}
	}

	// The phases describe the visit that was recorded, not the rows handed to
	// this read: a card that has since moved still reports where it was.
	for _, w := range work {
		w["phase"] = "done"
	}
	moved, err := s.OverviewChanges(ctx, "alice", work, false, nil, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range work {
		if moved.PriorPhases[w["id"].(string)] == "done" {
			t.Fatalf("prior phase followed the current row instead of the baseline: %v", moved.PriorPhases)
		}
	}

	// A principal with no recorded visit has no phases to compare against,
	// which must read as "unknown" rather than as an empty baseline.
	first, err := s.OverviewChanges(ctx, "bob", work, false, nil, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Since != nil || len(first.PriorPhases) != 0 {
		t.Fatalf("a first visit invented a baseline: %v %v", first.Since, first.PriorPhases)
	}
}
