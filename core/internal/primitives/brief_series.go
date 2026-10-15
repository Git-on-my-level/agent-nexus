package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strings"
	"time"

	"agent-nexus-core/internal/seriesref"
)

const (
	briefSeriesLimit         = 2
	briefSeriesRefCandidates = 200
	briefSeriesRefBytes      = 64 << 10
	briefMetricWindow        = 7 * 24 * time.Hour
	briefMetricFlatAfter     = 3 * 24 * time.Hour
)

type briefSeries struct {
	Ref       string            `json:"ref"`
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels"`
	LastValue any               `json:"last_value"`
	LastAt    *string           `json:"last_at"`
}

type briefMetricFlat struct {
	Flat          bool  `json:"flat"`
	ForSeconds    int64 `json:"for_seconds"`
	WindowSeconds int64 `json:"window_seconds"`
}

// initiativeSeries only reads the already admitted cards. Ref admission is in
// canonical card order, before visibility filtering, so hidden candidates never
// cause unbounded refill work. LatestRefs uses the ordinary scoped series reads
// and at most 100 label candidates / two matching streams per ref.
func (b *briefBuild) initiativeSeries(ids []string, items []map[string]any) error {
	if len(ids) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT id,CAST(substr(CAST(refs_json AS BLOB),1,?) AS TEXT) FROM cards WHERE id IN (SELECT value FROM json_each(?))`, briefSeriesRefBytes, string(encoded))
	if err != nil {
		return err
	}
	byCard := map[string][]seriesref.Ref{}
	refs := []seriesref.Ref{}
	seen := map[string]bool{}
	for rows.Next() {
		var id, raw string
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		for _, r := range briefCardSeriesRefs(raw) {
			candidate := r.Ref
			byCard[id] = append(byCard[id], r)
			if !seen[candidate] {
				seen[candidate] = true
				refs = append(refs, r)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	metrics, err := seriesref.LatestRefs(b.ctx, b.s.db, refs, b.now)
	if err != nil {
		return err
	}
	primary := map[string]seriesref.Metric{}
	for i, id := range ids {
		list := []briefSeries{}
		for _, r := range byCard[id] {
			m, ok := metrics[r.Ref]
			if !ok || (m.Labels == nil && m.Status != "ambiguous" && len(r.Labels) > 0) {
				continue
			}
			labels := m.Labels
			if labels == nil {
				labels = r.Labels
			}
			entry := briefSeries{Ref: r.Ref, Name: m.Name, Labels: labels}
			if m.Latest != nil {
				entry.LastAt = &m.Latest.TS
				if m.Latest.Value != nil {
					entry.LastValue = *m.Latest.Value
				} else if m.Latest.State != nil {
					entry.LastValue = *m.Latest.State
				}
			}
			list = append(list, entry)
			if len(list) == 1 {
				primary[r.Ref] = m
			}
		}
		items[i]["series"] = list
		items[i]["metric_flat"] = nil
	}
	flats, err := b.seriesFlatSignals(b.ctx, primary)
	if err != nil {
		return err
	}
	for _, item := range items {
		if list := item["series"].([]briefSeries); len(list) > 0 {
			item["metric_flat"] = flats[list[0].Ref]
		}
	}
	return nil
}

// Bound decoding as well as returned metrics. The SQL prefix caps materialized
// bytes; the streaming decoder stops at 200 authored atoms or two valid refs.
// A truncated final atom contributes nothing; already decoded refs remain usable.
func briefCardSeriesRefs(raw string) []seriesref.Ref {
	out := []seriesref.Ref{}
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return out
	}
	seen := map[string]bool{}
	for n := 0; n < briefSeriesRefCandidates && decoder.More() && len(out) < briefSeriesLimit; n++ {
		var candidate string
		if decoder.Decode(&candidate) != nil {
			break
		}
		r, err := seriesref.ParseRef(candidate)
		if err != nil || seen[candidate] {
			continue
		}
		seen[candidate] = true
		r.Range = briefMetricWindow
		r.Baseline = nil
		out = append(out, r)
	}
	return out
}

type briefMetricDay struct {
	Day       int64
	LastTS    int64
	Low, High sql.NullFloat64
}

// Daily minima/maxima capture excursions that endpoint-only comparisons miss.
// Two primary-key range seeks per primary ref read at most eight UTC days from
// each summary table, regardless of raw point density or workspace history.
func (b *briefBuild) seriesFlatSignals(ctx context.Context, metrics map[string]seriesref.Metric) (map[string]briefMetricFlat, error) {
	out := map[string]briefMetricFlat{}
	requests := []map[string]string{}
	for ref, m := range metrics {
		out[ref] = briefMetricFlat{WindowSeconds: int64(briefMetricWindow / time.Second)}
		if m.Labels == nil || m.Latest == nil || m.Latest.Value == nil || m.Kind == "state" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, m.Latest.TS)
		if err != nil || b.now.Sub(at) > 24*time.Hour || at.After(b.now) {
			continue
		}
		labels, _ := json.Marshal(m.Labels)
		requests = append(requests, map[string]string{"ref": ref, "name": m.Name, "labels": string(labels)})
	}
	if len(requests) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(requests)
	start := b.now.Add(-briefMetricWindow).Truncate(24 * time.Hour).UnixNano()
	end := b.now.Truncate(24 * time.Hour).UnixNano()
	rows, err := b.s.db.QueryContext(ctx, `WITH requested AS MATERIALIZED (
 SELECT json_extract(value,'$.ref') ref,json_extract(value,'$.name') name,json_extract(value,'$.labels') labels FROM json_each(?)
 ) SELECT r.ref,d.day,d.last_ts,d.low,d.high FROM requested r JOIN series_live_daily d ON d.series=r.name AND d.labels=r.labels AND d.day>=? AND d.day<=?
 UNION ALL SELECT r.ref,d.day,d.last_ts,d.low,d.high FROM requested r JOIN series_daily d ON d.series=r.name AND d.labels=r.labels AND d.day>=? AND d.day<=?`, string(encoded), start, end, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := map[string]map[int64]briefMetricDay{}
	for rows.Next() {
		var ref string
		var d briefMetricDay
		if err = rows.Scan(&ref, &d.Day, &d.LastTS, &d.Low, &d.High); err != nil {
			return nil, err
		}
		if days[ref] == nil {
			days[ref] = map[int64]briefMetricDay{}
		}
		if prior, ok := days[ref][d.Day]; ok {
			d.LastTS = max(prior.LastTS, d.LastTS)
			if prior.Low.Valid && (!d.Low.Valid || prior.Low.Float64 < d.Low.Float64) {
				d.Low = prior.Low
			}
			if prior.High.Valid && (!d.High.Valid || prior.High.Float64 > d.High.Float64) {
				d.High = prior.High
			}
		}
		days[ref][d.Day] = d
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, request := range requests {
		ref := request["ref"]
		out[ref] = computeBriefMetricFlat(metrics[ref], days[ref])
	}
	return out, nil
}

func computeBriefMetricFlat(m seriesref.Metric, days map[int64]briefMetricDay) briefMetricFlat {
	result := briefMetricFlat{WindowSeconds: int64(briefMetricWindow / time.Second)}
	latest, err := time.Parse(time.RFC3339Nano, m.Latest.TS)
	if err != nil {
		return result
	}
	tolerance := math.Max(1e-9, math.Abs(*m.Latest.Value)*1e-6)
	// The oldest bucket's last_ts is a conservative start: every later sample
	// is covered by extrema, including whole boundary buckets. A missing bucket,
	// future-contaminated summary or out-of-tolerance value ends the known span.
	for day, n := latest.Truncate(24*time.Hour).UnixNano(), 0; n < 8; day, n = day-int64(24*time.Hour), n+1 {
		d, ok := days[day]
		if !ok || !d.Low.Valid || !d.High.Valid || d.LastTS > latest.UnixNano() ||
			math.Abs(d.Low.Float64-*m.Latest.Value) > tolerance || math.Abs(d.High.Float64-*m.Latest.Value) > tolerance {
			break
		}
		result.ForSeconds = min(int64(briefMetricWindow/time.Second), int64(latest.Sub(time.Unix(0, d.LastTS))/time.Second))
	}
	result.Flat = result.ForSeconds >= int64(briefMetricFlatAfter/time.Second)
	return result
}
