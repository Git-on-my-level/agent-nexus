package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func expiringStreamTestApp(t *testing.T, serverURL string, offset *atomic.Int64) (*App, *bytes.Buffer) {
	t.Helper()
	a, out := dailyTestApp(t, serverURL)
	home, err := a.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(home, ".config", "anx", "hosts", "ws_test", "token-worker.json")
	cache := tokenCache{
		Token: "old-token", ExpiresAt: time.Now().Add(time.Hour),
		Agent: map[string]any{"id": "agent-1", "actor_id": "actor-1", "handle": "worker.host"},
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	a.now = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	return a, out
}

func refreshGrant(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"tokens":{"access_token":"new-token","expires_in":900}}`)
}

func TestAwaitRenewsExpiredDerivedTokenAndResumes(t *testing.T) {
	var offset atomic.Int64
	var streams, grants atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/ask-1":
			fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested","thread_id":"thread-1"}}`)
		case "/events":
			fmt.Fprint(w, `{"events":[]}`)
		case "/auth/token":
			grants.Add(1)
			refreshGrant(w)
		case "/stream/events":
			n := streams.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			if n == 1 {
				if got := r.Header.Get("Authorization"); got != "Bearer old-token" {
					t.Errorf("first stream token = %q", got)
				}
				fmt.Fprint(w, "id: first\nevent: event\ndata: {\"event\":{\"type\":\"other\"}}\n\n")
				offset.Store(int64(2 * time.Hour))
				return
			}
			if got := r.Header.Get("Authorization"); got != "Bearer new-token" {
				t.Errorf("reconnect token = %q", got)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if got := r.URL.Query().Get("last_event_id"); got != "first" || r.Header.Get("Last-Event-ID") != "first" {
				t.Errorf("lost resume cursor: query=%q header=%q", got, r.Header.Get("Last-Event-ID"))
			}
			fmt.Fprint(w, "id: response\nevent: event\ndata: {\"event\":{\"id\":\"response\",\"type\":\"human_attention_responded\",\"payload\":{\"request_event_ref\":\"event:ask-1\",\"outcome\":\"answered\",\"response_text\":\"Approved\"}}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := expiringStreamTestApp(t, server.URL, &offset)
	if exit := a.Run([]string{"--json", "--as", "worker", "await", "event:ask-1", "--timeout", "3s"}); exit != 0 {
		t.Fatalf("await exit %d: %s", exit, out.String())
	}
	if streams.Load() != 2 || grants.Load() != 1 || !strings.Contains(out.String(), "Approved") {
		t.Fatalf("await did not resume with one renewed grant: streams=%d grants=%d output=%s", streams.Load(), grants.Load(), out.String())
	}
}

func TestFollowStreamsRenewExpiredDerivedTokenAndResume(t *testing.T) {
	for _, kind := range []string{"events", "inbox"} {
		t.Run(kind, func(t *testing.T) {
			var offset atomic.Int64
			var streams, grants atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/token" {
					grants.Add(1)
					refreshGrant(w)
					return
				}
				if r.URL.Path != "/stream/"+kind {
					http.NotFound(w, r)
					return
				}
				n := streams.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					if got := r.Header.Get("Authorization"); got != "Bearer old-token" {
						t.Errorf("first stream token = %q", got)
					}
					fmt.Fprint(w, "id: first\nevent: event\ndata: {\"event\":{\"id\":\"first\"},\"item\":{\"id\":\"first\"}}\n\n")
					offset.Store(int64(2 * time.Hour))
					return
				}
				if got := r.Header.Get("Authorization"); got != "Bearer new-token" {
					t.Errorf("reconnect token = %q", got)
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if got := r.URL.Query().Get("last_event_id"); got != "first" || r.Header.Get("Last-Event-ID") != "first" {
					t.Errorf("lost resume cursor: query=%q header=%q", got, r.Header.Get("Last-Event-ID"))
				}
				fmt.Fprint(w, "id: second\nevent: event\ndata: {\"event\":{\"id\":\"second\"},\"item\":{\"id\":\"second\"}}\n\n")
			}))
			defer server.Close()
			a, out := expiringStreamTestApp(t, server.URL, &offset)
			if exit := a.Run([]string{"--json", "--as", "worker", "debug", kind, "stream", "--follow", "--max-events", "2"}); exit != 0 {
				t.Fatalf("stream exit %d: %s", exit, out.String())
			}
			if streams.Load() != 2 || grants.Load() != 1 || !strings.Contains(out.String(), "second") {
				t.Fatalf("stream did not resume with one renewed grant: streams=%d grants=%d output=%s", streams.Load(), grants.Load(), out.String())
			}
		})
	}
}

func TestLongRunningPMRequestRenewsExpiredDerivedToken(t *testing.T) {
	var grants, claims atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			grants.Add(1)
			refreshGrant(w)
		case "/pm/turns/claim":
			claims.Add(1)
			if got := r.Header.Get("Authorization"); got != "Bearer new-token" {
				t.Errorf("PM claim token = %q", got)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var offset atomic.Int64
	a, _ := expiringStreamTestApp(t, server.URL, &offset)
	home, err := a.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(home, ".config", "anx", "hosts", "ws_test", "token-worker.json")
	cache := tokenCache{Token: "old-token", ExpiresAt: time.Now().Add(-time.Minute), Agent: map[string]any{"id": "agent-1", "actor_id": "actor-1", "handle": "worker.host"}}
	raw, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Resolved{
		BaseURL: server.URL, As: "worker", Agent: "worker", HostID: "host-test", AccessToken: "old-token",
		AccessTokenExpiresAt: time.Now().Add(-time.Minute).Format(time.RFC3339Nano),
		Sources:              map[string]string{"base_url": "env:ANX_BASE_URL"},
	}
	if _, err := a.invokeRawJSON(context.Background(), cfg, "pm turns claim", "POST", "/pm/turns/claim", map[string]any{"runner_id": "pm"}); err != nil {
		t.Fatal(err)
	}
	if grants.Load() != 1 || claims.Load() != 1 {
		t.Fatalf("grant count=%d claim count=%d", grants.Load(), claims.Load())
	}
}
