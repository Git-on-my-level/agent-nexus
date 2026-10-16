package primitives_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestDocumentHandleReservationStaysIndexedAtHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	const documents = 10000
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO documents(id, handle, title, head_revision_id, head_revision_number, created_at, created_by, updated_at, updated_by) VALUES(?, ?, 'Synthetic', 'head', 1, 'now', 'owner', 'now', 'owner')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < documents; i++ {
		if _, err := stmt.ExecContext(ctx, fmt.Sprintf("doc-%d", i), fmt.Sprintf("doc-handle-%d", i)); err != nil {
			t.Fatal(err)
		}
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
	probes := []struct {
		name  string
		sql   string
		arg   string
		index string
	}{
		{"free handle", primitives.DocumentHandleReservationSQL, "requested-free-handle", "idx_documents_handle_unique"},
		{"taken handle", primitives.DocumentHandleReservationSQL, "doc-handle-1", "idx_documents_handle_unique"},
		{"free id", primitives.DocumentIDReservationSQL, "requested-free-handle", "sqlite_autoindex_documents_1"},
		{"taken id", primitives.DocumentIDReservationSQL, "doc-1", "sqlite_autoindex_documents_1"},
	}
	for _, probe := range probes {
		rows, err := conn.QueryContext(ctx, "EXPLAIN QUERY PLAN "+probe.sql, probe.arg)
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
		if !strings.Contains(plan, probe.index) {
			t.Fatalf("%s probe does not use %s: %s", probe.name, probe.index, plan)
		}
		for _, detail := range details {
			if strings.Contains(detail, "SCAN") && strings.Contains(detail, "documents") {
				t.Fatalf("%s probe scans documents: %s", probe.name, plan)
			}
		}
	}

	measured, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	capture.Start()
	var handleTaken, idTaken int
	if err := measured.QueryRowContext(ctx, primitives.DocumentHandleReservationSQL, "requested-free-handle").Scan(&handleTaken); err != nil {
		t.Fatal(err)
	}
	if err := measured.QueryRowContext(ctx, primitives.DocumentIDReservationSQL, "requested-free-handle").Scan(&idTaken); err != nil {
		t.Fatal(err)
	}
	if handleTaken != 0 || idTaken != 0 {
		t.Fatalf("free handle reserved: handle=%d id=%d", handleTaken, idTaken)
	}
	_, calls, readRows := capture.Stop()
	if err := capture.WorkError(); err != nil {
		t.Fatal(err)
	}
	work := capture.Work()
	// An OR of handle and id scans the documents table: about 50k VM steps at this size.
	if calls != 2 || readRows != 2 || work.FullScanSteps != 0 || work.VMSteps > 256 {
		t.Fatalf("document reservation probes calls=%d rows=%d vm=%d fullscan=%d", calls, readRows, work.VMSteps, work.FullScanSteps)
	}
}
