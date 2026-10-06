package server

import (
	"agent-nexus-core/internal/series"
	reports "agent-nexus-visualreport"
	"errors"
	"fmt"
	"sort"
	"time"
)

func (reader *reportReader) materializeSeries(panel reports.Panel) map[string]any {
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
