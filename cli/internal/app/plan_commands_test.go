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
