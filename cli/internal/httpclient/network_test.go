package httpclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestReadRetriesTransientFailures(t *testing.T) {
	for _, failure := range []string{"delay", "body-delay", "drop", "body-drop", "502", "503", "504"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					switch failure {
					case "delay", "body-delay":
						if failure == "body-delay" {
							w.WriteHeader(200)
							w.(http.Flusher).Flush()
						}
						<-r.Context().Done()
						return
					case "drop", "body-drop":
						if failure == "body-drop" {
							w.Header().Set("Content-Length", "100")
							w.WriteHeader(200)
							w.(http.Flusher).Flush()
						}
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
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer srv.Close()
			c, err := New(config.Resolved{BaseURL: srv.URL, Timeout: 900 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			// Inject a short per-attempt transport timeout while retaining the
			// complete exchange budget, so a timeout can recover without a 20s test.
			c.httpClient.Timeout = 100 * time.Millisecond
			resp, err := c.RawCall(context.Background(), RawRequest{Path: "/cards"})
			if err != nil || resp.StatusCode != 200 || calls.Load() != 2 {
				t.Fatalf("calls=%d status=%d err=%v", calls.Load(), resp.StatusCode, err)
			}
		})
	}
}

func TestKeyedWriteDropNotReplayedOnReusedConnection(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(`{"request_key":"same-key"}`)} {
		t.Run(fmt.Sprintf("body=%t", len(body) > 0), func(t *testing.T) {
			var writes atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				if r.Method == "POST" && writes.Add(1) == 1 {
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
					return
				}
				fmt.Fprint(w, `{}`)
			}))
			defer srv.Close()
			c, _ := New(config.Resolved{BaseURL: srv.URL, Timeout: time.Second})
			if _, err := c.RawCall(context.Background(), RawRequest{Path: "/warm"}); err != nil {
				t.Fatal(err)
			}
			_, err := c.RawCall(context.Background(), RawRequest{Method: "POST", Path: "/unverified-write", Body: body, Headers: map[string]string{"Idempotency-Key": "same-key"}})
			if err == nil || writes.Load() != 1 {
				t.Fatalf("unsupported replay: writes=%d err=%v", writes.Load(), err)
			}
		})
	}
}

func TestTerminalNetworkFailures(t *testing.T) {
	for _, scenario := range []string{"tls", "tls-handshake", "refused", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) }))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			class, wantAttempts := "tls", 1
			if scenario == "refused" {
				srv.Close()
				class, wantAttempts = "connection_refused", 3
			}
			if scenario == "canceled" {
				cancel()
				class, wantAttempts = "canceled", 0
			}
			c, _ := New(config.Resolved{BaseURL: srv.URL, Timeout: 300 * time.Millisecond})
			if scenario == "tls-handshake" {
				transport := networkTransport.Clone()
				transport.TLSClientConfig = &tls.Config{MaxVersion: tls.VersionTLS11}
				c.httpClient.Transport = noWriteReplay{base: transport}
				defer transport.CloseIdleConnections()
			}
			_, cause := c.RawCall(ctx, RawRequest{Path: "/auth/whoami?token=never-print"})
			err := errnorm.Normalize(errnorm.Wrap(errnorm.KindNetwork, "request_failed", "read failed", cause))
			details, _ := err.Details.(map[string]any)
			if cause == nil || details["failure_class"] != class || details["attempts"] != wantAttempts || strings.Contains(err.Error(), "never-print") {
				t.Fatalf("failure=%+v", err)
			}
		})
	}
}

func TestRetryAfterAndCancellationBoundBackoff(t *testing.T) {
	for _, scenario := range []string{"retry-after", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(503)
				if scenario == "cancel" {
					cancel()
				}
			}))
			defer srv.Close()
			c, _ := New(config.Resolved{BaseURL: srv.URL, Timeout: time.Second})
			start := time.Now()
			c.RawCall(ctx, RawRequest{Path: "/cards"})
			if calls.Load() != 1 || time.Since(start) > 500*time.Millisecond {
				t.Fatalf("unbounded backoff: calls=%d elapsed=%s", calls.Load(), time.Since(start))
			}
		})
	}
}

func TestRetriesBoundedAndWritesNotReplayed(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
			defer srv.Close()
			c, _ := New(config.Resolved{BaseURL: srv.URL, Timeout: time.Second})
			_, err := c.RawCall(context.Background(), RawRequest{Method: method, Path: "/cards", Body: []byte(`{"request_key":"same-key"}`), Headers: map[string]string{"Idempotency-Key": "same-key"}})
			want := int32(1)
			if method == "GET" {
				want = 3
			}
			if err != nil || calls.Load() != want {
				t.Fatalf("calls=%d want=%d err=%v", calls.Load(), want, err)
			}
		})
	}
}

func TestSlowNetworkDefaultTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("15 second slow-network acceptance test")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(15 * time.Second):
			fmt.Fprint(w, `{"ok":true}`)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	c, _ := New(config.Defaults(config.Overrides{BaseURL: &srv.URL}))
	resp, err := c.RawCall(context.Background(), RawRequest{Path: "/auth/whoami"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("slow working network failed: status=%d err=%v", resp.StatusCode, err)
	}
}
