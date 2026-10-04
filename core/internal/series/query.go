package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type Observation struct {
	TS    string   `json:"ts"`
	Value *float64 `json:"value,omitempty"`
	State *string  `json:"state,omitempty"`
}
type Stream struct {
	Labels     map[string]string `json:"labels"`
	Points     []Observation     `json:"points"`
	LastPoint  string            `json:"last_point,omitempty"`
	Stale      bool              `json:"stale"`
	StaleSince string            `json:"stale_since,omitempty"`
}
type Result struct {
	Definition
	Adapter          string   `json:"adapter"`
	Host             string   `json:"host"`
	HostID           string   `json:"host_id"`
	AgentID          string   `json:"agent_id"`
	ExpectedInterval int64    `json:"expected_interval_seconds"`
	LastPush         *string  `json:"last_push"`
	RevokedAt        *string  `json:"revoked_at"`
	Streams          []Stream `json:"streams"`
	Resolution       string   `json:"resolution"`
}

// DefaultStep rounds up rather than truncating a fractional bucket or day.
// Both HTTP queries and dashboard bindings use the same bounded default.
func DefaultStep(window time.Duration) time.Duration {
	if window > Retention {
		days := window / (24 * time.Hour)
		if window%(24*time.Hour) > 0 {
			days++
		}
		return ((days + MaxBuckets - 1) / MaxBuckets) * 24 * time.Hour
	}
	step := window / MaxBuckets
	if window%MaxBuckets > 0 {
		step++
	}
	if step < time.Second {
		step = time.Second
	}
	return step
}

// The primary keys index raw observations by (series, labels, ts), and daily
// observations by (series, labels, day). Scan each requested range once, then
// resolve at most 200 last-value rows by indexed joins, without correlated scans.
const bucketQuery = `WITH obs AS (
 SELECT CASE WHEN ? THEN (ts/?)*? ELSE ts END ts,1 n,value total,value low,value high,ts last_ts
 FROM series_points WHERE series=? AND labels=? AND ts>=? AND ts<=?
 UNION ALL SELECT day,n,total,low,high,last_ts FROM series_daily WHERE series=? AND labels=? AND day>=? AND day<=?
), grouped AS (
 SELECT MIN(ts) ts,SUM(n) n,SUM(total) total,MIN(low) low,MAX(high) high,MAX(last_ts) last_ts
 FROM obs GROUP BY MIN(((ts-?)/?),?)
)
SELECT g.ts,g.n,g.total,g.low,g.high,COALESCE(p.value,d.last_value),COALESCE(p.state,d.last_state)
FROM grouped g
LEFT JOIN series_points p ON p.series=? AND p.labels=? AND p.ts=g.last_ts
LEFT JOIN series_daily d ON d.series=? AND d.labels=? AND d.day=(g.last_ts/?)*?
ORDER BY g.ts LIMIT 200`

const dailyBucketQuery = `WITH obs AS (
 SELECT day ts,n,total,low,high,last_ts FROM series_live_daily WHERE series=? AND labels=? AND day>=? AND day<=?
 UNION ALL SELECT day,n,total,low,high,last_ts FROM series_daily WHERE series=? AND labels=? AND day>=? AND day<=?
), grouped AS (
 SELECT MIN(ts) ts,SUM(n) n,SUM(total) total,MIN(low) low,MAX(high) high,MAX(last_ts) last_ts
 FROM obs GROUP BY MIN(((ts-?)/?),?)
)
SELECT g.ts,g.n,g.total,g.low,g.high,COALESCE(l.last_value,d.last_value),COALESCE(l.last_state,d.last_state)
FROM grouped g
LEFT JOIN series_live_daily l ON l.series=? AND l.labels=? AND l.day=(g.last_ts/?)*? AND l.last_ts=g.last_ts
LEFT JOIN series_daily d ON d.series=? AND d.labels=? AND d.day=(g.last_ts/?)*? AND d.last_ts=g.last_ts
ORDER BY g.ts LIMIT 200`

// Count only budget+1 indexed rows; admission must not scan the oversized input
// that it is intended to avoid. The budget is shared by all matching label sets.
func overQueryBudget(ctx context.Context, tx *sql.Tx, name string, labels []string, since, end int64, daily bool) (bool, error) {
	remaining := MaxRawQueryPoints
	query := `SELECT COUNT(*) FROM (SELECT 1 FROM series_points WHERE series=? AND labels=? AND ts>=? AND ts<=? LIMIT ?)`
	if daily {
		remaining = MaxDailyQueryRows
		query = `SELECT COUNT(*) FROM (
 SELECT 1 FROM series_live_daily WHERE series=? AND labels=? AND day>=? AND day<=?
 UNION ALL SELECT 1 FROM series_daily WHERE series=? AND labels=? AND day>=? AND day<=? LIMIT ?)`
	}
	for _, label := range labels {
		args := []any{name, label, since, end}
		if daily {
			args = append(args, name, label, since, end)
		}
		args = append(args, remaining+1)
		var n int
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			return false, err
		}
		remaining -= n
		if remaining < 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s Store) List(ctx context.Context) ([]Result, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT s.name,s.kind,s.unit,d.name,h.slug,h.id,d.agent_id,d.expected_interval,d.last_push,d.revoked_at FROM series_definitions s JOIN series_adapters d ON d.name=s.adapter JOIN hosts h ON h.id=d.host_id ORDER BY s.name LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Result{}
	for rows.Next() {
		var r Result
		if err = rows.Scan(&r.Name, &r.Kind, &r.Unit, &r.Adapter, &r.Host, &r.HostID, &r.AgentID, &r.ExpectedInterval, &r.LastPush, &r.RevokedAt); err != nil {
			return nil, err
		}
		r.Streams = []Stream{}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s Store) Query(ctx context.Context, name string, labels map[string]string, window, step time.Duration, agg string, now time.Time) (Result, error) {
	out := Result{Streams: []Stream{}, Resolution: "raw"}
	if _, err := Labels(labels); err != nil {
		return out, err
	}
	if window < time.Second || window > 3650*24*time.Hour || step < time.Second || step > 3650*24*time.Hour || (window+step-1)/step > MaxBuckets {
		return out, fmt.Errorf("%w: range/step must yield at most 200 buckets", ErrInvalid)
	}
	allowed := map[string]bool{"last": true, "avg": true, "sum": true, "min": true, "max": true, "count": true}
	if !allowed[agg] {
		return out, ErrInvalid
	}
	// ReadOnly makes modernc SQLite use BEGIN (a WAL read snapshot), overriding
	// the workspace DSN's immediate mode for writes. Maintenance compacts data
	// independently; this snapshot sees each point in raw or rolled-up form.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT s.name,s.kind,s.unit,d.name,h.slug,h.id,d.agent_id,d.expected_interval,d.last_push,d.revoked_at FROM series_definitions s JOIN series_adapters d ON d.name=s.adapter JOIN hosts h ON h.id=d.host_id WHERE s.name=?`, name).Scan(&out.Name, &out.Kind, &out.Unit, &out.Adapter, &out.Host, &out.HostID, &out.AgentID, &out.ExpectedInterval, &out.LastPush, &out.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if out.Kind == "state" && agg != "last" && agg != "count" {
		return out, fmt.Errorf("%w: state aggregation must be last or count", ErrInvalid)
	}
	since := now.Add(-window).UnixNano()
	end := now.UnixNano()
	if window > Retention {
		if step < 24*time.Hour || step%(24*time.Hour) != 0 {
			return out, fmt.Errorf("%w: ranges older than 90d require whole-day steps", ErrInvalid)
		}
	}
	filter := ""
	args := []any{name}
	keys := []string{}
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filter += ` AND json_extract(labels,?)=?`
		args = append(args, `$."`+key+`"`, labels[key])
	}
	// Select at most 100 label sets, then aggregate within each bucket in SQL.
	groups, err := tx.QueryContext(ctx, `SELECT labels FROM series_labels WHERE series=?`+filter+` ORDER BY labels LIMIT 100`, args...)
	if err != nil {
		return out, err
	}
	labelSets := []string{}
	for groups.Next() {
		var l string
		if err = groups.Scan(&l); err != nil {
			groups.Close()
			return out, err
		}
		labelSets = append(labelSets, l)
	}
	err = groups.Err()
	groups.Close()
	if err != nil {
		return out, err
	}
	daily := step >= 24*time.Hour
	if !daily {
		over, err := overQueryBudget(ctx, tx, name, labelSets, since, end, false)
		if err != nil {
			return out, err
		}
		if over {
			if window < 24*time.Hour {
				return out, fmt.Errorf("%w: raw query limit is 4096 observations; narrow the range or labels, or use a daily step", ErrCapacity)
			}
			daily = true
			step = ((step + 24*time.Hour - 1) / (24 * time.Hour)) * 24 * time.Hour
		}
	}
	if daily {
		out.Resolution = "daily"
		since = since / Day * Day
		end = (end/Day+1)*Day - 1
		over, err := overQueryBudget(ctx, tx, name, labelSets, since, end, true)
		if err != nil {
			return out, err
		}
		if over {
			return out, fmt.Errorf("%w: daily query limit is 20000 summary rows; narrow the range or labels", ErrCapacity)
		}
	}
	stepNS := step.Nanoseconds()
	for _, l := range labelSets {
		stream := Stream{Labels: map[string]string{}, Points: []Observation{}, Stale: true}
		if err = json.Unmarshal([]byte(l), &stream.Labels); err != nil {
			return out, err
		}
		var last sql.NullInt64
		if err = tx.QueryRowContext(ctx, `SELECT MAX(ts) FROM (SELECT last_ts ts FROM (SELECT last_ts FROM series_live_daily WHERE series=? AND labels=? ORDER BY day DESC LIMIT 1) UNION ALL SELECT last_ts ts FROM (SELECT last_ts FROM series_daily WHERE series=? AND labels=? ORDER BY day DESC LIMIT 1))`, name, l, name, l).Scan(&last); err != nil {
			return out, err
		}
		if last.Valid {
			stream.LastPoint = time.Unix(0, last.Int64).UTC().Format(time.RFC3339Nano)
			staleAt := time.Unix(0, last.Int64).Add(time.Duration(out.ExpectedInterval) * 2 * time.Second)
			stream.Stale = now.After(staleAt) || out.RevokedAt != nil
			if stream.Stale {
				if out.RevokedAt != nil {
					revoked, err := time.Parse(time.RFC3339Nano, *out.RevokedAt)
					if err == nil && revoked.Before(staleAt) {
						staleAt = revoked
					}
				}
				stream.StaleSince = staleAt.UTC().Format(time.RFC3339Nano)
			}
		}
		// A daily rollup is an explicit one-day observation. Historical query edges
		// cover complete UTC days and expose resolution so clients do not imply raw precision.
		query := bucketQuery
		args := []any{false, Day, Day, name, l, since, end, name, l, since, end, since, stepNS, int((window+step-1)/step) - 1, name, l, name, l, Day, Day}
		if daily {
			query = dailyBucketQuery
			args = []any{name, l, since, end, name, l, since, end, since, stepNS, int((window+step-1)/step) - 1, name, l, Day, Day, name, l, Day, Day}
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var ts, n int64
			var total, low, high, value sql.NullFloat64
			var state sql.NullString
			if err = rows.Scan(&ts, &n, &total, &low, &high, &value, &state); err != nil {
				rows.Close()
				return out, err
			}
			p := Observation{TS: time.Unix(0, ts).UTC().Format(time.RFC3339Nano)}
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
			if out.Kind == "state" && agg == "last" && state.Valid {
				p.State = &state.String
			}
			stream.Points = append(stream.Points, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		out.Streams = append(out.Streams, stream)
	}
	return out, tx.Commit()
}
