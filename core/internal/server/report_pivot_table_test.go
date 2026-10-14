package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/series"
	reports "agent-nexus-visualreport"
)

// declareLoopSeries declares an adapter with the loop series the pivot tests
// read and returns its scoped write token.
func declareLoopSeries(t *testing.T, env authIntegrationEnv) string {
	t.Helper()
	body := map[string]any{
		"name": "loop-collector", "description": "Automation loop measurements", "agent_id": "series-owner",
		"expected_interval": "1m", "series": []map[string]string{
			{"name": "loops.state", "kind": "state", "unit": "status"},
			{"name": "loops.runs_7d", "kind": "gauge", "unit": "runs"},
			{"name": "loops.fail_streak", "kind": "gauge", "unit": "fails"},
		},
	}
	status, out := hostHTTP(t, "POST", env.server.URL+"/adapters", "admin-token", body)
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "POST", env.server.URL+"/adapters/loop-collector/token", "owner-token", nil)
	hostStatus(t, status, 200, out)
	return out["tokens"].(map[string]any)["access_token"].(string)
}

func pushLoopPoint(t *testing.T, env authIntegrationEnv, token, seriesName string, point map[string]any) {
	t.Helper()
	status, out := hostHTTP(t, "POST", env.server.URL+"/series/"+seriesName+"/points", token, point)
	hostStatus(t, status, 200, out)
}

func pivotSource(columns []any) *reports.SeriesSource {
	raw, err := json.Marshal(map[string]any{"columns": columns, "row_keys": []any{
		map[string]any{"key": "loop"}, map[string]any{"key": "host", "label": "Host"},
	}, "range": "24h"})
	if err != nil {
		panic(err)
	}
	var source reports.SeriesSource
	if err := json.Unmarshal(raw, &source); err != nil {
		panic(err)
	}
	return &source
}

func pivotReader(env authIntegrationEnv, now time.Time) reportReader {
	return reportReader{
		r:    httptest.NewRequest("GET", "/report", nil),
		opts: handlerOptions{seriesStore: &series.Store{DB: env.workspace.DB(), Auth: env.authStore}},
		now:  now,
	}
}

func TestLiveTablePivotRowsCellsAndMissingColumns(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareLoopSeries(t, env)
	now := time.Now().UTC()
	pushLoopPoint(t, env, token, "loops.state", map[string]any{"state": "running", "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-2 * time.Minute).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "loops.state", map[string]any{"state": "retrying", "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "loops.state", map[string]any{"state": "running", "labels": map[string]string{"loop": "beta", "host": "series-host"}, "ts": now.Add(-3 * time.Minute).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "loops.runs_7d", map[string]any{"value": 4, "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-2 * time.Minute).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "loops.runs_7d", map[string]any{"value": 6, "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "loops.fail_streak", map[string]any{"value": 2, "labels": map[string]string{"loop": "beta", "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)})
	// A label set whose extra dimension stays outside the row keys still lands
	// on the alpha row's label set, and an unrelated label set on one column
	// must not invent rows on the others.
	pushLoopPoint(t, env, token, "loops.runs_7d", map[string]any{"value": 99, "labels": map[string]string{"loop": "alpha", "host": "other-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)})

	reader := pivotReader(env, now.Add(time.Second))
	panel := reports.Panel{ID: "loops", Type: "live-table", Source: pivotSource([]any{
		map[string]any{"series": "loops.state", "label": "State"},
		map[string]any{"series": "loops.runs_7d", "label": "Runs (7d)", "agg": "sum"},
		map[string]any{"series": "loops.fail_streak", "label": "Fail streak", "agg": "max"},
	})}
	got := reader.materializeSeries(panel)
	if got["status"] != "ok" || got["truncated"] != false {
		t.Fatalf("pivot panel unavailable: %#v", got)
	}
	if got["observed_at"] == nil {
		t.Fatalf("pivot panel carries no observation time: %#v", got)
	}
	data := got["data"].(map[string]any)
	columns := data["columns"].([]map[string]any)
	if len(columns) != 5 || columns[0]["kind"] != "key" || columns[1]["label"] != "Host" || columns[2]["series"] != "loops.state" || columns[3]["agg"] != "sum" || columns[4]["unit"] != "fails" {
		t.Fatalf("column metadata: %#v", columns)
	}
	rows := data["rows"].([]map[string]any)
	if len(rows) != 3 {
		t.Fatalf("expected three label-set rows: %#v", rows)
	}
	// Rows sort by their row keys: alpha/other-host, alpha/series-host, then beta.
	text := func(row map[string]any, i int) string { return row["cells"].([]map[string]any)[i]["text"].(string) }
	first, second, third := rows[0], rows[1], rows[2]
	if text(first, 0) != "alpha" || text(first, 1) != "other-host" || text(second, 1) != "series-host" || text(third, 0) != "beta" {
		t.Fatalf("row order or key cells: %#v", []any{first, second, third})
	}
	// alpha/series-host: last state, summed runs, and an empty fail-streak cell
	// because that column never observed this label set.
	if text(second, 2) != "retrying" || text(second, 3) != "10" {
		t.Fatalf("agg cells: %#v", second["cells"])
	}
	missing := second["cells"].([]map[string]any)[4]
	if missing["text"] != "" || missing["observed_at"] != nil || missing["stale"] != false {
		t.Fatalf("missing cell must stay empty, never zero: %#v", missing)
	}
	// beta/series-host: state present, runs cell empty, fail streak present.
	if text(third, 2) != "running" || text(third, 3) != "" || text(third, 4) != "2" {
		t.Fatalf("beta row cells: %#v", third["cells"])
	}
	provenance := got["provenance"].([]map[string]any)
	if len(provenance) != 3 || provenance[0]["series"] != "loops.state" || provenance[1]["host"] != "series-host" || provenance[2]["adapter"] != "loop-collector" {
		t.Fatalf("per-column provenance: %#v", provenance)
	}
	// The served shape must satisfy the shared fallback contract.
	snapshot, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	panelSource := map[string]any{
		"columns":  []any{map[string]any{"series": "loops.state"}, map[string]any{"series": "loops.runs_7d"}, map[string]any{"series": "loops.fail_streak"}},
		"row_keys": []any{map[string]any{"key": "loop"}, map[string]any{"key": "host"}},
	}
	panelSpec := map[string]any{
		"id": "p", "project_id": "workspace", "type": "live-table", "title": "Pivot", "author": "a",
		"provenance": "reported", "observed_at": nil, "freshness": "unknown", "source_ids": []any{}, "data": map[string]any{},
		"source":   panelSource,
		"fallback": map[string]any{"as_of": now.Format(time.RFC3339Nano), "data": json.RawMessage(snapshot)},
	}
	panelDoc := map[string]any{
		"kind": reports.Kind, "schema_version": 1, "title": "Pivot", "summary": "s", "generated_at": now.Format(time.RFC3339Nano),
		"projects": []any{map[string]any{"id": "workspace", "title": "W", "summary": "s", "outcome": "o"}},
		"sources":  []any{}, "panels": []any{panelSpec},
	}
	encoded, err := json.Marshal(panelDoc)
	if err != nil {
		t.Fatal(err)
	}
	if result := reports.Validate(encoded); !result.Valid {
		t.Fatalf("served pivot shape fails the shared contract: %v", result.Errors)
	}
}

func TestLiveTablePivotRowCapMarksTruncation(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareLoopSeries(t, env)
	now := time.Now().UTC()
	const labelSets = pivotMaxRows + 5
	for i := 0; i < labelSets; i++ {
		pushLoopPoint(t, env, token, "loops.runs_7d", map[string]any{"value": i, "labels": map[string]string{"loop": fmt.Sprintf("loop-%02d", i), "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)})
	}
	reader := pivotReader(env, now.Add(time.Second))
	got := reader.materializeSeries(reports.Panel{ID: "loops", Type: "live-table", Source: pivotSource([]any{
		map[string]any{"series": "loops.runs_7d", "label": "Runs"},
	})})
	if got["status"] != "ok" || got["truncated"] != true {
		t.Fatalf("capped pivot status: %#v", got)
	}
	data := got["data"].(map[string]any)
	rows := data["rows"].([]map[string]any)
	if len(rows) != pivotMaxRows {
		t.Fatalf("row cap: %d rows", len(rows))
	}
	if data["more"] != 5 {
		t.Fatalf("more count: %#v", data["more"])
	}
}

func TestLiveTablePivotStaleCellsAndPanelStatus(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	body := map[string]any{
		"name": "stale-collector", "description": "Stale measurements", "agent_id": "series-owner",
		"expected_interval": "1s", "series": []map[string]string{
			{"name": "slow.state", "kind": "state", "unit": "status"},
			{"name": "slow.beats", "kind": "gauge", "unit": "beats"},
		},
	}
	status, out := hostHTTP(t, "POST", env.server.URL+"/adapters", "admin-token", body)
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "POST", env.server.URL+"/adapters/stale-collector/token", "owner-token", nil)
	hostStatus(t, status, 200, out)
	token := out["tokens"].(map[string]any)["access_token"].(string)
	now := time.Now().UTC()
	// Both label sets last pushed 10s ago against a 1s expected interval, so
	// every served cell is stale and the panel says so.
	pushLoopPoint(t, env, token, "slow.state", map[string]any{"state": "running", "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-10 * time.Second).Format(time.RFC3339Nano)})
	pushLoopPoint(t, env, token, "slow.beats", map[string]any{"value": 3, "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Add(-9 * time.Second).Format(time.RFC3339Nano)})
	reader := pivotReader(env, now)
	source := pivotSource([]any{
		map[string]any{"series": "slow.state", "label": "State"},
		map[string]any{"series": "slow.beats", "label": "Beats"},
	})
	got := reader.materializeSeries(reports.Panel{ID: "slow", Type: "live-table", Source: source})
	if got["status"] != "stale" || got["stale_since"] == "" || got["stale_since"] == nil {
		t.Fatalf("fully stale pivot: %#v", got)
	}
	cells := got["data"].(map[string]any)["rows"].([]map[string]any)[0]["cells"].([]map[string]any)
	if cells[2]["stale"] != true || cells[3]["stale"] != true {
		t.Fatalf("series cells must carry their staleness: %#v", cells)
	}
	if cells[2]["text"] != "running" || cells[3]["text"] != "3" {
		t.Fatalf("stale cells keep their last values: %#v", cells)
	}
	// One fresh cell flips the panel back to ok while the stale cell keeps its
	// own marker: per-cell freshness, not panel-wide blur.
	pushLoopPoint(t, env, token, "slow.beats", map[string]any{"value": 4, "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Format(time.RFC3339Nano)})
	got = reader.materializeSeries(reports.Panel{ID: "slow", Type: "live-table", Source: source})
	if got["status"] != "ok" {
		t.Fatalf("mixed freshness should read ok: %#v", got)
	}
	cells = got["data"].(map[string]any)["rows"].([]map[string]any)[0]["cells"].([]map[string]any)
	if cells[2]["stale"] != true || cells[3]["stale"] != false {
		t.Fatalf("mixed cells: %#v", cells)
	}
	if got["fresh_until"] == nil {
		t.Fatalf("fresh cells feed the panel freshness deadline: %#v", got)
	}
}

func TestLiveTablePivotUndeclaredSeriesColumnFailsWithMessage(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareLoopSeries(t, env)
	now := time.Now().UTC()
	pushLoopPoint(t, env, token, "loops.state", map[string]any{"state": "running", "labels": map[string]string{"loop": "alpha", "host": "series-host"}, "ts": now.Format(time.RFC3339Nano)})
	reader := pivotReader(env, now.Add(time.Second))
	got := reader.materializeSeries(reports.Panel{ID: "loops", Type: "live-table", Source: pivotSource([]any{
		map[string]any{"series": "loops.state", "label": "State"},
		map[string]any{"series": "loops.missing", "label": "Missing"},
	})})
	if got["status"] != "unavailable" {
		t.Fatalf("undeclared column must fail the panel: %#v", got)
	}
	if got["message"] != "Series loops.missing is not declared." {
		t.Fatalf("expected a readable reason: %#v", got["message"])
	}
	// A state series cannot take a numeric aggregation; the reason says so.
	got = reader.materializeSeries(reports.Panel{ID: "loops", Type: "live-table", Source: pivotSource([]any{
		map[string]any{"series": "loops.state", "label": "State", "agg": "sum"},
	})})
	if got["status"] != "unavailable" || !strings.Contains(got["message"].(string), "does not support the sum aggregation") {
		t.Fatalf("state aggregation reason: %#v", got)
	}
}

func TestLiveTablePivotHonorsResourceAccessRules(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	ctx := context.Background()
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "pivot-human", "pivot-human-actor", "pivot-human", "pivot-human-token")
	store := env.primitiveStore.(*primitives.Store)
	card, err := store.CreateWork(ctx, "series-owner-actor", "", map[string]any{"title": "private loop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "series-owner-actor", anyString(card["thread_id"]), map[string]any{"pm_actor_id": "series-owner-actor"}, nil); err != nil {
		t.Fatal(err)
	}
	token := declareLoopSeries(t, env)
	now := time.Now().UTC()
	for _, name := range []string{"loops.state", "loops.runs_7d"} {
		state := map[string]any{"state": "running", "labels": map[string]any{"loop": "private", "host": "series-host", "ref": card["ref"]}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)}
		if name == "loops.runs_7d" {
			state = map[string]any{"value": 7, "labels": map[string]any{"loop": "private", "host": "series-host", "ref": card["ref"]}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)}
		}
		pushLoopPoint(t, env, token, name, state)
		public := map[string]any{"state": "running", "labels": map[string]any{"loop": "public", "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)}
		if name == "loops.runs_7d" {
			public = map[string]any{"value": 3, "labels": map[string]any{"loop": "public", "host": "series-host"}, "ts": now.Add(-time.Minute).Format(time.RFC3339Nano)}
		}
		pushLoopPoint(t, env, token, name, public)
	}
	source := pivotSource([]any{
		map[string]any{"series": "loops.state", "label": "State"},
		map[string]any{"series": "loops.runs_7d", "label": "Runs"},
	})
	rowLoops := func(t *testing.T, reader reportReader) map[string]bool {
		t.Helper()
		got := reader.materializeSeries(reports.Panel{ID: "loops", Type: "live-table", Source: source})
		if got["status"] != "ok" {
			t.Fatalf("pivot lost: %#v", got)
		}
		loops := map[string]bool{}
		for _, row := range got["data"].(map[string]any)["rows"].([]map[string]any) {
			loops[row["cells"].([]map[string]any)[0]["text"].(string)] = true
		}
		return loops
	}
	owner := rowLoops(t, pivotReader(env, now.Add(time.Second)))
	if !owner["private"] || !owner["public"] {
		t.Fatalf("owner must see both label sets: %#v", owner)
	}
	// A reader without access to the private label set loses the whole row in
	// every column: a denied label set must not leak through its row keys, and
	// its last_push provenance is withheld.
	strangerReader := pivotReader(env, now.Add(time.Second))
	strangerReader.r = strangerReader.r.WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: stranger.ActorID}))
	visible := rowLoops(t, strangerReader)
	if visible["private"] || !visible["public"] {
		t.Fatalf("denied label set leaked: %#v", visible)
	}
	got := strangerReader.materializeSeries(reports.Panel{ID: "loops", Type: "live-table", Source: source})
	for _, entry := range got["provenance"].([]map[string]any) {
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"last_push":null`) {
			t.Fatalf("private adapter freshness withheld per column: %s", encoded)
		}
	}
}
