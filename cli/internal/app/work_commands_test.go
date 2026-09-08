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
		{[]string{"pm", "context", "--work-ref", "card:launch", "--query", "evidence", "--limit", "5"}, "GET", "/pm/context?limit=5&query=evidence&work_ref=card%3Alaunch", "", `{"items":[],"limitations":["partial"]}`},
		{[]string{"pm", "decisions", "list"}, "GET", "/pm/decisions", "", `{"items":[],"has_more":true}`},
		{[]string{"pm", "decisions", "get", "decision-1"}, "GET", "/pm/decisions/decision-1", "", `{"id":"decision-1","status":"answered","action_id":"action-1"}`},
		{[]string{"pm", "decisions", "create", "--from-file", "-"}, "POST", "/pm/decisions", `{"request_key":"decision-key","work_ref":"card:launch","instruction":"Review","scope":"review","target_revision":"abc"}`, `{"id":"decision-1","status":"awaiting_answer"}`},
		{[]string{"pm", "decisions", "answer", "decision-1", "--from-file", "-"}, "POST", "/pm/decisions/decision-1/answer", `{"revision":1,"approve":true,"text":"Approved"}`, `{"id":"decision-1","status":"answered"}`},
		{[]string{"pm", "decisions", "dispatch", "decision-1"}, "POST", "/pm/decisions/decision-1/dispatch", `{}`, `{"id":"action-1","status":"failed","receipt":{"detail":"unavailable"}}`},
		{[]string{"pm", "actions", "list"}, "GET", "/pm/actions", "", `{"items":[],"has_more":true}`},
		{[]string{"pm", "actions", "get", "action-1"}, "GET", "/pm/actions/action-1", "", `{"id":"action-1","status":"source_reported","receipt":{"independently_verified":false}}`},
		{[]string{"pm", "actions", "reconcile", "action-1"}, "POST", "/pm/actions/action-1/reconcile", `{}`, `{"id":"action-1","status":"unknown","receipt":{"independently_verified":false}}`},
		{[]string{"pm", "turns", "claim"}, "POST", "/pm/turns/claim", `{}`, `{"id":"turn-1","status":"sending","lease_token":"abc"}`},
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
	for _, args := range [][]string{{"help", "pm"}, {"pm", "decisions", "--help"}, {"pm", "actions", "reconcile", "--help"}} {
		assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json"}, args...)))
	}
}

func TestWorkAndPMMetadataDocsAreDiscoverable(t *testing.T) {
	for _, topic := range []string{"work", "work observations submit", "pm", "pm decisions answer", "pm actions reconcile"} {
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
	pm := formatWorkCommandText("pm actions list", map[string]any{"items": []any{map[string]any{"id": "action-1", "work_ref": "card:example", "status": "source_reported", "receipt": map[string]any{"independently_verified": false}}}, "next_cursor": "next", "has_more": true})
	if !strings.Contains(pm, "source_reported") || !strings.Contains(pm, "verified=false") || !strings.Contains(pm, "next_cursor: next") {
		t.Errorf("lost receipt uncertainty: %s", pm)
	}
}
