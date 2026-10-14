// Package seriesref resolves bounded metric references through scoped readers.
package seriesref

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid_request")
var ErrCapacity = errors.New("series_capacity")
var Name = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)

const Retention = 90 * 24 * time.Hour

func Duration(raw string) (time.Duration, error) {
	if strings.HasSuffix(raw, "d") {
		n, e := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if e != nil || n < 1 || n > 90 || strconv.Itoa(n)+"d" != raw {
			return 0, ErrInvalid
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(raw)
}
func Labels(labels map[string]string) error {
	if len(labels) > 8 {
		return ErrCapacity
	}
	for k, v := range labels {
		if !Name.MatchString(k) || len(v) > 128 {
			return ErrInvalid
		}
	}
	return nil
}

type Definition struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Unit string `json:"unit"`
}
type Observation struct {
	TS    string   `json:"ts,omitempty"`
	Value *float64 `json:"value,omitempty"`
	State *string  `json:"state,omitempty"`
}

// Ref reserves underscore options; ordinary query keys are label selectors.
type Ref struct {
	Ref      string            `json:"ref"`
	Name     string            `json:"name"`
	Labels   map[string]string `json:"labels"`
	Range    time.Duration     `json:"-"`
	Baseline *float64          `json:"-"`
	Since    int64             `json:"since"`
}

func ParseRef(raw string) (Ref, error) {
	r := Ref{Ref: raw, Labels: map[string]string{}, Range: 30 * 24 * time.Hour}
	if len(raw) > 2048 || !strings.HasPrefix(raw, "series:") {
		return r, ErrInvalid
	}
	name, query, _ := strings.Cut(strings.TrimPrefix(raw, "series:"), "?")
	if !Name.MatchString(name) {
		return r, ErrInvalid
	}
	r.Name = name
	values, err := url.ParseQuery(query)
	if err != nil {
		return r, ErrInvalid
	}
	for key, v := range values {
		if len(v) != 1 {
			return r, ErrInvalid
		}
		switch key {
		case "_range":
			r.Range, err = Duration(v[0])
			if err != nil || r.Range < time.Second || r.Range > Retention {
				return r, ErrInvalid
			}
		case "_baseline":
			n, e := strconv.ParseFloat(v[0], 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return r, ErrInvalid
			}
			r.Baseline = &n
		default:
			if strings.HasPrefix(key, "_") {
				return r, ErrInvalid
			}
			r.Labels[key] = v[0]
		}
	}
	if err = Labels(r.Labels); err != nil {
		return r, err
	}
	return r, nil
}

type Metric struct {
	Ref string `json:"ref"`
	Definition
	Status         string            `json:"status"`
	Labels         map[string]string `json:"labels,omitempty"`
	Latest         *Observation      `json:"latest,omitempty"`
	Baseline       *Observation      `json:"baseline,omitempty"`
	BaselineSource string            `json:"baseline_source,omitempty"`
	Change         *float64          `json:"change,omitempty"`
	ChangePercent  *float64          `json:"change_percent,omitempty"`
	Changed        *bool             `json:"changed,omitempty"`
	ObservedAt     string            `json:"observed_at,omitempty"`
	Age            int64             `json:"age,omitempty"`
	Stale          bool              `json:"stale"`
}
type RefReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// CompareRefs uses the caller's scoped read snapshot. Each ref seeks one
// definition, at most 100 registered label sets, and two raw endpoints per
// matching stream (at most two). No history aggregation or per-ref query.
// Ranges cannot exceed raw retention, so baseline is an exact observation.
func CompareRefs(ctx context.Context, reader RefReader, refs []Ref, now time.Time) (map[string]Metric, error) {
	out := map[string]Metric{}
	if len(refs) == 0 {
		return out, nil
	}
	if len(refs) > 200 {
		return nil, ErrCapacity
	}
	for i := range refs {
		refs[i].Since = now.Add(-refs[i].Range).UnixNano()
	}
	encoded, _ := json.Marshal(refs)
	rows, err := reader.QueryContext(ctx, `WITH requested AS MATERIALIZED (
 SELECT json_extract(value,'$.ref') ref,json_extract(value,'$.name') name,json_extract(value,'$.labels') selectors,json_extract(value,'$.since') since FROM json_each(?)
 ), selected AS MATERIALIZED (
 SELECT r.ref,r.name,r.since,d.kind,d.unit,a.expected_interval,a.revoked_at,labels.value labels
 FROM requested r JOIN series_definitions d ON d.name=r.name JOIN series_adapters a ON a.name=d.adapter
 LEFT JOIN json_each((SELECT json_group_array(labels) FROM (
 SELECT labels FROM (SELECT labels FROM series_labels WHERE series=r.name ORDER BY labels LIMIT 100) l
 WHERE NOT EXISTS (SELECT 1 FROM json_each(r.selectors) filter WHERE json_extract(l.labels,'$."'||filter.key||'"') IS NOT filter.value)
 ORDER BY labels LIMIT 2))) labels ON 1=1
 ) SELECT ref,name,kind,unit,expected_interval,revoked_at,labels,
 (SELECT json_object('ts',ts,'value',value,'state',state) FROM series_points WHERE series=selected.name AND labels=selected.labels AND ts>=selected.since AND ts<=? ORDER BY ts LIMIT 1),
 (SELECT json_object('ts',ts,'value',value,'state',state) FROM series_points WHERE series=selected.name AND labels=selected.labels AND ts>=selected.since AND ts<=? ORDER BY ts DESC LIMIT 1)
 FROM selected`, string(encoded), now.UnixNano(), now.UnixNano())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byRef := map[string]Ref{}
	for _, r := range refs {
		byRef[r.Ref] = r
	}
	counts := map[string]int{}
	for rows.Next() {
		var m Metric
		var interval int64
		var revoked, labels, first, last sql.NullString
		if err = rows.Scan(&m.Ref, &m.Name, &m.Kind, &m.Unit, &interval, &revoked, &labels, &first, &last); err != nil {
			return nil, err
		}
		counts[m.Ref]++
		if counts[m.Ref] > 1 {
			prior := out[m.Ref]
			prior.Status = "ambiguous"
			prior.Latest = nil
			prior.Baseline = nil
			prior.Labels = nil
			prior.Change = nil
			prior.ChangePercent = nil
			prior.Changed = nil
			prior.ObservedAt = ""
			prior.Age = 0
			prior.Stale = false
			prior.BaselineSource = ""
			out[m.Ref] = prior
			continue
		}
		m.Status = "unavailable"
		if labels.Valid {
			if err = json.Unmarshal([]byte(labels.String), &m.Labels); err != nil {
				return nil, err
			}
		}
		if first.Valid && last.Valid {
			decode := func(raw string) (*Observation, error) {
				var p struct {
					TS    int64    `json:"ts"`
					Value *float64 `json:"value"`
					State *string  `json:"state"`
				}
				if e := json.Unmarshal([]byte(raw), &p); e != nil {
					return nil, e
				}
				return &Observation{TS: time.Unix(0, p.TS).UTC().Format(time.RFC3339Nano), Value: p.Value, State: p.State}, nil
			}
			m.Baseline, err = decode(first.String)
			if err != nil {
				return nil, err
			}
			m.Latest, err = decode(last.String)
			if err != nil {
				return nil, err
			}
			m.BaselineSource = "first_in_range"
			if baseline := byRef[m.Ref].Baseline; baseline != nil {
				if m.Kind == "state" {
					m.Latest = nil
					m.Baseline = nil
					m.BaselineSource = ""
					out[m.Ref] = m
					continue
				}
				m.Baseline = &Observation{Value: baseline}
				m.BaselineSource = "declared"
			}
			if m.Latest.Value != nil && m.Baseline.Value != nil {
				delta := *m.Latest.Value - *m.Baseline.Value
				if !math.IsInf(delta, 0) && !math.IsNaN(delta) {
					m.Change = &delta
					if *m.Baseline.Value != 0 {
						pct := delta / math.Abs(*m.Baseline.Value) * 100
						if !math.IsInf(pct, 0) && !math.IsNaN(pct) {
							m.ChangePercent = &pct
						}
					}
				}
				changed := *m.Latest.Value != *m.Baseline.Value
				m.Changed = &changed
			} else if m.Latest.State != nil && m.Baseline.State != nil {
				changed := *m.Latest.State != *m.Baseline.State
				m.Changed = &changed
			}
			m.ObservedAt = m.Latest.TS
			at, _ := time.Parse(time.RFC3339Nano, m.ObservedAt)
			m.Age = int64(now.Sub(at) / time.Second)
			m.Stale = revoked.Valid || now.After(at.Add(time.Duration(interval)*2*time.Second))
			m.Status = "ok"
			if m.Stale {
				m.Status = "stale"
			}
		}
		out[m.Ref] = m
	}
	return out, rows.Err()
}
