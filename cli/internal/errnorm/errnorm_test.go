package errnorm

import (
	"fmt"
	"strings"
	"testing"
)

func TestFromHTTPFailureParsesRecoverableHint(t *testing.T) {
	t.Parallel()

	// Use not_found so CLI enrichment does not replace the API hint (unlike invalid_token).
	err := FromHTTPFailure(404, []byte(`{"error":{"code":"not_found","message":"missing","recoverable":true,"hint":"check id"}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "not_found" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
	if err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("expected recoverable=true, got %#v", err.Recoverable)
	}
	if err.Hint != "check id" {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrich401EmptyBodyGetsAuthHint(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, nil)
	if err.Code != "remote_error" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
	if !strings.Contains(err.Hint, "auth token-status") {
		t.Fatalf("expected auth recovery hint, got %q", err.Hint)
	}
}

func TestEnrichConflictDocumentStale(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"document has been updated; refresh and retry","recoverable":true,"hint":"generic"}}`))
	if !strings.Contains(err.Hint, "if_document_updated_at") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichConflictBoardStale(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"board has been updated; refresh and retry","recoverable":true,"hint":"Reload current state and retry with a fresh concurrency token."}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Hint, "if_board_updated_at") {
		t.Fatalf("expected stale board token hint, got %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	if got, _ := details["hint"].(string); got != err.Hint {
		t.Fatalf("details.hint should match normalized hint for JSON consumers: details=%q err.Hint=%q", got, err.Hint)
	}
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["field"] != "if_board_updated_at" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}
}

func TestEnrichInvalidTokenGetsAuthHint(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, []byte(`{"error":{"code":"invalid_token","message":"invalid","recoverable":true,"hint":"Refresh or rotate credentials, then retry."}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Hint, "auth token-status") {
		t.Fatalf("expected auth recovery hint, got %q", err.Hint)
	}
}

func TestEnrichAuthRequiredGetsAuthHint(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, []byte(`{"error":{"code":"auth_required","message":"missing bearer","recoverable":true,"hint":"Attach a valid Bearer token and retry."}}`))
	if !strings.Contains(err.Hint, "auth token-status") {
		t.Fatalf("expected auth recovery hint, got %q", err.Hint)
	}
}

func TestEnrichInvalidRequestIfUpdatedAtRequired(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"if_updated_at is required","recoverable":true,"hint":"Fix request shape/fields and retry."}}`))
	if !strings.Contains(err.Hint, "if_updated_at") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichColumnKeyInvalidMessage(t *testing.T) {
	t.Parallel()

	// Mirrors core boards_handlers.go / boards_store validation wording.
	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"column_key must be one of: backlog, ready, in_progress, blocked, review, done","recoverable":true,"hint":"Fix request shape/fields and retry."}}`))
	if !strings.Contains(err.Hint, "column_key") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	// Hint should not repeat the server's full "must be one of" line (error.message already has it).
	if strings.Count(err.Hint, "must be one of") != 0 {
		t.Fatalf("hint should not echo server's must-be-one-of sentence, got %q", err.Hint)
	}
	if !strings.Contains(err.Hint, "Use one of:") {
		t.Fatalf("expected compact enum guidance, got %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["field"] != "column_key" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}
}

func TestEnrichKeyMismatchAssertion(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, []byte(`{"error":{"code":"key_mismatch","message":"key assertion could not be validated","recoverable":true,"hint":"Rotate key material and retry token exchange."}}`))
	if !strings.Contains(err.Hint, "auth token-status") {
		t.Fatalf("expected key mismatch recovery hint, got %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["reason"] != "key_assertion_failed" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}
}

func TestEnrichKeyMismatchActorID(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(403, []byte(`{"error":{"code":"key_mismatch","message":"actor_id does not match authenticated principal","recoverable":true,"hint":"x"}}`))
	if !strings.Contains(err.Hint, "auth whoami") {
		t.Fatalf("expected actor mismatch hint, got %q", err.Hint)
	}
}

func TestEnrichPatchRiskMustBeOneOf(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"patch.risk must be one of: low, medium, high, critical","recoverable":true,"hint":"x"}}`))
	if !strings.Contains(err.Hint, "patch.risk") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	if strings.Contains(err.Hint, "must be one of: low") {
		t.Fatalf("hint should not repeat server enum line, got %q", err.Hint)
	}
}

func TestEnrichBoardStatusMustBeOneOf(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"board.status must be one of: active, paused, closed","recoverable":true,"hint":"x"}}`))
	if !strings.Contains(err.Hint, "board.status") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	if !strings.Contains(err.Hint, "anx help") || !strings.Contains(err.Hint, "boards update") {
		t.Fatalf("expected anx help discovery for boards update, got %q", err.Hint)
	}
}

func TestEnrichSchemaStrictEnumWrappedTopicStatus(t *testing.T) {
	t.Parallel()

	msg := `topic.status: invalid value "nope" for strict enum topic_status (allowed: active, archived, blocked, closed, paused, proposed, resolved)`
	err := FromHTTPFailure(400, []byte(fmt.Sprintf(`{"error":{"code":"invalid_request","message":%q,"recoverable":true,"hint":"x"}}`, msg)))
	if !strings.Contains(err.Hint, "topic_status") || !strings.Contains(err.Hint, "anx topics patch --help") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichBoardAlreadyExists(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"board already exists","recoverable":true,"hint":"x"}}`))
	if !strings.Contains(err.Hint, "boards list") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichAgentRevoked(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(403, []byte(`{"error":{"code":"agent_revoked","message":"agent has been revoked","recoverable":false,"hint":"x"}}`))
	if !strings.Contains(err.Hint, "auth register") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichInvalidRequestRFC3339Field(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"if_board_updated_at must be an RFC3339 datetime string","recoverable":true}}`))
	if !strings.Contains(err.Hint, "if_board_updated_at") || !strings.Contains(err.Hint, "RFC3339") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["kind"] != "invalid_timestamp_format" {
		t.Fatalf("unexpected recovery kind: %#v", rec)
	}
}

func TestEnrichResolutionRequiresDoneColumn(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"resolution requires column_key done","recoverable":true}}`))
	if !strings.Contains(err.Hint, "column_key") || !strings.Contains(err.Hint, "done") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["kind"] != "resolution_workflow" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}
}

func TestEnrichPatchStatusNotSupported(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"patch.status is not supported; use the move endpoint and patch.resolution","recoverable":true}}`))
	if !strings.Contains(err.Hint, "cards move") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestEnrichLimitPagination(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"limit must be between 1 and 1000","recoverable":true}}`))
	if !strings.Contains(err.Hint, "1000") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["oar_cli_recovery"].(map[string]any)
	if rec["kind"] != "invalid_pagination" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}
}

func TestEnrichLegacyAliasMixing(t *testing.T) {
	t.Parallel()

	msg := "patch.summary must not be combined with legacy aliases patch.body, patch.body_markdown"
	err := FromHTTPFailure(400, []byte(fmt.Sprintf(`{"error":{"code":"invalid_request","message":%q,"recoverable":true}}`, msg)))
	if !strings.Contains(err.Hint, "legacy aliases") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestNormalizeEnrichesLocalLimitInvalidRequest(t *testing.T) {
	t.Parallel()

	err := Normalize(Usage("invalid_request", "limit must be between 1 and 1000"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Hint, "1000") {
		t.Fatalf("expected pagination hint, got %q", err.Hint)
	}
	details, ok := err.Details.(map[string]any)
	if !ok {
		t.Fatalf("expected details map, got %T", err.Details)
	}
	rec, ok := details["oar_cli_recovery"].(map[string]any)
	if !ok || rec["kind"] != "invalid_pagination" {
		t.Fatalf("unexpected oar_cli_recovery: %#v", details["oar_cli_recovery"])
	}
}

func TestNormalizeAppliesMetadataDefaults(t *testing.T) {
	t.Parallel()

	err := Normalize(Usage("invalid_request", "bad request"))
	if err == nil {
		t.Fatal("expected normalized error")
	}
	if err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("expected recoverable=true for invalid_request, got %#v", err.Recoverable)
	}
	if err.Hint == "" {
		t.Fatal("expected non-empty hint")
	}
}
