package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanUsageBeforeIdentityAndHelp(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "step", "add", "card:launch"},
		{"plan", "step", "update", "card:launch", "build"},
		{"plan", "step", "rm", "card:launch"},
		{"plan", "set", "card:launch"},
		{"plan", "show", "card:launch", "--unknown"},
		{"refs", "resolve"},
	} {
		result := runCLIForTestJSONError(t, t.TempDir(), nil, append([]string{"--json"}, args...))
		var envelope map[string]any
		json.Unmarshal([]byte(result), &envelope)
		errObj, _ := envelope["error"].(map[string]any)
		if errObj["exit_code"] != float64(2) {
			t.Fatalf("%v: %s", args, result)
		}
	}
	for _, topic := range []string{"plan", "plan step", "plan step add", "refs resolve"} {
		result := runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"help"}, strings.Fields(topic)...))
		if !strings.Contains(result, "anx") {
			t.Fatalf("help %s: %+v", topic, result)
		}
	}
}

func TestPlanStatusUsageListsRepairBeforeIdentity(t *testing.T) {
	result := runCLIForTestJSONError(t, t.TempDir(), nil, []string{"--json", "plan", "step", "add", "card:launch", "--title", "Build", "--status", "in_progress"})
	for _, want := range []string{"done, active, blocked or not_started", "use active for work in progress"} {
		if !strings.Contains(result, want) {
			t.Fatalf("missing repair %q: %s", want, result)
		}
	}
}

func TestPlanReadbackModesAndErrorRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error":{"code":"conflict","message":"plan changed"}}`)
			return
		}
		io.WriteString(w, `{"card_ref":"card:launch","if_updated_at":"2026-10-13T00:00:00Z","plan":{"steps":[{"id":"build","title":"Build","after":[],"status":"active"}]},"plan_state":{"steps":[{"id":"build","status":"done","resolvable":true}],"progress":{"done":1,"total":1},"health":"done"},"summary":{"owner":"actor:alice"}}`)
	}))
	defer server.Close()
	env := map[string]string{"ANX_ACCESS_TOKEN": "fixture-token"}
	base := []string{"--base-url", server.URL}
	for _, mode := range []string{"", "--verbose", "--json", "--headers"} {
		args := append([]string{}, base...)
		if mode != "" {
			args = append(args, mode)
		}
		text := runCLIForTest(t, t.TempDir(), env, nil, append(args, "plan", "show", "card:launch"))
		if mode == "" {
			if !strings.Contains(text, "step id=build") || strings.Contains(text, "result.summary.owner") {
				t.Fatal(text)
			}
		} else if !strings.Contains(text, "alice") {
			t.Fatalf("%s omitted full fields: %s", mode, text)
		}
	}
	for _, command := range [][]string{
		{"plan", "step", "update", "card:launch", "missing", "--status", "done"},
		{"plan", "step", "add", "card:launch", "--title", "Build"},
		{"plan", "step", "update", "card:launch", "build", "--status", "done"},
	} {
		args := append(append([]string{"--json"}, base...), command...)
		text := runCLIForTestJSONError(t, t.TempDir(), env, args)
		var envelope map[string]any
		if err := json.Unmarshal([]byte(text), &envelope); err != nil {
			t.Fatal(err)
		}
		errObj := envelope["error"].(map[string]any)
		actions := errObj["next_actions"].([]any)
		if len(actions) != 1 {
			t.Fatalf("missing exact read action: %s", text)
		}
		a := actions[0].(map[string]any)
		argv := stringList(a["argv"])
		if strings.Join(argv, " ") != "anx plan show card:launch" || a["mutates"] != false {
			t.Fatal(text)
		}
		if _, err := parsePlanCommand(argv[1:]); err != nil {
			t.Fatalf("unrunnable repair: %v", err)
		}
	}
}

func TestPlanStepMutationsUseConcurrencyFence(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"plan", "step", "add", "card:launch", "--title", "Ship", "--after", "build", "--ref", "https://github.com/org/repo/pull/1"}, `{"id":"ship","title":"Ship","ref":"https://github.com/org/repo/pull/1","after":["build"]}`},
		{[]string{"plan", "step", "update", "card:launch", "build", "--status", "done", "--after", ""}, `{"id":"build","title":"Build","status":"done","after":[]}`},
		{[]string{"plan", "step", "rm", "card:launch", "build"}, ""},
		{[]string{"plan", "step", "update", "build", "--card-id", "card:launch", "--status", "done"}, `{"id":"build","title":"Build","status":"done","after":[]}`},
		{[]string{"plan", "step", "rm", "build", "--card-id", "card:launch"}, ""},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/cards/card:launch/plan" {
					t.Error(r.URL)
				}
				if calls == 1 {
					if r.Method != "GET" {
						t.Error(r.Method)
					}
					io.WriteString(w, `{"card_ref":"card:launch","if_updated_at":"2026-10-05T00:00:00Z","plan":{"steps":[{"id":"build","title":"Build","after":[]}]}}`)
					return
				}
				if r.Method != "PUT" {
					t.Error(r.Method)
				}
				var req struct {
					IfUpdatedAt string `json:"if_updated_at"`
					Plan        struct {
						Steps []map[string]any `json:"steps"`
					} `json:"plan"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}
				if req.IfUpdatedAt != "2026-10-05T00:00:00Z" {
					t.Error(req)
				}
				if tc.want == "" {
					if len(req.Plan.Steps) != 0 {
						t.Fatal(req)
					}
				} else {
					last := req.Plan.Steps[len(req.Plan.Steps)-1]
					raw, _ := json.Marshal(last)
					var want any
					json.Unmarshal([]byte(tc.want), &want)
					expected, _ := json.Marshal(want)
					if string(raw) != string(expected) {
						t.Errorf("got %s want %s", raw, expected)
					}
				}
				io.WriteString(w, `{"card_ref":"card:launch","if_updated_at":"2026-10-05T01:00:00Z","plan":{"steps":[]},"plan_state":{"progress":{"done":0,"total":0},"steps":[],"health":"on_track","shape":"chain","next_steps":[],"critical_path":[]}}`)
			}))
			defer server.Close()
			assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture-token"}, nil, append([]string{"--json", "--base-url", server.URL}, tc.args...)))
			if calls != 2 {
				t.Errorf("calls=%d", calls)
			}
		})
	}
}

func TestPlanSetAndBatchResolve(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		input string
	}{
		{[]string{"plan", "set", "card:launch", "--from-file", "-"}, `{"steps":[]}`},
		{[]string{"refs", "resolve", "card:launch", "card:missing", "card:launch"}, ""},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method == "GET" {
					io.WriteString(w, `{"card_ref":"card:launch","if_updated_at":"2026-10-05T00:00:00Z","plan":null}`)
					return
				}
				if tc.args[0] == "refs" {
					if r.URL.Path != "/refs/resolve" || r.Method != "POST" {
						t.Error(r.URL)
					}
					var req struct{ Refs []string }
					json.NewDecoder(r.Body).Decode(&req)
					if strings.Join(req.Refs, ",") != "card:launch,card:missing,card:launch" {
						t.Error(req)
					}
					io.WriteString(w, `{"items":[{"ref":"card:missing","resolvable":false}]}`)
				} else {
					if r.Method != "PUT" {
						t.Error(r.Method)
					}
					io.WriteString(w, `{"card_ref":"card:launch","plan":{"steps":[]},"plan_state":{"progress":{"done":0,"total":0}}}`)
				}
			}))
			defer server.Close()
			assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture-token"}, strings.NewReader(tc.input), append([]string{"--json", "--base-url", server.URL}, tc.args...)))
			want := 2
			if tc.args[0] == "refs" {
				want = 1
			}
			if calls != want {
				t.Errorf("calls=%d", calls)
			}
		})
	}
	if commandSideEffectClass("refs resolve") != "read_only" || commandSideEffectClass("plan step add") != "remote_coordination_write" {
		t.Fatal("incorrect side effect classification")
	}
}

func TestSeriesRefResolvePreservesMetricOutput(t *testing.T) {
	ref := "series:cost?loop=test&_baseline=100"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Refs []string }
		json.NewDecoder(r.Body).Decode(&request)
		if len(request.Refs) != 1 || request.Refs[0] != ref {
			t.Fatal(request)
		}
		io.WriteString(w, `{"items":[{"ref":"series:cost?loop=test&_baseline=100","resolvable":true,"kind":"series","metric":{"name":"cost","latest":{"value":80},"baseline":{"value":100},"change":-20,"status":"ok","observed_at":"2026-10-14T00:00:00Z"}}]}`)
	}))
	defer server.Close()
	for _, jsonMode := range []bool{false, true} {
		args := []string{"--base-url", server.URL, "refs", "resolve", ref}
		if jsonMode {
			args = append([]string{"--json"}, args...)
		}
		raw := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture-token"}, nil, args)
		if jsonMode {
			assertEnvelopeOK(t, raw)
			if !strings.Contains(raw, `"change": -20`) && !strings.Contains(raw, `"change":-20`) {
				t.Fatal(raw)
			}
		} else {
			if !strings.Contains(raw, "metric.change=-20") || !strings.Contains(raw, "metric.latest.value=80") {
				t.Fatal(raw)
			}
		}
	}
}
