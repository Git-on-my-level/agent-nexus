package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAwaitKnownAskAcrossSessionsAndReaders(t *testing.T) {
	for _, tc := range []struct {
		name, session, state string
		subscriptionStatus   int
		streamStatus         int
		exit                 int
	}{
		{"same_agent_new_session", "second-session", "answered", 201, 200, 0},
		{"authorized_other_agent_answered", "other-agent-session", "answered", 404, 200, 0},
		{"authorized_other_agent_pending", "other-agent-session", "open", 404, 200, 8},
		{"revoked_after_read", "other-agent-session", "answered", 404, 404, 3},
		{"subscription_auth_failure", "second-session", "answered", 401, 200, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var streamed, acknowledged atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /asks/legacy-ask":
					fmt.Fprint(w, `{"status":"open"}`)
				case "POST /asks/legacy-ask/subscriptions":
					w.WriteHeader(tc.subscriptionStatus)
					if tc.subscriptionStatus == 201 {
						fmt.Fprint(w, `{"id":"requester-subscription"}`)
					} else if tc.subscriptionStatus == 401 {
						fmt.Fprint(w, `{"error":{"code":"auth_required","message":"authentication required"}}`)
					} else {
						fmt.Fprint(w, `{"error":{"code":"not_found","message":"ask not found"}}`)
					}
				case "GET /stream/asks/legacy-ask":
					streamed.Store(true)
					if tc.streamStatus != 200 {
						w.WriteHeader(tc.streamStatus)
						fmt.Fprint(w, `{"error":{"code":"not_found","message":"ask not found"}}`)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprintf(w, "event: outcome\ndata: {\"status\":%q,\"response\":{\"outcome\":\"answered\",\"response_text\":\"Recorded answer\"}}\n\n", tc.state)
					w.(http.Flusher).Flush()
					if tc.state == "open" {
						<-r.Context().Done()
					}
				case "POST /asks/legacy-ask/delivery":
					acknowledged.Store(true)
					fmt.Fprint(w, `{"recorded":true}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			actor := "worker"
			if tc.subscriptionStatus == 404 {
				actor = "observer"
				home, _ := a.UserHomeDir()
				writeDerivedAgentFixture(t, home, actor, `{"agent_id":"agent-2","actor_id":"actor-2","username":"observer.host","access_token":"observer-token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
				_ = runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "--as", actor, "version"})
			}
			getenv := a.Getenv
			a.Getenv = func(key string) string {
				if key == "ANX_SESSION" {
					return tc.session
				}
				return getenv(key)
			}
			if exit := a.Run([]string{"--json", "--as", actor, "await", "event:legacy-ask", "--timeout", "300ms"}); exit != tc.exit {
				t.Fatalf("exit=%d want=%d: %s", exit, tc.exit, out.String())
			}
			if streamed.Load() != (tc.subscriptionStatus != 401) || acknowledged.Load() != (tc.subscriptionStatus == 201) {
				t.Fatalf("streamed=%v acknowledged=%v", streamed.Load(), acknowledged.Load())
			}
			if tc.exit == 0 && asMap(dailyJSON(t, out)["result"])["answer"] != "Recorded answer" {
				t.Fatalf("lost answer: %s", out.String())
			}
		})
	}
}

func TestAwaitInvisibleAskDoesNotSubscribeOrStream(t *testing.T) {
	for _, name := range []string{"unknown_id", "unauthorized_identity"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/asks/hidden-or-missing" {
					t.Errorf("invisible ask triggered another request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(404)
				fmt.Fprint(w, `{"error":{"code":"not_found","message":"ask not found"}}`)
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:hidden-or-missing", "--timeout", "300ms"}); exit != 3 {
				t.Fatalf("exit=%d want=3: %s", exit, out.String())
			}
		})
	}
}
