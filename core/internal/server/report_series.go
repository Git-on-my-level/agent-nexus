package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/series"
	reports "agent-nexus-visualreport"
)

// pivotMaxRows caps the served rows; the remainder is counted in data.more.
// Mirrors the fallback row bound in the shared contract.
const pivotMaxRows = 50

func (reader *reportReader) materializeSeries(panel reports.Panel) map[string]any {
	if panel.Type == "live-table" {
		return reader.materializeLiveTable(panel)
	}
	out := map[string]any{"id": panel.ID, "type": panel.Type, "status": "unavailable", "data": map[string]any{}, "observed_at": nil, "truncated": false}
	if reader.opts.seriesStore == nil {
		return out
	}
	source := panel.Source
	window := 24 * time.Hour
	var err error
	if source.Range != "" {
		window, err = reports.SeriesRange(source.Range)
		if err != nil {
			return out
		}
	}
	step := series.DefaultStep(window)

	// Metrics aggregate the complete requested window; charts/tables use bins.
	if panel.Type == "metric" || panel.Type == "metric-strip" {
		step = window
		if window > series.Retention {
			step = ((window + 24*time.Hour - 1) / (24 * time.Hour)) * 24 * time.Hour
		}
	}
	agg := source.Agg
	if agg == "" {
		agg = "last"
	}
	var r series.Result
	if panel.Type == "live-timeline" {
		var truncated bool
		r, truncated, err = reader.opts.seriesStore.Timeline(reader.r.Context(), source.Series, source.Labels, window, reader.now)
		out["truncated"] = truncated
	} else {
		r, err = reader.opts.seriesStore.Query(reader.r.Context(), source.Series, source.Labels, window, step, agg, reader.now)
	}
	if err != nil {
		if errors.Is(err, series.ErrCapacity) {
			out["message"] = "Select fewer labels or a shorter range to show this panel."
		}
		return out
	}
	labels := source.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	out["provenance"] = map[string]any{"adapter": r.Adapter, "host": r.Host, "host_id": r.HostID, "agent_id": r.AgentID, "last_push": r.LastPush, "expected_interval_seconds": r.ExpectedInterval, "resolution": r.Resolution, "series": r.Name, "labels": labels}
	status := "ok"
	latest := ""
	staleSince := ""
	freshUntil := time.Time{}
	streams := []series.Stream{}
	for _, stream := range r.Streams {
		streamTime, _ := time.Parse(time.RFC3339Nano, stream.LastPoint)
		latestTime, _ := time.Parse(time.RFC3339Nano, latest)
		if !streamTime.IsZero() {
			until := streamTime.Add(time.Duration(r.ExpectedInterval) * 2 * time.Second)
			if freshUntil.IsZero() || until.Before(freshUntil) {
				freshUntil = until
			}
		}
		if streamTime.After(latestTime) {
			latest = stream.LastPoint
		}
		if stream.Stale {
			status = "stale"
			staleTime, _ := time.Parse(time.RFC3339Nano, stream.StaleSince)
			priorStaleTime, _ := time.Parse(time.RFC3339Nano, staleSince)
			if staleSince == "" || staleTime.Before(priorStaleTime) {
				staleSince = stream.StaleSince
			}
		}
		if len(stream.Points) > 0 {
			streams = append(streams, stream)
		}
	}
	if len(streams) == 0 {
		if status == "ok" {
			status = "unavailable"
			out["message"] = "No observations in the selected range."
		}
		out["status"] = status
		out["stale_since"] = staleSince
		return out
	}
	out["status"] = status
	out["observed_at"] = latest
	out["stale_since"] = staleSince
	if !freshUntil.IsZero() {
		out["fresh_until"] = freshUntil.UTC().Format(time.RFC3339Nano)
	}
	label := func(stream series.Stream) string {
		keys := []string{}
		for key := range stream.Labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		text := r.Name
		for _, key := range keys {
			text += " " + key + "=" + stream.Labels[key]
		}
		if len([]rune(text)) > 200 {
			text = string([]rune(text)[:199]) + "…"
		}
		return text
	}
	last := func(stream series.Stream) any {
		p := stream.Points[len(stream.Points)-1]
		if p.State != nil {
			return *p.State
		}
		if p.Value != nil {
			return *p.Value
		}
		return ""
	}
	switch panel.Type {
	case "live-timeline":
		items := []map[string]any{}
		for _, stream := range streams {
			for _, point := range stream.Points {
				var value any
				if point.State != nil {
					value = *point.State
				} else if point.Value != nil {
					value = *point.Value
				} else {
					continue
				}
				items = append(items, map[string]any{"at": point.TS, "label": label(stream), "value": value})
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			a, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(items[i]["at"]))
			b, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(items[j]["at"]))
			if a.Equal(b) {
				return fmt.Sprint(items[i]["label"]) < fmt.Sprint(items[j]["label"])
			}
			return a.After(b)
		})
		if len(items) > 100 {
			items = items[:100]
			out["truncated"] = true
		}
		out["data"] = map[string]any{"items": items}
	case "metric":
		// A metric is one value. Multiple matching label sets require a strip/table
		// or an exact label filter rather than a silently chosen fleet member.
		if len(streams) != 1 {
			out["status"] = "unavailable"
			out["message"] = "Select one label set for this metric."
			return out
		}
		out["data"] = map[string]any{"value": last(streams[0]), "unit": r.Unit}
	case "metric-strip":
		items := []map[string]any{}
		for i, stream := range streams {
			if i >= 6 {
				out["truncated"] = true
				break
			}
			items = append(items, map[string]any{"label": label(stream), "value": fmt.Sprint(last(stream)), "detail": r.Unit})
		}
		out["data"] = map[string]any{"items": items}
	case "table", "evidence-table":
		rows := []map[string]any{}
		for _, stream := range streams {
			for _, p := range stream.Points {
				if len(rows) >= 200 {
					out["truncated"] = true
					break
				}
				value := ""
				if p.Value != nil {
					value = fmt.Sprint(*p.Value)
				} else if p.State != nil {
					value = *p.State
				}
				rows = append(rows, map[string]any{"cells": []string{p.TS, label(stream), value}, "source_ids": []string{}})
			}
		}
		out["data"] = map[string]any{"columns": []string{"Time", "Series", "Value (" + r.Unit + ")"}, "rows": rows}
	case "metric-chart":
		points := []map[string]any{}
		for _, stream := range streams {
			for _, p := range stream.Points {
				if p.Value != nil && len(points) < 200 {
					points = append(points, map[string]any{"label": p.TS, "value": *p.Value})
				}
			}
		}
		out["truncated"] = len(streams) > 1
		out["data"] = map[string]any{"unit": r.Unit, "label": r.Name, "points": points, "illustrative": false}
	case "chart":
		if r.Kind == "state" {
			out["status"] = "unavailable"
			out["message"] = "Use a metric or table for state series."
			return out
		}
		charts := []map[string]any{}
		for i, stream := range streams {
			if i >= 8 {
				out["truncated"] = true
				break
			}
			points := [][]any{}
			for _, p := range stream.Points {
				if p.Value != nil {
					ts, _ := time.Parse(time.RFC3339Nano, p.TS)
					points = append(points, []any{ts.UnixMilli(), *p.Value})
				}
			}
			charts = append(charts, map[string]any{"name": label(stream), "type": "line", "data": points})
		}
		out["data"] = map[string]any{"option": map[string]any{"xAxis": map[string]any{"type": "time"}, "yAxis": map[string]any{"type": "value", "name": r.Unit}, "series": charts}}
	}
	return out
}

// materializeLiveTable pivots series bindings into one row per distinct
// combination of the row-key label values, with one column per bound series.
// A series cell holds the value its column's aggregation last observed in the
// requested range, when that value was observed, and whether the label set has
// missed its adapter's expected interval.
//
// Per-request cost is bounded by the binding, never by workspace size: one
// bounded store query per column (at most eight, each capped at 100 label sets
// and the shared 4,096-raw-point or 20,000-summary-row budget, backed by the
// (series, labels, ts) indexes), and cells materialize only for the capped
// rows actually served.
func (reader *reportReader) materializeLiveTable(panel reports.Panel) map[string]any {
	out := map[string]any{"id": panel.ID, "type": panel.Type, "status": "unavailable", "data": map[string]any{}, "observed_at": nil, "truncated": false}
	if reader.opts.seriesStore == nil || panel.Source == nil {
		return out
	}
	source := panel.Source
	if len(source.Columns) == 0 || len(source.Columns) > reports.MaxPivotColumns || len(source.RowKeys) == 0 || len(source.RowKeys) > reports.MaxPivotRowKeys {
		return out
	}
	window := 24 * time.Hour
	var err error
	if source.Range != "" {
		if window, err = reports.SeriesRange(source.Range); err != nil {
			return out
		}
	}
	// One bucket covering the requested window, as metric panels do; ranges
	// beyond raw retention aggregate on whole days.
	step := window
	if window > series.Retention {
		step = ((window + 24*time.Hour - 1) / (24 * time.Hour)) * 24 * time.Hour
	}
	results := make([]series.Result, 0, len(source.Columns))
	byLabels := make([]map[string]series.Stream, 0, len(source.Columns))
	for _, column := range source.Columns {
		agg := column.Agg
		if agg == "" {
			agg = "last"
		}
		result, err := reader.opts.seriesStore.Query(reader.r.Context(), column.Series, source.Labels, window, step, agg, reader.now)
		if err != nil {
			switch {
			case errors.Is(err, series.ErrCapacity):
				out["message"] = "Select fewer labels or a shorter range to show this panel."
			case errors.Is(err, series.ErrNotFound):
				out["message"] = fmt.Sprintf("Series %s is not declared.", column.Series)
			case errors.Is(err, series.ErrInvalid):
				out["message"] = fmt.Sprintf("Series %s does not support the %s aggregation.", column.Series, agg)
			}
			return out
		}
		results = append(results, result)
		streams := map[string]series.Stream{}
		for _, stream := range result.Streams {
			if len(stream.Points) == 0 {
				continue
			}
			streams[labelsKey(stream.Labels)] = stream
		}
		byLabels = append(byLabels, streams)
	}
	type pivotRow struct {
		values  []string
		encoded string
		streams []series.Stream
		present []bool
	}
	rows := map[string]*pivotRow{}
	order := []string{}
	for ci, streams := range byLabels {
		for encoded, stream := range streams {
			row := rows[encoded]
			if row == nil {
				values := make([]string, len(source.RowKeys))
				for ki, rowKey := range source.RowKeys {
					values[ki] = stream.Labels[rowKey.Key]
				}
				row = &pivotRow{values: values, encoded: encoded, streams: make([]series.Stream, len(source.Columns)), present: make([]bool, len(source.Columns))}
				rows[encoded] = row
				order = append(order, encoded)
			}
			row.streams[ci] = stream
			row.present[ci] = true
		}
	}
	// The comparator is total: row-key values first, then the full encoded
	// label set, so the served order never depends on map iteration.
	sort.Slice(order, func(a, b int) bool {
		left, right := rows[order[a]], rows[order[b]]
		for i := range left.values {
			if left.values[i] != right.values[i] {
				return left.values[i] < right.values[i]
			}
		}
		return left.encoded < right.encoded
	})
	columns := make([]map[string]any, 0, len(source.RowKeys)+len(source.Columns))
	for _, rowKey := range source.RowKeys {
		label := rowKey.Label
		if label == "" {
			label = rowKey.Key
		}
		columns = append(columns, map[string]any{"label": label, "kind": "key"})
	}
	for ci, column := range source.Columns {
		label := column.Label
		if label == "" {
			label = column.Series
		}
		agg := column.Agg
		if agg == "" {
			agg = "last"
		}
		columns = append(columns, map[string]any{"label": label, "kind": "series", "series": column.Series, "agg": agg, "unit": results[ci].Unit})
	}
	latest := time.Time{}
	staleSince := time.Time{}
	freshUntil := time.Time{}
	anyStale := false
	fresh, stale := 0, 0
	tableRows := make([]map[string]any, 0, min(len(order), pivotMaxRows))
	more := 0
	for _, encoded := range order {
		row := rows[encoded]
		if len(tableRows) >= pivotMaxRows {
			more++
			continue
		}
		cells := make([]map[string]any, 0, len(row.values)+len(source.Columns))
		for _, value := range row.values {
			cells = append(cells, map[string]any{"text": value})
		}
		for ci := range source.Columns {
			if !row.present[ci] {
				cells = append(cells, map[string]any{"text": "", "observed_at": nil, "stale": false})
				continue
			}
			stream := row.streams[ci]
			if stream.Stale {
				anyStale = true
			}
			observed, _ := time.Parse(time.RFC3339Nano, stream.LastPoint)
			if observed.After(latest) {
				latest = observed
			}
			if stream.Stale {
				stale++
				if since, err := time.Parse(time.RFC3339Nano, stream.StaleSince); err == nil && (staleSince.IsZero() || since.Before(staleSince)) {
					staleSince = since
				}
			} else {
				fresh++
				if !observed.IsZero() {
					until := observed.Add(time.Duration(results[ci].ExpectedInterval) * 2 * time.Second)
					if freshUntil.IsZero() || until.Before(freshUntil) {
						freshUntil = until
					}
				}
			}
			cells = append(cells, map[string]any{"text": clipText(streamValue(stream)), "observed_at": stream.LastPoint, "stale": stream.Stale})
		}
		tableRows = append(tableRows, map[string]any{"cells": cells, "source_ids": []string{}})
	}
	provenance := make([]map[string]any, 0, len(results))
	for ci, result := range results {
		labels := source.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		provenance = append(provenance, map[string]any{"adapter": result.Adapter, "host": result.Host, "host_id": result.HostID, "agent_id": result.AgentID, "last_push": result.LastPush, "expected_interval_seconds": result.ExpectedInterval, "resolution": result.Resolution, "series": source.Columns[ci].Series, "labels": labels})
	}
	status := "unavailable"
	if fresh > 0 {
		status = "ok"
	} else if stale > 0 || anyStale {
		// A stale publisher is a definite answer even when nothing was observed
		// in the requested range: name when it went quiet, as other panels do.
		status = "stale"
	}
	if status == "unavailable" {
		out["message"] = "No observations in the selected range."
	}
	if !latest.IsZero() {
		out["observed_at"] = latest.UTC().Format(time.RFC3339Nano)
	}
	if staleSince.IsZero() {
		out["stale_since"] = ""
	} else {
		out["stale_since"] = staleSince.UTC().Format(time.RFC3339Nano)
	}
	if !freshUntil.IsZero() {
		out["fresh_until"] = freshUntil.UTC().Format(time.RFC3339Nano)
	}
	out["status"] = status
	out["truncated"] = more > 0
	out["provenance"] = provenance
	out["data"] = map[string]any{"columns": columns, "rows": tableRows, "more": more}
	return out
}

// streamValue renders the last observation of a queried stream as cell text.
func streamValue(stream series.Stream) string {
	point := stream.Points[len(stream.Points)-1]
	if point.State != nil {
		return *point.State
	}
	if point.Value != nil {
		return fmt.Sprint(*point.Value)
	}
	return ""
}

func clipText(text string) string {
	if runes := []rune(text); len(runes) > 200 {
		return string(runes[:199]) + "…"
	}
	return text
}

// labelsKey identifies a label set across column queries. json.Marshal sorts
// map keys, so the same label set encodes identically from every query.
func labelsKey(labels map[string]string) string {
	encoded, err := json.Marshal(labels)
	if err != nil {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+labels[key])
		}
		return strings.Join(parts, "\x1f")
	}
	return string(encoded)
}
