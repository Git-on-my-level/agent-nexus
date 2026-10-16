package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestLoopWorkGrammar(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		bad  bool
	}{
		{"work cadence", []string{"card:x", "--every", "1d", "--budget", "7d"}, false},
		{"work cadence", []string{"card:x", "--clear"}, false},
		{"work cadence", []string{"card:x", "--every", "1m", "--budget", "59s"}, true},
		{"work cadence", []string{"card:x", "--every", "59s"}, true},
		{"work cadence", []string{"card:x", "--clear", "--every", "1d"}, true},
		{"work goal", []string{"card:x", "--metric", "avo.score", "--direction", "up", "--baseline", "0", "--target", "0.9"}, false},
		{"work goal", []string{"card:x", "--metric", "score", "--direction", "sideways"}, true},
		{"work goal", []string{"card:x", "--metric", "score", "--direction", "up", "--target", "NaN"}, true},
		{"work receipt", []string{"card:x", "--run-id", "one", "--outcome", "acted", "--summary", "Kept candidate", "--metric", "score=0", "--candidate", "x", "--decision", "kept", "--score-before", "0", "--score-after", "0.9"}, false},
		{"work receipt", []string{"card:x", "--run-id", "one", "--outcome", "measured", "--summary", "Score", "--metric", "score=NaN"}, true},
		{"work receipt", []string{"card:x", "--run-id", "one", "--outcome", "measured", "--summary", "Score", "--decision", "rejected"}, true},
		{"work receipt", []string{"card:x", "--run-id", "one", "--outcome", "measured", "--summary", "Score", "--started-at", "yesterday"}, true},
	} {
		t.Run(tc.name+strings.Join(tc.args, " "), func(t *testing.T) {
			_, _, err := parseLoopWork(tc.name, tc.args)
			if (err != nil) != tc.bad {
				t.Fatal(err)
			}
		})
	}
}
func TestLoopWorkWireRequests(t *testing.T) {
	for _, verb := range []string{"cadence", "goal", "receipt"} {
		t.Run(verb, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					if strings.HasPrefix(r.URL.Path, "/work/") {
						w.Write([]byte(`{"work":{"id":"x","ref":"card:x","version":3}}`))
					} else {
						w.Write([]byte(`{"card":{"id":"x","ref":"card:x","thread_id":"t"}}`))
					}
					return
				}
				writes++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if verb == "receipt" {
					e := asMap(body["event"])
					if e["type"] != "run_recorded" || asMap(e["payload"])["run_id"] != "stable" {
						t.Error(body)
					}
					w.Write([]byte(`{"event":{"id":"original-event"}}`))
				} else {
					if body["if_version"] != 3. || asMap(body["patch"])[verb] == nil {
						t.Error(body)
					}
					w.Write([]byte(`{"work":{"id":"x"}}`))
				}
			}))
			defer srv.Close()
			a := newTestApp(t)
			args := []string{"card:x"}
			switch verb {
			case "cadence":
				args = append(args, "--every", "1d")
			case "goal":
				args = append(args, "--metric", "score", "--direction", "up")
			case "receipt":
				args = append(args, "--run-id", "stable", "--outcome", "no_op", "--summary", "Nothing changed")
			}
			result, err := a.runLoopWork(context.Background(), "work "+verb, args, config.Resolved{BaseURL: srv.URL, AccessToken: "token"})
			if err != nil {
				t.Fatal(err)
			}
			if writes != 1 {
				t.Fatal(writes)
			}
			if verb == "receipt" && result.Text != "original-event" {
				t.Fatal(result.Text)
			}
		})
	}
}

func TestLoopReceiptTextAndJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			w.Write([]byte(`{"card":{"id":"x","ref":"card:x"}}`))
			return
		}
		w.Write([]byte(`{"event":{"id":"original-event","type":"run_recorded"}}`))
	}))
	defer srv.Close()
	args := []string{"work", "receipt", "card:x", "--run-id", "stable", "--outcome", "no_op", "--summary", "Nothing changed"}
	env := map[string]string{"ANX_BASE_URL": srv.URL, "ANX_ACCESS_TOKEN": "token"}
	text := runCLIForTest(t, t.TempDir(), env, nil, args)
	if text != "original-event\n" {
		t.Fatal(text)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), env, nil, append([]string{"--json"}, args...)))
	if asMap(asMap(payload["result"])["event"])["id"] != "original-event" {
		t.Fatal(payload)
	}
}

// The external optimizer adapter uses this full positional/flag grammar.
func TestLoopAdapterFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"receipt", []string{"card:optimizer", "--run-id", "r1", "--outcome", "acted", "--summary", "Kept candidate", "--metric", "quality=0.9", "--metric", "latency=10", "--candidate", "c1", "--decision", "kept", "--score-before", "0.8", "--score-after", "0.9", "--ref", "card:proof", "--ref", "https://example.org/change/1", "--started-at", "2026-10-16T00:00:00Z"}, `{"type":"run_recorded","summary":"Kept candidate","payload":{"run_id":"r1","outcome":"acted","metrics":{"quality":0.9,"latency":10},"experiment":{"candidate":"c1","decision":"kept","score_before":0.8,"score_after":0.9},"effect_refs":["card:proof","https://example.org/change/1"],"started_at":"2026-10-16T00:00:00Z"}}`},
		{"cadence", []string{"card:optimizer", "--every", "1d", "--budget", "7d", "--stop-rule", "target reached"}, `{"cadence":{"every_seconds":86400,"progress_budget_seconds":604800,"stop_rule":"target reached"}}`},
		{"goal", []string{"card:optimizer", "--metric", "quality", "--direction", "up", "--target", "0.9", "--baseline", "0", "--unit", "score", "--series", "series:quality?project=optimizer"}, `{"goal":{"metric":"quality","direction":"up","target":0.9,"baseline":0,"unit":"score","series_ref":"series:quality?project=optimizer"}}`},
		{"cadence", []string{"card:optimizer", "--clear"}, `{"cadence":null}`},
		{"goal", []string{"card:optimizer", "--clear"}, `{"goal":null}`},
	}
	for _, tc := range cases {
		t.Run(tc.name+strings.Join(tc.args, " "), func(t *testing.T) {
			card, got, err := parseLoopWork("work "+tc.name, tc.args)
			if err != nil || card != "card:optimizer" {
				t.Fatal(card, err)
			}
			raw, _ := json.Marshal(got)
			var actual, want any
			if err := json.Unmarshal(raw, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, want) {
				t.Fatalf("got %s want %s", raw, tc.want)
			}
		})
	}
}

func TestLoopValuesCannotBecomeGlobalFlags(t *testing.T) {
	for _, tc := range []struct{ verb, flag, value string }{
		{"receipt", "summary", "--timeout=x"},
		{"receipt", "summary", "--base-url=http://evil"},
		{"receipt", "summary", "--json"},
		{"cadence", "stop-rule", "-x"},
		{"goal", "unit", "--base-url=http://evil"},
	} {
		t.Run(tc.verb+tc.value, func(t *testing.T) {
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == "GET" {
					w.Write([]byte(`{"card":{"id":"x","ref":"card:x","thread_id":"t"},"work":{"id":"x","ref":"card:x","version":3}}`))
					return
				}
				writes++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				var got any
				if tc.verb == "receipt" {
					got = asMap(body["event"])["summary"]
					w.Write([]byte(`{"event":{"id":"recorded"}}`))
				} else {
					key := strings.ReplaceAll(tc.flag, "-", "_")
					got = asMap(asMap(body["patch"])[tc.verb])[key]
					w.Write([]byte(`{"work":{"id":"x"}}`))
				}
				if got != tc.value {
					t.Errorf("value changed: got=%v want=%s", got, tc.value)
				}
			}))
			defer srv.Close()
			args := []string{"work", tc.verb, "card:x"}
			switch tc.verb {
			case "receipt":
				args = append(args, "--run-id", "one", "--outcome", "measured")
			case "cadence":
				args = append(args, "--every", "1d")
			case "goal":
				args = append(args, "--metric", "score", "--direction", "up")
			}
			args = append(args, "--"+tc.flag, tc.value)
			overrides, remaining, _, err := parseGlobalFlags(args)
			if err != nil || !reflect.DeepEqual(remaining, args) || overrides.BaseURL != nil || overrides.Timeout != nil || overrides.JSON != nil {
				t.Fatalf("global scan: %+v %v %v", overrides, remaining, err)
			}
			runCLIForTest(t, t.TempDir(), map[string]string{"ANX_BASE_URL": srv.URL, "ANX_ACCESS_TOKEN": "token"}, nil, args)
			if writes != 1 {
				t.Fatalf("writes=%d", writes)
			}
		})
	}
}

func TestGlobalScanRespectsOtherCommandValues(t *testing.T) {
	for _, args := range [][]string{
		{"cards", "message", "card:x", "--body", "--base-url=http://evil", "--json"},
		{"cards", "archive", "card:x", "--reason", "--timeout=x", "--json"},
		{"work", "--json", "receipt", "card:x", "--summary", "--base-url=http://evil"},
		{"await", "event:x", "--timeout", "--base-url=http://evil", "--json"},
	} {
		overrides, remaining, _, err := parseGlobalFlags(args)
		if err != nil || overrides.BaseURL != nil || overrides.Timeout != nil || overrides.JSON == nil || !*overrides.JSON {
			t.Fatalf("%v: %+v %v %v", args, overrides, remaining, err)
		}
	}
}
