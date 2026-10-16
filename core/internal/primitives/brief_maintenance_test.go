package primitives

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

type codedMaintenanceError struct {
	code int
	text string
}

func (e codedMaintenanceError) Error() string { return e.text }
func (e codedMaintenanceError) Code() int     { return e.code }

func TestBriefMaintenanceGateSkipsBusyAndRateLimits(t *testing.T) {
	gate := newBriefMaintenanceGate(time.Minute)
	var got []string
	emit := func(err error) { got = append(got, err.Error()) }
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, err := range []error{
		codedMaintenanceError{code: 5, text: "database is locked (5) (SQLITE_BUSY)"},
		codedMaintenanceError{code: 6, text: "database table is locked (6) (SQLITE_LOCKED)"},
		codedMaintenanceError{code: 261, text: "database is locked (261)"},
		fmtWrap(codedMaintenanceError{code: 517, text: "database is locked (517) (SQLITE_BUSY)"}),
	} {
		gate.maybe(err, now, emit)
	}
	if len(got) != 0 {
		t.Fatalf("busy yield reported: %v", got)
	}

	// Text alone is not the admission yield. A non-coded error is still reported.
	gate.maybe(errors.New("database is locked (5) (SQLITE_BUSY)"), now, emit)
	gate.maybe(errors.New("disk i/o error"), now, emit)
	gate.maybe(errors.New("disk i/o error"), now.Add(30*time.Second), emit)
	gate.maybe(errors.New("schema mismatch"), now, emit)
	if len(got) != 3 || got[0] != "database is locked (5) (SQLITE_BUSY)" || got[1] != "disk i/o error" || got[2] != "schema mismatch" {
		t.Fatalf("rate limit: %#v", got)
	}
	gate.maybe(errors.New("disk i/o error"), now.Add(time.Minute), emit)
	if len(got) != 4 {
		t.Fatalf("expected the distinct error again after a minute, got %#v", got)
	}
}

func fmtWrap(err error) error { return wrappedError{err} }

type wrappedError struct{ error }

func (w wrappedError) Unwrap() error { return w.error }

func TestRunBriefMaintenanceDoesNotReportBusy(t *testing.T) {
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	conn, err := workspace.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin hold: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	refreshCtx, cancelRefresh := context.WithTimeout(ctx, 2*time.Second)
	defer cancelRefresh()
	_, refreshErr := store.RefreshBriefInitiatives(refreshCtx, 1)
	var coded interface{ Code() int }
	if !errors.As(refreshErr, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
		t.Fatalf("expected SQLITE_BUSY or SQLITE_LOCKED from the 0ms admission, got %v", refreshErr)
	}

	var mu sync.Mutex
	var reported []error
	runCtx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	store.RunBriefMaintenance(runCtx, func(err error) {
		mu.Lock()
		reported = append(reported, err)
		mu.Unlock()
	})
	mu.Lock()
	defer mu.Unlock()
	if len(reported) != 0 {
		t.Fatalf("maintenance reported %v", reported)
	}
}
