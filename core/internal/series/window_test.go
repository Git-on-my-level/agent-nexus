package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDailyRollingWindowExcludesEarlierAndFuturePoints(t *testing.T) {
	for _, hour := range []int{0, 12, 23} {
		t.Run(fmt.Sprint(hour), func(t *testing.T) {
			s, _, _, writer := fixture(t)
			ctx := context.Background()
			now := time.Now().UTC().Truncate(24 * time.Hour).Add(-24*time.Hour + time.Duration(hour)*time.Hour)
			for _, p := range []struct {
				age   time.Duration
				value float64
				zone  int
			}{{-30 * time.Hour, 100, -5}, {-time.Hour, 2, 7}, {time.Minute, 50, 7}} {
				stamp := now.Add(p.age).In(time.FixedZone("offset", p.zone*3600))
				if err := s.Push(ctx, "builds", Point{TS: stamp.Format(time.RFC3339Nano), Value: number(p.value)}, writer, now); err != nil {
					t.Fatal(err)
				}
			}
			for agg, want := range map[string]float64{"sum": 2, "avg": 2, "min": 2, "max": 2, "last": 2, "count": 1} {
				r, err := s.Query(ctx, "builds", nil, 24*time.Hour, 24*time.Hour, agg, now)
				if err != nil || len(r.Streams) != 1 || len(r.Streams[0].Points) != 1 || *r.Streams[0].Points[0].Value != want {
					t.Fatalf("%s must ignore out-of-window points: %#v %v", agg, r, err)
				}
				if r.Streams[0].LastPoint != now.Add(-time.Hour).Format(time.RFC3339Nano) {
					t.Fatalf("future point changed freshness: %#v", r.Streams[0])
				}
				stamp, _ := time.Parse(time.RFC3339Nano, r.Streams[0].Points[0].TS)
				if stamp.Before(now.Add(-24*time.Hour)) || stamp.After(now) {
					t.Fatalf("bucket timestamp outside query: %s", stamp)
				}
				raw, err := s.Query(ctx, "builds", nil, 24*time.Hour, time.Hour, agg, now)
				if err != nil || raw.Resolution != "raw" || len(raw.Streams[0].Points) != 1 || *raw.Streams[0].Points[0].Value != *r.Streams[0].Points[0].Value {
					t.Fatalf("24h panel differs from raw aggregation: %#v %v", raw, err)
				}
			}
		})
	}
}

// A raw-only SQL oracle checks the daily-resolution UTC bucket grid without
// reading either summary table. The WHERE bounds remain the exact rolling
// interval, including nanosecond endpoints. Compare all values and state payloads.
func rawDailyOracle(t *testing.T, s Store, name string, since, end, step int64, buckets int, kind, agg string) []Observation {
	t.Helper()
	rows, err := s.DB.Query(bucketQuery, true, Day, Day, name, "{}", since, end,
		name, "{}", int64(1), int64(0), since/Day*Day, step, buckets-1, name, "{}", name, "{}", Day, Day)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []Observation{}
	for rows.Next() {
		var ts, n int64
		var total, low, high, value sql.NullFloat64
		var state sql.NullString
		if err := rows.Scan(&ts, &n, &total, &low, &high, &value, &state); err != nil {
			t.Fatal(err)
		}
		p := Observation{TS: time.Unix(0, max(ts, since)).UTC().Format(time.RFC3339Nano)}
		switch agg {
		case "avg":
			value = sql.NullFloat64{Float64: total.Float64 / float64(n), Valid: total.Valid}
		case "sum":
			value = total
		case "min":
			value = low
		case "max":
			value = high
		case "count":
			value = sql.NullFloat64{Float64: float64(n), Valid: true}
		}
		if value.Valid {
			v := value.Float64
			p.Value = &v
		}
		if kind == "state" && agg == "last" && state.Valid {
			v := state.String
			p.State = &v
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDailyQueriesEqualRawForRandomRollingWindows(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	rng := rand.New(rand.NewSource(254))
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, name := range []string{"builds", "health"} {
		if _, err := tx.Exec(`INSERT INTO series_labels VALUES(?,'{}')`, name); err != nil {
			t.Fatal(err)
		}
		// Include exact window/UTC-day boundaries and future observations, in
		// addition to random points in the retained history.
		stamps := []time.Time{now, now.Add(time.Nanosecond), now.Add(time.Minute), now.Add(-24 * time.Hour), now.Add(-24*time.Hour - time.Nanosecond), now.Truncate(24 * time.Hour)}
		for i := 0; i < 1500; i++ {
			stamps = append(stamps, now.Add(-time.Duration(rng.Int63n(int64(60*24*time.Hour)))))
		}
		for i, stamp := range stamps {
			var value, state any
			if name == "health" {
				state = fmt.Sprintf("state-%d", i%7)
			} else {
				value = i%41 - 20
			}
			if _, err := tx.Exec(`INSERT INTO series_points VALUES(?,'{}',?,?,?,0)`, name, stamp.UnixNano(), value, state); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		window := 24*time.Hour + time.Duration(rng.Int63n(int64(50*24*time.Hour)))
		step := time.Duration(1+rng.Intn(5))*24*time.Hour + time.Duration(rng.Intn(2))*12*time.Hour
		end := now.Add(-time.Duration(rng.Int63n(int64(12 * time.Hour)))).In(time.FixedZone("offset", []int{7, -5, 0}[i%3]*3600))
		if i < 3 {
			end = now.Truncate(24 * time.Hour).Add(time.Duration(i) * time.Nanosecond)
			window = 24 * time.Hour
		}
		for _, name := range []string{"builds", "health"} {
			kind, aggs := "gauge", []string{"sum", "avg", "min", "max", "last", "count"}
			if name == "health" {
				kind, aggs = "state", []string{"last", "count"}
			}
			for _, agg := range aggs {
				got, err := s.Query(ctx, name, nil, window, step, agg, end)
				if err != nil {
					t.Fatal(err)
				}
				want := rawDailyOracle(t, s, name, end.Add(-window).UnixNano(), end.UnixNano(), step.Nanoseconds(), int((window+step-1)/step), kind, agg)
				if len(got.Streams) != 1 || !reflect.DeepEqual(got.Streams[0].Points, want) {
					t.Fatalf("window %d %s/%s range=%s step=%s end=%s:\ngot=%#v\nwant=%#v", i, name, agg, window, step, end, got.Streams, want)
				}
			}
		}
	}
}

func TestDailyEdgeBudgetBoundsConcentratedBackfill(t *testing.T) {
	root := t.TempDir()
	s, _, _, _ := fixtureAt(t, root)
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	removeRecentRollupMigration(t, s)
	if _, err := s.DB.Exec(`INSERT INTO series_labels VALUES('builds','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<?)
 INSERT INTO series_points SELECT 'builds','{}',?-i,1,NULL,i/? FROM n`, MaxDailyEdgePoints+1000, now.UnixNano(), MaxPointsPerDay); err != nil {
		t.Fatal(err)
	}
	s = reopenStore(t, s, root)
	if _, err := s.Query(context.Background(), "builds", nil, 24*time.Hour, 24*time.Hour, "sum", now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("concentrated backfill must hit edge safety cap: %v", err)
	}
}

func TestDailyEdgesUseCoveringRangeIndex(t *testing.T) {
	s, _, _, _ := fixture(t)
	rows, err := s.DB.Query(`EXPLAIN QUERY PLAN SELECT labels,COUNT(*),SUM(value),MIN(value),MAX(value),MAX(ts)
 FROM series_points WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND ts>=? AND ts<=? GROUP BY labels`, "builds", `["{}"]`, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan string
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
	if !strings.Contains(plan, "COVERING INDEX series_points_query (series=? AND labels=? AND ts>?") || strings.Contains(plan, "TEMP B-TREE FOR GROUP BY") {
		t.Fatalf("edge aggregation must use the covering range index without sorting raw points:\n%s", plan)
	}
}

func TestDailyEdgeIndexUpgradesExistingRollupWorkspace(t *testing.T) {
	root := t.TempDir()
	s, _, _, writer := fixtureAt(t, root)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Push(ctx, "builds", Point{Value: number(7)}, writer, now); err != nil {
		t.Fatal(err)
	}
	// Keep migration 51 and its summaries intact, simulating an already upgraded
	// workspace rather than testing only the pre-rollup backfill path.
	if _, err := s.DB.Exec(`DROP INDEX series_points_query`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM schema_migrations WHERE version=52`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		s = reopenStore(t, s, root)
		var index int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='series_points_query'`).Scan(&index); err != nil || index != 1 {
			t.Fatalf("covering index not installed across restart: %d %v", index, err)
		}
		r, err := s.Query(ctx, "builds", nil, 24*time.Hour, 24*time.Hour, "sum", now)
		if err != nil || len(r.Streams) != 1 || len(r.Streams[0].Points) != 1 || *r.Streams[0].Points[0].Value != 7 {
			t.Fatalf("index migration lost or duplicated data: %#v %v", r, err)
		}
	}
}

func BenchmarkDailyEdgeSafetyCapacity(b *testing.B) {
	root := b.TempDir()
	s, _, _, _ := fixtureAt(b, root)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	removeRecentRollupMigration(b, s)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer tx.Rollback()
	for label := 0; label < MaxLabelSets; label++ {
		l := fmt.Sprintf(`{"slot":"%03d"}`, label)
		if _, err := tx.Exec(`INSERT INTO series_labels VALUES('builds',?)`, l); err != nil {
			b.Fatal(err)
		}
		if _, err := tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?)
 INSERT INTO series_points SELECT 'builds',?,?-i,1,NULL,(?+i)/? FROM n`, MaxDailyEdgePoints/MaxLabelSets, l, now.UnixNano(), label*MaxDailyEdgePoints/MaxLabelSets, MaxPointsPerDay); err != nil {
			b.Fatal(err)
		}
		for d := 1; d <= MaxDailyQueryRows/MaxLabelSets; d++ {
			day := now.Add(-time.Duration(d) * 24 * time.Hour).Truncate(24 * time.Hour).UnixNano()
			if _, err := tx.Exec(`INSERT INTO series_daily VALUES('builds',?,?,1,1,1,1,?,1,NULL)`, l, day, day+int64(time.Hour)); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	s = reopenStore(b, s, root)
	// Use the same recent/archive split as retention maintenance. This benchmark
	// exercises both tables at once, with raw observations only in the edge day.
	if _, err := s.DB.Exec(`INSERT INTO series_live_daily SELECT * FROM series_daily WHERE day>=?`, now.Add(-89*24*time.Hour).Truncate(24*time.Hour).UnixNano()); err != nil {
		b.Fatal(err)
	}
	if _, err := s.DB.Exec(`DELETE FROM series_daily WHERE day>=?`, now.Add(-89*24*time.Hour).Truncate(24*time.Hour).UnixNano()); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result, err := s.Query(ctx, "builds", nil, 200*24*time.Hour+12*time.Hour, 48*time.Hour, "sum", now)
		if err != nil || len(result.Streams) != MaxLabelSets {
			b.Fatalf("combined safety capacity query: %#v %v", result, err)
		}
		var sum float64
		for _, stream := range result.Streams {
			for _, point := range stream.Points {
				sum += *point.Value
			}
		}
		if sum != MaxDailyEdgePoints+MaxDailyQueryRows {
			b.Fatalf("missing or duplicated edge/interior samples: %v", sum)
		}
	}
}
