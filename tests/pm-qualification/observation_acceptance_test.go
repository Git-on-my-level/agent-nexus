package qualification_test

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

// Real reader normalization -> canonical store seam, using only synthetic
// source facts. This is intentionally independent of either package's tests.
func TestUnchangedActualReadAdvancesFreshnessWithoutReplayConflict(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "synthetic-actor", map[string]any{"title": "Synthetic source seam"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, "synthetic-actor", board["id"].(string), map[string]any{
		"title": "Unchanged source", "source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "fixture/repo#1"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	activity := now.Add(-time.Hour)
	target := observation.Target{WorkspaceID: "synthetic-workspace", ConnectionID: "fixture", Source: "github", Kind: "issue", Repository: "fixture/repo", NativeID: "1"}
	binding := observation.RemoteBinding{WorkspaceID: target.WorkspaceID, ReaderID: "synthetic-reader", Target: target, MaxClockSkew: time.Minute}
	source := observation.Report{Target: target, ReaderID: binding.ReaderID, ReaderRevision: "v1", SourceRevision: "unchanged-r1",
		ObservedAt: now.Add(-time.Minute), Knowledge: "reported", SourceActivityAt: &activity,
		Facts: map[string]any{"phase": "in_progress", "native_status": "OPEN"}, Evidence: []observation.Evidence{}}
	first, err := observation.NormalizeRemoteReport(source, binding, now)
	if err != nil {
		t.Fatal(err)
	}
	id := work["id"].(string)
	if _, err = store.SubmitWorkObservation(ctx, "synthetic-actor", id, first.Observation(time.Minute)); err != nil {
		t.Fatal(err)
	}
	replay, err := store.SubmitWorkObservation(ctx, "synthetic-actor", id, first.Observation(time.Minute))
	if err != nil || replay["duplicate"] != true {
		t.Fatal("exact report delivery replay lost deduplication")
	}
	source.ObservedAt = now
	second, err := observation.NormalizeRemoteReport(source, binding, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SubmitWorkObservation(ctx, "synthetic-actor", id, second.Observation(time.Minute)); err != nil {
		t.Fatalf("a second actual read of unchanged source conflicts at reader/store boundary: %v", err)
	}
	current, err := store.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	fresh := current["freshness"].(map[string]any)
	if fresh["last_observed_at"] != now.Format(time.RFC3339Nano) {
		t.Fatal("successful actual reread did not advance checked-at freshness")
	}
	if fresh["source_activity_at"] != activity.Format(time.RFC3339Nano) {
		t.Fatal("polling manufactured source activity")
	}
}

func TestMaterialObservationEmitsOneCanonicalEvent(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "synthetic-actor", map[string]any{"title": "Synthetic event seam"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, "synthetic-actor", board["id"].(string), map[string]any{
		"title": "Source item", "source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "fixture/repo#2"}})
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := ws.DB().QueryRowContext(ctx, "SELECT count(*) FROM events").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	obs := map[string]any{"reader_id": "fixture", "reader_revision": "v1", "idempotency_key": "material-change",
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported",
		"facts": map[string]any{"phase": "blocked", "native_status": "BLOCKED"}, "evidence": []any{}}
	if _, err = store.SubmitWorkObservation(ctx, "synthetic-actor", work["id"].(string), obs); err != nil {
		t.Fatal(err)
	}
	after := count()
	if after <= before {
		t.Fatal("material source observation produced no canonical event for the event/inbox surface")
	}
	if _, err = store.SubmitWorkObservation(ctx, "synthetic-actor", work["id"].(string), obs); err != nil {
		t.Fatal(err)
	}
	if count() != after {
		t.Fatal("duplicate source report emitted another semantic event")
	}
}
