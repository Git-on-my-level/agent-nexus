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
