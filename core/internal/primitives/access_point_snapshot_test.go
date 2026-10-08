package primitives

import (
	"context"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestPointSnapshotMissingAndExpiredProofDenies(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	snapshot := &denialSnapshot{rows: `[["card","private"],["card","private\u0000tail"],["work_evidence_alias",42],[null,null]]`}
	snapshot.preparePointIndex()
	token := pointSnapshotSequence.Add(1)
	pointSnapshots.Store(token, snapshot)
	defer pointSnapshots.Delete(token)
	check := func(key any, kind any, id any, want int) {
		t.Helper()
		var got int
		if err := ws.DB().QueryRowContext(ctx, `SELECT anx_point_snapshot_denied(?,?,CAST(? AS BLOB))`, key, kind, id).Scan(&got); err != nil || got != want {
			t.Fatalf("got=%d want=%d error=%v", got, want, err)
		}
	}
	check(token, "card", "private", 1)
	check(token, "card", "private\x00tail", 1)
	check(token, "work_evidence_alias", int64(42), 1)
	check(token, "work_evidence_alias", int64(43), 0)
	check(token, "card", "public", 0)
	check(token, "card", nil, 1)
	check(nil, "card", "public", 1)
	check(int64(-1), "card", "public", 1)
	pointSnapshots.Delete(token)
	check(token, "card", "public", 1)
}

func TestPointSnapshotInvalidUTF8RetainsCanonicalFallback(t *testing.T) {
	snapshot := &denialSnapshot{rows: "[[\"card\",\"private\xff\"]]"}
	snapshot.preparePointIndex()
	if snapshot.pointIndex != nil {
		t.Fatal("lossy UTF-8 proof admitted")
	}
}
