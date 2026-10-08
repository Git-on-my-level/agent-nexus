package app

import (
	"agent-nexus-cli/internal/config"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPMProgressProtocolIsBoundedAndDoesNotForwardPayloads(t *testing.T) {
	p := newPMProgress(map[string]any{}, 128)
	p.Write([]byte(`{"type":"pm_activity","kind":"tool","label":"Reading the task","target":"card:release","arguments":{"secret":"never-forward"}}` + "\n"))
	p.Write([]byte(`{"type":"pm_partial","text":"Draft answer"}` + "\n"))
	p.Write([]byte(`{"type":"tool","label":"raw native event","arguments":{"secret":"never-forward"}}` + "\n"))
	body := p.body("lease")
	events := body["activity"].([]map[string]any)
	if len(events) != 2 || events[1]["arguments"] != nil || body["partial_response"] != "Draft answer" || body["partial_sequence"] != 1 {
		t.Fatalf("body %+v", body)
	}
	p.Write([]byte(strings.Repeat("x", 3000) + "\n" + `{"type":"pm_activity","kind":"status","label":"Recovered"}` + "\n"))
	if len(p.line) > 1792 || p.body("lease")["activity"].([]map[string]any)[2]["label"] != "Recovered" {
		t.Fatal("oversized line recovery")
	}
	raw := []byte("{\"type\":\"pm_activity\",\"kind\":\"status\",\"label\":\"Step\"}\nFinal answer")
	if string(withoutPMProgress(raw)) != "Final answer" {
		t.Fatal("activity leaked into final answer")
	}
	p.disable()
	if len(p.body("lease")) != 1 {
		t.Fatal("old-core fallback includes activity")
	}
}

func TestDirectRunnerFlushesShortTurnActivityBeforeCompletion(t *testing.T) {
	var mu sync.Mutex
	var toolSeen bool
	var answer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/heartbeat") {
			for _, raw := range asSlice(body["activity"]) {
				if asMap(raw)["label"] == "Reading the task" {
					toolSeen = true
				}
			}
			io.WriteString(w, `{"lease_expires_at":"2099-01-01T00:00:00Z"}`)
		} else if strings.HasSuffix(r.URL.Path, "/complete") {
			answer = anyString(body["text"])
			io.WriteString(w, `{"status":"delivered"}`)
		} else {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	app := newTestApp(t)
	app.Stdout = io.Discard
	app.Stderr = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	turn := claimedTurn()
	turn["activity_supported"] = true
	argv := []string{"/bin/sh", "-c", `printf '%s\n' '{"type":"pm_activity","kind":"tool","label":"Reading the task","target":"card:release"}' '{"role":"assistant","content":"It is awaiting review."}'`, "{prompt}"}
	if !app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", argv, nil, turn, nil) {
		t.Fatal("turn did not settle")
	}
	mu.Lock()
	defer mu.Unlock()
	if !toolSeen || answer != "It is awaiting review." {
		t.Fatalf("tool=%v answer=%q", toolSeen, answer)
	}
}

func TestPMProgressAcceptsEscapedDraftWithinDecodedLimit(t *testing.T) {
	p := newPMProgress(map[string]any{}, 16000)
	text := strings.Repeat("\"", 12000)
	raw, _ := json.Marshal(map[string]string{"type": "pm_partial", "text": text})
	p.Write(append(raw, '\n'))
	if p.body("lease")["partial_response"] != text {
		t.Fatal("valid escaped snapshot discarded")
	}
}
