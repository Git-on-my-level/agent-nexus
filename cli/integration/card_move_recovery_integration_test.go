//go:build integration

package integration

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCommittedMoveRecoveryDoesNotEmitSecondMove(t *testing.T) {
	for _, command := range []string{"cards_move", "work_block"} {
		t.Run(command, func(t *testing.T) {
			h := newPasskeyLiveCoreHarness(t)
			target, err := url.Parse(h.baseURL)
			if err != nil {
				t.Fatal(err)
			}
			var moveRequests atomic.Int32
			proxy := httputil.NewSingleHostReverseProxy(target)
			proxy.ModifyResponse = func(resp *http.Response) error {
				if resp.Request.Method == http.MethodPost && strings.HasSuffix(resp.Request.URL.Path, "/move") {
					n := moveRequests.Add(1)
					if resp.StatusCode == http.StatusOK && n == 1 {
						// Core has committed and emitted its real lifecycle event.
						// Force a post-commit response failure seen by the real CLI.
						resp.Body.Close()
						body := `{"error":{"code":"outcome_unknown","message":"post-commit fixture failure","recoverable":false,"hint":"Read back; do not repeat an already-applied move."}}`
						resp.StatusCode = http.StatusConflict
						resp.Body = io.NopCloser(strings.NewReader(body))
						resp.ContentLength = int64(len(body))
						resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
					}
				}
				return nil
			}
			server := httptest.NewServer(proxy)
			defer server.Close()
			h.baseURL = server.URL
			h.enrollHost(t, "worker")
			created := h.runCLIExpectOK(t, "worker", map[string]any{"title": "Recover a committed move", "phase": "ready"}, "work", "create", "--from-file", "-")
			ref := mustStringPath(t, created.Payload, "result.work.ref")
			before := h.getCore(t, "/work/"+url.PathEscape(ref))["work"].(map[string]any)
			var result cliResult
			if command == "cards_move" {
				result = h.runCLIExpectOK(t, "worker", nil, "cards", "move", ref, "--column", "blocked")
			} else {
				result = h.runCLIExpectOK(t, "worker", nil, "work", "block", "Fixture dependency", ref)
			}
			t.Logf("recovered CLI exit=%d stdout=%s stderr=%s", result.ExitCode, result.Stdout, result.Stderr)
			after := h.getCore(t, "/work/"+url.PathEscape(ref))["work"].(map[string]any)
			if after["phase"] != "blocked" || after["version"].(float64) != before["version"].(float64)+1 || moveRequests.Load() != 1 {
				t.Fatalf("move was repeated: before=%v after=%v requests=%d", before, after, moveRequests.Load())
			}
			events := h.getCore(t, "/events?type=card_moved")["events"].([]any)
			if len(events) != 1 {
				t.Fatalf("expected one move event, got %d: %v", len(events), events)
			}
			t.Log("one committed move, one version increment, one move event; recovery issued no second POST")
		})
	}
}
