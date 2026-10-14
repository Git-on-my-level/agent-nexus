package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/series"
	reports "agent-nexus-visualreport"
)

func TestResourceAccessSeriesFreshnessAndReportProvenance(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	ctx := context.Background()
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "fresh-human", "fresh-human-actor", "fresh-human", "fresh-human-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "fresh-agent", "fresh-agent-actor", "fresh-agent", "fresh-agent-token")
	s := env.primitiveStore.(*primitives.Store)
	card, err := s.CreateWork(ctx, "series-owner-actor", "", map[string]any{"title": "private source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "series-owner-actor", anyString(card["thread_id"]), map[string]any{"pm_actor_id": "series-owner-actor"}, nil); err != nil {
		t.Fatal(err)
	}
	token := declareSeriesHTTP(t, env)
	if _, err = env.workspace.DB().Exec(`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES('sibling','github','count','gauge')`); err != nil {
		t.Fatal(err)
	}
	status, out := hostHTTP(t, "POST", env.server.URL+"/series/prs/points", token, map[string]any{"value": 5})
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "GET", env.server.URL+"/series/prs/query?range=1h&step=1m", stranger.AccessToken, nil)
	hostStatus(t, status, 200, out)
	if out["series"].(map[string]any)["last_push"] == nil {
		t.Fatal("fully visible adapter lost freshness")
	}
	status, out = hostHTTP(t, "POST", env.server.URL+"/series/sibling/points", token, map[string]any{"value": 9, "labels": map[string]any{"ref": card["ref"]}})
	hostStatus(t, status, 200, out)
	deniedRef := "series:sibling?ref=" + url.QueryEscape(anyString(card["ref"]))
	initiative, err := s.CreateWork(ctx, "series-owner-actor", "", map[string]any{"title": "Public metric link", "related_refs": []string{deniedRef}})
	if err != nil {
		t.Fatal(err)
	}
	for _, reader := range []struct {
		actor, token string
		owner        bool
	}{{"series-owner-actor", "owner-token", true}, {stranger.ActorID, stranger.AccessToken, false}, {agent.ActorID, agent.AccessToken, false}} {
		for _, path := range []string{"/series", "/series/prs/query?range=1h&step=1m"} {
			status, out = hostHTTP(t, "GET", env.server.URL+path, reader.token, nil)
			hostStatus(t, status, 200, out)
			var rows []any
			if path == "/series" {
				rows = out["series"].([]any)
			} else {
				rows = []any{out["series"]}
			}
			for _, row := range rows {
				if visible := row.(map[string]any)["last_push"] != nil; visible != reader.owner {
					t.Fatalf("%s %s freshness: %#v", reader.actor, path, row)
				}
			}
		}
		scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: reader.actor})
		previews, e := s.ResolveRefs(scope, []string{deniedRef}, nil, time.Now().UTC(), 0)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(previews)
		if has := strings.Contains(string(b), `"value":9`); has != reader.owner {
			t.Fatalf("series ref disclosure for %s: %s", reader.actor, b)
		}

		if !reader.owner {
			var preview []map[string]any
			if e = json.Unmarshal(b, &preview); e != nil {
				t.Fatal(e)
			}
			if len(preview) != 1 || len(preview[0]) != 2 || preview[0]["ref"] != deniedRef || preview[0]["resolvable"] != false {
				t.Fatalf("denied preview must contain only ref and false resolvable: %s", b)
			}
		}
		cards := []map[string]any{map[string]any{"id": initiative["id"], "ref": initiative["ref"]}}
		if e = s.EnrichCardPlans(scope, cards, nil, time.Now().UTC(), 0); e != nil {
			t.Fatal(e)
		}
		summary := cards[0]["work_summary"].(*primitives.WorkSummary)
		if reader.owner {
			if len(summary.Metrics) != 1 || summary.Metrics[0].Latest == nil {
				t.Fatal("owner lost metric", summary)
			}
		} else {
			raw, _ := json.Marshal(summary.Metrics)
			if len(summary.Metrics) != 0 {
				t.Fatalf("denied card metrics must omit kind/unit/baseline/stale/observed_at/resolvable: %s", raw)
			}
		}

		rr := reportReader{r: httptest.NewRequest("GET", "/report", nil).WithContext(scope), opts: handlerOptions{seriesStore: &series.Store{DB: env.workspace.DB(), Auth: env.authStore}}, now: time.Now().UTC()}
		panel := rr.materializeSeries(reports.Panel{ID: "public", Type: "metric", Source: &reports.SeriesSource{Series: "prs", Range: "1h"}})
		body, _ := json.Marshal(panel)
		if null := strings.Contains(string(body), `"last_push":null`); null == reader.owner {
			t.Fatalf("report freshness for %s: %s", reader.actor, body)
		}
		if panel["status"] != "ok" {
			t.Fatalf("public report lost: %s", body)
		}
		if panel["data"].(map[string]any)["value"] != float64(5) {
			t.Fatalf("public report value changed: %s", body)
		}
	}
}
