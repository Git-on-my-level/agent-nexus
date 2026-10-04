// Package series owns push-only, workspace-local time series. It performs no
// external I/O and shares the existing auth store and grant audit log.
package series

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/storage"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid_request")
var ErrCapacity = errors.New("series_capacity")
var ErrRateLimited = errors.New("series_rate_limited")
var ErrNotFound = errors.New("not_found")
var ErrConflict = errors.New("conflict")
var Name = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)

const Day = int64(86400000000000)
const Retention = 90 * 24 * time.Hour
const MaxSeries = 1000
const MaxLabelSets = 100
const MaxPointsPerDay = 100000
const MaxBuckets = 200
const MaxRequestsPerMinute = 2400
const MaxAdapterRequestsPerMinute = 1200
const MaxConcurrentRequests = 4
const MaxAdapterConcurrentRequests = 2
const MaxRawQueryPoints = 4096
const MaxDailyQueryRows = 20000

type Definition struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Unit string `json:"unit"`
}
type Declaration struct {
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	AgentID          string       `json:"agent_id"`
	ExpectedInterval string       `json:"expected_interval"`
	Series           []Definition `json:"series"`
}
type Adapter struct {
	Declaration
	HostID    string  `json:"host_id"`
	Host      string  `json:"host"`
	CreatedAt string  `json:"created_at"`
	RevokedAt *string `json:"revoked_at"`
}
type Point struct {
	TS     string            `json:"ts,omitempty"`
	Value  *float64          `json:"value,omitempty"`
	State  *string           `json:"state,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}
type Store struct {
	DB   *sql.DB
	Auth *auth.Store
}

// Durations extend Go duration syntax with whole days for query windows.
func Duration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		var n int
		if _, err := fmt.Sscanf(s, "%dd", &n); err != nil || fmt.Sprint(n)+"d" != s || n < 1 || n > 3650 {
			return 0, ErrInvalid
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
func Labels(labels map[string]string) (string, error) {
	if len(labels) > 8 {
		return "", fmt.Errorf("%w: at most 8 label keys", ErrCapacity)
	}
	for k, v := range labels {
		if !Name.MatchString(k) || len(v) > 128 {
			return "", fmt.Errorf("%w: invalid label key or value", ErrInvalid)
		}
	}
	if labels == nil {
		labels = map[string]string{}
	}
	b, err := json.Marshal(labels)
	return string(b), err
}

func (s Store) Declare(ctx context.Context, d Declaration, actor auth.Principal) (Adapter, error) {
	interval, err := Duration(d.ExpectedInterval)
	if err != nil || interval < time.Second || interval%time.Second != 0 || interval > 30*24*time.Hour || !Name.MatchString(d.Name) || len(d.Description) > 2000 || strings.TrimSpace(d.Description) == "" || len(d.Series) < 1 || len(d.Series) > 100 {
		return Adapter{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, def := range d.Series {
		if !Name.MatchString(def.Name) || seen[def.Name] || len(def.Unit) > 80 || (def.Kind != "gauge" && def.Kind != "counter" && def.Kind != "state") {
			return Adapter{}, ErrInvalid
		}
		seen[def.Name] = true
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Adapter{}, err
	}
	defer tx.Rollback()
	if err = auth.RequireSeriesAdministratorTx(ctx, tx, actor); err != nil {
		return Adapter{}, err
	}
	out := Adapter{Declaration: d}
	var agent string
	err = tx.QueryRowContext(ctx, `SELECT a.id,h.id,h.slug FROM agents a JOIN host_agents ha ON ha.agent_id=a.id JOIN hosts h ON h.id=ha.host_id WHERE (a.id=? OR a.username=?) AND a.revoked_at IS NULL AND h.revoked_at IS NULL AND COALESCE(json_extract(a.metadata_json,'$.principal_kind'),'agent')='agent' AND NOT EXISTS (SELECT 1 FROM host_exclusions e WHERE e.host_id=h.id AND e.name=ha.name)`, d.AgentID, d.AgentID).Scan(&agent, &out.HostID, &out.Host)
	if errors.Is(err, sql.ErrNoRows) {
		return out, fmt.Errorf("%w: owner must be an active enrolled host agent", ErrInvalid)
	}
	if err != nil {
		return out, err
	}
	out.AgentID = agent
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_definitions`).Scan(&n); err != nil {
		return out, err
	}
	if n+len(d.Series) > MaxSeries {
		return out, fmt.Errorf("%w: workspace limit is 1000 series", ErrCapacity)
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_adapters WHERE name=?`, d.Name).Scan(&exists)
	if err != nil {
		return out, err
	}
	if exists > 0 {
		return out, ErrConflict
	}
	for _, def := range d.Series {
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_definitions WHERE name=?`, def.Name).Scan(&exists); err != nil {
			return out, err
		}
		if exists > 0 {
			return out, ErrConflict
		}
	}
	out.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at) VALUES(?,?,?,?,?,?)`, d.Name, d.Description, agent, out.HostID, int64(interval.Seconds()), out.CreatedAt)
	if err != nil {
		return out, err
	}
	for _, def := range d.Series {
		if _, err = tx.ExecContext(ctx, `INSERT INTO series_definitions(name,adapter,kind,unit) VALUES(?,?,?,?)`, def.Name, d.Name, def.Kind, def.Unit); err != nil {
			return out, err
		}
	}
	if err = s.Auth.AuditSeriesTx(ctx, tx, actor, "series_write_granted", d.Name); err != nil {
		return out, err
	}
	return out, tx.Commit()
}
func (s Store) Remove(ctx context.Context, name string, deleteData bool, actor auth.Principal) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = auth.RequireSeriesAdministratorTx(ctx, tx, actor); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE series_adapters SET revoked_at=COALESCE(revoked_at,?) WHERE name=? AND deleted_at IS NULL`, now, name)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	event := "series_write_revoked"
	if deleteData {
		event = "adapter_deleted"
		// Workspace SQLite deliberately does not enable foreign-key cascades.
		// Delete dependents explicitly before allowing a series name to be reused.
		for _, table := range []string{"series_points", "series_live_daily", "series_daily", "series_labels"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE series IN (SELECT name FROM series_definitions WHERE adapter=?)`, name); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM series_definitions WHERE adapter=?`, name); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE series_adapters SET deleted_at=? WHERE name=?`, now, name); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_access_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE series_adapter=?`, now, name); err != nil {
		return err
	}
	if err = s.Auth.AuditSeriesTx(ctx, tx, actor, event, name); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) Adapters(ctx context.Context) ([]Adapter, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.name,d.description,d.agent_id,d.host_id,h.slug,d.expected_interval,d.created_at,d.revoked_at FROM series_adapters d JOIN hosts h ON h.id=d.host_id WHERE d.deleted_at IS NULL ORDER BY d.name LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Adapter{}
	for rows.Next() {
		var a Adapter
		var interval int64
		if err = rows.Scan(&a.Name, &a.Description, &a.AgentID, &a.HostID, &a.Host, &interval, &a.CreatedAt, &a.RevokedAt); err != nil {
			return nil, err
		}
		a.ExpectedInterval = (time.Duration(interval) * time.Second).String()
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		defs, err := s.DB.QueryContext(ctx, `SELECT name,kind,unit FROM series_definitions WHERE adapter=? ORDER BY name`, out[i].Name)
		if err != nil {
			return nil, err
		}
		out[i].Series = []Definition{}
		for defs.Next() {
			var d Definition
			if err = defs.Scan(&d.Name, &d.Kind, &d.Unit); err != nil {
				defs.Close()
				return nil, err
			}
			out[i].Series = append(out[i].Series, d)
		}
		err = defs.Err()
		defs.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Compaction is transactional with reads/writes so a point exists either in raw
// storage or a daily rollup, never twice. Aggregate state preserves last by ts.
func compact(ctx context.Context, tx *sql.Tx, now time.Time) error {
	cutoff := now.Add(-Retention).UnixNano()
	_, err := tx.ExecContext(ctx, `WITH old AS (SELECT * FROM series_points WHERE ts<?)
 INSERT INTO series_daily(series,labels,day,n,total,low,high,last_ts,last_value,last_state)
 SELECT p.series,p.labels,(p.ts/?)*?,COUNT(*),SUM(p.value),MIN(p.value),MAX(p.value),MAX(p.ts),
 (SELECT q.value FROM old q WHERE q.series=p.series AND q.labels=p.labels AND q.ts>=((p.ts/?)*?) AND q.ts<((p.ts/?)*?)+? ORDER BY q.ts DESC LIMIT 1),
 (SELECT q.state FROM old q WHERE q.series=p.series AND q.labels=p.labels AND q.ts>=((p.ts/?)*?) AND q.ts<((p.ts/?)*?)+? ORDER BY q.ts DESC LIMIT 1)
 FROM old p GROUP BY p.series,p.labels,p.ts/?
 ON CONFLICT(series,labels,day) DO UPDATE SET
 n=series_daily.n+excluded.n,total=COALESCE(series_daily.total,0)+COALESCE(excluded.total,0),low=MIN(series_daily.low,excluded.low),high=MAX(series_daily.high,excluded.high),
 last_value=CASE WHEN excluded.last_ts>series_daily.last_ts THEN excluded.last_value ELSE series_daily.last_value END,
 last_state=CASE WHEN excluded.last_ts>series_daily.last_ts THEN excluded.last_state ELSE series_daily.last_state END,
 last_ts=MAX(series_daily.last_ts,excluded.last_ts)`, cutoff, Day, Day, Day, Day, Day, Day, Day, Day, Day, Day, Day, Day, Day)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM series_points WHERE ts<?`, cutoff)
	if err != nil {
		return err
	}
	// Remove retired summaries and rebuild only the partially retained UTC day.
	// This keeps live + archived disjoint, even at a non-midnight cutoff.
	cutoffDay := cutoff / Day * Day
	if _, err = tx.ExecContext(ctx, `DELETE FROM series_live_daily WHERE day<=?`, cutoffDay); err != nil {
		return err
	}
	boundary := strings.Replace(storage.SeriesLiveDailyBackfillSQL, "FROM series_points GROUP BY", "FROM series_points WHERE ts>=? AND ts<? GROUP BY", 1)
	if _, err = tx.ExecContext(ctx, boundary, cutoff, cutoffDay+Day); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM series_ingestion_days WHERE day<?`, now.UnixNano()/Day-2)
	return err
}
func (s Store) Push(ctx context.Context, name string, p Point, actor auth.Principal, now time.Time) error {
	labels, err := Labels(p.Labels)
	if err != nil {
		return err
	}
	ts := now
	if p.TS != "" {
		ts, err = time.Parse(time.RFC3339Nano, p.TS)
		if err != nil {
			return ErrInvalid
		}
	}
	if ts.Before(now.Add(-Retention)) || ts.After(now.Add(5*time.Minute)) {
		return fmt.Errorf("%w: timestamp must be within 90 days and no more than 5 minutes ahead", ErrInvalid)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = auth.RequireSeriesWriteTx(ctx, tx, actor, name, now); err != nil {
		return err
	}
	var kind string
	if err = tx.QueryRowContext(ctx, `SELECT kind FROM series_definitions WHERE name=?`, name).Scan(&kind); err != nil {
		return err
	}
	if kind == "state" {
		if p.Value != nil || p.State == nil || strings.TrimSpace(*p.State) == "" || len(*p.State) > 128 {
			return ErrInvalid
		}
	} else if p.State != nil || p.Value == nil || math.IsNaN(*p.Value) || math.IsInf(*p.Value, 0) || math.Abs(*p.Value) > 1e12 || (kind == "counter" && *p.Value < 0) {
		return ErrInvalid
	}
	// Count exact retries as requests even when they do not create a new point.
	// Both scopes commit with the point (or dedupe checkpoint), surviving restart.
	if err = requestBudget(ctx, tx, actor.SeriesAdapter, now); err != nil {
		return err
	}
	var existingValue sql.NullFloat64
	var existingState sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT value,state FROM series_points WHERE series=? AND labels=? AND ts=?`, name, labels, ts.UnixNano()).Scan(&existingValue, &existingState)
	if err == nil {
		same := p.Value != nil && existingValue.Valid && *p.Value == existingValue.Float64 || p.State != nil && existingState.Valid && *p.State == existingState.String
		if same {
			return tx.Commit()
		}
		// A collector can correct a raw observation at the same timestamp.
		// Exact retries are free; changed observations consume ingestion budget.
		err = sql.ErrNoRows
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_labels WHERE series=? AND labels=?`, name, labels).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_labels WHERE series=?`, name).Scan(&n); err != nil {
			return err
		}
		if n >= MaxLabelSets {
			return fmt.Errorf("%w: limit is 100 label sets per series", ErrCapacity)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO series_labels(series,labels) VALUES(?,?)`, name, labels); err != nil {
			return err
		}
	}
	day := now.UnixNano() / Day
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT n FROM series_ingestion_days WHERE day=?),0)`, day).Scan(&n); err != nil {
		return err
	}
	if n >= MaxPointsPerDay {
		return fmt.Errorf("%w: workspace limit is 100000 points per day", ErrCapacity)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO series_points(series,labels,ts,value,state,received_day) VALUES(?,?,?,?,?,?) ON CONFLICT(series,labels,ts) DO UPDATE SET value=excluded.value,state=excluded.state,received_day=excluded.received_day`, name, labels, ts.UnixNano(), p.Value, p.State, day)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE series_adapters SET last_push=? WHERE name=?`, now.UTC().Format(time.RFC3339Nano), actor.SeriesAdapter); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO series_ingestion_days(day,n) VALUES(?,1) ON CONFLICT(day) DO UPDATE SET n=n+1`, day)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func requestBudget(ctx context.Context, tx *sql.Tx, adapter string, now time.Time) error {
	minute := now.Unix() / 60
	for _, scope := range []struct {
		name string
		max  int
	}{{"workspace", MaxRequestsPerMinute}, {"adapter:" + adapter, MaxAdapterRequestsPerMinute}} {
		result, err := tx.ExecContext(ctx, `INSERT INTO series_request_budgets(scope,minute,n) VALUES(?,?,1) ON CONFLICT(scope,minute) DO UPDATE SET n=n+1 WHERE n<?`, scope.name, minute, scope.max)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("%w: %s request budget exceeded", ErrRateLimited, scope.name)
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM series_request_budgets WHERE minute<?`, minute-1)
	return err
}

// Compact applies retention without needing an active collector or reader.
func (s Store) Compact(ctx context.Context, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = compact(ctx, tx, now); err != nil {
		return err
	}
	return tx.Commit()
}
