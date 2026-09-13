package app

import (
	"bytes"
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
	err := errnorm.FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"target is not current"}}`))
	exit := a.renderError(machineCommandIdentity{Command: "pm decisions answer", CommandID: "pm.decisions.answer"}, false, err)
	if exit != 1 {
		t.Fatalf("expected exit 1, got %d", exit)
	}
	out := stderr.String()
	if !strings.Contains(out, "Hint:") || !strings.Contains(out, "task changed after this proposal") || !strings.Contains(out, "pm decisions answer --decline") {
		t.Fatalf("expected stale-approve hint, got %q", out)
	}
}
