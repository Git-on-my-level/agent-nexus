package series

import (
	"agent-nexus-core/internal/primitives"
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

func TestPerformanceScopedSeriesRefEndpointBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") == "" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store := primitives.NewTestStore(s.DB, t.TempDir())
	private, e := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private contributor"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.PatchThread(ctx, "owner", fmt.Sprint(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); e != nil {
		t.Fatal(e)
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	for i := 0; i < 100; i++ {
		labels := fmt.Sprintf(`{"stream":"s%03d"}`, i)
		if _, e = tx.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',?)`, labels); e != nil {
			t.Fatal(e)
		}
		for j := 0; j < 100; j++ {
			if _, e = tx.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('builds',?,?,?,0)`, labels, now.Add(-time.Duration(j+1)*time.Minute).UnixNano(), j); e != nil {
				t.Fatal(e)
			}
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var seq int
	var name, path string
	if e = s.DB.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); e != nil {
		t.Fatal(e)
	}
	db, capture, e := perfguard.Open("file:" + path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	scopedStore := primitives.NewTestStore(db, "")
	scoped, closeRead, e := scopedStore.BeginResourceRead(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "human"}))
	if e != nil {
		t.Fatal(e)
	}
	defer closeRead()
	refs := []string{"series:builds?stream=s000"}
	capture.Start()
	previews, e := scopedStore.ResolveRefs(scoped, refs, nil, now)
	statements, queries, rows := capture.Stop()
	work := capture.Work()
	if e != nil || capture.WorkError() != nil || len(previews) != 1 || previews[0].Metric == nil {
		t.Fatalf("error=%v capture=%v previews=%+v", e, capture.WorkError(), previews)
	}
	found := false
	for _, statement := range statements {
		if !strings.Contains(statement.SQL, "FROM series_points") {
			continue
		}
		found = true
		if !strings.Contains(statement.SQL, "series_points AS NOT MATERIALIZED") {
			t.Error("series point privacy shadow must explicitly prohibit full materialization")
		}
		details, e := perfguard.Explain(ctx, s.DB, statement)
		if e != nil {
			t.Fatal(e)
		}
		joined := strings.Join(details, "\n")
		if strings.Contains(joined, "MATERIALIZE series_points") || strings.Contains(joined, "SCAN series_points") || strings.Contains(joined, "SCAN _row") {
			t.Errorf("full point shadow copy/scan:\n%s", joined)
		}
		if strings.Count(joined, "SEARCH _row USING INDEX sqlite_autoindex_series_points_1") < 2 {
			t.Errorf("endpoint seeks missing:\n%s", joined)
		}
	}
	if !found {
		t.Fatal("scoped point query not captured")
	}
	if queries > 8 || rows > 16 || work.VMSteps > 20000 {
		t.Errorf("scoped budget SQL=%d rows=%d VM=%d", queries, rows, work.VMSteps)
	}
	t.Logf("scoped 100-stream/10000-point metric SQL=%d rows=%d VM=%d", queries, rows, work.VMSteps)
}
