package integrations

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxPayloadBytes = 64 << 10

var taskPattern = regexp.MustCompile(`\btask_[0-9a-f]{8}\b`)

type Event struct {
	Key       string    `json:"key"`
	TaskID    string    `json:"task_id,omitempty"`
	SessionID string    `json:"session_id"`
	CWD       string    `json:"cwd"`
	Title     string    `json:"title"`
	TLDR      string    `json:"tldr"`
	Prompt    string    `json:"prompt"`
	Host      string    `json:"host"`
	Timestamp time.Time `json:"timestamp"`
}

func parseSuggestedTask(raw []byte) (Event, error) {
	var p struct {
		SessionID     string `json:"session_id"`
		CWD           string `json:"cwd"`
		HookEventName string `json:"hook_event_name"`
		ToolName      string `json:"tool_name"`
		ToolUseID     string `json:"tool_use_id"`
		ToolInput     struct {
			Title  string `json:"title"`
			TLDR   string `json:"tldr"`
			Prompt string `json:"prompt"`
		} `json:"tool_input"`
		ToolResponse json.RawMessage `json:"tool_response"`
	}
	if len(raw) > MaxPayloadBytes {
		return Event{}, fmt.Errorf("payload_too_large")
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return Event{}, fmt.Errorf("invalid_payload")
	}
	if p.HookEventName != "PostToolUse" || p.ToolName != "mcp__ccd_session__spawn_task" {
		return Event{}, fmt.Errorf("unexpected_event")
	}
	if strings.TrimSpace(p.ToolInput.Title) == "" || !filepath.IsAbs(p.CWD) {
		return Event{}, fmt.Errorf("invalid_payload")
	}
	task := taskPattern.FindString(string(p.ToolResponse))
	key := task
	if key == "" {
		if p.SessionID == "" || p.ToolUseID == "" {
			return Event{}, fmt.Errorf("missing_event_identity")
		}
		key = p.SessionID + "\x00" + p.ToolUseID
	}
	return Event{Key: "anx-integration-" + Digest(key), TaskID: task, SessionID: p.SessionID, CWD: p.CWD, Title: p.ToolInput.Title, TLDR: p.ToolInput.TLDR, Prompt: p.ToolInput.Prompt}, nil
}
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
func CardBody(e Event, target Target) map[string]any {
	// Quote the prompt linearly; no user-provided fence can terminate it.
	provenance := fmt.Sprintf("\n\nHarness: claude-code\nSession: %s\nCwd: %s\nTask: %s\nHost: %s\nTimestamp: %s", truncate(e.SessionID, 256), truncate(e.CWD, 4096), e.TaskID, truncate(e.Host, 255), e.Timestamp.UTC().Format(time.RFC3339Nano))
	quoted := "> " + strings.ReplaceAll(e.Prompt, "\n", "\n> ")
	body := truncate(e.TLDR, 4000) + "\n\n" + truncate(quoted, 54000) + provenance
	card := map[string]any{"title": truncate(e.Title, 500), "summary": truncate(body, 64000), "assignee_refs": []any{}, "resolution_refs": []any{}, "related_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}
	if target.Column != "" {
		card["column_key"] = target.Column
	}
	board := target.Board
	if !strings.HasPrefix(board, "board:") {
		board = "board:" + board
	}
	return map[string]any{"board_ref": board, "request_key": e.Key, "card": card}
}
