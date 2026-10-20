package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type compactDriverCode int

func (compactDriverCode) Error() string { return "driver-secret SQLITE_INTERRUPT" }
func (e compactDriverCode) Code() int   { return int(e) }

func TestCompactMigrationDiagnosticsAreSafeAndClassified(t *testing.T) {
	for _, tc := range []struct {
		stage  string
		cause  error
		reason string
		code   int
	}{
		{"shadow_install", context.DeadlineExceeded, "deadline_exceeded", 0},
		{"shadow_install", errors.Join(context.DeadlineExceeded, compactDriverCode(9)), "deadline_exceeded", 9},
		{"compact_backfill", errors.Join(context.DeadlineExceeded, compactDriverCode(5)), "deadline_exceeded", 5},
		{"verification", ErrCompactAccessMismatch, "ledger_mismatch_requires_offline_verification", 0},
		{"cutover", ErrCompactAccessChanged, "ledger_changed_retry", 0},
		{"shadow_install", errors.New("SQL contains private-sentinel"), "unexpected_error", 0},
		{"shadow_install", compactDriverCode(5), "write_contention_retry", 5},
	} {
		err := compactMigrationError(tc.stage, tc.cause)
		var diagnostic *CompactAccessMigrationError
		if !errors.As(err, &diagnostic) || diagnostic.Stage != tc.stage || diagnostic.Reason != tc.reason || diagnostic.SQLiteCode != tc.code || !errors.Is(err, tc.cause) {
			t.Fatalf("wrong diagnostic: %+v", diagnostic)
		}
		if strings.Contains(err.Error(), "private-sentinel") || strings.Contains(err.Error(), "driver-secret") {
			t.Fatal("driver contents leaked")
		}
	}
}

func TestCompactMigrationDeadlineJoinKeepsSQLiteCode(t *testing.T) {
	// Maintenance chunks are no longer cancelled, but a caller deadline joined
	// with a driver error must still keep the SQLite code.
	err := errors.Join(context.DeadlineExceeded, compactDriverCode(9))
	diagnostic := compactMigrationError("compact_backfill", err).(*CompactAccessMigrationError)
	if diagnostic.Reason != "deadline_exceeded" || diagnostic.SQLiteCode != 9 || strings.Contains(diagnostic.Error(), "driver-secret") {
		t.Fatalf("lost driver code: %+v error=%q", diagnostic, diagnostic.Error())
	}
	diagnostic.Reason = "budget_exhausted_" + diagnostic.Reason
	if diagnostic.Reason != "budget_exhausted_deadline_exceeded" || diagnostic.SQLiteCode != 9 {
		t.Fatalf("budget prefix dropped code: %+v", diagnostic)
	}
}

func TestCompactWorkerReportsLedgerDirectionWithoutData(t *testing.T) {
	w := compactFixture(t, 3)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	compactFinish(t, w)
	compactExec(t, w, `UPDATE resource_access_compact_edges SET target_key=zeroblob(32) WHERE edge_id=1`)
	var reported error
	w.RunCompactAccessMigration(ctx, func(_ CompactAccessVerification, err error) { reported = err })
	var diagnostic *CompactAccessMigrationError
	if !errors.As(reported, &diagnostic) || diagnostic.Stage != "verification" || diagnostic.Reason != "exact_to_compact_mismatch_requires_offline_verification" {
		t.Fatalf("unexpected real worker diagnostic: %v", reported)
	}
	if !errors.Is(reported, ErrCompactAccessMismatch) {
		t.Fatal("mismatch cause lost")
	}
}
