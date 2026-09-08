package server

import (
	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func TestObservationConfigSupportsExplicitTrustedMulticaCLI(t *testing.T) {
	h := newPrimitivesTestServer(t)
	path := filepath.Join(t.TempDir(), "readers.json")
	config := `{"targets":[{"work_ref":"card:tracked","source_native_id":"issue-1","target":{"source":"multica","connection_id":"approved","kind":"issue","native_id":"issue-1"},"transport":"multica_cli","cli_binary":"/opt/approved/multica","cli_profile":"approved-profile","base_url":"https://example.test","source_workspace_id":"source-workspace"}]}`
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	rt, err := LoadObservationRuntime(path, "ws_main", h.primitiveStore.(*primitives.Store))
	if err != nil {
		t.Fatal(err)
	}
	if rt == nil || len(rt.bindings) != 1 || rt.bindings[0].Reader.Capabilities().Source != "multica" {
		t.Fatal("CLI reader not configured")
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadObservationRuntime(path, "ws_main", h.primitiveStore.(*primitives.Store)); err == nil {
		t.Fatal("writable operator configuration accepted")
	}
}

func TestJITAndInvestigationTransportsFailClosedWithoutSandbox(t *testing.T) {
	h := newPrimitivesTestServer(t)
	root := filepath.Join(t.TempDir(), "jit")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(resolved, 0700); err != nil {
		t.Fatal(err)
	}
	policy := observation.JITPolicy{MaxArtifactBytes: 16 << 20, Limits: observation.IsolationLimits{Timeout: time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 65536, CPUSeconds: 1, Processes: 8, FileBytes: 65536}, FailureThreshold: 2}
	rawPolicy, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "readers.json")
	config := fmt.Sprintf(`{"targets":[{"work_ref":"card:tracked","source_native_id":"1","target":{"source":"github","connection_id":"c","kind":"issue","native_id":"1","repository":"o/r"},"transport":"jit","base_url":"https://api.github.com","jit_state_root":%q,"jit_adapter_id":"fixture","jit_policy":%s}]}`, resolved, rawPolicy)
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	rt, err := LoadObservationRuntime(path, "ws_main", h.primitiveStore.(*primitives.Store))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rt.bindings[0].Reader.Read(context.Background(), rt.bindings[0].Target); err == nil {
		t.Fatal("JIT reader executed without an isolation envelope")
	}
	invPath := filepath.Join(t.TempDir(), "investigation.json")
	invConfig := `{"targets":[{"work_ref":"card:tracked","source_native_id":"1","target":{"source":"github","connection_id":"c","kind":"issue","native_id":"1","repository":"o/r"},"transport":"investigation","investigation_id":"investigation-1","base_url":"https://api.github.com"}]}`
	if err = os.WriteFile(invPath, []byte(invConfig), 0600); err != nil {
		t.Fatal(err)
	}
	invRuntime, err := LoadObservationRuntime(invPath, "ws_main", h.primitiveStore.(*primitives.Store))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = invRuntime.bindings[0].Reader.Read(context.Background(), invRuntime.bindings[0].Target); err == nil {
		t.Fatal("investigation ran without a bound isolated executor")
	}
}
