package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newWorkTestStore(t *testing.T) (*primitives.Store, string) {
	t.Helper()
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(context.Background(), "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	return s, b["id"].(string)
}
func registerWork(t *testing.T, s *primitives.Store, b string) map[string]any {
	t.Helper()
	w, err := s.CreateWork(context.Background(), "actor-1", b, map[string]any{"title": "Ship release", "source": map[string]any{"authority": "github", "connection_id": "test", "native_id": "org/repo/issues/1"}})
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func TestWorkSourceDeduplicationAndAuthority(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	again := registerWork(t, s, b)
	if w["ref"] != again["ref"] {
		t.Fatalf("duplicate commitments: %v %v", w, again)
	}
	cards, err := s.ListCards(ctx, primitives.CardListFilter{})
	if err != nil || len(cards) != 1 {
		t.Fatalf("must extend cards: %v %v", cards, err)
	}
	_, err = s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"phase": "done"})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("source status writable: %v", err)
	}
	updated, err := s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"next_action": "Review acceptance", "priority": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated["next_action"] != "Review acceptance" {
		t.Fatal(updated)
	}
	_, err = s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"next_action": "stale write"})
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("CAS bypass: %v", err)
	}
}
func TestWorkObservationReplayOrderingAndFailure(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	report := func(key string, seq int, status, phase string) map[string]any {
		return map[string]any{"idempotency_key": key, "reader_id": "github", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": seq, "status": status, "facts": map[string]any{"phase": phase, "native_status": "custom"}, "evidence": []any{map[string]any{"url": "https://example.test/evidence"}}}
	}
	good := report("second", 2, "verified", "review")
	r, err := s.SubmitWorkObservation(ctx, "actor-1", id, good)
	if err != nil {
		t.Fatal(err)
	}
	if r["observation"].(map[string]any)["verification"] != "reported" {
		t.Fatal("client self-certified verification")
	}
	duplicate, err := s.SubmitWorkObservation(ctx, "actor-1", id, good)
	if err != nil || duplicate["duplicate"] != true {
		t.Fatalf("replay: %v %v", duplicate, err)
	}
	if _, err = s.SubmitWorkObservation(ctx, "actor-1", id, report("older", 1, "reported", "in_progress")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitWorkObservation(ctx, "actor-1", id, report("failed", 3, "error", "done")); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "review" {
		t.Fatalf("regressed good evidence: %v", current)
	}
	if current["freshness"].(map[string]any)["status"] != "error" {
		t.Fatal(current)
	}
	obs, _, err := s.ListWorkObservations(ctx, id, 50, "")
	if err != nil || len(obs) != 3 {
		t.Fatalf("audit lost/replayed: %v %v", obs, err)
	}
}
func TestWorkRefreshCoalescesAndUnknownIsNotHealthy(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	if w["freshness"].(map[string]any)["status"] != "unknown" {
		t.Fatal(w)
	}
	a, err := s.RequestWorkRefresh(ctx, "actor-1", id)
	if err != nil {
		t.Fatal(err)
	}
	bmap, err := s.RequestWorkRefresh(ctx, "actor-1", id)
	if err != nil {
		t.Fatal(err)
	}
	if a["requested_at"] != bmap["requested_at"] || bmap["state"] != "queued" {
		t.Fatalf("noncoalesced: %v %v", a, bmap)
	}
}

func TestWorkRejectsLegacySourceMutations(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	owner := "someone"
	_, err := s.UpdateBoardCard(ctx, "actor-1", "", id, primitives.UpdateBoardCardInput{Assignee: &owner})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external owner bypass: %v", err)
	}
	_, err = s.MoveBoardCard(ctx, "actor-1", b, id, primitives.MoveBoardCardInput{ColumnKey: "ready"})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external phase bypass: %v", err)
	}
	title := "changed"
	_, _, err = s.CreateCardRevision(ctx, "actor-1", id, primitives.CreateCardRevisionInput{Title: &title})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external revision bypass: %v", err)
	}
}
func TestWorkConflictingReplayAndCompletionEvidence(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	o := map[string]any{"idempotency_key": "key", "reader_id": "r", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "review"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); err != nil {
		t.Fatal(err)
	}
	o["facts"] = map[string]any{"phase": "blocked"}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("conflicting replay accepted: %v", err)
	}
	o["idempotency_key"] = "done"
	o["facts"] = map[string]any{"phase": "done"}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unproven completion: %v", err)
	}
	o["facts"] = map[string]any{"phase": "review"}
	o["observed_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("future timestamp accepted: %v", err)
	}
}

func TestWorkUnsequencedOutageAfterSequencedSuccess(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	now := time.Now().UTC()
	good := map[string]any{"idempotency_key": "good", "reader_id": "reader", "reader_revision": "v1", "observed_at": now.Add(-time.Second).Format(time.RFC3339Nano), "source_sequence": 9, "status": "reported", "facts": map[string]any{"phase": "review"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, good); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"idempotency_key": "outage", "reader_id": "reader", "reader_revision": "v1", "observed_at": now.Format(time.RFC3339Nano), "status": "error", "error": map[string]any{"code": "unreachable", "message": "fixture"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, failed); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "review" || current["freshness"].(map[string]any)["status"] != "error" {
		t.Fatalf("outage hidden: %v", current)
	}
}

func TestWorkUnchangedSequenceRefreshesWithoutProgress(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		o := map[string]any{"idempotency_key": fmt.Sprintf("poll-%d", i), "reader_id": "r", "reader_revision": "1", "observed_at": now.Add(time.Duration(i-1) * time.Second).Format(time.RFC3339Nano), "source_sequence": 9, "source_revision": "9", "status": "reported", "facts": map[string]any{"phase": "review"}}
		if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	fresh := current["freshness"].(map[string]any)
	if fresh["last_observed_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("unchanged source failed freshness: %v", fresh)
	}
	if _, ok := fresh["meaningful_progress_at"]; ok {
		t.Fatal("poll invented progress")
	}
}

func TestWorkObservationPaginationSurvivesInsert(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	ref := w["ref"].(string)
	submit := func(key string) {
		t.Helper()
		_, err := s.SubmitWorkObservation(ctx, "actor-1", ref, map[string]any{"idempotency_key": key, "reader_id": "r", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "review"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	submit("one")
	submit("two")
	page, cursor, err := s.ListWorkObservations(ctx, ref, 1, "")
	if err != nil || len(page) != 1 || cursor == "" {
		t.Fatalf("first page: %v %v", page, err)
	}
	submit("three")
	older, next, err := s.ListWorkObservations(ctx, ref, 1, cursor)
	if err != nil || len(older) != 1 || older[0]["idempotency_key"] != "one" || next != "" {
		t.Fatalf("insert destabilized cursor: %v %s %v", older, next, err)
	}
	events, err := s.ListEventsByThread(ctx, w["thread_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("routine polls created semantic events: %d", len(events))
	}
}

func TestWorkReferencesRemainWorkspaceScoped(t *testing.T) {
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	_, err := s.CreateWork(ctx, "actor-1", b, map[string]any{"title": "Invalid project", "project_ref": "topic:missing"})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unresolved project accepted: %v", err)
	}
	w := registerWork(t, s, b)
	_, err = s.PatchWork(ctx, "actor-1", w["ref"].(string), 1, map[string]any{"relations": []any{map[string]any{"kind": "depends_on", "ref": "card:other-workspace"}}})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unresolved dependency accepted: %v", err)
	}
}
