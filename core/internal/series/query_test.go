package series

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func TestDefaultStepRoundsUpAtRawAndHistoricalBoundaries(t *testing.T) {
	for _, tc := range []struct {
		window time.Duration
		want   time.Duration
	}{
		{time.Second, time.Second},
		{200 * time.Second, time.Second},
		{200*time.Second + time.Nanosecond, time.Second + time.Nanosecond},
		{Retention, Retention / MaxBuckets},
		{Retention + time.Nanosecond, 24 * time.Hour},
		{200 * 24 * time.Hour, 24 * time.Hour},
		{4801 * time.Hour, 48 * time.Hour},
		{400*24*time.Hour + time.Nanosecond, 72 * time.Hour},
		{3650 * 24 * time.Hour, 19 * 24 * time.Hour},
	} {
		t.Run(tc.window.String(), func(t *testing.T) {
			step := DefaultStep(tc.window)
			if step != tc.want || (tc.window+step-1)/step > MaxBuckets {
				t.Fatalf("step=%s want=%s buckets=%d", step, tc.want, (tc.window+step-1)/step)
			}
		})
	}
}

func TestQueryDoesNotCompactAndAgreesAcrossMaintenance(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	for _, def := range []string{"builds", "health"} {
		if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES(?,'{}')`, def); err != nil {
			t.Fatal(err)
		}
		// Compaction also splits the 90-day boundary's UTC day between a rollup
		// and raw rows. Weighted aggregates and latest values must still agree.
		for i, age := range []time.Duration{95 * 24 * time.Hour, 94 * 24 * time.Hour, Retention + time.Hour, Retention - time.Hour, 20 * 24 * time.Hour} {
			var value, state any
			if def == "health" {
				state = fmt.Sprintf("state-%d", i)
			} else {
				value = i + 1
			}
			if _, err := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,value,state,received_day) VALUES(?,'{}',?,?,?,0)`, def, now.Add(-age).UnixNano(), value, state); err != nil {
				t.Fatal(err)
			}
		}
	}
	type query struct {
		series, agg string
		window      time.Duration
	}
	queries := []query{}
	for _, window := range []time.Duration{100 * 24 * time.Hour, Retention} {
		for _, agg := range []string{"sum", "avg", "min", "max", "last", "count"} {
			queries = append(queries, query{"builds", agg, window})
		}
		queries = append(queries, query{"health", "last", window}, query{"health", "count", window})
	}
	before := make([]Result, len(queries))
	for i, q := range queries {
		r, err := s.Query(ctx, q.series, nil, q.window, 2*24*time.Hour, q.agg, now)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = r
	}
	var raw, daily int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_points`).Scan(&raw); err != nil || raw != 10 {
		t.Fatalf("query changed raw rows: %d %v", raw, err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_daily`).Scan(&daily); err != nil || daily != 0 {
		t.Fatalf("query materialized rollups: %d %v", daily, err)
	}
	if err := s.Compact(ctx, now); err != nil {
		t.Fatal(err)
	}
	for i, q := range queries {
		after, err := s.Query(ctx, q.series, nil, q.window, 2*24*time.Hour, q.agg, now)
		if err != nil || !reflect.DeepEqual(before[i], after) {
			t.Fatalf("maintenance changed %s/%s result: before=%#v after=%#v err=%v", q.series, q.agg, before[i], after, err)
		}
	}
}

var pauseFunctionID atomic.Int64

func TestQueryReadSnapshotDoesNotTakeWriteLock(t *testing.T) {
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	function := fmt.Sprintf("series_query_pause_%d", pauseFunctionID.Add(1))
	if err := sqlite.RegisterScalarFunction(function, 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		once.Do(func() { close(paused); <-resume })
		return int64(1), nil
	}); err != nil {
		t.Fatal(err)
	}
	s, _, _, writer := fixture(t)
	now := time.Now().UTC()
	if err := s.Push(context.Background(), "builds", Point{Value: number(7)}, writer, now); err != nil {
		t.Fatal(err)
	}
	// Pause the actual query after its read snapshot is established. A view keeps
	// the production query unchanged and injects no production test hooks.
	if _, err := s.DB.Exec(`ALTER TABLE series_points RENAME TO query_test_points`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`CREATE VIEW series_points AS SELECT * FROM query_test_points WHERE ` + function + `(ts)=1`); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	var release sync.Once
	unblock := func() { release.Do(func() { close(resume) }) }
	collected := false
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		r, err := s.Query(ctx, "builds", nil, time.Hour, time.Minute, "sum", now)
		done <- outcome{r, err}
	}()
	defer func() {
		unblock()
		if !collected {
			<-done
		}
	}()
	select {
	case <-paused:
	case <-ctx.Done():
		t.Fatal("query did not reach its read snapshot")
	}
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA busy_timeout=100`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil) // The normal immediate mutation transaction.
	if err != nil {
		t.Fatalf("dashboard query took the write lock: %v", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE actors SET display_name='Human mutation' WHERE id='human'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO query_test_points(series,labels,ts,value,received_day) VALUES('builds','{}',?,99,0)`, now.Add(-time.Second).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatalf("human mutation could not commit during query: %v", err)
	}
	// Release and inspect the reader after the independent mutation committed.
	unblock()
	got := <-done
	collected = true
	if got.err != nil || len(got.result.Streams) != 1 || len(got.result.Streams[0].Points) != 1 || *got.result.Streams[0].Points[0].Value != 7 {
		t.Fatalf("query lost its consistent snapshot: %#v %v", got.result, got.err)
	}
}

func TestBucketQueryUsesRangeAndLastValueIndexes(t *testing.T) {
	s, _, _, _ := fixture(t)
	now := time.Now().UTC().UnixNano()
	rows, err := s.DB.Query(`EXPLAIN QUERY PLAN `+bucketQuery, false, Day, Day, "builds", "{}", now-Day, now, "builds", "{}", now-Day, now, now-Day, Day, 199, "builds", "{}", "builds", "{}", Day, Day)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan, "CORRELATED") || !strings.Contains(plan, "series=? AND labels=? AND ts>?") || !strings.Contains(plan, "series=? AND labels=? AND ts=?") || !strings.Contains(plan, "series=? AND labels=? AND day=?") {
		t.Fatalf("aggregation must scan the indexed range once and use indexed last lookups:\n%s", plan)
	}
}

func BenchmarkQueryCapacity(b *testing.B) {
	// A rolling 90-day window can span 91 UTC ingestion days. Seed the full
	// 100,000/day budget, 1,000 series and up to 100 label sets; time only queries.
	for _, tc := range []struct{ points, labels int }{{MaxPointsPerDay, 1}, {91 * MaxPointsPerDay, 1}, {91 * MaxPointsPerDay, MaxLabelSets}} {
		b.Run(fmt.Sprintf("points-%d-labels-%d", tc.points, tc.labels), func(b *testing.B) {
			root := b.TempDir()
			s, human, _, _ := fixtureAt(b, root)
			ctx := context.Background()
			// Midday forces both partial edges to use raw observations, while the
			// fully covered interior stays on the rollup path.
			now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
			for first := 2; first < MaxSeries; first += 100 {
				defs := []Definition{}
				for i := first; i < first+100 && i < MaxSeries; i++ {
					defs = append(defs, Definition{Name: fmt.Sprintf("s%d", i), Kind: "gauge", Unit: "items"})
				}
				if _, err := s.Declare(ctx, Declaration{Name: fmt.Sprintf("capacity-%d", first), Description: "Capacity benchmark", AgentID: "owner", ExpectedInterval: "1m", Series: defs}, human); err != nil {
					b.Fatal(err)
				}
			}
			// Bulk-load a v50 workspace, then run the real upgrade/backfill. Setup
			// and index creation stay outside timing; no synthetic summary cache.
			removeRecentRollupMigration(b, s)
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer tx.Rollback()
			perLabel := tc.points / tc.labels
			spacing := Retention.Nanoseconds() / int64(perLabel-1)
			for label := 0; label < tc.labels; label++ {
				labels := "{}"
				if tc.labels > 1 {
					data, _ := json.Marshal(map[string]string{"slot": fmt.Sprint(label)})
					labels = string(data)
				}
				if _, err := tx.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',?)`, labels); err != nil {
					b.Fatal(err)
				}
				if _, err := tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?) INSERT INTO series_points(series,labels,ts,value,received_day) SELECT 'builds',?,?+i*?,i%100,?+(i*?+?)/? FROM n`, perLabel, labels, now.Add(-Retention).UnixNano(), spacing, now.Add(-Retention).UnixNano()/Day, tc.labels, label, MaxPointsPerDay); err != nil {
					b.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				b.Fatal(err)
			}
			s = reopenStore(b, s, root)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := s.Query(ctx, "builds", nil, Retention, DefaultStep(Retention), "sum", now)
				if err != nil || len(r.Streams) != tc.labels || r.Resolution != "daily" {
					b.Fatalf("capacity query: %d streams %v", len(r.Streams), err)
				}
				var count int
				for _, stream := range r.Streams {
					if len(stream.Points) > MaxBuckets {
						b.Fatal("unbounded capacity result")
					}
					count += len(stream.Points)
				}
				b.ReportMetric(float64(count), "buckets/op")
			}
		})
	}
}
