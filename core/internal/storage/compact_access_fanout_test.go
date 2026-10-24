package storage

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestCompactAccessDenseFanoutBudget(t *testing.T) {
	w := compactFixture(t, 4096)
	ctx := context.Background()
	compactExec(t, w, `UPDATE resource_access_edges SET target_ref='document:dense'`)
	proof := compactCertify(t, w)
	if err := cutoverForTest(t, w, ctx, proof); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open(sqliteDSN(w.Layout().DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := strings.Join(resourceaccess.ExactEdgeTerms(resourceaccess.SelectedLedger, "(SELECT 1) d", "", resourceaccess.CompactReaderSQL, "1", []string{resourceaccess.AtomKeySQL("'document:dense'")}), " UNION ALL ")
	var baseline uint64
	for _, unrelated := range []int{0, 8192} {
		if unrelated > 0 {
			compactExec(t, w, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) WITH RECURSIVE s(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM s WHERE n<8192) SELECT 'card','unrelated-'||n,'document:unrelated-'||n FROM s`)
		}
		capture.Start()
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rows.Next() {
			var kind, id string
			if err = rows.Scan(&kind, &id); err != nil {
				t.Fatal(err)
			}
			if kind != "card" || !strings.HasPrefix(id, "source-") {
				t.Fatal("wrong compact source")
			}
			n++
		}
		err = rows.Err()
		rows.Close()
		_, queries, returned := capture.Stop()
		work := capture.Work()
		if err != nil || capture.WorkError() != nil {
			t.Fatal(err, capture.WorkError())
		}
		t.Logf("fanout=4096 unrelated=%d queries=%d rows=%d work=%+v", unrelated, queries, returned, work)
		if n != 4096 || queries != 1 || returned != 4096 || work.FullScanSteps != 0 || work.Sorts != 0 || work.AutoIndexRows != 0 || work.VMSteps > 4096*32+500 {
			t.Fatalf("fanout cost unbounded: %+v", work)
		}
		if unrelated == 0 {
			baseline = work.VMSteps
		} else if work.VMSteps > baseline+32 {
			t.Fatalf("unrelated workspace grew probe: %d -> %d", baseline, work.VMSteps)
		}
	}
}
