package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const curateCandidatesBody = `{"candidates":[
 {"key":"k1","action":"withdraw","targets":["ask:a1"],"title":"Stale ask","rationale":"overtaken","score":3},
 {"key":"k2","action":"merge","targets":["card:x","card:y"],"title":"Dup cards","rationale":"same work","score":2},
 {"key":"k3","action":"batch","targets":["ask:b1","ask:b2"],"title":"Old asks","rationale":"old","score":1}],"limitations":[]}`

type curateServer struct {
	mu        sync.Mutex
	posts     []map[string]any
	paths     []string
	series    []string
	inventory string
	srv       *httptest.Server
}

func newCurateServer(t *testing.T, candidates string) *curateServer {
	t.Helper()
	cs := &curateServer{inventory: `{"series":[]}`}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		cs.paths = append(cs.paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/curation/candidates":
			io.WriteString(w, candidates)
		case r.Method == "POST" && r.URL.Path == "/curation/proposals":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			cs.posts = append(cs.posts, body)
			fmt.Fprintf(w, `{"proposal":{"key":"p","status":"created","action":%q,"targets":%s,"event_id":"evt-%d"}}`, body["action"], mustJSON(body["targets"]), len(cs.posts))
		case r.Method == "POST" && r.URL.Path == "/curation/run":
			io.WriteString(w, `{"proposals":[{"key":"f","status":"existing","action":"withdraw","targets":["ask:z"],"event_id":"evt-f"}],"skipped":2}`)
		case r.Method == "GET" && r.URL.Path == "/curation/metrics":
			io.WriteString(w, `{"metrics":{"s17":{"decisions":4,"weighted_median_hours":6.5},"s18":{"asks":5,"answered":4,"answered_recommended_pct":75,"needs_context_pct":null,"withdrawn_moot_after_24h_pct":10},"s20":{"promises":0,"due":0,"met_pct":null,"median_slip_hours":null,"moved_2plus_pct":null},"truncated":false}}`)
		case r.Method == "GET" && r.URL.Path == "/series":
			io.WriteString(w, cs.inventory)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/token"):
			io.WriteString(w, `{"tokens":{"access_token":"scoped"}}`)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/points"):
			cs.series = append(cs.series, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/series/"), "/points"))
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func stubCurateHarness(t *testing.T, out string, err error) {
	t.Helper()
	prev := pmCurateHarness
	pmCurateHarness = func(context.Context, []string, string, string, []string) ([]byte, error) {
		return []byte(out), err
	}
	t.Cleanup(func() { pmCurateHarness = prev })
}

func runCurate(t *testing.T, cs *curateServer, args ...string) string {
	t.Helper()
	base := []string{"--json", "--base-url", cs.srv.URL, "pm", "curate", "--work-dir", t.TempDir()}
	return runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, append(base, args...))
}

func TestPMCurateDryRunPrintsCandidatesAndPostsNothing(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	stubCurateHarness(t, "", errors.New("harness must not run"))
	payload := assertEnvelopeOK(t, runCurate(t, cs, "--dry-run"))
	if got := len(asSlice(asMap(payload["result"])["candidates"])); got != 3 {
		t.Fatalf("candidates=%d %v", got, payload)
	}
	if len(cs.posts) != 0 {
		t.Fatalf("posted %v", cs.posts)
	}
}

func TestPMCurateNoCandidates(t *testing.T) {
	cs := newCurateServer(t, `{"candidates":[],"limitations":[]}`)
	stubCurateHarness(t, "", errors.New("harness must not run"))
	out := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--base-url", cs.srv.URL, "pm", "curate", "--runner", "echo {prompt_file}", "--work-dir", t.TempDir()})
	if !strings.Contains(out, "no curation candidates") {
		t.Fatalf("out=%s", out)
	}
}

func TestPMCurateFormulaPostsRun(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	payload := assertEnvelopeOK(t, runCurate(t, cs, "--formula"))
	result := asMap(payload["result"])
	if len(asSlice(result["proposals"])) != 1 || result["skipped"] != float64(2) {
		t.Fatalf("result=%v", result)
	}
	if strings.Join(cs.paths, ",") != "POST /curation/run" {
		t.Fatalf("paths=%v", cs.paths)
	}
}

func TestPMCurateJudgmentParsesFencedJSONAndHonorsMax(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	stubCurateHarness(t, "Here you go:\n```json\n[{\"key\":\"k1\",\"propose\":true,\"rationale\":\"Overtaken by the launch.\"},{\"key\":\"zzz\",\"propose\":true,\"rationale\":\"unknown\"},{\"key\":\"k2\",\"propose\":false,\"rationale\":\"keep\"},{\"key\":\"k3\",\"propose\":true,\"rationale\":\"\"}]\n```\n", nil)
	payload := assertEnvelopeOK(t, runCurate(t, cs, "--runner", "pm-harness {prompt_file}"))
	result := asMap(payload["result"])
	if len(cs.posts) != 2 {
		t.Fatalf("posts=%v", cs.posts)
	}
	if cs.posts[0]["action"] != "withdraw" || cs.posts[0]["rationale"] != "Overtaken by the launch." {
		t.Fatalf("first post %v", cs.posts[0])
	}
	if cs.posts[1]["rationale"] != "old" {
		t.Fatalf("empty rationale did not fall back: %v", cs.posts[1])
	}
	if len(asSlice(result["proposals"])) != 2 || result["candidates"] != float64(3) || result["skipped"] != float64(1) {
		t.Fatalf("result=%v", result)
	}

	cs2 := newCurateServer(t, curateCandidatesBody)
	stubCurateHarness(t, `[{"key":"k1","propose":true},{"key":"k2","propose":true},{"key":"k3","propose":true}]`, nil)
	assertEnvelopeOK(t, runCurate(t, cs2, "--runner", "pm-harness {prompt_file}", "--max", "1"))
	if len(cs2.posts) != 1 {
		t.Fatalf("--max not honored: %v", cs2.posts)
	}
}

func TestPMCurateHarnessFailurePostsNothing(t *testing.T) {
	for name, harness := range map[string]func(t *testing.T){
		"error":   func(t *testing.T) { stubCurateHarness(t, "", errors.New("boom")) },
		"no json": func(t *testing.T) { stubCurateHarness(t, "I could not decide.", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			cs := newCurateServer(t, curateCandidatesBody)
			harness(t)
			assertEnvelopeError(t, runCurate(t, cs, "--runner", "pm-harness {prompt_file}"))
			if len(cs.posts) != 0 {
				t.Fatalf("posted %v", cs.posts)
			}
		})
	}
}

func TestPMCurateRequiresRunner(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	payload := assertEnvelopeError(t, runCurate(t, cs))
	if code := asMap(payload["error"])["code"]; code != "invalid_request" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestPMCuratePushMetrics(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	cs.inventory = `{"series":[` + strings.Join([]string{
		`{"name":"anx.health.s17_weighted_median_hours","adapter":"health"}`,
		`{"name":"anx.health.s18_answered_recommended_pct","adapter":"health"}`,
		`{"name":"anx.health.s18_withdrawn_moot_pct","adapter":"health"}`,
	}, ",") + `]}`
	payload := assertEnvelopeOK(t, runCurate(t, cs, "--formula", "--push-metrics"))
	if strings.Join(cs.series, ",") != "anx.health.s17_weighted_median_hours,anx.health.s18_answered_recommended_pct,anx.health.s18_withdrawn_moot_pct" {
		t.Fatalf("pushed %v (payload %v)", cs.series, payload)
	}
}

func TestPMCuratePushMetricsHintsWhenSeriesUndeclared(t *testing.T) {
	cs := newCurateServer(t, curateCandidatesBody)
	payload := assertEnvelopeOK(t, runCurate(t, cs, "--formula", "--push-metrics"))
	if len(cs.series) != 0 {
		t.Fatalf("pushed %v", cs.series)
	}
	if hint := anyString(asMap(payload["result"])["metrics_hint"]); !strings.Contains(hint, "declare an adapter with these anx.health.* series") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestParsePMCurateDecisionsExtractsArray(t *testing.T) {
	got, err := parsePMCurateDecisions("noise [ {\"key\":\"a\",\"propose\":true} ] trailing")
	if err != nil || len(got) != 1 || got[0]["key"] != "a" {
		t.Fatalf("got %v %v", got, err)
	}
}
