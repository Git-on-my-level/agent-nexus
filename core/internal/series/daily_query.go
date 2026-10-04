package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// At most 100 label rows, with two primary-key seeks per row. Clamp freshness to
// now as well: ingestion permits a little future skew, which is not visible yet.
func lastPoints(ctx context.Context, tx *sql.Tx, name string, labels []string, end int64) (map[string]sql.NullInt64, error) {
	encoded, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.labels,
 (SELECT ts FROM series_points WHERE series=l.series AND labels=l.labels AND ts<=? ORDER BY ts DESC LIMIT 1),
 (SELECT last_ts FROM series_daily WHERE series=l.series AND labels=l.labels AND day<=? AND last_ts<=? ORDER BY day DESC LIMIT 1)
 FROM series_labels l WHERE l.series=? AND l.labels IN (SELECT value FROM json_each(?))`, end, end/Day*Day, end, name, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]sql.NullInt64, len(labels))
	for rows.Next() {
		var label string
		var live, archived sql.NullInt64
		if err := rows.Scan(&label, &live, &archived); err != nil {
			return nil, err
		}
		if archived.Valid && (!live.Valid || archived.Int64 > live.Int64) {
			live = archived
		}
		out[label] = live
	}
	return out, rows.Err()
}

// The interval is inclusive at both ends. Only fully covered UTC days can use
// summaries; the remaining (at most two) partial days must use exact raw points.
func completeDays(since, end int64) (first, until int64) {
	first = since / Day * Day
	if first < since {
		first += Day
	}
	until = end / Day * Day
	if end-until == Day-1 {
		until += Day
	}
	return first, until
}

// Each edge is a single UTC day. Grouping by indexed labels, without sorting
// individual points by a day expression, keeps the maximum edge scan cheap.
// A budget+1 preflight in the same read snapshot bounds both scans together.
const edgeBudgetQuery = `SELECT COUNT(*) FROM (
 SELECT 1 FROM series_points WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND ts>=? AND ts<=?
 UNION ALL SELECT 1 FROM series_points WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND ts>=? AND ts<=? LIMIT ?)`

const dailyQuery = `WITH edges AS (
 SELECT labels,? day,COUNT(*) n,SUM(value) total,MIN(value) low,MAX(value) high,MAX(ts) last_ts
 FROM series_points WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND ts>=? AND ts<=? GROUP BY labels
 UNION ALL SELECT labels,? day,COUNT(*) n,SUM(value) total,MIN(value) low,MAX(value) high,MAX(ts) last_ts
 FROM series_points WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND ts>=? AND ts<=? GROUP BY labels
)
SELECT e.labels,e.day,e.n,e.total,e.low,e.high,e.last_ts,p.value,p.state
 FROM edges e CROSS JOIN series_points p ON p.series=? AND p.labels=e.labels AND p.ts=e.last_ts
 UNION ALL SELECT labels,day,n,total,low,high,last_ts,last_value,last_state
 FROM series_live_daily WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND day>=? AND day<?
 UNION ALL SELECT labels,day,n,total,low,high,last_ts,last_value,last_state
 FROM series_daily WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND day>=? AND day<?
 ORDER BY labels,day`

func dailyPoints(ctx context.Context, tx *sql.Tx, name string, labels []string, since, end, step int64, maxBucket int, kind, agg string) (map[string][]Observation, error) {
	encoded, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	first, until := completeDays(since, end)
	leftEnd := min(end, first-1)
	rightStart := max(first, until)
	var edgePoints int
	err = tx.QueryRowContext(ctx, edgeBudgetQuery, name, string(encoded), since, leftEnd,
		name, string(encoded), rightStart, end, MaxDailyEdgePoints+1).Scan(&edgePoints)
	if err != nil {
		return nil, err
	}
	if edgePoints > MaxDailyEdgePoints {
		return nil, fmt.Errorf("%w: daily query edges exceed 200000 raw observations; narrow the range or labels", ErrCapacity)
	}
	rows, err := tx.QueryContext(ctx, dailyQuery,
		since/Day*Day, name, string(encoded), since, leftEnd, end/Day*Day, name, string(encoded), rightStart, end,
		name, name, string(encoded), first, until, name, string(encoded), first, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]Observation, len(labels))
	type bucket struct {
		label                   string
		index                   int
		ts, n, last             int64
		total, low, high, value sql.NullFloat64
		state                   sql.NullString
	}
	var b bucket
	flush := func() {
		if b.n == 0 {
			return
		}
		p := Observation{TS: time.Unix(0, b.ts).UTC().Format(time.RFC3339Nano)}
		value := b.value
		switch agg {
		case "avg":
			value = sql.NullFloat64{Float64: b.total.Float64 / float64(b.n), Valid: b.total.Valid}
		case "sum":
			value = b.total
		case "min":
			value = b.low
		case "max":
			value = b.high
		case "count":
			value = sql.NullFloat64{Float64: float64(b.n), Valid: true}
		}
		if value.Valid {
			v := value.Float64
			p.Value = &v
		}
		if kind == "state" && agg == "last" && b.state.Valid {
			v := b.state.String
			p.State = &v
		}
		out[b.label] = append(out[b.label], p)
	}
	var label string
	var day, n, last int64
	var total, low, high, value sql.NullFloat64
	var state sql.NullString
	grid := since / Day * Day
	for rows.Next() {
		if err := rows.Scan(&label, &day, &n, &total, &low, &high, &last, &value, &state); err != nil {
			return nil, err
		}
		index := min(int((day-grid)/step), maxBucket)
		if b.n > 0 && (label != b.label || index != b.index) {
			flush()
			b = bucket{}
		}
		if b.n == 0 {
			b.label, b.index, b.ts = label, index, max(day, since)
			if out[label] == nil {
				out[label] = make([]Observation, 0, maxBucket+1)
			}
		}
		if b.n == 0 || last > b.last {
			b.last, b.value, b.state = last, value, state
		}
		b.n += n
		if total.Valid {
			b.total.Float64 += total.Float64
			b.total.Valid = true
		}
		if low.Valid && (!b.low.Valid || low.Float64 < b.low.Float64) {
			b.low = low
		}
		if high.Valid && (!b.high.Valid || high.Float64 > b.high.Float64) {
			b.high = high
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	flush()
	return out, nil
}
