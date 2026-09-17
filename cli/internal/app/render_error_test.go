package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"agent-nexus-cli/internal/errnorm"
)

func TestRenderErrorTextModeIncludesHintLine(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	a := &App{Stderr: &stderr}
	err := errnorm.FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"board has been updated; refresh and retry","recoverable":true,"hint":"generic"}}`))
	exit := a.renderError(machineCommandIdentity{Command: "boards", CommandID: "boards.get"}, false, err)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	out := stderr.String()
	if !strings.Contains(out, "Error (conflict):") {
		t.Fatalf("expected error line, got %q", out)
	}
	if !strings.HasPrefix(out, "Error (conflict):") {
		t.Fatalf("unexpected format: %q", out)
	}
	if !strings.Contains(out, "Hint:") || !strings.Contains(out, "if_board_updated_at") {
		t.Fatalf("expected hint line with token name, got %q", out)
	}
}

func TestRenderErrorPMConflictUsesRevisionHint(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	a := &App{Stderr: &stderr}
	err := errnorm.FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict"}}`))
	exit := a.renderError(machineCommandIdentity{Command: "pm decisions answer", CommandID: "pm.decisions.answer"}, false, err)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	out := stderr.String()
	if !strings.Contains(out, "Hint:") || !strings.Contains(out, "pm decisions get") || !strings.Contains(out, "status") {
		t.Fatalf("expected PM status-check hint, got %q", out)
	}
	if strings.Contains(out, "if_updated_at") || strings.Contains(out, "retry using its current") {
		t.Fatalf("PM 409 still offered a revision retry: %q", out)
	}
}

func TestRenderErrorPMStaleApprovalHint(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	a := &App{Stderr: &stderr}
	err := errnorm.FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"target is not current","details":{"reason":"revision_changed"}}}`))
	exit := a.renderError(machineCommandIdentity{Command: "pm decisions answer", CommandID: "pm.decisions.answer"}, false, err)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	out := stderr.String()
	if !strings.Contains(out, "Hint:") || !strings.Contains(out, "task changed after this proposal") || !strings.Contains(out, "pm decisions answer <id> --from-file -") {
		t.Fatalf("expected stale-approve hint, got %q", out)
	}
	if strings.Contains(out, "--decline") {
		t.Fatalf("hint still names a nonexistent --decline flag: %q", out)
	}
}

func TestPMDecisionsAnswerStaleHintUsesRealFlagSet(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"revision_changed", "already_at_target", "work_missing", ""} {
		body := `{"error":{"code":"source_revision_changed","message":"x","details":{"reason":"` + reason + `"}}}`
		err := errnorm.FromHTTPFailure(409, []byte(body))
		errnorm.EnrichForCommand(err, "pm.decisions.answer")
		var found bool
		for _, cmd := range backtickCommands(err.Hint) {
			if !strings.Contains(cmd, "pm decisions answer") {
				continue
			}
			found = true
			args := tokenizeHintCommand(cmd)
			_, parseErr := parseWorkCommand(args)
			if parseErr == nil {
				continue
			}
			var typed *errnorm.Error
			if errors.As(parseErr, &typed) && typed.Code == "invalid_flags" {
				t.Fatalf("reason %q hint command %q uses unknown flags: %v", reason, cmd, parseErr)
			}
		}
		if !found {
			t.Fatalf("reason %q hint had no pm decisions answer command: %q", reason, err.Hint)
		}
	}
}

func backtickCommands(s string) []string {
	var out []string
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			return out
		}
		s = s[start+1:]
		end := strings.Index(s, "`")
		if end < 0 {
			return out
		}
		out = append(out, s[:end])
		s = s[end+1:]
	}
}

func tokenizeHintCommand(cmd string) []string {
	fields := strings.Fields(cmd)
	if len(fields) > 0 && fields[0] == "anx" {
		fields = fields[1:]
	}
	for i, f := range fields {
		if f == "<id>" || f == "<ref>" {
			fields[i] = "decision-1"
		}
	}
	return fields
}
