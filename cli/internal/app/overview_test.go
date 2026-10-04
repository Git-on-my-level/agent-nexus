package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOverviewAndDashboardCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/workspace/dashboard" {
			if r.Method != "PUT" {
				t.Errorf("pin method: %s", r.Method)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["document_ref"]; !ok {
				t.Errorf("missing document_ref: %v", body)
			}
		} else if r.URL.Path != "/overview" && r.URL.Path != "/overview/changes" && r.URL.Path != "/workspace/dashboard/reports" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"generated_at":"2026-10-04T12:00:00Z","work":{"total":7},"initiatives":{"items":[{"ref":"card:pilot","title":"Launch pilot","progress":{"done":3,"total":7},"priority":"p1","needs":["Needs David: approve"]}]},"needs_you":{"count":2,"rows":[]},"dashboard":{"reports":[]}}`))
	}))
	defer server.Close()
	for _, args := range [][]string{{"overview"}, {"overview", "changes"}, {"workspace", "dashboard", "list"}, {"workspace", "dashboard", "set", "document:ceo"}, {"workspace", "dashboard", "set", "none"}} {
		argv := append([]string{"--json", "--base-url", server.URL}, args...)
		payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{}, nil, argv))
		if payload["ok"] != true {
			t.Fatal(payload)
		}
	}
	text := runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{"--base-url", server.URL, "overview"})
	if !strings.Contains(text, "progress.done=3") || !strings.Contains(text, "progress.total=7") || !strings.Contains(text, `needs.0="Needs David: approve"`) {
		t.Fatalf("text Overview diverged from the shared initiative projection: %s", text)
	}
	if commandSideEffectClass("workspace dashboard set") != "remote_coordination_write" {
		t.Fatal("pin classified as read")
	}
	if err := preflightWorkspaceSubcommand([]string{"dashboard", "set"}); err == nil {
		t.Fatal("missing doc accepted")
	}
}
