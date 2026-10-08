package app

import (
	"agent-nexus-cli/internal/config"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPMToolStepNeverIncludesFreeText(t *testing.T) {
	for _, tc := range []struct {
		args          []string
		label, target string
	}{
		{[]string{"cards", "get", "card:release"}, "anx cards get", "card:release"},
		{[]string{"cards", "get", "private-body"}, "anx cards get", ""},
		{[]string{"docs", "create", "--body", "--card-id", "card:private"}, "anx docs create", ""},
		{[]string{"cards", "get", "https://secret.example/token"}, "anx cards get", ""},
		{[]string{"pm", "turns", "heartbeat", "turn", "--lease-token", "secret"}, "", ""},
		{[]string{"api", "get", "/private"}, "", ""},
	} {
		label, target := pmToolStep(tc.args)
		if label != tc.label || target != tc.target {
			t.Fatalf("%v: %q %q", tc.args, label, target)
		}
	}
}

func TestPMToolActivityIsScopedSilentAndBestEffort(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/pm/turns/turn/heartbeat" {
			t.Errorf("path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		raw, _ := json.Marshal(body)
		if strings.Contains(string(raw), "private-body") || !strings.Contains(string(raw), "anx cards get") {
			t.Errorf("payload %s", raw)
		}
		w.WriteHeader(503)
	}))
	defer srv.Close()
	a := newTestApp(t)
	a.Stdout = io.Discard
	a.Stderr = io.Discard
	env := map[string]string{"ANX_PM_ACTIVITY_ENABLED": "1", "ANX_PM_TURN_ID": "turn", "ANX_PM_LEASE_TOKEN": "lease", "ANX_PM_BASE_URL": srv.URL, "ANX_PM_AGENT": "pm"}
	a.Getenv = func(k string) string { return env[k] }
	cfg := config.Resolved{BaseURL: srv.URL, Agent: "pm", AccessToken: "fixture", Timeout: time.Second}
	a.emitPMToolActivity([]string{"cards", "get", "private-body"}, cfg)
	if calls != 1 {
		t.Fatal(calls)
	}
	cfg.Agent = "other"
	a.emitPMToolActivity([]string{"cards", "get", "card:release"}, cfg)
	cfg.Agent = "pm"
	env["ANX_PM_BASE_URL"] = "http://other"
	a.emitPMToolActivity([]string{"cards", "get", "card:release"}, cfg)
	if calls != 1 {
		t.Fatal("scope override emitted")
	}
}

func TestRunReportsPMToolOnceWithoutChangingOutput(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/heartbeat") {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path != "/cards/card:release" {
			t.Errorf("path %s", r.URL.Path)
		}
		io.WriteString(w, `{"id":"release","title":"Release","status":"in_review"}`)
	}))
	defer srv.Close()
	a := newTestApp(t)
	var stdout, stderr strings.Builder
	a.Stdout = &stdout
	a.Stderr = &stderr
	env := map[string]string{"ANX_PM_ACTIVITY_ENABLED": "1", "ANX_PM_TURN_ID": "turn", "ANX_PM_LEASE_TOKEN": "lease", "ANX_PM_BASE_URL": srv.URL, "ANX_PM_AGENT": "pm", "ANX_BASE_URL": srv.URL, "ANX_ACCESS_TOKEN": "fixture", "ANX_AS": "pm"}
	a.Getenv = func(k string) string { return env[k] }
	if code := a.Run([]string{"--json", "--as", "pm", "cards", "get", "card:release"}); code != 0 {
		t.Fatalf("code %d stderr %s output %s", code, stderr.String(), stdout.String())
	}
	if len(paths) != 2 || paths[0] != "/pm/turns/turn/heartbeat" {
		t.Fatalf("requests %v", paths)
	}
	if !strings.Contains(stdout.String(), "Release") || strings.Contains(stdout.String(), "heartbeat") || stderr.Len() != 0 {
		t.Fatalf("stdout %s stderr %s", stdout.String(), stderr.String())
	}
}

func TestPMToolReportingDeadlineDoesNotBlockTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	a := newTestApp(t)
	env := map[string]string{"ANX_PM_ACTIVITY_ENABLED": "1", "ANX_PM_TURN_ID": "turn", "ANX_PM_LEASE_TOKEN": "lease", "ANX_PM_BASE_URL": srv.URL, "ANX_PM_AGENT": "pm"}
	a.Getenv = func(k string) string { return env[k] }
	start := time.Now()
	a.emitPMToolActivity([]string{"cards", "get", "card:release"}, config.Resolved{BaseURL: srv.URL, Agent: "pm", AccessToken: "fixture", Timeout: 5 * time.Second})
	if elapsed := time.Since(start); elapsed > 750*time.Millisecond {
		t.Fatalf("telemetry blocked for %s", elapsed)
	}
}
