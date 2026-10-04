package series

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func removeRecentRollupMigration(t testing.TB, s Store) {
	t.Helper()
	for _, stmt := range []string{
		`DROP INDEX series_points_query`, `DELETE FROM schema_migrations WHERE version=52`,
		`DROP TRIGGER series_points_daily_insert`, `DROP TRIGGER series_points_daily_update`,
		`DROP INDEX series_points_daily_value`, `DROP TABLE series_live_daily`,
		`DELETE FROM schema_migrations WHERE version=51`,
	} {
		if _, err := s.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDailyMultiDayBucketsPreserveWeightedAggregatesAndLabels(t *testing.T) {
	s, _, _, writer := fixture(t)
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)
	now := day
	for _, label := range []string{`{"cohort":"a"}`, `{"cohort":"b"}`} {
		if _, err := s.DB.Exec(`INSERT INTO series_labels VALUES('builds',?)`, label); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		table, label           string
		days, n                int
		total, low, high, last float64
	}{
		{"series_daily", `{"cohort":"a"}`, -4, 2, 6, 2, 4, 4},
		{"series_live_daily", `{"cohort":"a"}`, -4, 1, 6, 6, 6, 6},
		{"series_live_daily", `{"cohort":"a"}`, -3, 1, 10, 10, 10, 10},
		{"series_live_daily", `{"cohort":"a"}`, -2, 1, 20, 20, 20, 20},
		{"series_live_daily", `{"cohort":"b"}`, -3, 1, 7, 7, 7, 7},
	} {
		d := day.Add(time.Duration(row.days) * 24 * time.Hour).UnixNano()
		if _, err := s.DB.Exec(`INSERT INTO `+row.table+` VALUES('builds',?,?,?,?,?,?,?,?,NULL)`, row.label, d, row.n, row.total, row.low, row.high, d+int64(time.Hour), row.last); err != nil {
			t.Fatal(err)
		}
	}
	for agg, want := range map[string]float64{"sum": 22, "avg": 5.5, "min": 2, "max": 10, "count": 4, "last": 10} {
		result, err := s.Query(ctx, "builds", nil, 4*24*time.Hour, 2*24*time.Hour, agg, now)
		if err != nil {
			t.Fatal(err)
		}
		second := float64(7)
		if agg == "count" {
			second = 1
		}
		if len(result.Streams) != 2 || result.Streams[0].Labels["cohort"] != "a" || len(result.Streams[0].Points) != 2 || *result.Streams[0].Points[0].Value != want || len(result.Streams[1].Points) != 1 || *result.Streams[1].Points[0].Value != second {
			t.Fatalf("%s: %#v", agg, result.Streams)
		}
	}
	for i, state := range []string{"old", "new"} {
		if err := s.Push(ctx, "health", Point{State: &state, TS: day.Add(time.Duration(i-3) * 24 * time.Hour).Format(time.RFC3339Nano)}, writer, now); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Query(ctx, "health", nil, 4*24*time.Hour, 24*time.Hour, "last", now)
	if err != nil || len(result.Streams) != 1 || len(result.Streams[0].Points) != 2 || *result.Streams[0].Points[0].State != "old" || *result.Streams[0].Points[1].State != "new" {
		t.Fatalf("state bucket payloads: %#v %v", result, err)
	}
}

func BenchmarkDailyQuerySafetyCapacity(b *testing.B) {
	s, _, _, _ := fixtureAt(b, b.TempDir())
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < MaxLabelSets; i++ {
		label := fmt.Sprintf(`{"cohort":"%03d"}`, i)
		if _, err := tx.Exec(`INSERT INTO series_labels VALUES('builds',?)`, label); err != nil {
			b.Fatal(err)
		}
		for d := 0; d < MaxDailyQueryRows/MaxLabelSets; d++ {
			day := now.Add(-time.Duration(d+1) * 24 * time.Hour).Truncate(24 * time.Hour).UnixNano()
			table := "series_live_daily"
			if d >= 90 {
				table = "series_daily"
			}
			if _, err := tx.Exec(`INSERT INTO `+table+` VALUES('builds',?,?,455,455,1,1,?,1,NULL)`, label, day, day+int64(time.Hour)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	for _, agg := range []string{"sum", "last"} {
		b.Run(agg, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result, err := s.Query(ctx, "builds", nil, 200*24*time.Hour, 24*time.Hour, agg, now)
				if err != nil || result.Resolution != "daily" || len(result.Streams) != MaxLabelSets {
					b.Fatalf("capacity query: %d %v", len(result.Streams), err)
				}
				buckets := 0
				for _, stream := range result.Streams {
					buckets += len(stream.Points)
				}
				if buckets != MaxDailyQueryRows {
					b.Fatalf("missing capacity buckets: %d", buckets)
				}
			}
		})
	}
}

func BenchmarkRawQuerySafetyCapacity(b *testing.B) {
	s, _, _, _ := fixtureAt(b, b.TempDir())
	ctx := context.Background()
	now := time.Now().UTC()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < MaxLabelSets; i++ {
		if _, err := tx.Exec(`INSERT INTO series_labels VALUES('builds',?)`, fmt.Sprintf(`{"cohort":"%03d"}`, i)); err != nil {
			b.Fatal(err)
		}
	}
	for i := 0; i < MaxRawQueryPoints; i++ {
		label := fmt.Sprintf(`{"cohort":"%03d"}`, i%MaxLabelSets)
		ts := now.Add(-time.Hour + time.Duration(i/MaxLabelSets)*time.Minute).UnixNano()
		if _, err := tx.Exec(`INSERT INTO series_points VALUES('builds',?,?,1,NULL,0)`, label, ts); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := s.Query(ctx, "builds", nil, time.Hour, DefaultStep(time.Hour), "sum", now)
		if err != nil || result.Resolution != "raw" || len(result.Streams) != MaxLabelSets {
			b.Fatalf("raw capacity query: %d %v", len(result.Streams), err)
		}
		buckets := 0
		for _, stream := range result.Streams {
			buckets += len(stream.Points)
		}
		if buckets != MaxRawQueryPoints {
			b.Fatalf("missing raw capacity buckets: %d", buckets)
		}
	}
}

func TestDailySummaryCorrectionsAndRetriesRemainExact(t *testing.T) {
	s, _, _, writer := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	timestamps := []time.Time{now.Add(-time.Hour), now.Add(-50 * time.Minute), now.Add(-40 * time.Minute)}
	for _, p := range []struct {
		slot  int
		value float64
	}{{0, 1}, {1, 4}, {2, 2}, {1, 0}, {0, 6}, {1, 3}, {2, 8}, {2, 8}} {
		if err := s.Push(ctx, "builds", Point{TS: timestamps[p.slot].Format(time.RFC3339Nano), Value: number(p.value)}, writer, now); err != nil {
			t.Fatal(err)
		}
	}
	for agg, want := range map[string]float64{"sum": 17, "avg": 17.0 / 3, "min": 3, "max": 8, "count": 3, "last": 8} {
		r, err := s.Query(ctx, "builds", nil, 24*time.Hour, 24*time.Hour, agg, now)
		if err != nil || r.Resolution != "daily" || len(r.Streams) != 1 || len(r.Streams[0].Points) != 1 || *r.Streams[0].Points[0].Value != want {
			t.Fatalf("daily %s=%v: %#v %v", agg, want, r, err)
		}
	}
	for _, p := range []struct {
		slot  int
		state string
	}{{0, "starting"}, {1, "healthy"}, {1, "degraded"}, {1, "degraded"}} {
		if err := s.Push(ctx, "health", Point{TS: timestamps[p.slot].Format(time.RFC3339Nano), State: &p.state}, writer, now); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Query(ctx, "health", nil, 24*time.Hour, 24*time.Hour, "last", now)
	if err != nil || *r.Streams[0].Points[0].State != "degraded" {
		t.Fatalf("daily state correction: %#v %v", r, err)
	}
	r, err = s.Query(ctx, "health", nil, 24*time.Hour, 24*time.Hour, "count", now)
	if err != nil || *r.Streams[0].Points[0].Value != 2 {
		t.Fatalf("daily state dedupe: %#v %v", r, err)
	}
}

func TestDailySummaryMigrationAndRestartPreserveArchivedData(t *testing.T) {
	root := t.TempDir()
	s, _, _, writer := fixtureAt(t, root)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Push(ctx, "builds", Point{TS: now.Add(-89 * 24 * time.Hour).Format(time.RFC3339Nano), Value: number(2)}, writer, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(5)}, writer, now); err != nil {
		t.Fatal(err)
	}
	future := now.Add(3 * 24 * time.Hour)
	if err := s.Compact(ctx, future); err != nil {
		t.Fatal(err)
	}
	removeRecentRollupMigration(t, s)
	for i := 0; i < 2; i++ {
		s = reopenStore(t, s, root)
		r, err := s.Query(ctx, "builds", nil, 100*24*time.Hour, 100*24*time.Hour, "sum", future)
		if err != nil || *r.Streams[0].Points[0].Value != 7 {
			t.Fatalf("migration/restart lost or duplicated data: %#v %v", r, err)
		}
	}
}

func TestDailyAndAdaptiveQueriesAvoidRawAggregation(t *testing.T) {
	var interiorReads atomic.Int64
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	interiorEnd := now.Add(-24 * time.Hour).UnixNano()
	fn := fmt.Sprintf("series_read_budget_%d", pauseFunctionID.Add(1))
	if err := sqlite.RegisterScalarFunction(fn, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0].(int64) < interiorEnd && interiorReads.Add(1) > MaxRawQueryPoints+1 {
			return nil, errors.New("unbounded raw aggregation")
		}
		return int64(1), nil
	}); err != nil {
		t.Fatal(err)
	}
	s, _, _, _ := fixture(t)
	if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4999) INSERT INTO series_points(series,labels,ts,value,received_day) SELECT 'builds','{}',?-i*1000000,1,0 FROM n`, now.Add(-48*time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<9) INSERT INTO series_points(series,labels,ts,value,received_day) SELECT 'builds','{}',?-i*1000000,1,0 FROM n`, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`ALTER TABLE series_points RENAME TO budget_test_points`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`CREATE VIEW series_points AS SELECT * FROM budget_test_points WHERE ` + fn + `(ts)=1`); err != nil {
		t.Fatal(err)
	}
	for _, step := range []time.Duration{24 * time.Hour, DefaultStep(Retention)} {
		interiorReads.Store(0)
		r, err := s.Query(context.Background(), "builds", nil, Retention, step, "sum", now)
		if err != nil || r.Resolution != "daily" || *r.Streams[0].Points[0].Value != 5000 || *r.Streams[0].Points[1].Value != 10 {
			t.Fatalf("rollup path failed: %#v %v", r, err)
		}
		if step >= 24*time.Hour && interiorReads.Load() != 0 {
			t.Fatalf("daily query touched interior raw points: %d", interiorReads.Load())
		}
	}
	interiorReads.Store(0)
	if _, err := s.Query(context.Background(), "builds", nil, time.Hour, time.Minute, "sum", now.Add(-48*time.Hour)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("dense short query must be capped: %v", err)
	}
}

func TestDailyQuerySummaryBudgetIsBounded(t *testing.T) {
	s, _, _, _ := fixture(t)
	now := time.Now().UTC()
	day := now.UnixNano() / Day * Day
	for label := 0; label < 7; label++ {
		l := fmt.Sprintf(`{"slot":"%d"}`, label)
		if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',?)`, l); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DB.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<3649) INSERT INTO series_daily(series,labels,day,n,total,low,high,last_ts,last_value) SELECT 'builds',?,?-i*?,1,1,1,1,?-i*?,1 FROM n`, l, day, Day, day+1, Day); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Query(context.Background(), "builds", nil, 3650*24*time.Hour, 19*24*time.Hour, "sum", now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("unbounded daily summary query: %v", err)
	}
	if _, err := s.Query(context.Background(), "builds", map[string]string{"slot": "0"}, 3650*24*time.Hour, 19*24*time.Hour, "sum", now); err != nil {
		t.Fatalf("bounded filtered history should work: %v", err)
	}
}

func TestDailyCorrectionExtremaUseValueIndex(t *testing.T) {
	s, _, _, _ := fixture(t)
	rows, err := s.DB.Query(`EXPLAIN QUERY PLAN SELECT value FROM series_points WHERE series=? AND labels=? AND (ts/86400000000000)*86400000000000=? AND value IS NOT NULL ORDER BY value LIMIT 1`, "builds", "{}", int64(0))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "series_points_daily_value") {
			indexed = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !indexed {
		t.Fatal("correction must seek extrema through the daily value index")
	}
}
