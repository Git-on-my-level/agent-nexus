package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCompactMigrationDiagnosticsAreSafeAndClassified(t *testing.T) {
	for _, tc := range []struct {
		stage  string
		cause  error
		reason string
	}{
		{"shadow_install", context.DeadlineExceeded, "deadline_exceeded"},
		{"verification", ErrCompactAccessMismatch, "ledger_mismatch_requires_offline_verification"},
		{"cutover", ErrCompactAccessChanged, "ledger_changed_retry"},
		{"shadow_install", errors.New("SQL contains private-sentinel"), "unexpected_error"},
	} {
		err := compactMigrationError(tc.stage, tc.cause)
		var diagnostic *CompactAccessMigrationError
		if !errors.As(err, &diagnostic) || diagnostic.Stage != tc.stage || diagnostic.Reason != tc.reason || !errors.Is(err, tc.cause) {
			t.Fatalf("wrong diagnostic: %v", err)
		}
		if strings.Contains(err.Error(), "private-sentinel") {
			t.Fatal("driver contents leaked")
		}
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
