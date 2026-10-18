package primitives_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestSourceEvidenceResetsPromiseWithoutPollingProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	w, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Synced evidence"})
	if err != nil {
		t.Fatal(err)
	}
	id := w["id"].(string)
	note := func() {
		t.Helper()
		_, err := s.AppendEvent(ctx, "actor-1", map[string]any{"type": "message_posted", "thread_id": w["thread_id"], "refs": []string{w["ref"].(string)}, "payload": map[string]any{"text": "Waiting for source", "next": map[string]any{"at": "2d", "what": "Verify change"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	fold := func() plans.PromiseFold {
		t.Helper()
		if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
		folds, err := s.PromiseFolds(ctx, []string{id})
		if err != nil {
			t.Fatal(err)
		}
		// The ordinary plan_health read must agree with the maintained brief fold.
		card, err := s.GetWork(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		health := card["plan_health"].(plans.Health)
		healthAt := time.Time{}
		if health.LastEvidenceAt != nil {
			healthAt, _ = time.Parse(time.RFC3339Nano, *health.LastEvidenceAt)
		}
		if healthAt.UTC().Format(time.RFC3339) != folds[id].LastEvidenceAt.UTC().Format(time.RFC3339) {
			t.Fatalf("health=%#v fold=%#v", health, folds[id])
		}
		if (health.Promise == nil) != (folds[id].Promise == nil) {
			t.Fatalf("promise disagrees: %#v %#v", health, folds[id])
		}
		return folds[id]
	}
	patch := func(refs []any) {
		t.Helper()
		w, err = s.PatchWork(ctx, "actor-1", id, w["version"].(int64), map[string]any{"source_refs": refs})
		if err != nil {
			t.Fatal(err)
		}
	}
	note()
	if fold().Promise == nil {
		t.Fatal("missing starting promise")
	}
	refs := []any{}
	for i := 0; i < 65; i++ {
		refs = append(refs, map[string]any{"authority": "tracker", "connection_id": "fixture", "native_id": fmt.Sprint(i), "status": "open", "observed_at": "2026-01-01T00:00:00Z"})
	}
	before := time.Now()
	patch(refs)
	reset := fold()
	if reset.Promise != nil || reset.LastEvidenceAt.Before(before) {
		t.Fatalf("linked old evidence did not reset at write time: %#v", reset)
	}
	note()
	// Identical polls, reorderings and unlinking alone must retain the promise.
	patch(refs)
	for _, raw := range refs {
		raw.(map[string]any)["observed_at"] = "2026-01-02T00:00:00Z"
	}
	patch(refs)
	refs[0], refs[64] = refs[64], refs[0]
	patch(refs)
	patch(refs[:64])
	unchanged := fold()
	if unchanged.Promise == nil || !unchanged.LastEvidenceAt.Equal(reset.LastEvidenceAt) {
		t.Fatalf("poll/removal became evidence: %#v", unchanged)
	}
	refs[0].(map[string]any)["status"] = "merged"
	patch(refs[:64])
	changed := fold()
	if changed.Promise != nil || !changed.LastEvidenceAt.After(reset.LastEvidenceAt) {
		t.Fatalf("changed source did not reset: %#v", changed)
	}
	// Local prose and unknown create inputs cannot forge the internal clock.
	if _, err = s.PatchWork(ctx, "actor-1", id, w["version"].(int64), map[string]any{"_anx_source_evidence_at": "2099-01-01T00:00:00Z"}); err == nil {
		t.Fatal("accepted internal watermark")
	}
}

func TestChangedObservationResetsPromiseButIdenticalErrorAndStaleDoNot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	w, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Observed", "source": map[string]any{"authority": "tracker", "connection_id": "fixture", "native_id": "one"}})
	if err != nil {
		t.Fatal(err)
	}
	id := w["id"].(string)
	baseline := time.Now().Add(-time.Hour)
	submit := func(key, status, phase string, seq int, evidence ...[]any) {
		t.Helper()
		input := map[string]any{"idempotency_key": key, "reader_id": "fixture", "reader_revision": "1", "observed_at": baseline.Add(time.Duration(seq) * time.Minute).Format(time.RFC3339Nano), "source_sequence": seq, "status": status, "facts": map[string]any{"phase": phase}}
		if len(evidence) > 0 {
			input["evidence"] = evidence[0]
		}
		_, err := s.SubmitWorkObservation(ctx, "actor-1", id, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	fold := func() plans.PromiseFold {
		t.Helper()
		if _, err := s.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
		folds, err := s.PromiseFolds(ctx, []string{id})
		if err != nil {
			t.Fatal(err)
		}
		return folds[id]
	}
	note := func() {
		t.Helper()
		_, err := s.AppendEvent(ctx, "actor-1", map[string]any{"type": "message_posted", "thread_id": w["thread_id"], "refs": []string{w["ref"].(string)}, "payload": map[string]any{"text": "Next checkpoint", "next": map[string]any{"at": "2d", "what": "Deliver"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	submit("first", "reported", "ready", 1)
	first := fold()
	if first.LastEvidenceAt.IsZero() {
		t.Fatal("first observation missing evidence")
	}
	note()
	submit("poll", "reported", "ready", 2)
	submit("error", "error", "blocked", 3)
	submit("stale", "reported", "blocked", 0)
	unchanged := fold()
	if unchanged.Promise == nil || !unchanged.LastEvidenceAt.Equal(first.LastEvidenceAt) {
		t.Fatalf("poll/error/stale reset: %#v", unchanged)
	}
	submit("changed", "reported", "in_progress", 4)
	changed := fold()
	if changed.Promise != nil || !changed.LastEvidenceAt.After(first.LastEvidenceAt) {
		t.Fatalf("changed observation missing reset: %#v", changed)
	}
	note()
	evidence := []any{map[string]any{"url": "https://example.test/proof"}}
	submit("evidence-only", "reported", "in_progress", 5, evidence)
	evidenceReset := fold()
	if evidenceReset.Promise != nil || !evidenceReset.LastEvidenceAt.After(changed.LastEvidenceAt) {
		t.Fatalf("new evidence with unchanged facts did not reset: %#v", evidenceReset)
	}
	note()
	submit("evidence-only", "reported", "in_progress", 5, evidence)
	replay := fold()
	if replay.Promise == nil || !replay.LastEvidenceAt.Equal(evidenceReset.LastEvidenceAt) {
		t.Fatalf("idempotent replay reset promise: %#v", replay)
	}
	changed = evidenceReset
	// Ordinary annotation writes preserve the source clock.
	if _, err = s.PatchWork(ctx, "actor-1", id, w["version"].(int64), map[string]any{"next_action": "Verify"}); err != nil {
		t.Fatal(err)
	}
	if next := fold(); !next.LastEvidenceAt.Equal(changed.LastEvidenceAt) {
		t.Fatalf("annotation rewound source evidence: %#v", next)
	}
}
