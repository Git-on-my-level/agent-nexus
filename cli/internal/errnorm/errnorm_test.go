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
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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

func TestEnrichWakeProofRequiredGetsHostedRecoveryHint(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, []byte(`{"error":{"code":"wake_proof_required","message":"validated workspace wake proof is required","recoverable":true,"hint":"generic"}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Hint, "/auth/token") || !strings.Contains(err.Hint, "auth whoami") {
		t.Fatalf("expected hosted auth recovery hint, got %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["anx_cli_recovery"].(map[string]any)
	if rec["kind"] != "hosted_wake_recovery" || rec["recovery_route"] != "/auth/token" {
		t.Fatalf("unexpected recovery details: %#v", rec)
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
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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

func TestEnrichSchemaStrictEnumWrappedBoardColumnKey(t *testing.T) {
	t.Parallel()

	msg := `invalid value "nope" for strict enum board_column_key (allowed: backlog, ready, in_progress, blocked, review, done)`
	err := FromHTTPFailure(400, []byte(fmt.Sprintf(`{"error":{"code":"invalid_request","message":%q,"recoverable":true,"hint":"x"}}`, msg)))
	if !strings.Contains(err.Hint, "board_column_key") || !strings.Contains(err.Hint, "anx cards move --help") {
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
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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

	errBoard := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"patch.status is not supported; use archive/trash lifecycle routes","recoverable":true,"hint":"x"}}`))
	if errBoard == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(errBoard.Message, "patch.status") {
		t.Fatalf("unexpected message: %q", errBoard.Message)
	}
}

func TestEnrichLimitPagination(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"limit must be between 1 and 1000","recoverable":true}}`))
	if !strings.Contains(err.Hint, "1000") {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	rec, _ := details["anx_cli_recovery"].(map[string]any)
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
	rec, ok := details["anx_cli_recovery"].(map[string]any)
	if !ok || rec["kind"] != "invalid_pagination" {
		t.Fatalf("unexpected anx_cli_recovery: %#v", details["anx_cli_recovery"])
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

func TestEnrichForCommandPMDecisionConflicts(t *testing.T) {
	t.Parallel()

	revision := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict"}}`))
	if !strings.Contains(revision.Hint, "if_updated_at") {
		t.Fatalf("generic conflict hint should mention if_updated_at before command enrichment: %q", revision.Hint)
	}
	EnrichForCommand(revision, "pm.decisions.answer")
	if !strings.Contains(revision.Hint, "pm decisions get") || !strings.Contains(revision.Hint, "status") {
		t.Fatalf("expected PM status-check hint, got %q", revision.Hint)
	}
	if strings.Contains(revision.Hint, "retry using its current") {
		t.Fatalf("answer conflict still told the caller to retry with revision: %q", revision.Hint)
	}
	if strings.Contains(revision.Hint, "if_updated_at") {
		t.Fatalf("PM conflict hint still used card/board language: %q", revision.Hint)
	}

	dispatch := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict"}}`))
	EnrichForCommand(dispatch, "pm.decisions.dispatch")
	if !strings.Contains(dispatch.Hint, "status") || strings.Contains(dispatch.Hint, "retry using its current") {
		t.Fatalf("expected dispatch to check status before retry, got %q", dispatch.Hint)
	}

	superseded := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict","details":{"superseded_by":"decision-2","status":"superseded"}}}`))
	EnrichForCommand(superseded, "pm.decisions.answer")
	if !strings.Contains(superseded.Hint, "replaced") || !strings.Contains(superseded.Hint, "decision-2") {
		t.Fatalf("expected superseded replacement id, got %q", superseded.Hint)
	}
	if strings.Contains(superseded.Hint, "retry using its current") {
		t.Fatalf("superseded hint still offered a retry: %q", superseded.Hint)
	}
	supersededDetails, _ := superseded.Details.(map[string]any)
	supersededRec, _ := supersededDetails["anx_cli_recovery"].(map[string]any)
	if supersededRec["superseded_by"] != "decision-2" || supersededRec["status"] != "superseded" {
		t.Fatalf("recovery missing superseded fields: %#v", supersededRec)
	}

	dispatchSuperseded := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict","details":{"superseded_by":"decision-9","status":"superseded"}}}`))
	EnrichForCommand(dispatchSuperseded, "pm.decisions.dispatch")
	if !strings.Contains(dispatchSuperseded.Hint, "decision-9") || strings.Contains(dispatchSuperseded.Hint, "retry using its current") {
		t.Fatalf("expected dispatch superseded hint, got %q", dispatchSuperseded.Hint)
	}

	stale := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed"}}`))
	EnrichForCommand(stale, "pm.actions.reconcile")
	if !strings.Contains(stale.Hint, "stale") || !strings.Contains(strings.ToLower(stale.Hint), "propose") {
		t.Fatalf("expected stale-approval propose-again hint, got %q", stale.Hint)
	}

	ack := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict"}}`))
	EnrichForCommand(ack, "pm.actions.acknowledge")
	if !strings.Contains(ack.Hint, "not in a state that can be acknowledged") || !strings.Contains(ack.Hint, "pm actions reconcile") {
		t.Fatalf("expected acknowledge state-conflict hint, got %q", ack.Hint)
	}
	if strings.Contains(ack.Hint, "if_updated_at") || strings.Contains(ack.Hint, "retry using its current") {
		t.Fatalf("acknowledge hint still used revision language: %q", ack.Hint)
	}

	existing := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict","details":{"existing_decision_id":"decision-9"}}}`))
	EnrichForCommand(existing, "pm.decisions.create")
	if !strings.Contains(existing.Hint, "error.details.existing_decision_id") || !strings.Contains(existing.Hint, "decision-9") {
		t.Fatalf("expected request-key conflict to name existing_decision_id, got %q", existing.Hint)
	}
	details, _ := existing.Details.(map[string]any)
	rec, _ := details["anx_cli_recovery"].(map[string]any)
	if rec["existing_decision_id"] != "decision-9" {
		t.Fatalf("recovery missing existing_decision_id: %#v", rec)
	}

	card := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"card has been updated; refresh and retry"}}`))
	EnrichForCommand(card, "cards.patch")
	if !strings.Contains(card.Hint, "if_updated_at") {
		t.Fatalf("non-PM commands must keep card hints, got %q", card.Hint)
	}
}

func TestEnrichForCommandPMConversationBusy(t *testing.T) {
	t.Parallel()

	generic := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	if !strings.Contains(strings.ToLower(generic.Hint), "command help") {
		t.Fatalf("busy without a command should keep the generic hint, got %q", generic.Hint)
	}

	err := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	EnrichForCommand(err, "pm.conversations.messages.create")
	if !strings.Contains(err.Hint, "queued or being answered") || !strings.Contains(err.Hint, "pm conversations get") {
		t.Fatalf("expected conversation busy hint, got %q", err.Hint)
	}
	if strings.Contains(strings.ToLower(err.Hint), "command help") {
		t.Fatalf("busy hint still generic: %q", err.Hint)
	}
	details, _ := err.Details.(map[string]any)
	if got, _ := details["hint"].(string); got != err.Hint {
		t.Fatalf("details.hint should match: details=%q hint=%q", got, err.Hint)
	}
	rec, _ := details["anx_cli_recovery"].(map[string]any)
	if rec["kind"] != "busy" {
		t.Fatalf("unexpected recovery: %#v", rec)
	}

	alias := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	EnrichForCommand(alias, "pm.conversations.message")
	if !strings.Contains(alias.Hint, "queued or being answered") {
		t.Fatalf("CLI path command id should also enrich busy, got %q", alias.Hint)
	}

	other := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	EnrichForCommand(other, "pm.decisions.answer")
	if strings.Contains(other.Hint, "queued or being answered") {
		t.Fatalf("non-message PM commands must not get the conversation busy hint, got %q", other.Hint)
	}
}
