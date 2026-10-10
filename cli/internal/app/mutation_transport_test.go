package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateTransportUnknownOutcomePreservesRequestKey(t *testing.T) {
	var keys []string
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		keys = append(keys, anyString(body["request_key"]))
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	app := New()
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "test", Timeout: 20 * time.Millisecond}
	body := map[string]any{"board_id": "board-1", "card": map[string]any{"title": "Create once"}}
	for i := 0; i < 2; i++ {
		_, err := app.invokeTypedJSON(context.Background(), cfg, "cards create", "cards.create", nil, nil, body)
		typed, ok := err.(*errnorm.Error)
		if !ok || typed.Code != "outcome_unknown" || typed.Recoverable == nil || *typed.Recoverable {
			t.Fatalf("unsafe outcome: %v", err)
		}
		if !strings.Contains(typed.Hint, "anx cards list") || !strings.Contains(typed.Hint, anyString(body["request_key"])) {
			t.Fatalf("missing recovery: %#v", typed)
		}
		if len(deriveErrorActions("cards create", typed)) == 0 {
			t.Fatal("missing read action")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("retry keys changed: %v", keys)
	}
}

func TestReadTransportFailureRemainsRetryable(t *testing.T) {
	err := mutationTransportError("cards list", "GET", nil, context.DeadlineExceeded).(*errnorm.Error)
	if err.Code != "request_failed" || err.Recoverable != nil {
		t.Fatalf("read changed: %#v", err)
	}
}

func TestPostReadFailureRemainsReadFailure(t *testing.T) {
	err := mutationTransportError("refs resolve", "POST", nil, context.DeadlineExceeded).(*errnorm.Error)
	if err.Code != "request_failed" {
		t.Fatalf("read-only POST treated as mutation: %#v", err)
	}
}
