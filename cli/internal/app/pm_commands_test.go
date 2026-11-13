package app

import (
	"agent-nexus-cli/internal/config"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestPMContextKeepsFilters(t *testing.T) {
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

func TestPMCardReadUsesContextRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/pm/context" || r.URL.Query().Get("work_ref") != "card:x" {
			t.Error(r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[{"ref":"card:x","title":"X","status":"ready"}]}`))
	}))
	defer srv.Close()
	a := New()
	a.Getenv = func(string) string { return "" }
	r, err := a.runPMCardRead(context.Background(), []string{"card:x"}, config.Resolved{BaseURL: srv.URL, AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	finishPMCardRead(r)
	if !strings.Contains(r.Text, "card card:x X status=ready") {
		t.Fatalf("text %q", r.Text)
	}
}

func TestPMProposeBuildsPayloadAndHidesPlumbing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pm/context":
			if r.URL.Query().Get("work_ref") != "card:x" {
				t.Error(r.URL)
			}
			_, _ = w.Write([]byte(`{"items":[{"ref":"card:x","title":"X","decision_revision":"1.2"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/pm/decisions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["lease_token"]; ok {
				t.Errorf("proposal must not carry a lease: %v", body)
			}
			if body["work_ref"] != "card:x" || body["target_revision"] != "1.2" || body["instruction"] != "Evidence reviewed" {
				t.Error(body)
			}
			payload := asMap(body["payload"])
			if payload["phase"] != "done" || payload["resolution_refs"].([]any)[0] != "event:proof" {
				t.Error(body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "d", "work_ref": "card:x", "status": "awaiting_answer", "scope": "work.phase", "payload": payload})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	a := New()
	a.Getenv = func(string) string { return "" }
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
