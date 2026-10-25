package primitives

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/testsql"
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

func TestBriefMaintenanceGateEvictsOldestWithoutRewindingRecent(t *testing.T) {
	gate := newBriefMaintenanceGate(time.Minute)
	var got []string
	emit := func(err error) { got = append(got, err.Error()) }
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < briefMaintenanceReportLimit; i++ {
		gate.maybe(errors.New(fmt.Sprintf("err-%02d", i)), base.Add(time.Duration(i)*time.Second), emit)
	}
	if len(got) != briefMaintenanceReportLimit {
		t.Fatalf("fill: %d", len(got))
	}
	// A 33rd distinct error must not wipe the map. The newest key stays
	// suppressed; only the oldest slot is released.
	newest := fmt.Sprintf("err-%02d", briefMaintenanceReportLimit-1)
	gate.maybe(errors.New("err-new"), base.Add(time.Duration(briefMaintenanceReportLimit)*time.Second), emit)
	gate.maybe(errors.New(newest), base.Add(time.Duration(briefMaintenanceReportLimit)*time.Second), emit)
	if got[len(got)-1] != "err-new" || countStrings(got, newest) != 1 {
		t.Fatalf("recent key logged again: %#v", got)
	}
	gate.maybe(errors.New("err-00"), base.Add(61*time.Second), emit)
	gate.maybe(errors.New(newest), base.Add(61*time.Second), emit)
	if countStrings(got, "err-00") != 2 || countStrings(got, newest) != 1 {
		t.Fatalf("expiry: %#v", got)
	}
}

func countStrings(values []string, want string) int {
	n := 0
	for _, value := range values {
		if value == want {
			n++
		}
	}
	return n
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

	// Exercise real pending work; idle passes deliberately need no writer.
	if _, err = workspace.DB().Exec(`INSERT INTO brief_dirty(card_id) VALUES('pending-card')`); err != nil {
		t.Fatal(err)
	}

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

func TestBriefIdleRefreshDoesNotAcquireWriter(t *testing.T) {
	ctx := context.Background()
	w, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := NewStore(w.DB(), blob.NewFilesystemBackend(w.Layout().ArtifactContentDir), w.Layout().ArtifactContentDir)
	holder, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	warm, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	warm.Close()
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	c, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	if done, err := s.RefreshBriefInitiatives(c, 200); err != nil || !done {
		t.Fatalf("empty brief fold took writer: done=%v err=%v", done, err)
	}
}

func TestBriefIdlePollsBackOff(t *testing.T) {
	ctx := context.Background()
	w, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db, counter := testsql.Open("file:" + w.Layout().DatabasePath)
	defer db.Close()
	s := NewStore(db, blob.NewFilesystemBackend(w.Layout().ArtifactContentDir), w.Layout().ArtifactContentDir)
	counter.Reset()
	runCtx, cancel := context.WithTimeout(ctx, 1600*time.Millisecond)
	defer cancel()
	s.RunBriefMaintenance(runCtx, func(e error) { t.Errorf("maintenance: %v", e) })
	if n := counter.Count(); n > 3 {
		t.Fatalf("empty fold issued %d reads without backing off", n)
	}
}
