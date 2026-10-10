package app

import (
	"agent-nexus-cli/internal/errnorm"
	"fmt"
	"net/http"
	"strings"
)

// A transport error does not establish whether the server committed a write.
// This includes response-body timeouts and broken connections, not just dial errors.
func mutationTransportError(command, method string, body any, cause error) error {
	if method == http.MethodGet || method == http.MethodHead || commandSideEffectClass(command) == "read_only" {
		return errnorm.Wrap(errnorm.KindNetwork, "request_failed", fmt.Sprintf("%s request failed", command), cause)
	}
	retryable := false
	err := errnorm.Wrap(errnorm.KindNetwork, "outcome_unknown", fmt.Sprintf("%s outcome is unknown; the server may have applied the change", command), cause)
	err.Recoverable = &retryable
	err.Hint = "Check current state before submitting another mutation."
	if parts := strings.Fields(command); len(parts) > 0 {
		switch parts[0] {
		case "cards", "boards", "topics", "docs", "work":
			err.Hint = "Check `anx " + parts[0] + " list` before retrying."
		}
	}
	if payload, ok := body.(map[string]any); ok {
		if key := strings.TrimSpace(anyString(payload["request_key"])); key != "" {
			err.Details = map[string]any{"request_key": key, "outcome": "unknown"}
			err.Hint += " Retry the original command and body with --request-key " + key + " (or the same request_key in JSON)."
		}
	}
	return err
}
