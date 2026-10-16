package primitives_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestTombstoneHandleProbeStaysIndexedAtHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	const tombstones = 10000
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO resource_access_tombstones(kind, id, ref, owner) VALUES('artifact', ?, ?, 'owner')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < tombstones; i++ {
		if _, err := stmt.ExecContext(ctx, fmt.Sprintf("retired-%d", i), fmt.Sprintf("retired-ref-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_access_tombstones(kind, id, ref, owner) VALUES('artifact', 'reserved', 'Reserved-Handle', 'owner')`); err != nil {
		t.Fatal(err)
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	conn, err := ws.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA automatic_index=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"missing-handle", "reserved-handle"} {
		rows, err := conn.QueryContext(ctx, "EXPLAIN QUERY PLAN "+primitives.TombstoneHandleReservationSQL, "artifact", ref)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "idx_resource_access_tombstones_kind_ref") {
			t.Fatalf("tombstone probe for %q does not use idx_resource_access_tombstones_kind_ref: %s", ref, plan)
		}
		for _, detail := range details {
			if strings.Contains(detail, "resource_access_tombstones") && strings.Contains(detail, "SCAN") {
				t.Fatalf("tombstone probe for %q scans tombstones: %s", ref, plan)
			}
		}
	}

	measured, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	capture.Start()
	for _, ref := range []string{"missing-handle", "reserved-handle"} {
		var taken int
		if err := measured.QueryRowContext(ctx, primitives.TombstoneHandleReservationSQL, "artifact", ref).Scan(&taken); err != nil {
			t.Fatal(err)
		}
		if ref == "reserved-handle" && taken == 0 {
			t.Fatal("case-different tombstone was not reserved")
		}
		if ref == "missing-handle" && taken != 0 {
			t.Fatal("missing handle was reserved")
		}
	}
	_, calls, readRows := capture.Stop()
	if err := capture.WorkError(); err != nil {
		t.Fatal(err)
	}
	work := capture.Work()
	// A kind-prefix primary-key scan of this history is tens of thousands of VM steps per probe.
	if calls != 2 || readRows != 2 || work.FullScanSteps != 0 || work.VMSteps > 256 {
		t.Fatalf("tombstone probes calls=%d rows=%d vm=%d fullscan=%d", calls, readRows, work.VMSteps, work.FullScanSteps)
	}
}
