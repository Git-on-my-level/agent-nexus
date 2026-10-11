package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPMProposalPreflight(t *testing.T) {
	for _, args := range [][]string{{}, {"card:x"}, {"card:x", "--status", "done", "--why", "Done"}, {"card:x", "--status", "ready", "--note", "a", "--why", "b"}, {"card:x", "--status", "invalid", "--why", "b"}, {"card:x", "--note", "a", "--why", "b", "--evidence", "untyped"}} {
		if _, err := preflightConfigIndependentUsage(append([]string{"pm", "propose"}, args...)); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestClaimedPMContextRejectsEveryArgumentBeforeTransport(t *testing.T) {
	for _, args := range [][]string{{"--limit", "8"}, {"--work-ref", "card:private"}, {"--query", "secret"}, {"--cursor", "x"}, {"card:private"}} {
		a := New()
		a.Getenv = func(k string) string {
			if k == "ANX_PM_TURN_ID" {
				return "turn"
			}
			return ""
		}
		_, name, err := a.runWorkCommand(context.Background(), append([]string{"pm", "context"}, args...), config.Resolved{})
		if err == nil || name != "pm context" || !strings.Contains(err.Error(), "anx pm context") {
			t.Fatalf("%v: %s %v", args, name, err)
		}
	}
}

func TestPMContextOutsideTurnKeepsLegacyFlags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/pm/context" || r.URL.Query().Get("limit") != "8" || r.URL.Query().Get("work_ref") != "card:x" {
			t.Error(r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	a := New()
	a.Getenv = func(string) string { return "" }
	if _, _, err := a.runWorkCommand(context.Background(), []string{"pm", "context", "--limit", "8", "--work-ref", "card:x"}, config.Resolved{BaseURL: srv.URL, AccessToken: "token"}); err != nil {
		t.Fatal(err)
	}
}

func TestInTurnPM403ExplainsIdentityMismatch(t *testing.T) {
	for _, command := range []string{"context", "card", "propose"} {
		t.Run(command, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if command == "propose" && strings.HasSuffix(r.URL.Path, "/context") {
					_, _ = w.Write([]byte(`{"items":[{"ref":"card:x","decision_revision":"1.2"}]}`))
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"PM permission denied"}}`))
			}))
			defer srv.Close()

			a := New()
			a.Getenv = func(key string) string {
				switch key {
				case "ANX_PM_TURN_ID":
					return "turn"
				case "ANX_PM_LEASE_TOKEN":
					return "lease"
				case "ANX_PM_AGENT":
					return "pm"
				default:
					return ""
				}
			}
			cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "token", Agent: "hermes"}

			var err error
			switch command {
			case "context":
				_, err = a.runPMCardRead(context.Background(), nil, cfg)
			case "card":
				_, err = a.runPMCardRead(context.Background(), []string{"card:x"}, cfg)
			case "propose":
				_, err = a.runPMPropose(context.Background(), []string{"card:x", "--status", "ready", "--why", "Move it"}, cfg)
			}
			if err == nil {
				t.Fatal("expected forbidden response")
			}
			var typed *errnorm.Error
			if !errors.As(err, &typed) {
				t.Fatalf("error type %T: %v", err, err)
			}
			want := "Acting as `hermes`, but this turn belongs to `pm`; run the anx CLI the runner started."
			if typed.Hint != want {
				t.Fatalf("hint %q, want %q", typed.Hint, want)
			}
		})
	}
}

func TestPMTurnLeaseIdentityOverridesExplicitIdentityForInTurnCommands(t *testing.T) {
	for _, command := range []string{"context", "card", "propose"} {
		t.Run(command, func(t *testing.T) {
			a := New()
			a.Getenv = func(key string) string {
				switch key {
				case "ANX_PM_TURN_ID":
					return "turn"
				case "ANX_PM_LEASE_TOKEN":
					return "lease"
				case "ANX_PM_AGENT":
					return "pm"
				}
				return ""
			}
			got, warning := a.resolvePMTurnLeaseIdentity([]string{"pm", command}, config.Resolved{As: "hermes", Agent: "hermes"})
			if got.As != "pm" || got.Agent != "pm" {
				t.Fatalf("resolved identity As=%q Agent=%q", got.As, got.Agent)
			}
			want := "warning: ignoring explicit identity `hermes`; this PM turn lease is bound to `pm`, so acting as `pm`."
			if warning != want {
				t.Fatalf("warning %q, want %q", warning, want)
			}
		})
	}
}

func TestPMTurnLeaseIdentityDoesNotChangeOtherCommandsOrIncompleteLease(t *testing.T) {
	a := New()
	a.Getenv = func(key string) string {
		switch key {
		case "ANX_PM_TURN_ID":
			return "turn"
		case "ANX_PM_LEASE_TOKEN":
			return "lease"
		case "ANX_PM_AGENT":
			return "pm"
		}
		return ""
	}
	for _, args := range [][]string{{"pm", "turns", "context"}, {"pm", "status"}, {"cards", "get", "card:x"}} {
		got, warning := a.resolvePMTurnLeaseIdentity(args, config.Resolved{As: "hermes", Agent: "hermes"})
		if got.As != "hermes" || got.Agent != "hermes" || warning != "" {
			t.Fatalf("%v changed identity to As=%q Agent=%q warning=%q", args, got.As, got.Agent, warning)
		}
	}
	a.Getenv = func(key string) string {
		if key == "ANX_PM_TURN_ID" {
			return "turn"
		}
		return ""
	}
	got, warning := a.resolvePMTurnLeaseIdentity([]string{"pm", "context"}, config.Resolved{As: "hermes", Agent: "hermes"})
	if got.As != "hermes" || got.Agent != "hermes" || warning != "" {
		t.Fatalf("incomplete lease changed identity to As=%q Agent=%q warning=%q", got.As, got.Agent, warning)
	}
}

func TestPMContextUsesLeaseAgentWhenANXASIsDifferent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		envAs string
	}{
		{name: "ANX_AS", args: []string{"pm", "context"}, envAs: "hermes"},
		{name: "flag", args: []string{"--as", "hermes", "pm", "context"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requestAgent string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/pm/turns/turn/context" {
					t.Errorf("request %s %s", r.Method, r.URL.Path)
				}
				requestAgent = r.Header.Get("X-ANX-Agent")
				_, _ = w.Write([]byte(`{"items":[]}`))
			}))
			defer srv.Close()

			env := map[string]string{
				"ANX_BASE_URL":       srv.URL,
				"ANX_ACCESS_TOKEN":   "fixture",
				"ANX_PM_TURN_ID":     "turn",
				"ANX_PM_LEASE_TOKEN": "lease",
				"ANX_PM_AGENT":       "pm",
			}
			if tc.envAs != "" {
				env["ANX_AS"] = tc.envAs
			}
			a := New()
			a.Getenv = func(key string) string { return env[key] }
			var stdout, stderr strings.Builder
			a.Stdout = &stdout
			a.Stderr = &stderr
			if code := a.Run(tc.args); code != 0 {
				t.Fatalf("exit %d; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if requestAgent != "pm" {
				t.Fatalf("request acted as %q, want pm", requestAgent)
			}
			wantWarning := "warning: ignoring explicit identity `hermes`; this PM turn lease is bound to `pm`, so acting as `pm`.\n"
			if stderr.String() != wantWarning {
				t.Fatalf("warning %q, want one line %q", stderr.String(), wantWarning)
			}
		})
	}
}

func TestPMProposeBuildsPayloadAndHidesPlumbing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls++
		if body["lease_token"] != "lease" {
			t.Errorf("missing lease: %v", body)
		}
		if calls == 1 {
			if body["view"] != "card" || body["context_ref"] != "card:x" {
				t.Error(body)
			}
			_, _ = w.Write([]byte(`{"items":[{"ref":"card:x","title":"X","decision_revision":"1.2"}]}`))
			return
		}
		if body["work_ref"] != "card:x" || body["target_revision"] != "1.2" || body["instruction"] != "Evidence reviewed" {
			t.Error(body)
		}
		payload := asMap(body["payload"])
		if payload["phase"] != "done" || payload["resolution_refs"].([]any)[0] != "event:proof" {
			t.Error(body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "d", "work_ref": "card:x", "status": "awaiting_answer", "scope": "work.phase", "payload": payload})
	}))
	defer srv.Close()
	a := New()
	a.Getenv = func(k string) string {
		switch k {
		case "ANX_PM_TURN_ID":
			return "turn"
		case "ANX_PM_LEASE_TOKEN":
			return "lease"
		}
		return ""
	}
	r, err := a.runPMPropose(context.Background(), []string{"card:x", "--status", "done", "--why", "Evidence reviewed", "--evidence", "event:proof"}, config.Resolved{BaseURL: srv.URL, AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.Contains(r.Text, "work") || strings.Contains(r.Text, "lease") {
		t.Fatal(r.Text, calls)
	}
	if asMap(commandResultBody(r))["scope"] != "status" {
		t.Fatal(r.Data)
	}
}

func TestPMTurnPromptMeasurement(t *testing.T) {
	before, err := os.ReadFile("testdata/pm_prompt_before.txt")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("testdata/pm_prompt_after.txt")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildPMPrompt("pm", map[string]any{"id": "pm_turn_1", "actor_id": "human-reader", "deadline": "2026-10-10T10:00:00Z", "text": "What needs my decision?", "context_refs": []string{"card:eval-card"}}, 16000)
	if prompt != string(after) {
		t.Fatal("refresh rendered after prompt fixture")
	}
	if len(after)*100 >= len(before)*40 {
		t.Fatalf("prompt over 40%%: %d/%d", len(after), len(before))
	}
	t.Logf("prompt bytes before=%d after=%d ratio=%.1f%%", len(before), len(after), 100*float64(len(after))/float64(len(before)))
}
