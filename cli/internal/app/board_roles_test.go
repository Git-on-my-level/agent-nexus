package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestBoardRoleFlagBodies(t *testing.T) {
	a := &App{Stdin: strings.NewReader("")}
	body, dry, err := a.parseBoardCreateInput([]string{"--title", "Initiatives", "--role", "initiatives", "--dry-run"}, config.Resolved{}, "boards create")
	if err != nil || !dry {
		t.Fatalf("body=%v dry=%v err=%v", body, dry, err)
	}
	if body.(map[string]any)["board"].(map[string]any)["role"] != "initiatives" {
		t.Fatal(body)
	}
	for _, role := range []string{"initiatives", ""} {
		id, patch, dry, err := a.parseIDAndBodyInputWithOptions([]string{"board:initiatives", "--role", role, "--dry-run"}, "board-id", "board id", "boards patch", jsonBodyInputOptions{allowDryRun: true})
		if err != nil || id != "board:initiatives" || !dry || patch.(map[string]any)["patch"].(map[string]any)["role"] != role {
			t.Fatalf("id=%s body=%v dry=%v err=%v", id, patch, dry, err)
		}
	}
}

func TestBoardRolePatchDiscoversOrUsesUpdatedAt(t *testing.T) {
	t.Parallel()

	const (
		boardID      = "board:initiatives"
		discoveredAt = "2026-10-07T00:00:00Z"
		explicitlyAt = "2026-10-06T23:00:00Z"
	)
	var mu sync.Mutex
	getCount := 0
	patchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID:
			mu.Lock()
			getCount++
			mu.Unlock()
			_, _ = io.WriteString(w, `{"board":{"id":"`+boardID+`","updated_at":"`+discoveredAt+`"}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/boards/"+boardID:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode board patch body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			patch, _ := body["patch"].(map[string]any)
			role := anyStringValue(patch["role"])
			wantUpdatedAt := discoveredAt
			if role == "launch" {
				wantUpdatedAt = explicitlyAt
			}
			if got := anyStringValue(body["if_updated_at"]); got != wantUpdatedAt {
				t.Errorf("expected if_updated_at %q, got %#v", wantUpdatedAt, body["if_updated_at"])
			}
			mu.Lock()
			patchCount++
			gotGetCount := getCount
			mu.Unlock()
			if role == "initiatives" && gotGetCount != 1 {
				t.Errorf("automatic role patch should read the board first; get count=%d", gotGetCount)
			}
			if role == "launch" && gotGetCount != 1 {
				t.Errorf("explicit token should skip the board read; get count=%d", gotGetCount)
			}
			if role == "conflict" {
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":{"code":"conflict","message":"board has been updated"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"board":{"id":"`+boardID+`","updated_at":"2026-10-07T00:05:00Z"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	dryRun := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "initiatives", "--dry-run"})
	assertEnvelopeOK(t, dryRun)
	mu.Lock()
	if getCount != 0 || patchCount != 0 {
		mu.Unlock()
		t.Fatalf("dry run must not make requests, got reads=%d patches=%d", getCount, patchCount)
	}
	mu.Unlock()

	emptyToken := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "initiatives", "--if-updated-at="})
	emptyTokenError := assertEnvelopeError(t, emptyToken)
	if got := anyStringValue(asMap(emptyTokenError["error"])["code"]); got != "invalid_request" {
		t.Fatalf("expected empty token to be rejected, got %#v", emptyTokenError)
	}
	mu.Lock()
	if getCount != 0 || patchCount != 0 {
		mu.Unlock()
		t.Fatalf("empty token must not make requests, got reads=%d patches=%d", getCount, patchCount)
	}
	mu.Unlock()

	auto := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "initiatives"})
	assertEnvelopeOK(t, auto)

	explicit := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "launch", "--if-updated-at", explicitlyAt})
	assertEnvelopeOK(t, explicit)

	conflict := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "conflict"})
	conflictError := assertEnvelopeError(t, conflict)
	if got := anyStringValue(asMap(conflictError["error"])["code"]); got != "conflict" {
		t.Fatalf("expected concurrent board change to remain a conflict, got %#v", conflictError)
	}

	mu.Lock()
	defer mu.Unlock()
	if getCount != 2 || patchCount != 3 {
		t.Fatalf("expected two board reads and three patches, got reads=%d patches=%d", getCount, patchCount)
	}
}

func TestBoardRolePatchRequiresDiscoveredUpdatedAt(t *testing.T) {
	t.Parallel()

	const boardID = "board:initiatives"
	var mu sync.Mutex
	patchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID {
			_, _ = io.WriteString(w, `{"board":{"id":"`+boardID+`"}}`)
			return
		}
		if r.Method == http.MethodPatch && r.URL.Path == "/boards/"+boardID {
			mu.Lock()
			patchCount++
			mu.Unlock()
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	raw := runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--role", "initiatives"})
	errorEnvelope := assertEnvelopeError(t, raw)
	if got := anyStringValue(asMap(errorEnvelope["error"])["code"]); got != "invalid_request" {
		t.Fatalf("expected missing updated_at to be rejected, got %#v", errorEnvelope)
	}
	mu.Lock()
	defer mu.Unlock()
	if patchCount != 0 {
		t.Fatalf("missing updated_at must prevent PATCH, got %d patches", patchCount)
	}
}

func TestBoardRolePatchFromFilePreservesAndOverridesToken(t *testing.T) {
	t.Parallel()

	const (
		boardID       = "board:initiatives"
		fileUpdatedAt = "2026-10-07T00:00:00Z"
		explicitAt    = "2026-10-06T23:00:00Z"
	)
	filePath := filepath.Join(t.TempDir(), "board-patch.json")
	if err := os.WriteFile(filePath, []byte(`{"if_updated_at":"`+fileUpdatedAt+`","patch":{"title":"Launch"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	patchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPatch || r.URL.Path != "/boards/"+boardID {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode board patch body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		patch, _ := body["patch"].(map[string]any)
		role := anyStringValue(patch["role"])
		wantUpdatedAt := fileUpdatedAt
		if role == "launch" {
			wantUpdatedAt = explicitAt
		}
		if got := anyStringValue(body["if_updated_at"]); got != wantUpdatedAt {
			t.Errorf("expected if_updated_at %q, got %#v", wantUpdatedAt, body["if_updated_at"])
		}
		mu.Lock()
		patchCount++
		mu.Unlock()
		_, _ = io.WriteString(w, `{"board":{"id":"`+boardID+`","updated_at":"2026-10-07T00:05:00Z"}}`)
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	preserved := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--from-file", filePath, "--role", "initiatives"})
	assertEnvelopeOK(t, preserved)
	overridden := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "patch", boardID, "--from-file", filePath, "--role", "launch", "--if-updated-at", explicitAt})
	assertEnvelopeOK(t, overridden)
	mu.Lock()
	defer mu.Unlock()
	if patchCount != 2 {
		t.Fatalf("expected two file-backed patches, got %d", patchCount)
	}
}
