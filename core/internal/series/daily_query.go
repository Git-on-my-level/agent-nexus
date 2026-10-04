package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// At most 100 label rows, with two primary-key seeks per row. No permanent
// history scan and no statement allocation per label set.
func lastPoints(ctx context.Context, tx *sql.Tx, name string, labels []string) (map[string]sql.NullInt64, error) {
	encoded, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT l.labels,
 (SELECT last_ts FROM series_live_daily WHERE series=l.series AND labels=l.labels ORDER BY day DESC LIMIT 1),
 (SELECT last_ts FROM series_daily WHERE series=l.series AND labels=l.labels ORDER BY day DESC LIMIT 1)
 FROM series_labels l WHERE l.series=? AND l.labels IN (SELECT value FROM json_each(?))`, name, string(encoded))
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

// Scan the bounded summaries once, including their last payloads. Aggregating
// here avoids repeated SQL compilation and two payload joins per output bucket.
func dailyPoints(ctx context.Context, tx *sql.Tx, name string, labels []string, since, end, step int64, maxBucket int, kind, agg string) (map[string][]Observation, error) {
	encoded, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT labels,day,n,total,low,high,last_ts,last_value,last_state
 FROM series_live_daily WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND day>=? AND day<=?
 UNION ALL SELECT labels,day,n,total,low,high,last_ts,last_value,last_state
 FROM series_daily WHERE series=? AND labels IN (SELECT value FROM json_each(?)) AND day>=? AND day<=?
 ORDER BY labels,day`, name, string(encoded), since, end, name, string(encoded), since, end)
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
	for rows.Next() {
		if err := rows.Scan(&label, &day, &n, &total, &low, &high, &last, &value, &state); err != nil {
			return nil, err
		}
		index := min(int((day-since)/step), maxBucket)
		if b.n > 0 && (label != b.label || index != b.index) {
			flush()
			b = bucket{}
		}
		if b.n == 0 {
			b.label, b.index, b.ts = label, index, day
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
