package server

import (
	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/primitives"
	"context"
	"fmt"
	"testing"
	"time"
)

type workFixtureReader struct {
	calls int
	fail  bool
}

func (r *workFixtureReader) Capabilities() observation.Capabilities {
	return observation.Capabilities{Source: "github", ReadOne: true}
}
func (r *workFixtureReader) Read(_ context.Context, target observation.Target) (observation.Report, error) {
	r.calls++
	if r.fail {
		return observation.Report{}, fmt.Errorf("fixture unavailable")
	}
	return observation.Report{Target: target, ReaderID: "fixture", ReaderRevision: "1", ObservedAt: time.Now().UTC(), Knowledge: "reported", Title: "unchanged", SourceRevision: "same-revision", Facts: map[string]any{"phase": "review"}, Coverage: observation.Coverage{Complete: true}, Evidence: []observation.Evidence{{Kind: "issue", Reference: "https://example.test/issue/1", Knowledge: "reported"}}}, nil
}
func TestObservationRuntimePersistsUnchangedReadsAndOutage(t *testing.T) {
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Reader integration"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateWork(ctx, "actor-1", asString(b["id"]), map[string]any{"title": "Initial", "source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "org/repo#1"}})
	if err != nil {
		t.Fatal(err)
	}
	ref := asString(work["ref"])
	reader := &workFixtureReader{}
	binding := ObservationBinding{WorkRef: ref, SourceNativeID: "org/repo#1", Target: observation.Target{WorkspaceID: "ws_main", ConnectionID: "fixture", Source: "github", Kind: "issue", NativeID: "1", Repository: "org/repo"}, Reader: reader, Policy: observation.RefreshPolicy{Interval: time.Minute, StaleAfter: time.Hour, Timeout: time.Second, MaxBackoff: time.Hour}}
	runtime, err := NewObservationRuntime(s, []ObservationBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := s.GetWork(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if first["refresh"].(map[string]any)["state"] != "succeeded" || first["title"] != "unchanged" {
		t.Fatal(first)
	}
	if _, err = s.RequestWorkRefresh(ctx, "actor-1", ref); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	observations, _, err := s.ListWorkObservations(ctx, ref, 50, "")
	if err != nil || len(observations) != 2 {
		t.Fatalf("unchanged poll lost freshness: %v %v", observations, err)
	}
	reader.fail = true
	if _, err = s.RequestWorkRefresh(ctx, "actor-1", ref); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Tick(ctx); err == nil {
		t.Fatal("outage not returned")
	}
	current, err := s.GetWork(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if current["title"] != "unchanged" || current["freshness"].(map[string]any)["status"] != "error" {
		t.Fatal(current)
	}
	restarted, err := NewObservationRuntime(s, []ObservationBinding{binding})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 3 {
		t.Fatal("restart ignored persisted backoff")
	}
}
func TestObservationRuntimeRejectsBindingMismatch(t *testing.T) {
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, _ := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Binding"})
	w, err := s.CreateWork(ctx, "actor-1", asString(b["id"]), map[string]any{"title": "Private", "source": map[string]any{"authority": "github", "connection_id": "actual", "native_id": "org/repo#1"}})
	if err != nil {
		t.Fatal(err)
	}
	reader := &workFixtureReader{}
	rt, err := NewObservationRuntime(s, []ObservationBinding{{WorkRef: asString(w["ref"]), SourceNativeID: "other", Target: observation.Target{WorkspaceID: "ws_main", ConnectionID: "wrong", Source: "github", Kind: "issue", NativeID: "1"}, Reader: reader, Policy: observation.RefreshPolicy{Interval: time.Minute, StaleAfter: time.Hour, Timeout: time.Second, MaxBackoff: time.Hour}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = rt.Tick(ctx); err == nil || reader.calls != 0 {
		t.Fatal("unregistered target read")
	}
}
