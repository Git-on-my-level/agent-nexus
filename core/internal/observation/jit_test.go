package observation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func fixtureJITPolicy() JITPolicy {
	return JITPolicy{MaxArtifactBytes: 16 << 20, Limits: IsolationLimits{Timeout: time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 65536, CPUSeconds: 1, Processes: 8, FileBytes: 65536}, FailureThreshold: 2}
}
func TestGeneratedReaderCapabilitiesFailClosed(t *testing.T) {
	p := fixtureJITPolicy()
	manifest := Manifest{AdapterID: "fixture", Target: fixtureTarget(), Envelope: Envelope{}, Limits: p.Limits}
	if err := manifest.Validate(p); err != nil {
		t.Fatal(err)
	}
	for _, expanded := range []Envelope{{NetworkHosts: []string{"example.test"}}, {ReadPaths: []string{"/fixture-private"}}, {CredentialHandles: []string{"fixture-token"}}, {Dependencies: []string{"fixture-package"}}, {ScratchPaths: []string{"/other"}}} {
		manifest.Envelope = expanded
		if err := manifest.Validate(p); err == nil {
			t.Fatalf("expanded capability accepted: %+v", expanded)
		}
	}
}
func TestIsolationNeverFallsBackToHostExecution(t *testing.T) {
	runner := NewBubblewrapRunner()
	if runtime.GOOS != "linux" {
		if runner.Available() == nil {
			t.Fatal("unsupported OS reported isolation")
		}
		sentinel := filepath.Join(t.TempDir(), "must-not-run")
		_, err := runner.Run(context.Background(), sentinel, []byte(`{}`), fixtureJITPolicy().Limits)
		if err == nil {
			t.Fatal("unsupported host execution accepted")
		}
	}
}

// The fake executor tests durable lifecycle transitions only. It is package-private
// and cannot be supplied by production callers as an isolation substitute.
type fixtureIsolator struct {
	output []byte
	err    error
	calls  int
}

func (f *fixtureIsolator) Available() error { return nil }
func (f *fixtureIsolator) Run(context.Context, string, []byte, IsolationLimits) ([]byte, error) {
	f.calls++
	return f.output, f.err
}
func TestJITRequiresFixturesCanaryAndAtomicActivation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "managed")
	manager, err := NewJITManager(root, fixtureJITPolicy())
	if err != nil {
		t.Fatal(err)
	}
	output, _ := json.Marshal(TransformOutput{Facts: map[string]any{"summary": "fixture observation"}, Uncertainty: []string{"deployment not verified"}})
	fake := &fixtureIsolator{output: output}
	manager.runner = fake
	manifest := Manifest{AdapterID: "fixture", Target: fixtureTarget(), Envelope: Envelope{}, Limits: fixtureJITPolicy().Limits}
	// minimalStaticELF is a harmless structurally valid ELF fixture, never executed.
	first, err := manager.Stage(manifest, minimalStaticELF(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("fixture", first.Revision); err == nil {
		t.Fatal("untested version activated")
	}
	fixtures := []ValidationCase{{Name: "happy", Input: []byte(`{}`), WantValid: true}, {Name: "missing", Input: []byte(`{}`), WantValid: false}}
	if err := manager.Validate(context.Background(), "fixture", first.Revision, fixtures); err == nil {
		t.Fatal("negative fixture did not gate validation")
	}
	fixtures[1].WantValid = true
	if err := manager.Validate(context.Background(), "fixture", first.Revision, fixtures); err == nil {
		t.Fatal("suite without rejection case accepted")
	}
	// Configure a fixture runner that rejects the negative input.
	manager.runner = &validationIsolator{output: output}
	fixtures[1].WantValid = false
	fixtures[1].Input = []byte(`{"reject":true}`)
	if err := manager.Validate(context.Background(), "fixture", first.Revision, fixtures); err != nil {
		t.Fatal(err)
	}
	source := trustedFixtureReader{fixtureReader{read: func(ctx context.Context, t Target) (Report, error) {
		return finishReport(newReport(t, "builtin:fixture"))
	}}}
	if _, err := manager.Canary(context.Background(), "fixture", first.Revision, source); err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("fixture", first.Revision); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewJITManager(root, fixtureJITPolicy())
	if err != nil {
		t.Fatal(err)
	}
	restoredState, _ := reopened.Status("fixture")
	if restoredState.Active != first.Revision {
		t.Fatal("activation lost on restart")
	}
	// Artifact alteration is denied even when the test executor would accept it.
	if err := os.Chmod(filepath.Join(root, "fixture", first.Revision, "reader"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture", first.Revision, "reader"), []byte("fixture alteration"), 0500); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Read(context.Background(), "fixture", source); err == nil {
		t.Fatal("modified artifact executed")
	}
	suspendedState, _ := manager.Status("fixture")
	if suspendedState.Active != "" {
		t.Fatal("policy violation did not suspend")
	}
}

type validationIsolator struct{ output []byte }

func (f *validationIsolator) Available() error { return nil }
func (f *validationIsolator) Run(_ context.Context, _ string, in []byte, _ IsolationLimits) ([]byte, error) {
	if string(in) == `{"reject":true}` {
		return nil, failure(ErrInvalidOutput, "fixture rejection")
	}
	return f.output, nil
}
func minimalStaticELF(marker byte) []byte {
	// ELF64 little endian, ET_EXEC, AMD64, one executable PT_LOAD and no interpreter.
	b := make([]byte, 128)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	b[16] = 2
	b[18] = 62
	b[20] = 1
	b[32] = 64
	b[52] = 64
	b[54] = 56
	b[56] = 1
	b[64] = 1
	b[68] = 5
	b[120] = marker
	return b
}

type trustedFixtureReader struct{ fixtureReader }

func (trustedFixtureReader) Capabilities() Capabilities {
	return Capabilities{TrustedBuiltin: true, ReadOne: true}
}

func TestJITRollbackAndFailureSuspension(t *testing.T) {
	manager, err := NewJITManager(filepath.Join(t.TempDir(), "managed"), fixtureJITPolicy())
	if err != nil {
		t.Fatal(err)
	}
	output, _ := json.Marshal(TransformOutput{Facts: map[string]any{"fixture": true}, Uncertainty: []string{}})
	manager.runner = &validationIsolator{output: output}
	manifest := Manifest{AdapterID: "fixture", Target: fixtureTarget(), Limits: fixtureJITPolicy().Limits}
	source := trustedFixtureReader{fixtureReader{read: func(ctx context.Context, t Target) (Report, error) {
		return finishReport(newReport(t, "builtin:fixture"))
	}}}
	create := func(marker byte) Version {
		t.Helper()
		v, err := manager.Stage(manifest, minimalStaticELF(marker))
		if err != nil {
			t.Fatal(err)
		}
		cases := []ValidationCase{{Name: "valid", Input: []byte(`{}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
		if err := manager.Validate(context.Background(), "fixture", v.Revision, cases); err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Canary(context.Background(), "fixture", v.Revision, source); err != nil {
			t.Fatal(err)
		}
		if err := manager.Activate("fixture", v.Revision); err != nil {
			t.Fatal(err)
		}
		return v
	}
	first := create(1)
	second := create(2)
	if first.Revision == second.Revision {
		t.Fatal("artifact version collision")
	}
	if err := manager.Rollback("fixture"); err != nil {
		t.Fatal(err)
	}
	state, _ := manager.Status("fixture")
	if state.Active != first.Revision || state.Versions[second.Revision].State != "suspended" {
		t.Fatal("rollback did not atomically restore prior version")
	}
	manager.runner = &fixtureIsolator{err: failure(ErrInvalidOutput, "fixture invalid output")}
	for i := 0; i < 2; i++ {
		if _, err := manager.Read(context.Background(), "fixture", source); err == nil {
			t.Fatal("failed output accepted")
		}
	}
	state, _ = manager.Status("fixture")
	if state.Active != "" || state.Versions[first.Revision].State != "suspended" {
		t.Fatal("repeat failures did not suspend")
	}
}

func TestGeneratedOutputCannotInventReferencesOrOverwriteAuthority(t *testing.T) {
	raw := []byte(`{"facts":{"phase":"done"},"evidence":[{"kind":"deployment","reference":"https://unapproved.invalid/proof","knowledge":"verified"}],"uncertainty":[]}`)
	if _, err := parseTransform(raw, 65536); err == nil {
		t.Fatal("generated verification assertion accepted")
	}
	if _, err := parseTransform([]byte(`{"facts":{},"command":"fixture","uncertainty":[]}`), 65536); err == nil {
		t.Fatal("unknown executable output field accepted")
	}
}
