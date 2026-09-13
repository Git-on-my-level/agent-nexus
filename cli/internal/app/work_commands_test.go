package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/registry"
)

func TestWorkUsageBeforeProfileResolution(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"one", "two"} {
		writeAgentProfile(t, home, name, `{"agent":"`+name+`","base_url":"http://127.0.0.1:1","access_token":"fixture","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	}
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"work", "list", "--nonsense"}, "invalid_flags"},
		{[]string{"work", "list", "--limit", "0"}, "invalid_request"},
		{[]string{"work", "list", "--limit", "abc"}, "invalid_flags"},
		{[]string{"work", "get"}, "invalid_request"},
		{[]string{"work", "get", "first", "second"}, "invalid_args"},
		{[]string{"work", "get", "../decisions"}, "invalid_request"},
		{[]string{"work", "get", "id", "--work-id", "other"}, "invalid_request"},
		{[]string{"work", "invent"}, "unknown_subcommand"},
		{[]string{"work", "observations", "submit"}, "invalid_request"},
		{[]string{"work", "observations", "submit", "--workspace-id", "other"}, "invalid_flags"},
		{[]string{"pm", "turns", "get"}, "invalid_request"},
		{[]string{"pm", "turns", "invent"}, "unknown_subcommand"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			a := New()
			a.Stdout = &stdout
			a.Stderr = &stderr
			a.Stdin = strings.NewReader("")
			a.UserHomeDir = func() (string, error) { return home, nil }
			a.ReadFile = os.ReadFile
			a.Getenv = func(string) string { return "" }
			if got := a.Run(append([]string{"--json"}, tc.args...)); got != 2 {
				t.Errorf("exit=%d want 2: %s", got, stdout.String())
			}
			payload := assertEnvelopeError(t, stdout.String())
			if got := asMap(payload["error"])["code"]; got != tc.code {
				t.Errorf("code=%v want %s: %s", got, tc.code, stdout.String())
			}
		})
	}
}

func TestWorkHelpOffline(t *testing.T) {
	for _, args := range [][]string{{"help", "work"}, {"work", "list", "--help"}, {"work", "observations", "submit", "--help"}} {
		payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		raw, _ := json.Marshal(payload["data"])
		if !strings.Contains(string(raw), "anx ") {
			t.Errorf("missing useful help: %s", raw)
		}
	}
}

func TestWorkRequestsUseCentralAPI(t *testing.T) {
	for _, tc := range []struct {
		args                                []string
		method, path, query, body, response string
	}{
		{[]string{"work", "list", "--project-ref", "topic:launch", "--source", "github", "--owner", "actor:dev", "--phase", "review", "--freshness", "stale", "--limit", "2", "--cursor", "opaque+cursor"}, "GET", "/work", "cursor=opaque%2Bcursor&freshness=stale&limit=2&owner=actor%3Adev&phase=review&project_ref=topic%3Alaunch&source=github", "", `{"work":[],"next_cursor":"next"}`},
		{[]string{"work", "get", "card:launch"}, "GET", "/work/card:launch", "", "", `{"work":{"ref":"card:launch","phase":"unknown"}}`},
		{[]string{"work", "observations", "list", "card:launch", "--limit", "1", "--cursor", "next"}, "GET", "/work/card:launch/observations", "cursor=next&limit=1", "", `{"observations":[],"next_cursor":"older"}`},
		{[]string{"work", "observations", "submit", "card:launch", "--from-file", "-"}, "POST", "/work/card:launch/observations", "", `{"observation":{"idempotency_key":"report-1","reader_id":"reader","reader_revision":"v1","observed_at":"2026-09-08T00:00:00Z","status":"reported","facts":{},"evidence":[]}}`, `{"duplicate":true,"observation":{"id":"obs-1"},"work":{"ref":"card:launch"}}`},
		{[]string{"work", "refresh", "request", "card:launch"}, "POST", "/work/card:launch/refresh", "", `{}`, `{"refresh":{"state":"queued"}}`},
		{[]string{"work", "refresh", "get", "card:launch"}, "GET", "/work/card:launch/refresh", "", "", `{"refresh":{"state":"failed","last_error":"unavailable"}}`},
		{[]string{"work", "capabilities"}, "GET", "/work/capabilities", "", "", `{"capabilities":{"refresh":false}}`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tc.method || r.URL.Path != tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("request=%s %s, want %s %s?%s", r.Method, r.URL, tc.method, tc.path, tc.query)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer fixture-token" {
					t.Errorf("auth=%q", got)
				}
				body, _ := io.ReadAll(r.Body)
				if tc.body == "" {
					if len(body) > 0 {
						t.Errorf("unexpected body %s", body)
					}
				} else {
					var got, want any
					_ = json.Unmarshal(body, &got)
					_ = json.Unmarshal([]byte(tc.body), &want)
					g, _ := json.Marshal(got)
					w, _ := json.Marshal(want)
					if string(g) != string(w) {
						t.Errorf("body=%s want %s", g, w)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			result := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture-token"}, strings.NewReader(tc.body), append([]string{"--json", "--base-url", server.URL}, tc.args...)))
			if calls != 1 {
				t.Errorf("calls=%d", calls)
			}
			got, _ := json.Marshal(result["data"])
			var want any
			_ = json.Unmarshal([]byte(tc.response), &want)
			w, _ := json.Marshal(want)
			if string(got) != string(w) {
				t.Errorf("payload=%s want %s", got, w)
			}
		})
	}
}

func TestWorkContextReadOnlyAndFreshnessHonest(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.RequestURI())
		if r.Method != "GET" {
			t.Errorf("read triggered %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/card:launch":
			io.WriteString(w, `{"work":{"ref":"card:launch","phase":"done","freshness":{"status":"error","last_observed_at":"2026-01-01T00:00:00Z"},"executions":[{"result_ref":"artifact:build"}],"latest_observation":{"status":"uncertain"}}}`)
		case "/work/card:launch/observations":
			io.WriteString(w, `{"observations":[{"status":"error"}],"next_cursor":"older"}`)
		case "/work/card:launch/refresh":
			io.WriteString(w, `{"refresh":{"state":"failed","last_error":"reader unavailable"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "work", "context", "card:launch", "--limit", "1", "--cursor", "page"}))
	body := asMap(payload["data"])
	if got := asMap(body["observations"])["next_cursor"]; got != "older" {
		t.Errorf("pagination lost: %v", body)
	}
	if got := asMap(body["refresh"])["state"]; got != "failed" {
		t.Errorf("failure lost: %v", body)
	}
	if len(calls) != 3 || calls[1] != "/work/card:launch/observations?cursor=page&limit=1" {
		t.Errorf("requests=%v", calls)
	}
	calls = nil
	payload = assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "work", "freshness", "card:launch"}))
	body = asMap(payload["data"])
	if got := asMap(body["freshness"])["status"]; got != "error" {
		t.Errorf("freshness=%v", body)
	}
	if len(calls) != 1 {
		t.Errorf("freshness calls=%v", calls)
	}
}

func TestWorkRemoteErrorsAndInvalidBodies(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"code":"scope_denied","message":"fixture denial","details":{"workspace":"other"}}}`)
			}))
			defer server.Close()
			payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, strings.NewReader(`{"observation":{}}`), []string{"--json", "--base-url", server.URL, "work", "observations", "submit", "card:launch", "--from-file", "-"}))
			if payload["command_id"] != "work.observations.submit" {
				t.Errorf("identity lost: %v", payload)
			}
			if !strings.Contains(fmt.Sprint(payload["error"]), "fixture denial") {
				t.Errorf("remote failure lost: %v", payload)
			}
			if calls != 1 {
				t.Errorf("unexpected write retry: %d", calls)
			}
		})
	}
	for _, body := range []string{"", `null`, `[]`, `{"observation":{}} {}`, `{"observation":`} {
		result := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, strings.NewReader(body), []string{"--json", "--base-url", "http://127.0.0.1:1", "work", "observations", "submit", "card:launch", "--from-file", "-"}))
		if code := asMap(result["error"])["code"]; code != "invalid_json" {
			t.Errorf("body %q code=%v", body, code)
		}
	}
}

func TestPMCommandsUseDurableDecisionAndReceiptAPI(t *testing.T) {
	for _, tc := range []struct {
		args                         []string
		method, path, body, response string
	}{
		{[]string{"pm", "context", "--work-ref", "card:launch", "--query", "evidence", "--limit", "5", "--cursor", "ctx+page"}, "GET", "/pm/context?cursor=ctx%2Bpage&limit=5&query=evidence&work_ref=card%3Alaunch", "", `{"items":[],"limitations":["partial"],"next_cursor":"next-ctx"}`},
		{[]string{"pm", "decisions", "list"}, "GET", "/pm/decisions", "", `{"items":[],"has_more":true}`},
		{[]string{"pm", "decisions", "get", "decision-1"}, "GET", "/pm/decisions/decision-1", "", `{"id":"decision-1","status":"answered","action_id":"action-1"}`},
		{[]string{"pm", "decisions", "create", "--from-file", "-"}, "POST", "/pm/decisions", `{"request_key":"decision-key","work_ref":"card:launch","instruction":"Review","scope":"review","target_revision":"abc"}`, `{"id":"decision-1","status":"awaiting_answer"}`},
		{[]string{"pm", "decisions", "answer", "decision-1", "--from-file", "-"}, "POST", "/pm/decisions/decision-1/answer", `{"revision":1,"approve":true,"text":"Approved"}`, `{"id":"decision-1","status":"answered"}`},
		{[]string{"pm", "decisions", "dispatch", "decision-1"}, "POST", "/pm/decisions/decision-1/dispatch", `{}`, `{"id":"action-1","status":"failed","receipt":{"detail":"unavailable"}}`},
		{[]string{"pm", "actions", "list"}, "GET", "/pm/actions", "", `{"items":[],"has_more":true}`},
		{[]string{"pm", "actions", "get", "action-1"}, "GET", "/pm/actions/action-1", "", `{"id":"action-1","status":"source_reported","receipt":{"independently_verified":false}}`},
		{[]string{"pm", "actions", "reconcile", "action-1"}, "POST", "/pm/actions/action-1/reconcile", `{}`, `{"id":"action-1","status":"unknown","receipt":{"independently_verified":false}}`},
		{[]string{"pm", "actions", "acknowledge", "action-1"}, "POST", "/pm/actions/action-1/acknowledge", `{}`, `{"id":"action-1","status":"acknowledged","acknowledged_by":"actor:human","acknowledged_at":"2026-09-13T02:00:00Z"}`},
		{[]string{"pm", "turns", "claim"}, "POST", "/pm/turns/claim", `{}`, `{"id":"turn-1","status":"sending","lease_token":"abc"}`},
		{[]string{"pm", "turns", "get", "turn-1"}, "GET", "/pm/turns/turn-1", "", `{"id":"turn-1","status":"failed","deadline":"2026-09-08T22:00:00Z","failure":"deadline passed"}`},
		{[]string{"pm", "turns", "fail", "turn-1", "--from-file", "-"}, "POST", "/pm/turns/turn-1/fail", `{"reason":"harness timeout","lease_token":"abc"}`, `{"id":"turn-1","status":"failed"}`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.RequestURI() != tc.path {
					t.Errorf("request=%s %s want %s %s", r.Method, r.URL.RequestURI(), tc.method, tc.path)
				}
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("missing existing identity")
				}
				got, _ := io.ReadAll(r.Body)
				var a, b any
				_ = json.Unmarshal(got, &a)
				_ = json.Unmarshal([]byte(tc.body), &b)
				if fmt.Sprint(a) != fmt.Sprint(b) {
					t.Errorf("body=%s want %s", got, tc.body)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, strings.NewReader(tc.body), append([]string{"--json", "--base-url", server.URL}, tc.args...)))
			var want any
			_ = json.Unmarshal([]byte(tc.response), &want)
			got, _ := json.Marshal(payload["data"])
			w, _ := json.Marshal(want)
			if string(got) != string(w) {
				t.Errorf("receipt/status altered: %s want %s", got, w)
			}
		})
	}
	for _, args := range [][]string{{"help", "pm"}, {"pm", "decisions", "--help"}, {"pm", "actions", "reconcile", "--help"}, {"pm", "actions", "acknowledge", "--help"}} {
		assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
	}
}

func TestWorkAndPMMetadataDocsAreDiscoverable(t *testing.T) {
	for _, topic := range []string{"work", "work observations submit", "pm", "pm decisions answer", "pm actions reconcile", "pm actions acknowledge"} {
		payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "meta", "doc", topic}))
		if !strings.Contains(fmt.Sprint(payload), "anx ") {
			t.Errorf("missing actionable help %s: %v", topic, payload)
		}
	}
}

func TestWorkGroupHelpDoesNotRequireProfileSelection(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"one", "two"} {
		writeAgentProfile(t, home, name, `{"agent":"`+name+`","base_url":"http://127.0.0.1:1","access_token":"fixture"}`)
	}
	for _, args := range [][]string{{"work"}, {"work", "observations"}, {"work", "refresh"}, {"pm"}, {"pm", "decisions"}} {
		assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, append([]string{"--json"}, args...)))
	}
}

func TestPMRejectsUnsupportedScopeAndPaginationFlags(t *testing.T) {
	for _, args := range [][]string{
		{"pm", "decisions", "list", "--project-ref", "invented"},
		{"pm", "actions", "get", "action-1", "--actor-id", "human"},
		{"pm", "context", "--workspace-id", "other"},
		{"pm", "context", "--limit", "51"},
		{"pm", "turns", "complete", "turn-1", "--from-file", "-", "--actor-id", "other"},
	} {
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
		code := fmt.Sprint(asMap(payload["error"])["code"])
		if code != "invalid_flags" && code != "invalid_request" {
			t.Errorf("unexpected local failure %s: %v", code, payload)
		}
	}
}

func TestPMPaginationCarriesOpaqueCursor(t *testing.T) {
	for _, kind := range []string{"conversations", "decisions", "actions"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pm/"+kind || r.URL.Query().Get("cursor") != "bound+opaque" || r.URL.Query().Get("limit") != "200" {
					t.Errorf("request=%s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"items":[],"has_more":true,"next_cursor":"next-page"}`)
			}))
			defer server.Close()
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "pm", kind, "list", "--limit", "200", "--cursor", "bound+opaque"}))
			if asMap(payload["data"])["next_cursor"] != "next-page" {
				t.Errorf("cursor lost: %v", payload)
			}
		})
	}
}

func TestPMContextPaginationCarriesOpaqueCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/context" || r.URL.Query().Get("cursor") != "bound+opaque" || r.URL.Query().Get("limit") != "20" {
			t.Errorf("request=%s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"items":[{"id":"item-1","work_ref":"card:launch"}],"has_more":true,"next_cursor":"next-ctx","limitations":["partial"]}`)
	}))
	defer server.Close()
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "pm", "context", "--limit", "20", "--cursor", "bound+opaque"}))
	data := asMap(payload["data"])
	if data["next_cursor"] != "next-ctx" || data["has_more"] != true {
		t.Errorf("json envelope changed or lost pagination: %v", payload)
	}
	text := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--base-url", server.URL, "pm", "context", "--limit", "20", "--cursor", "bound+opaque"})
	if !strings.Contains(text, "has_more: true") || !strings.Contains(text, "next_cursor: next-ctx") {
		t.Errorf("text mode lost pagination: %s", text)
	}
}

func TestPMTurnsContextPaginationCarriesOpaqueCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/turns/turn-1/context" || r.URL.Query().Get("cursor") != "turn+page" {
			t.Errorf("request=%s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"items":[],"has_more":true,"next_cursor":"next-turn"}`)
	}))
	defer server.Close()
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "pm", "turns", "context", "turn-1", "--cursor", "turn+page"}))
	if asMap(payload["data"])["next_cursor"] != "next-turn" {
		t.Errorf("cursor lost: %v", payload)
	}
}

func TestPMContextHelpDocumentsCursor(t *testing.T) {
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "help", "pm", "context"}))
	raw := fmt.Sprint(payload["data"])
	if !strings.Contains(raw, "--cursor") {
		t.Errorf("pm context help missing --cursor: %s", raw)
	}
}

func TestWorkRejectsNonObjectSuccessResponse(t *testing.T) {
	for _, body := range []string{`<html>proxy login</html>`, `null`, `[]`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "--base-url", server.URL, "work", "capabilities"}))
		if asMap(payload["error"])["code"] != "invalid_response" {
			t.Errorf("not a protocol error: %v", payload)
		}
		server.Close()
	}
}

func TestWorkTextKeepsPaginationAndReceiptUncertainty(t *testing.T) {
	work := formatWorkCommandText("work list", map[string]any{"work": []any{map[string]any{"ref": "card:example", "title": "Synthetic", "phase": "done", "freshness": map[string]any{"status": "unknown"}}}, "next_cursor": "next"})
	if !strings.Contains(work, "card:example") || !strings.Contains(work, "freshness=unknown") || !strings.Contains(work, "next_cursor: next") {
		t.Errorf("lost work semantics: %s", work)
	}
	errored := formatWorkCommandText("work list", map[string]any{"work": []any{map[string]any{"ref": "card:launch", "title": "Tracked", "phase": "review", "source": map[string]any{"authority": "github"}, "freshness": map[string]any{"status": "error", "last_error": map[string]any{"code": "policy_denied", "message": "Generated reader has no active version"}}, "refresh": map[string]any{"state": "failed", "last_error": map[string]any{"code": "policy_denied", "message": "Generated reader has no active version"}}}}})
	if !strings.Contains(errored, "freshness=error") || !strings.Contains(errored, "last_error=policy_denied: Generated reader has no active version") {
		t.Fatalf("list hid refresh error: %s", errored)
	}
	got := formatWorkCommandText("work get", map[string]any{"work": map[string]any{"ref": "card:launch", "title": "Tracked", "phase": "review", "source": map[string]any{"authority": "github"}, "freshness": map[string]any{"status": "error", "last_error": map[string]any{"code": "rate_limited", "message": "GitHub rate limit reached; next attempt at 2026-09-13T05:00:00Z"}}, "refresh": map[string]any{"state": "failed", "last_error": map[string]any{"code": "rate_limited", "message": "GitHub rate limit reached; next attempt at 2026-09-13T05:00:00Z"}}}})
	if !strings.Contains(got, "last_error=rate_limited: GitHub rate limit reached") {
		t.Fatalf("get hid refresh error: %s", got)
	}
	bindings := formatWorkCommandText("pm bindings list", map[string]any{"items": []any{map[string]any{"id": "binding-1", "actor_id": "actor-david", "origin": map[string]any{"transport": "telegram", "tenant_id": "bot-1", "channel_id": "-100", "external_user_id": "42"}, "can_approve": true, "enabled": true, "revision": float64(1)}}, "has_more": false})
	if !strings.Contains(bindings, "bindings: 1") || !strings.Contains(bindings, "binding-1  telegram bot-1/-100 user=42 -> actor-david can_approve=true enabled=true revision=1") {
		t.Fatalf("unexpected bindings list text: %s", bindings)
	}
	pm := formatWorkCommandText("pm actions list", map[string]any{"items": []any{map[string]any{"id": "action-1", "work_ref": "card:example", "status": "source_reported", "receipt": map[string]any{"independently_verified": false}}}, "next_cursor": "next", "has_more": true})
	if !strings.Contains(pm, "source_reported") || !strings.Contains(pm, "verified=false") || !strings.Contains(pm, "next_cursor: next") {
		t.Errorf("lost receipt uncertainty: %s", pm)
	}
	ctx := formatWorkCommandText("pm context", map[string]any{"items": []any{map[string]any{"id": "ctx-1", "work_ref": "card:example", "status": "authorized"}}, "next_cursor": "next-ctx", "has_more": true, "limitations": []any{"partial"}})
	if !strings.Contains(ctx, "ctx-1") || !strings.Contains(ctx, "has_more: true") || !strings.Contains(ctx, "next_cursor: next-ctx") || !strings.Contains(ctx, "partial") {
		t.Errorf("lost pm context pagination: %s", ctx)
	}
	turn := formatWorkCommandText("pm turns get", map[string]any{"id": "turn-1", "status": "failed", "deadline": "2026-09-08T22:00:00Z", "failure": "deadline passed"})
	if !strings.Contains(turn, "turn-1") || !strings.Contains(turn, "status=failed") || !strings.Contains(turn, "deadline=2026-09-08T22:00:00Z") || !strings.Contains(turn, "failure=deadline passed") {
		t.Errorf("lost turn fields: %s", turn)
	}
	queued := formatWorkCommandText("pm turns get", map[string]any{"id": "turn-2", "status": "sending", "claimed": false, "deadline": "2026-09-08T22:00:00Z"})
	if !strings.Contains(queued, "status=queued") || strings.Contains(queued, "status=sending") || strings.Contains(queued, "claimed_at=") {
		t.Errorf("unclaimed sending turn should render as queued: %s", queued)
	}
	inProgress := formatWorkCommandText("pm turns get", map[string]any{"id": "turn-3", "status": "sending", "claimed": true, "claimed_at": "2026-09-08T21:00:00Z", "deadline": "2026-09-08T22:00:00Z"})
	if !strings.Contains(inProgress, "status=in progress") || !strings.Contains(inProgress, "claimed_at=2026-09-08T21:00:00Z") || strings.Contains(inProgress, "status=sending") {
		t.Errorf("claimed sending turn should render as in progress: %s", inProgress)
	}
	dispatch := formatWorkCommandText("pm decisions dispatch", map[string]any{"id": "action-1", "status": "failed", "receipt": map[string]any{"status": "failed", "detail": "unavailable"}})
	if !strings.Contains(dispatch, "action-1") || !strings.Contains(dispatch, "status=failed") || !strings.Contains(dispatch, "receipt=failed") || !strings.Contains(dispatch, "unavailable") || !strings.Contains(dispatch, "nothing was sent") {
		t.Errorf("dispatch text lost receipt outcome: %s", dispatch)
	}
	ack := formatWorkCommandText("pm actions acknowledge", map[string]any{"id": "action-1", "status": "acknowledged", "acknowledged_at": "2026-09-13T02:00:00Z"})
	if ack != "action-1  status=acknowledged  acknowledged_at=2026-09-13T02:00:00Z" {
		t.Errorf("acknowledge text lost id/status/timestamp: %s", ack)
	}
	reconcile := formatWorkCommandText("pm actions reconcile", map[string]any{"id": "action-1", "status": "source_reported", "reconciliation_conflict": true, "receipt": map[string]any{"status": "source_reported", "detail": "read-back mismatch"}})
	if !strings.Contains(reconcile, "action-1") || !strings.Contains(reconcile, "status=source_reported") || !strings.Contains(reconcile, "receipt=source_reported") || !strings.Contains(reconcile, "read-back mismatch") || !strings.Contains(reconcile, "reconciliation_conflict=true") {
		t.Errorf("reconcile text lost fields: %s", reconcile)
	}
	if strings.Contains(reconcile, `"id"`) {
		t.Errorf("reconcile still dumped JSON: %s", reconcile)
	}
}

func TestWorkCommandDispatchCoversRegistry(t *testing.T) {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range meta.Commands {
		path := strings.TrimSpace(cmd.CLIPath)
		if !strings.HasPrefix(path, "pm ") && !strings.HasPrefix(path, "work ") {
			continue
		}
		runtimePath := runtimePathFromRegistryPath(path)
		if cmd.CommandID == "pm.turns.release" {
			if _, ok := workCommands[runtimePath]; !ok {
				continue
			}
		}
		if _, ok := workCommands[runtimePath]; !ok {
			t.Errorf("registry command %s (%s) missing from workCommands as %q", cmd.CommandID, path, runtimePath)
		}
	}
}

func TestPMConflictHintsUseRevisionNotIfUpdatedAt(t *testing.T) {
	type tc struct {
		name, commandPath, errorCode, body, want, notWant string
		args                                              []string
		details                                           string
	}
	cases := []tc{
		{
			name:        "answer revision conflict",
			commandPath: "/pm/decisions/decision-1/answer",
			errorCode:   "conflict",
			body:        `{"revision":1,"approve":true,"text":"ok"}`,
			args:        []string{"pm", "decisions", "answer", "decision-1", "--from-file", "-"},
			want:        "status",
			notWant:     "retry using its current",
		},
		{
			name:        "answer superseded conflict",
			commandPath: "/pm/decisions/decision-1/answer",
			errorCode:   "conflict",
			details:     `{"superseded_by":"decision-2","status":"superseded"}`,
			body:        `{"revision":1,"approve":true,"text":"ok"}`,
			args:        []string{"pm", "decisions", "answer", "decision-1", "--from-file", "-"},
			want:        "decision-2",
			notWant:     "retry using its current",
		},
		{
			name:        "dispatch superseded conflict",
			commandPath: "/pm/decisions/decision-1/dispatch",
			errorCode:   "conflict",
			details:     `{"superseded_by":"decision-8","status":"superseded"}`,
			body:        `{}`,
			args:        []string{"pm", "decisions", "dispatch", "decision-1"},
			want:        "decision-8",
			notWant:     "retry using its current",
		},
		{
			name:        "dispatch status conflict",
			commandPath: "/pm/decisions/decision-1/dispatch",
			errorCode:   "conflict",
			body:        `{}`,
			args:        []string{"pm", "decisions", "dispatch", "decision-1"},
			want:        "status",
			notWant:     "retry using its current",
		},
		{
			name:        "create request-key conflict",
			commandPath: "/pm/decisions",
			errorCode:   "conflict",
			details:     `{"existing_decision_id":"decision-9"}`,
			body:        `{"request_key":"k","work_ref":"card:x","instruction":"Review","scope":"review","target_revision":"abc"}`,
			args:        []string{"pm", "decisions", "create", "--from-file", "-"},
			want:        "error.details.existing_decision_id",
			notWant:     "if_updated_at",
		},
		{
			name:        "reconcile stale source",
			commandPath: "/pm/actions/action-1/reconcile",
			errorCode:   "source_revision_changed",
			body:        `{}`,
			args:        []string{"pm", "actions", "reconcile", "action-1"},
			want:        "propose",
			notWant:     "if_updated_at",
		},
		{
			name:        "acknowledge status conflict",
			commandPath: "/pm/actions/action-1/acknowledge",
			errorCode:   "conflict",
			body:        `{}`,
			args:        []string{"pm", "actions", "acknowledge", "action-1"},
			want:        "not in a state that can be acknowledged",
			notWant:     "if_updated_at",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.commandPath {
					t.Errorf("path=%s want %s", r.URL.Path, tc.commandPath)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				details := tc.details
				if details == "" {
					details = "{}"
				}
				fmt.Fprintf(w, `{"error":{"code":%q,"message":"PM revision or state conflict","details":%s}}`, tc.errorCode, details)
			}))
			defer server.Close()
			stdin := strings.NewReader(tc.body)
			payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, stdin, append([]string{"--json", "--base-url", server.URL}, tc.args...)))
			errObj := asMap(payload["error"])
			hint := fmt.Sprint(errObj["hint"])
			if !strings.Contains(strings.ToLower(hint), strings.ToLower(tc.want)) {
				t.Errorf("hint=%q want substring %q payload=%v", hint, tc.want, payload)
			}
			if strings.Contains(hint, tc.notWant) {
				t.Errorf("hint still uses %q: %q", tc.notWant, hint)
			}
			if strings.Contains(tc.details, "existing_decision_id") && !strings.Contains(hint, "decision-9") {
				t.Errorf("hint did not name existing_decision_id: %q", hint)
			}
			if strings.Contains(hint, "if_updated_at") {
				t.Errorf("PM hint still used card/board language: %q", hint)
			}
		})
	}
}

func TestPMReconcileNothingDeliveredHint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/actions/action-1/reconcile" {
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":{"code":"invalid_request","message":"invalid PM request: Nothing has been delivered yet, so there is nothing to read back"}}`)
	}))
	defer server.Close()
	payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, strings.NewReader(`{}`), []string{"--json", "--base-url", server.URL, "pm", "actions", "reconcile", "action-1"}))
	hint := fmt.Sprint(asMap(payload["error"])["hint"])
	if hint != "nothing has been delivered yet; deliver first or acknowledge the failure" {
		t.Fatalf("hint=%q payload=%v", hint, payload)
	}
	if strings.Contains(strings.ToLower(hint), "required fields") {
		t.Fatalf("still generic required-fields hint: %q", hint)
	}
}

func TestPMReconcileTextMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"action-1","status":"unknown","reconciliation_conflict":false,"receipt":{"status":"unknown","detail":"still pending"}}`)
	}))
	defer server.Close()
	text := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, strings.NewReader(`{}`), []string{"--base-url", server.URL, "pm", "actions", "reconcile", "action-1"})
	if !strings.Contains(text, "action-1") || !strings.Contains(text, "status=unknown") || !strings.Contains(text, "receipt=unknown") || !strings.Contains(text, "still pending") || !strings.Contains(text, "reconciliation_conflict=false") {
		t.Fatalf("text=%s", text)
	}
	if strings.Contains(text, `"id":`) {
		t.Fatalf("raw JSON in text mode: %s", text)
	}
}

func TestPMConversationMessageBusyHints(t *testing.T) {
	for _, tc := range []struct {
		reason, want, hide string
	}{
		{reason: "conversation", want: "queued or being answered", hide: "in-flight limit"},
		{reason: "capacity", want: "in-flight limit for this workspace", hide: "queued or being answered"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pm/conversations/conv-1/messages" {
					t.Errorf("path=%s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":{"code":"busy","message":"PM execution capacity reached","details":{"reason":%q}}}`, tc.reason)
			}))
			defer server.Close()
			payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, strings.NewReader(`{"request_key":"k","text":"hello"}`), []string{"--json", "--base-url", server.URL, "pm", "conversations", "message", "conv-1", "--from-file", "-"}))
			hint := fmt.Sprint(asMap(payload["error"])["hint"])
			if !strings.Contains(hint, tc.want) {
				t.Fatalf("hint=%q want %q payload=%v", hint, tc.want, payload)
			}
			if strings.Contains(hint, tc.hide) {
				t.Fatalf("hint still has %q: %q", tc.hide, hint)
			}
		})
	}
}

func TestPMAskBusyHints(t *testing.T) {
	for _, tc := range []struct {
		reason, want, hide string
	}{
		{reason: "conversation", want: "queued or being answered", hide: "in-flight limit"},
		{reason: "capacity", want: "in-flight limit for this workspace", hide: "queued or being answered"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/pm/conversations":
					io.WriteString(w, `{"id":"conv-1","title":"What needs my decision?"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/pm/conversations/conv-1/messages":
					w.WriteHeader(http.StatusTooManyRequests)
					fmt.Fprintf(w, `{"error":{"code":"busy","message":"PM execution capacity reached","details":{"reason":%q}}}`, tc.reason)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", server.URL, "pm", "ask", "What needs my decision?"}))
			if fmt.Sprint(payload["command_id"]) != "pm.ask" {
				t.Fatalf("command_id=%v payload=%v", payload["command_id"], payload)
			}
			hint := fmt.Sprint(asMap(payload["error"])["hint"])
			if !strings.Contains(hint, tc.want) {
				t.Fatalf("hint=%q want %q payload=%v", hint, tc.want, payload)
			}
			if strings.Contains(hint, tc.hide) {
				t.Fatalf("hint still has %q: %q", tc.hide, hint)
			}
		})
	}
}

func TestPMDispatchSendNoteDistinguishesFirstSendFromReplay(t *testing.T) {
	started := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	delivered := map[string]any{
		"id":     "action-1",
		"status": "delivered",
		"receipt": map[string]any{
			"status": "delivered",
			"detail": "accepted",
		},
		"attempts": []any{
			map[string]any{"sent_at": "2026-09-13T10:00:00Z", "status": "delivered"},
		},
	}
	if got := pmDispatchSendNote(delivered, started, ""); got != "delivered" {
		t.Fatalf("first delivery note=%q", got)
	}
	if got := pmDispatchSendNote(delivered, started.Add(time.Second), ""); got != "already delivered" {
		t.Fatalf("replay note=%q", got)
	}

	reported := map[string]any{
		"id":     "action-2",
		"status": "source_reported",
		"attempts": []any{
			map[string]any{"sent_at": "2026-09-08T21:00:00Z", "status": "source_reported"},
		},
	}
	got := pmDispatchSendNote(reported, started, "pending_delivery")
	if !strings.Contains(got, "reported by source, not yet verified") || !strings.Contains(got, "anx pm actions reconcile action-2") {
		t.Fatalf("pending-advance source_reported note=%q", got)
	}
	if strings.Contains(got, "already delivered") {
		t.Fatalf("pending-advance still said already delivered: %q", got)
	}

	preflight := map[string]any{
		"id":     "action-3",
		"status": "failed",
		"receipt": map[string]any{
			"status": "failed",
			"detail": "unavailable",
		},
		"attempts": []any{
			map[string]any{"started_at": "2026-09-13T10:00:00Z", "status": "failed"},
		},
	}
	if got := pmDispatchSendNote(preflight, started, "pending_delivery"); got != "nothing was sent" {
		t.Fatalf("preflight note=%q", got)
	}
}

func TestPMDispatchTextRendersReceiptAndNothingSent(t *testing.T) {
	firstSentAt := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	for _, tc := range []struct {
		name, body string
		want, hide []string
	}{
		{
			name: "preflight failed",
			body: `{"id":"action-1","status":"failed","receipt":{"status":"failed","detail":"unavailable"}}`,
			want: []string{"action-1", "status=failed", "receipt=failed", "unavailable", "nothing was sent"},
			hide: []string{"already delivered", "already verified"},
		},
		{
			name: "failed attempt without sent_at",
			body: `{"id":"action-1b","status":"failed","receipt":{"status":"failed","detail":"preflight"},"attempts":[{"started_at":"2026-09-08T21:00:00Z","status":"failed"}]}`,
			want: []string{"nothing was sent"},
			hide: []string{"already delivered", "already verified"},
		},
		{
			name: "first delivery",
			body: fmt.Sprintf(`{"id":"action-new","status":"delivered","receipt":{"status":"delivered","detail":"accepted"},"attempts":[{"sent_at":%q,"status":"delivered"}]}`, firstSentAt),
			want: []string{"action-new", "status=delivered", "receipt=delivered", "accepted", "delivered"},
			hide: []string{"already delivered", "already verified", "nothing was sent"},
		},
		{
			name: "first source_reported",
			body: fmt.Sprintf(`{"id":"action-src","status":"source_reported","receipt":{"status":"source_reported","detail":"source accepted"},"attempts":[{"sent_at":%q,"status":"source_reported"}]}`, firstSentAt),
			want: []string{"action-src", "reported by source, not yet verified", "anx pm actions reconcile action-src"},
			hide: []string{"already delivered", "nothing was sent"},
		},
		{
			name: "already delivered",
			body: `{"id":"action-2","status":"delivered","receipt":{"status":"delivered","detail":"accepted"},"attempts":[{"sent_at":"2026-09-08T21:00:00Z","status":"delivered"}]}`,
			want: []string{"action-2", "status=delivered", "receipt=delivered", "accepted", "already delivered"},
			hide: []string{"nothing was sent"},
		},
		{
			name: "already verified",
			body: `{"id":"action-3","status":"verified","receipt":{"status":"verified","detail":"Read back canonical Nexus phase: ready"},"attempts":[{"sent_at":"2026-09-08T21:00:00Z","status":"verified"}]}`,
			want: []string{"action-3", "status=verified", "receipt=verified", "already verified", "Read back canonical Nexus phase: ready"},
			hide: []string{"nothing was sent"},
		},
		{
			name: "pending without send",
			body: `{"id":"action-4","status":"pending_delivery","receipt":{"status":"unknown"}}`,
			want: []string{"nothing was sent"},
			hide: []string{"already delivered", "already verified"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/pm/decisions/decision-1/dispatch" {
					t.Errorf("request=%s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			text := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--base-url", server.URL, "pm", "decisions", "dispatch", "decision-1"})
			if strings.Contains(text, `"id"`) || strings.Contains(text, `"receipt"`) {
				t.Fatalf("text mode still printed JSON: %s", text)
			}
			for _, needle := range tc.want {
				if !strings.Contains(text, needle) {
					t.Fatalf("missing %q in %s", needle, text)
				}
			}
			for _, needle := range tc.hide {
				if strings.Contains(text, needle) {
					t.Fatalf("unexpected %q in %s", needle, text)
				}
			}
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", server.URL, "pm", "decisions", "dispatch", "decision-1"}))
			var want any
			_ = json.Unmarshal([]byte(tc.body), &want)
			got, _ := json.Marshal(payload["data"])
			encodedWant, _ := json.Marshal(want)
			if string(got) != string(encodedWant) {
				t.Fatalf("JSON output changed: %s want %s", got, encodedWant)
			}
		})
	}
}

func TestPMAcknowledgeTextRendersIDStatusAndTimestamp(t *testing.T) {
	body := `{"id":"action-1","status":"acknowledged","acknowledged_by":"actor:human","acknowledged_at":"2026-09-13T02:00:00Z"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pm/actions/action-1/acknowledge" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		got, _ := io.ReadAll(r.Body)
		if strings.TrimSpace(string(got)) != "{}" {
			t.Errorf("body=%s want {}", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	defer server.Close()
	text := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--base-url", server.URL, "pm", "actions", "acknowledge", "action-1"})
	want := "action-1  status=acknowledged  acknowledged_at=2026-09-13T02:00:00Z"
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in %s", want, text)
	}
	if strings.Contains(text, `"acknowledged_by"`) {
		t.Fatalf("text mode still printed JSON: %s", text)
	}
	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", server.URL, "pm", "actions", "acknowledge", "action-1"}))
	var wantBody any
	_ = json.Unmarshal([]byte(body), &wantBody)
	got, _ := json.Marshal(payload["data"])
	encodedWant, _ := json.Marshal(wantBody)
	if string(got) != string(encodedWant) {
		t.Fatalf("JSON output changed: %s want %s", got, encodedWant)
	}
}

func TestPMTurnsGetTextRendersQueuedAndInProgress(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want, hide []string
	}{
		{
			name: "unclaimed sending",
			body: `{"id":"turn-1","status":"sending","claimed":false,"deadline":"2026-09-08T22:00:00Z"}`,
			want: []string{"turn-1", "status=queued", "deadline=2026-09-08T22:00:00Z"},
			hide: []string{"status=sending", "claimed_at="},
		},
		{
			name: "claimed sending",
			body: `{"id":"turn-2","status":"sending","claimed":true,"claimed_at":"2026-09-08T21:00:00Z","deadline":"2026-09-08T22:00:00Z"}`,
			want: []string{"turn-2", "status=in progress", "claimed_at=2026-09-08T21:00:00Z"},
			hide: []string{"status=sending"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/pm/turns/turn-1" {
					t.Errorf("request=%s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			text := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--base-url", server.URL, "pm", "turns", "get", "turn-1"})
			for _, needle := range tc.want {
				if !strings.Contains(text, needle) {
					t.Fatalf("missing %q in %s", needle, text)
				}
			}
			for _, needle := range tc.hide {
				if strings.Contains(text, needle) {
					t.Fatalf("unexpected %q in %s", needle, text)
				}
			}
		})
	}
}
