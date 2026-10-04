package app

import (
	"agent-nexus-cli/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSeriesUsageBeforeHostResolution(t *testing.T) {
	for _, args := range [][]string{{"series", "push", "count", "1", "--unknown"}, {"series", "push", "count", "NaN"}, {"series", "push", "--from-command"}, {"series", "query", "x", "--adapter", "y"}, {"adapters", "declare"}, {"adapters", "revoke"}, {"series", "push", "x", "1", "--label", "x=a", "--label", "x=b"}} {
		a := New()
		a.Getenv = func(string) string { return "" }
		a.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
		a.Stderr = &bytes.Buffer{}
		a.Stdout = &bytes.Buffer{}
		if code := a.Run(args); code != 2 {
			t.Fatalf("%v returned %d: %s", args, code, a.Stdout)
		}
	}
	for _, args := range [][]string{{"series", "push", "count", "-12"}, {"series", "push", "--from-command", "--series", "count", "--", "printf", "12"}, {"series", "push", "count", "--from-command", "--", "printf", "12"}, {"series", "query", "count", "--range", "7d", "--label", "initiative=launch"}, {"adapters", "declare", "--body-file", "declaration.json"}} {
		if _, err := parseSeriesCommand(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
func TestSeriesCommandOutputAndEffects(t *testing.T) {
	for _, raw := range []string{"12", `{"series":"health","state":"healthy","labels":{"host":"a"}}`} {
		if _, err := decodeSeriesOutput([]byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`true`, `{"url":"https://evil.test"}`, `{"value":1,"state":"healthy"}`, `{"value":"abc"}`, `1e20`} {
		if _, err := decodeSeriesOutput([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for command, want := range map[string]string{"series query": "read_only", "series push": "remote_coordination_write", "series push --from-command": "external_side_effect", "adapters declare": "remote_coordination_write"} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatalf("%s: %s", command, got)
		}
	}
}

func TestSeriesPushExchangesScopeAndCommandPayload(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/series" {
			if r.Header.Get("Authorization") != "Bearer owner" {
				t.Error("inventory identity")
			}
			io.WriteString(w, `{"series":[{"name":"builds","adapter":"collector"}]}`)
			return
		}
		if r.URL.Path == "/adapters/collector/token" {
			if r.Header.Get("Authorization") != "Bearer owner" {
				t.Error("exchange identity")
			}
			io.WriteString(w, `{"tokens":{"access_token":"scoped"}}`)
			return
		}
		if r.URL.Path == "/series/builds/points" {
			if r.Header.Get("Authorization") != "Bearer scoped" {
				t.Error("push must use scoped token")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["value"] != float64(7) || asMap(body["labels"])["initiative"] != "launch" {
				t.Errorf("point: %#v", body)
			}
			io.WriteString(w, `{"series":"builds","accepted":true}`)
			return
		}
		t.Errorf("unexpected call %s", r.URL.Path)
		w.WriteHeader(404)
	}))
	defer server.Close()
	a := New()
	a.Stderr = &bytes.Buffer{}
	_, _, err := a.runSeriesCommand(context.Background(), []string{"series", "push", "--from-command", "--label", "initiative=launch", "--", "printf", "%s", `{"series":"builds","value":7}`}, config.Resolved{BaseURL: server.URL, AccessToken: "owner", Timeout: time.Second * 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("scope flow: %v", calls)
	}
}

func TestSeriesHelpAvailableWithoutEnrollment(t *testing.T) {
	for _, topic := range []string{"series", "adapters", "series push", "series query", "adapters declare"} {
		text, ok := helpTopicText(topic)
		if !ok || text == "" {
			t.Fatalf("missing %s help", topic)
		}
	}
}
