package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
