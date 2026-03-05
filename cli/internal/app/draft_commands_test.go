package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"organization-autorunner-cli/internal/profile"
)

func TestDraftCreateListCommitDiscard(t *testing.T) {
	t.Parallel()

	var createCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/threads" {
			http.NotFound(w, r)
			return
		}
		createCalls++
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"thread":{"title":"Draft Thread"}`) {
			t.Fatalf("unexpected commit payload: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Draft Thread"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	createdRaw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Draft Thread"}}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads.create",
	})
	createdPayload := assertEnvelopeOK(t, createdRaw)
	createdData, _ := createdPayload["data"].(map[string]any)
	draftID, _ := createdData["draft_id"].(string)
	if strings.TrimSpace(draftID) == "" {
		t.Fatalf("missing draft id: %#v", createdPayload)
	}

	listRaw := runCLIForTest(t, home, env, nil, []string{"--json", "--agent", "agent-a", "draft", "list"})
	listPayload := assertEnvelopeOK(t, listRaw)
	listData, _ := listPayload["data"].(map[string]any)
	drafts, _ := listData["drafts"].([]any)
	if len(drafts) != 1 {
		t.Fatalf("expected one draft, got %#v", listPayload)
	}

	commitRaw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "commit", draftID,
	})
	assertEnvelopeOK(t, commitRaw)
	if createCalls != 1 {
		t.Fatalf("expected one committed server write, got %d", createCalls)
	}

	draftPath := filepath.Join(profile.DraftsDir(home), draftID+".json")
	if _, err := os.Stat(draftPath); !os.IsNotExist(err) {
		t.Fatalf("expected draft to be deleted after commit, stat err=%v", err)
	}

	discardRaw := runCLIForTest(t, home, env, nil, []string{"--json", "--agent", "agent-a", "draft", "discard", draftID})
	discardPayload := assertEnvelopeError(t, discardRaw)
	errObj, _ := discardPayload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_not_found" {
		t.Fatalf("unexpected discard payload: %#v", discardPayload)
	}
}

func TestDraftCommitKeep(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"event":{"id":"event_1"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	createdRaw := runCLIForTest(t, home, env, strings.NewReader(`{"event":{"type":"decision_needed","thread_id":"thread_1","summary":"needs review","refs":[],"provenance":{"kind":"actor_statement","actor_id":"actor_1"}}}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "create",
		"--command", "events.create",
	})
	createdPayload := assertEnvelopeOK(t, createdRaw)
	createdData, _ := createdPayload["data"].(map[string]any)
	draftID, _ := createdData["draft_id"].(string)
	if strings.TrimSpace(draftID) == "" {
		t.Fatalf("missing draft id: %#v", createdPayload)
	}

	commitRaw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "commit", draftID, "--keep",
	})
	assertEnvelopeOK(t, commitRaw)

	draftPath := filepath.Join(profile.DraftsDir(home), draftID+".json")
	if _, err := os.Stat(draftPath); err != nil {
		t.Fatalf("expected draft to remain after --keep commit: %v", err)
	}
}

func TestDraftCreateValidationFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"not_thread":{}}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads.create",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_validation_failed" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}

	draftsDir := profile.DraftsDir(home)
	entries, err := os.ReadDir(draftsDir)
	if err == nil && len(entries) > 0 {
		t.Fatalf("expected no drafts to be written on validation failure, got %d", len(entries))
	}
}

func TestDraftCreateAcceptsCLIPathCommand(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Alpha"}}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads create",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStringValue(data["command_id"])) != "threads.create" {
		t.Fatalf("unexpected command resolution payload: %#v", payload)
	}
}

func anyStringValue(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
}

func TestDraftCreateRejectsPathTraversalID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Alpha"}}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads.create",
		"--draft-id", "../escape",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestDraftCommitRejectsMismatchedTarget(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	createdRaw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Draft Thread"}}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads.create",
	})
	createdPayload := assertEnvelopeOK(t, createdRaw)
	createdData, _ := createdPayload["data"].(map[string]any)
	draftID, _ := createdData["draft_id"].(string)
	if draftID == "" {
		t.Fatalf("missing draft id: %#v", createdPayload)
	}

	commitRaw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", "http://127.0.0.1:65535",
		"--agent", "agent-a",
		"draft", "commit", draftID,
	})
	commitPayload := assertEnvelopeError(t, commitRaw)
	errObj, _ := commitPayload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_target_mismatch" {
		t.Fatalf("unexpected error payload: %#v", commitPayload)
	}
}

func TestDraftCommitSuccessWhenCleanupFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/threads" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Draft Thread"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	createdRaw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Draft Thread"}}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "create",
		"--command", "threads.create",
	})
	createdPayload := assertEnvelopeOK(t, createdRaw)
	createdData, _ := createdPayload["data"].(map[string]any)
	draftID, _ := createdData["draft_id"].(string)
	if draftID == "" {
		t.Fatalf("missing draft id: %#v", createdPayload)
	}

	draftsDir := profile.DraftsDir(home)
	if err := os.Chmod(draftsDir, 0o500); err != nil {
		t.Fatalf("chmod drafts dir: %v", err)
	}
	defer os.Chmod(draftsDir, 0o700)

	commitRaw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"draft", "commit", draftID,
	})
	commitPayload := assertEnvelopeOK(t, commitRaw)
	commitData, _ := commitPayload["data"].(map[string]any)
	if commitData == nil {
		t.Fatalf("missing commit data: %#v", commitPayload)
	}
	if commitData["warning"] == nil {
		t.Fatalf("expected cleanup warning in commit payload: %#v", commitPayload)
	}
}

func TestDraftListStableJSON(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	draftsDir := profile.DraftsDir(home)
	if err := os.MkdirAll(draftsDir, 0o700); err != nil {
		t.Fatalf("mkdir drafts: %v", err)
	}
	draft := persistedDraft{
		Version:   1,
		DraftID:   "draft-test",
		CommandID: "threads.create",
		Agent:     "agent-a",
		BaseURL:   "http://127.0.0.1:8000",
		Body:      map[string]any{"thread": map[string]any{"title": "Alpha"}},
		CreatedAt: "2026-03-05T00:00:00Z",
		UpdatedAt: "2026-03-05T00:00:00Z",
	}
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatalf("marshal draft: %v", err)
	}
	if err := os.WriteFile(filepath.Join(draftsDir, "draft-test.json"), encoded, 0o600); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	env := map[string]string{}
	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--agent", "agent-a", "draft", "list"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	drafts, _ := data["drafts"].([]any)
	if len(drafts) != 1 {
		t.Fatalf("unexpected drafts payload: %#v", payload)
	}
}
