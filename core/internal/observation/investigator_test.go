package observation

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type countingReader struct {
	calls int
	inner Reader
}

func (c *countingReader) Capabilities() Capabilities { return c.inner.Capabilities() }
func (c *countingReader) Read(ctx context.Context, t Target) (Report, error) {
	c.calls++
	return c.inner.Read(ctx, t)
}

type trustedBuiltin struct {
	fixtureReader
	source string
}

func (t trustedBuiltin) Capabilities() Capabilities {
	return Capabilities{Source: t.source, ReadOne: true, TrustedBuiltin: true}
}

func TestInvestigationRuntimeFailsClosedWithoutIsolation(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewInvestigationStore(db, func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	spec := InvestigationSpec{Objective: "Inspect deployment evidence", Target: fixtureTarget(), PresetRef: "approved-existing-agent", AllowedSources: []string{"fixture"}, EvidenceRequirements: []string{"serving revision"}, Limits: Limits{Timeout: time.Minute, MaxOutputBytes: 65536, MaxRequests: 10, MaxTokens: 1000}, Interval: time.Hour}
	if _, err = store.Save(context.Background(), "w", "investigation-1", spec, 0); err != nil {
		t.Fatal(err)
	}
	rt, err := NewInvestigationRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	if rt.exec != nil {
		t.Fatal("production constructor bundled a model executor")
	}
	source := &countingReader{inner: trustedBuiltin{source: "fixture", fixtureReader: fixtureReader{read: func(context.Context, Target) (Report, error) {
		t.Fatal("source read before isolation")
		return Report{}, nil
	}}}}
	_, err = rt.Run(context.Background(), "w", "investigation-1", source)
	if err == nil {
		t.Fatal("investigation ran without isolation")
	}
	var re *ReadError
	if !errors.As(err, &re) || (re.Kind != ErrIsolation && re.Kind != ErrUnavailable) {
		t.Fatalf("want isolation/unavailable fail-closed, got %v", err)
	}
	if source.calls != 0 {
		t.Fatal("source snapshot was taken without an isolation envelope")
	}
	untrusted := fixtureReader{read: func(context.Context, Target) (Report, error) {
		t.Fatal("untrusted reader invoked")
		return Report{}, nil
	}}
	if _, err = rt.Run(context.Background(), "w", "investigation-1", untrusted); err == nil {
		t.Fatal("untrusted reader accepted")
	}
}

func TestInvestigationRuntimeRejectsHostExecutorFallback(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux isolation availability is host-dependent; macOS proves the fail-closed constructor path")
	}
	runner := NewBubblewrapRunner()
	if runner.Available() == nil {
		t.Fatal("unsupported OS reported isolation")
	}
}

type fakeIsolatedInvestigator struct {
	available error
	report    Report
}

func (f fakeIsolatedInvestigator) Available() error { return f.available }
func (f fakeIsolatedInvestigator) Investigate(context.Context, InvestigationSpec, Report) (Report, error) {
	return f.report, nil
}

func TestInvestigationRuntimeRejectsSelfCertifiedVerification(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewInvestigationStore(db, func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	spec := InvestigationSpec{Objective: "Inspect deployment evidence", Target: fixtureTarget(), PresetRef: "approved-existing-agent", AllowedSources: []string{"fixture"}, EvidenceRequirements: []string{"serving revision"}, Limits: Limits{Timeout: time.Minute, MaxOutputBytes: 65536, MaxRequests: 10, MaxTokens: 1000}, Interval: time.Hour}
	if _, err = store.Save(context.Background(), "w", "investigation-1", spec, 0); err != nil {
		t.Fatal(err)
	}
	rt, err := NewInvestigationRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	rt.runner = &fixtureIsolator{}
	rt.exec = fakeIsolatedInvestigator{report: Report{Knowledge: "verified", ObservedAt: time.Now().UTC(), ReaderRevision: "1"}}
	source := trustedBuiltin{source: "fixture", fixtureReader: fixtureReader{read: func(_ context.Context, target Target) (Report, error) {
		return Report{Target: target, ReaderID: "builtin:fixture", ReaderRevision: "1", ObservedAt: time.Now().UTC(), Knowledge: "reported", Coverage: Coverage{Complete: true}}, nil
	}}}
	_, err = rt.Run(context.Background(), "w", "investigation-1", source)
	if err == nil {
		t.Fatal("self-certified verification accepted")
	}
	var re *ReadError
	if !errors.As(err, &re) || re.Kind != ErrPolicy {
		t.Fatalf("want policy denial, got %v", err)
	}
}
