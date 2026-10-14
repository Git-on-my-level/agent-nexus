package series

import (
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/seriesref"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPerformanceSeriesRefEndpointBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") == "" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 100; i++ {
		labels := fmt.Sprintf(`{"stream":"s%03d"}`, i)
		if _, err = tx.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',?)`, labels); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 100; j++ {
			if _, err = tx.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('builds',?,?,?,0)`, labels, now.Add(-time.Duration(j+1)*time.Minute).UnixNano(), j); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err = s.DB.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	refs := []seriesref.Ref{}
	for i := 0; i < 4; i++ {
		r, _ := seriesref.ParseRef(fmt.Sprintf("series:builds?stream=s%03d", i))
		refs = append(refs, r)
	}
	capture.Start()
	metrics, err := seriesref.CompareRefs(ctx, resourceaccess.NewDB(db), refs, now)
	statements, queries, rows := capture.Stop()
	work := capture.Work()
	if err != nil || capture.WorkError() != nil || len(metrics) != 4 || queries != 1 || rows != 4 || work.VMSteps > 20000 {
		t.Fatalf("err=%v instrumentation=%v metrics=%d SQL=%d rows=%d VM=%d", err, capture.WorkError(), len(metrics), queries, rows, work.VMSteps)
	}
	for _, statement := range statements {
		details, e := perfguard.Explain(ctx, s.DB, statement)
		if e != nil {
			t.Fatal(e)
		}
		joined := strings.Join(details, "\n")
		if strings.Contains(joined, "SCAN series_points") || !strings.Contains(joined, "SEARCH series_points USING INDEX sqlite_autoindex_series_points_1") {
			t.Fatal(joined)
		}
	}
	t.Logf("dense 100-stream/10000-point metric batch SQL=%d rows=%d VM=%d", queries, rows, work.VMSteps)
}
