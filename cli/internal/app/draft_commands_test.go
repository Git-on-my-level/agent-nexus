package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"organization-autorunner-cli/internal/profile"
)

func TestDraftCreateAggregatesEventValidationErrors(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"event":{"thread_id":"thread_1","actor_id":"actor_1","type":"actor_statement"}}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "events.create",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_validation_failed" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	details, _ := errObj["details"].(map[string]any)
	rawErrors, _ := details["errors"].([]any)
	if len(rawErrors) < 3 {
		t.Fatalf("expected aggregated validation errors, got %#v", payload)
	}
	joined := make([]string, 0, len(rawErrors))
	for _, item := range rawErrors {
		joined = append(joined, anyStringValue(item))
	}
	joinedText := strings.Join(joined, "\n")
	for _, expected := range []string{"event.summary is required", "event.refs is required", "event.provenance is required"} {
		if !strings.Contains(joinedText, expected) {
			t.Fatalf("expected error %q in payload=%#v", expected, payload)
		}
	}
}

func TestDraftCreateEventRequiresThreadIDForThreadScopedTypes(t *testing.T) {
	t.Parallel()
	t.Skip("obsolete compatibility coverage")

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"event":{"type":"message_posted","summary":"hello","refs":["thread:thread_1"],"provenance":{"sources":["actor_statement:event_seed"]}}}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "events.create",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_validation_failed" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	details, _ := errObj["details"].(map[string]any)
	rawErrors, _ := details["errors"].([]any)
	joined := make([]string, 0, len(rawErrors))
	for _, item := range rawErrors {
		joined = append(joined, anyStringValue(item))
	}
	joinedText := strings.Join(joined, "\n")
	if !strings.Contains(joinedText, `event.thread_id is required for event.type="message_posted"`) {
		t.Fatalf("expected thread requirement validation in payload=%#v", payload)
	}
}

func TestDraftCreateAllowsDerivedRebuildWithoutActorID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "derived.rebuild",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStringValue(data["command_id"])) != "derived.rebuild" {
		t.Fatalf("unexpected command payload: %#v", payload)
	}
}

func TestDraftCreateDerivedRebuildRejectsEmptyActorIDWhenProvided(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"   "}`), []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "derived.rebuild",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "draft_validation_failed" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestDraftCreateAcceptsCLIPathCommand(t *testing.T) {
	t.Parallel()
	t.Skip("obsolete compatibility coverage")

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Alpha","type":"incident","status":"active","priority":"p2","tags":[],"cadence":"reactive","current_summary":"seed","next_actions":[],"key_artifacts":[],"provenance":{"sources":["actor_statement:event_seed"]}}}`), []string{
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

func TestDraftCreateHelpWithCommandShowsTargetSchema(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"draft", "create", "--command", "events.create", "--help"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Target command: events.create") {
		t.Fatalf("expected target command help output=%s", output)
	}
	if !strings.Contains(output, "Inputs:") {
		t.Fatalf("expected input block in draft create help output=%s", output)
	}
	if !strings.Contains(output, "receipt_added") {
		t.Fatalf("expected enum values in draft create help output=%s", output)
	}
	if !strings.Contains(output, "Communication: direct communication or important non-structured information") {
		t.Fatalf("expected communication group hint in draft create help output=%s", output)
	}
	if !strings.Contains(output, "`receipt_added`: prefer `oar receipts create`") {
		t.Fatalf("expected higher-level packet lifecycle hint in draft create help output=%s", output)
	}
}

func TestDraftCreateTreatsHelpAsFlagValue(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}

	fromFile := filepath.Join(t.TempDir(), "help")
	body := `{"topic":{"type":"incident","status":"active","title":"Alpha","summary":"seed","owner_refs":["thread:thread_1"],"document_refs":[],"board_refs":[],"related_refs":[],"provenance":{"sources":["actor_statement:event_seed"]}}}`
	if err := os.WriteFile(fromFile, []byte(body), 0o600); err != nil {
		t.Fatalf("write from-file body: %v", err)
	}

	raw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--agent", "agent-a",
		"draft", "create",
		"--command", "topics.create",
		"--from-file", fromFile,
		"--draft-id", "help",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if strings.TrimSpace(anyStringValue(data["draft_id"])) != "help" {
		t.Fatalf("expected draft id=help payload=%#v", payload)
	}
}

func TestValidateDraftBodySupportsInboxAcknowledge(t *testing.T) {
	t.Parallel()

	errors := validateDraftBody("inbox.acknowledge", map[string]any{
		"actor_id":      "actor_1",
		"subject_ref":   "thread:thread_1",
		"inbox_item_id": "inbox:decision_needed:thread_1:none:event_1",
	})
	if len(errors) != 0 {
		t.Fatalf("expected inbox.acknowledge draft validation to pass, got %#v", errors)
	}
}

func anyStringValue(raw any) string {
	text, _ := raw.(string)
	return strings.TrimSpace(text)
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
		CommandID: "topics.create",
		Agent:     "agent-a",
		BaseURL:   "http://127.0.0.1:8000",
		Body:      map[string]any{"topic": map[string]any{"title": "Alpha"}},
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
