package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"errors"
	"testing"
	"time"
)

func TestWorkRefreshLeaseFencing(t *testing.T) {
	s, b := newWorkTestStore(t)
	w := registerWork(t, s, b)
	ctx := context.Background()
	id := w["id"].(string)
	if _, err := s.RequestWorkRefresh(ctx, "actor-1", id); err != nil {
		t.Fatal(err)
	}
	claim, err := s.ClaimWorkRefresh(ctx, id, "worker-one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimWorkRefresh(ctx, id, "worker-two", time.Minute); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("overlapping claim: %v", err)
	}
	token := claim["lease_token"].(string)
	_, err = s.SubmitLeasedWorkObservation(ctx, "actor-1", id, "wrong-token", map[string]any{"idempotency_key": "fenced", "reader_id": "r", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported"})
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale worker observation was not fenced: %v", err)
	}
	if _, err := s.FinishWorkRefresh(ctx, id, "wrong-token", map[string]any{"state": "succeeded"}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("unfenced finish: %v", err)
	}
	_, err = s.FinishWorkRefresh(ctx, id, token, map[string]any{"state": "failed", "last_error": map[string]any{"code": "read_failed", "message": "fixture"}, "next_due_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["refresh"].(map[string]any)["state"] != "failed" {
		t.Fatal(current)
	}
	if _, err := s.FinishWorkRefresh(ctx, id, token, map[string]any{"state": "succeeded"}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("old lease reused: %v", err)
	}
}
