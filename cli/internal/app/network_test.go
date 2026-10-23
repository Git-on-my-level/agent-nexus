package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/hostidentity"
)

func TestHostAssertionRetriesWithFreshProof(t *testing.T) {
	for _, failure := range []string{"delay", "drop", "502", "503", "504", "401"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			if failure == "delay" && testing.Short() {
				t.Skip("20 second host assertion timeout acceptance test")
			}
			pub, key, _ := ed25519.GenerateKey(nil)
			var calls atomic.Int32
			var mu sync.Mutex
			seen := map[string]bool{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				proof, _ := base64.StdEncoding.DecodeString(body["signature"])
				msg := "anx-host-agent-token|host-test|key-test|codex|" + body["signed_at"]
				if !ed25519.Verify(pub, []byte(msg), proof) {
					t.Error("invalid signed proof")
				}
				mu.Lock()
				replayed := seen[body["signature"]]
				seen[body["signature"]] = true
				mu.Unlock()
				if replayed {
					t.Error("single-use proof replayed")
					w.WriteHeader(401)
					return
				}
				if calls.Add(1) == 1 {
					switch failure {
					case "delay":
						<-r.Context().Done()
						return
					case "drop":
						conn, _, _ := w.(http.Hijacker).Hijack()
						conn.Close()
						return
					default:
						var status int
						fmt.Sscan(failure, &status)
						w.WriteHeader(status)
						return
					}
				}
				fmt.Fprint(w, `{"agent":{"id":"agent-test","actor_id":"actor-test"},"tokens":{"access_token":"fixture-token","expires_in":3600}}`)
			}))
			defer srv.Close()
			dir := t.TempDir()
			if err := hostidentity.SaveAt(dir, hostidentity.Host{ID: "host-test", KeyID: "key-test", WorkspaceID: "ws-test", BaseURL: srv.URL}, key); err != nil {
				t.Fatal(err)
			}
			a := newTestApp(t)
			cfg := config.Resolved{BaseURL: srv.URL, ConfigDir: dir, As: "codex", Timeout: 900 * time.Millisecond, Sources: map[string]string{"base_url": "fixture"}}
			if failure == "delay" {
				cfg.Timeout = config.DefaultTimeout
			}
			got, err := a.resolveHostAgent(context.Background(), cfg)
			if failure == "401" {
				if err == nil || calls.Load() != 1 {
					t.Fatalf("auth rejection retried: calls=%d err=%v", calls.Load(), err)
				}
				return
			}
			if err != nil || got.AccessToken != "fixture-token" || calls.Load() != 2 {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestGeneratedReadRetriesAndRendersFailure(t *testing.T) {
	for _, scenario := range []string{"recover", "timeout", "auth", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				switch scenario {
				case "timeout":
					<-r.Context().Done()
					return
				case "auth":
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"code":"invalid_token","message":"token rejected"}}`)
					return
				case "unavailable":
					w.WriteHeader(503)
					return
				case "recover":
					if n == 1 {
						w.WriteHeader(502)
						return
					}
				}
				fmt.Fprint(w, `{"card":{"id":"card-test","ref":"card:test","title":"Test"}}`)
			}))
			defer srv.Close()
			a := newTestApp(t)
			cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "never-print-this-token", Timeout: 300 * time.Millisecond}
			_, err := a.invokeTypedJSON(context.Background(), cfg, "cards get", "cards.get", map[string]string{"card_id": "card:test"}, nil, nil)
			if scenario == "recover" {
				if err != nil || calls.Load() != 2 {
					t.Fatalf("calls=%d err=%v", calls.Load(), err)
				}
				return
			}
			class, exit, wantCalls := "timeout", 8, int32(1)
			if scenario == "auth" {
				class, exit, wantCalls = "auth_rejection", 5, 1
			}
			if scenario == "unavailable" {
				class, exit = "http_status", 6
				wantCalls = 3
			}
			for _, jsonMode := range []bool{false, true} {
				var out bytes.Buffer
				a.Stdout, a.Stderr = &out, &out
				gotExit := a.renderError(machineCommandIdentity{Command: "cards get", CommandID: "cards.get"}, jsonMode, err)
				validCalls := calls.Load() == wantCalls
				if gotExit != exit || !validCalls || strings.Contains(out.String(), "never-print-this-token") {
					t.Fatalf("exit=%d calls=%d output=%s", gotExit, calls.Load(), out.String())
				}
				if jsonMode {
					var envelope map[string]any
					if e := json.Unmarshal(out.Bytes(), &envelope); e != nil {
						t.Fatal(e)
					}
					failure := asMap(envelope["error"])
					for _, field := range []string{"code", "message", "retryable", "exit_code", "details", "next_actions"} {
						if _, ok := failure[field]; !ok {
							t.Fatalf("missing envelope field %s", field)
						}
					}
					details := asMap(failure["details"])
					if details["failure_class"] != class || envelope["schema_version"] != float64(2) {
						t.Fatalf("output=%s", out.String())
					}
					if scenario == "timeout" && (details["phase"] != "response_headers" || details["attempts"] != float64(calls.Load())) {
						t.Fatalf("missing phase/attempts: %s", out.String())
					}
				} else if !strings.Contains(out.String(), "timeout") && scenario == "timeout" {
					t.Fatalf("timeout hidden: %s", out.String())
				}
			}
		})
	}
}
