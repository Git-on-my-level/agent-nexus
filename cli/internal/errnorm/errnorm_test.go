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
	if !strings.Contains(revision.Hint, "re-read it and retry") || strings.Contains(revision.Hint, "if_updated_at") {
		t.Fatalf("generic conflict hint should be command-neutral: %q", revision.Hint)
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
	if !strings.Contains(stale.Hint, "read-back") || !strings.Contains(stale.Hint, "Propose the decision again") {
		t.Fatalf("expected neutral stale-read-back hint, got %q", stale.Hint)
	}
	if strings.Contains(stale.Hint, "The PM must propose") || strings.Contains(strings.ToLower(stale.Hint), "approval") {
		t.Fatalf("reconcile hint asserted who must propose or used approval language: %q", stale.Hint)
	}

	dispatchHuman := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"origin_kind":"human","proposed_by":"actor-maya"}}}`))
	EnrichForCommand(dispatchHuman, "pm.decisions.dispatch")
	if !strings.Contains(dispatchHuman.Hint, "Propose it again from the board") || !strings.Contains(dispatchHuman.Hint, "anx pm decisions create") || strings.Contains(dispatchHuman.Hint, "The PM must propose") {
		t.Fatalf("expected human dispatch propose-again hint, got %q", dispatchHuman.Hint)
	}

	dispatchPM := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"origin_kind":"pm_turn","proposed_by":"actor-gds-pm"}}}`))
	EnrichForCommand(dispatchPM, "pm.decisions.dispatch")
	if !strings.Contains(dispatchPM.Hint, "The PM must propose the decision again") || strings.Contains(dispatchPM.Hint, "from the board") {
		t.Fatalf("expected pm_turn dispatch propose-again hint, got %q", dispatchPM.Hint)
	}

	answerStale := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"target is not current","details":{"reason":"revision_changed"}}}`))
	EnrichForCommand(answerStale, "pm.decisions.answer")
	if !strings.Contains(answerStale.Hint, "task changed after this proposal") || !strings.Contains(answerStale.Hint, "failed at delivery") {
		t.Fatalf("expected stale-approve delivery hint, got %q", answerStale.Hint)
	}
	if !strings.Contains(answerStale.Hint, "pm decisions answer <id> --from-file -") || !strings.Contains(answerStale.Hint, "pm turns propose") || !strings.Contains(answerStale.Hint, "board move") {
		t.Fatalf("expected decline or fresh-proposal recovery, got %q", answerStale.Hint)
	}
	if strings.Contains(answerStale.Hint, "--decline") {
		t.Fatalf("hint still names --decline: %q", answerStale.Hint)
	}
	answerDetails, _ := answerStale.Details.(map[string]any)
	answerRec, _ := answerDetails["anx_cli_recovery"].(map[string]any)
	if answerRec["kind"] != "stale_source_revision" || answerRec["reason"] != "revision_changed" {
		t.Fatalf("answer recovery=%#v", answerRec)
	}

	answerMoot := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"Approved source revision has changed (approved at 0, source now 0)","details":{"reason":"already_at_target"}}}`))
	EnrichForCommand(answerMoot, "pm.decisions.answer")
	if !strings.Contains(answerMoot.Hint, "already where this proposal asks") || strings.Contains(answerMoot.Hint, "task changed") {
		t.Fatalf("expected already-at-target hint, got %q", answerMoot.Hint)
	}

	answerGone := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"work missing","details":{"reason":"work_missing"}}}`))
	EnrichForCommand(answerGone, "pm.decisions.answer")
	if !strings.Contains(answerGone.Hint, "task no longer exists") || strings.Contains(answerGone.Hint, "task changed") {
		t.Fatalf("expected work-missing hint, got %q", answerGone.Hint)
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

	genericCard := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"state conflict"}}`))
	if strings.Contains(genericCard.Hint, "if_updated_at") {
		t.Fatalf("generic conflict should not mention if_updated_at: %q", genericCard.Hint)
	}
	EnrichForCommand(genericCard, "cards.patch")
	if !strings.Contains(genericCard.Hint, "if_updated_at") {
		t.Fatalf("card commands should keep if_updated_at wording, got %q", genericCard.Hint)
	}

	genericBoard := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"state conflict"}}`))
	EnrichForCommand(genericBoard, "boards.patch")
	if !strings.Contains(genericBoard.Hint, "if_board_updated_at") {
		t.Fatalf("board commands should keep board token wording, got %q", genericBoard.Hint)
	}

	genericDoc := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"state conflict"}}`))
	EnrichForCommand(genericDoc, "docs.revisions.create")
	if !strings.Contains(genericDoc.Hint, "if_document_updated_at") {
		t.Fatalf("document commands should keep document token wording, got %q", genericDoc.Hint)
	}

	lease := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"this turn is not claimed; claim it first"}}`))
	EnrichForCommand(lease, "pm.turns.claim")
	if strings.Contains(lease.Hint, "if_updated_at") {
		t.Fatalf("PM lease conflict still used card language: %q", lease.Hint)
	}
	if !strings.Contains(lease.Hint, "--lease-token") || !strings.Contains(lease.Hint, "ANX_PM_LEASE_TOKEN") {
		t.Fatalf("PM lease conflict should name the lease token, got %q", lease.Hint)
	}
	if strings.Contains(lease.Hint, "re-read it and retry") {
		t.Fatalf("PM lease conflict still used the revision hint, got %q", lease.Hint)
	}
}

func TestEnrichLeaseTokenErrors(t *testing.T) {
	t.Parallel()

	required := FromHTTPFailure(409, []byte(`{"error":{"code":"lease_required","message":"lease token is required"}}`))
	if !strings.Contains(required.Hint, "--lease-token") || !strings.Contains(required.Hint, "ANX_PM_LEASE_TOKEN") {
		t.Fatalf("lease_required hint=%q", required.Hint)
	}
	if strings.Contains(required.Hint, "re-read it and retry") {
		t.Fatalf("lease_required still used revision hint: %q", required.Hint)
	}

	mismatch := FromHTTPFailure(409, []byte(`{"error":{"code":"lease_mismatch","message":"lease token does not match"}}`))
	if !strings.Contains(mismatch.Hint, "released or re-claimed") || !strings.Contains(mismatch.Hint, "claim") {
		t.Fatalf("lease_mismatch hint=%q", mismatch.Hint)
	}
	if !strings.Contains(mismatch.Hint, "--lease-token") || !strings.Contains(mismatch.Hint, "ANX_PM_LEASE_TOKEN") {
		t.Fatalf("lease_mismatch should still name the token flag: %q", mismatch.Hint)
	}

	conflictLease := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"lease token does not match the current lease"}}`))
	if !strings.Contains(conflictLease.Hint, "released or re-claimed") {
		t.Fatalf("conflict lease message hint=%q", conflictLease.Hint)
	}

	revision := FromHTTPFailure(409, []byte(`{"error":{"code":"conflict","message":"PM revision or state conflict"}}`))
	if !strings.Contains(revision.Hint, "re-read it and retry") {
		t.Fatalf("generic PM conflict should keep revision hint, got %q", revision.Hint)
	}

	delivered := FromHTTPFailure(409, []byte(`{"error":{"code":"lease_mismatch","message":"the turn is already delivered and no retry is needed"}}`))
	if !strings.Contains(delivered.Hint, "already delivered and no retry is needed") {
		t.Fatalf("delivered replay hint=%q", delivered.Hint)
	}
	if strings.Contains(delivered.Hint, "claim the turn again") || strings.Contains(delivered.Hint, "re-read it and retry") {
		t.Fatalf("delivered replay still asked for retry: %q", delivered.Hint)
	}
	EnrichForCommand(delivered, "pm.turns.complete")
	if !strings.Contains(delivered.Hint, "already delivered and no retry is needed") {
		t.Fatalf("complete delivered hint=%q", delivered.Hint)
	}

	releaseMismatch := FromHTTPFailure(409, []byte(`{"error":{"code":"lease_mismatch","message":"the lease was released or re-claimed; claim the turn again"}}`))
	EnrichForCommand(releaseMismatch, "pm.turns.release")
	if !strings.Contains(releaseMismatch.Hint, "does not match the current lease") {
		t.Fatalf("release mismatch hint=%q", releaseMismatch.Hint)
	}
	if strings.Contains(releaseMismatch.Hint, "re-read it and retry") {
		t.Fatalf("release mismatch still used revision hint: %q", releaseMismatch.Hint)
	}

	notClaimed := FromHTTPFailure(409, []byte(`{"error":{"code":"turn_not_claimed","message":"this turn is not claimed"}}`))
	if !strings.Contains(notClaimed.Hint, "not claimed; there is nothing to release") {
		t.Fatalf("turn_not_claimed hint=%q", notClaimed.Hint)
	}
	EnrichForCommand(notClaimed, "pm.turns.release")
	if !strings.Contains(notClaimed.Hint, "nothing to release") {
		t.Fatalf("release turn_not_claimed hint=%q", notClaimed.Hint)
	}
}

func TestEnrichForCommandPMBusyReasons(t *testing.T) {
	t.Parallel()

	generic := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	if !strings.Contains(strings.ToLower(generic.Hint), "command help") {
		t.Fatalf("busy without a command should keep the generic hint, got %q", generic.Hint)
	}

	conversationBody := []byte(`{"error":{"code":"busy","message":"PM execution capacity reached","details":{"reason":"conversation"}}}`)
	capacityBody := []byte(`{"error":{"code":"busy","message":"PM execution capacity reached","details":{"reason":"capacity"}}}`)
	conversationHint := "queued or being answered"
	capacityHint := "in-flight limit for this workspace"

	for _, commandID := range []string{"pm.conversations.messages.create", "pm.conversations.message", "pm.ask"} {
		conv := FromHTTPFailure(429, conversationBody)
		EnrichForCommand(conv, commandID)
		if !strings.Contains(conv.Hint, conversationHint) || !strings.Contains(conv.Hint, "pm conversations get") {
			t.Fatalf("%s conversation hint=%q", commandID, conv.Hint)
		}
		if strings.Contains(conv.Hint, capacityHint) {
			t.Fatalf("%s used capacity wording for conversation: %q", commandID, conv.Hint)
		}
		details, _ := conv.Details.(map[string]any)
		rec, _ := details["anx_cli_recovery"].(map[string]any)
		if rec["reason"] != "conversation" {
			t.Fatalf("%s recovery=%#v", commandID, rec)
		}

		capErr := FromHTTPFailure(429, capacityBody)
		EnrichForCommand(capErr, commandID)
		if !strings.Contains(capErr.Hint, capacityHint) || !strings.Contains(capErr.Hint, "release a stuck runner") {
			t.Fatalf("%s capacity hint=%q", commandID, capErr.Hint)
		}
		if strings.Contains(capErr.Hint, conversationHint) {
			t.Fatalf("%s used conversation wording for capacity: %q", commandID, capErr.Hint)
		}
		capDetails, _ := capErr.Details.(map[string]any)
		capRec, _ := capDetails["anx_cli_recovery"].(map[string]any)
		if capRec["reason"] != "capacity" {
			t.Fatalf("%s capacity recovery=%#v", commandID, capRec)
		}
	}

	missing := FromHTTPFailure(429, []byte(`{"error":{"code":"busy","message":"PM execution capacity reached"}}`))
	EnrichForCommand(missing, "pm.ask")
	if !strings.Contains(missing.Hint, capacityHint) {
		t.Fatalf("missing reason should use capacity hint, got %q", missing.Hint)
	}

	queueBody := []byte(`{"error":{"code":"busy","message":"PM execution capacity reached","details":{"reason":"queue"}}}`)
	queueHint := "PM queue for this workspace is full"
	queue := FromHTTPFailure(429, queueBody)
	if !strings.Contains(queue.Hint, queueHint) {
		t.Fatalf("queue busy without a command should already name the queue, got %q", queue.Hint)
	}
	if strings.Contains(queue.Hint, capacityHint) || strings.Contains(queue.Hint, conversationHint) {
		t.Fatalf("queue busy used another reason's wording: %q", queue.Hint)
	}
	EnrichForCommand(queue, "pm.ask")
	if !strings.Contains(queue.Hint, queueHint) || !strings.Contains(queue.Hint, "attach more capacity") {
		t.Fatalf("pm.ask queue hint=%q", queue.Hint)
	}
	queueDetails, _ := queue.Details.(map[string]any)
	queueRec, _ := queueDetails["anx_cli_recovery"].(map[string]any)
	if queueRec["reason"] != "queue" {
		t.Fatalf("queue recovery=%#v", queueRec)
	}

	other := FromHTTPFailure(429, conversationBody)
	EnrichForCommand(other, "pm.decisions.answer")
	if strings.Contains(other.Hint, conversationHint) || strings.Contains(other.Hint, capacityHint) {
		t.Fatalf("non-message PM commands must not get message busy hints, got %q", other.Hint)
	}
}

func TestEnrichHumanProposalPending(t *testing.T) {
	t.Parallel()

	generic := FromHTTPFailure(409, []byte(`{"error":{"code":"human_proposal_pending","message":"Human proposal decision-7 is pending; a human must decline or answer it first.","details":{"pending_decision_id":"decision-7"}}}`))
	if strings.Contains(strings.ToLower(generic.Hint), "command help") {
		t.Fatalf("human_proposal_pending still used the generic hint: %q", generic.Hint)
	}
	if !strings.Contains(generic.Hint, "A human proposal decision-7 is already waiting on this task") {
		t.Fatalf("hint missing pending id, got %q", generic.Hint)
	}
	if !strings.Contains(generic.Hint, "answered or declined") || !strings.Contains(generic.Hint, "identical proposal is accepted") {
		t.Fatalf("hint missing recovery, got %q", generic.Hint)
	}
	details, _ := generic.Details.(map[string]any)
	rec, _ := details["anx_cli_recovery"].(map[string]any)
	if rec["kind"] != "human_proposal_pending" || rec["pending_decision_id"] != "decision-7" {
		t.Fatalf("recovery=%#v", rec)
	}

	for _, commandID := range []string{"pm.turns.decisions.create", "pm.decisions.create"} {
		err := FromHTTPFailure(409, []byte(`{"error":{"code":"human_proposal_pending","message":"Human proposal decision-3 is pending; a human must decline or answer it first.","details":{"pending_decision_id":"decision-3"}}}`))
		EnrichForCommand(err, commandID)
		if !strings.Contains(err.Hint, "A human proposal decision-3 is already waiting on this task") {
			t.Fatalf("%s hint=%q", commandID, err.Hint)
		}
		if strings.Contains(strings.ToLower(err.Hint), "command help") {
			t.Fatalf("%s still used the generic hint: %q", commandID, err.Hint)
		}
	}

	missing := FromHTTPFailure(409, []byte(`{"error":{"code":"human_proposal_pending","message":"Human proposal is pending; a human must decline or answer it first."}}`))
	if !strings.Contains(missing.Hint, "A human proposal is already waiting on this task") {
		t.Fatalf("missing id hint=%q", missing.Hint)
	}
	if strings.Contains(missing.Hint, "A human proposal  is already") {
		t.Fatalf("missing id left a double space: %q", missing.Hint)
	}
}

func TestEnrichStaleSourceRevisionHints(t *testing.T) {
	t.Parallel()

	dispatchWorkMissing := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"reason":"work_missing","origin_kind":"pm_turn","proposed_by":"actor-gds-pm"}}}`))
	EnrichForCommand(dispatchWorkMissing, "pm.decisions.dispatch")
	if dispatchWorkMissing.Hint != "The task this approval refers to no longer exists; nothing was sent. Acknowledge the failed action with `anx pm actions acknowledge <id>`." {
		t.Fatalf("dispatch work_missing hint=%q", dispatchWorkMissing.Hint)
	}
	if strings.Contains(dispatchWorkMissing.Hint, "The PM must propose") {
		t.Fatalf("work_missing should not add propose-again advice: %q", dispatchWorkMissing.Hint)
	}

	reconcileWorkMissing := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"reason":"work_missing"}}}`))
	EnrichForCommand(reconcileWorkMissing, "pm.actions.reconcile")
	if !strings.Contains(reconcileWorkMissing.Hint, "this read-back refers to no longer exists") || !strings.Contains(reconcileWorkMissing.Hint, "anx pm actions acknowledge") {
		t.Fatalf("reconcile work_missing hint=%q", reconcileWorkMissing.Hint)
	}
	if strings.Contains(strings.ToLower(reconcileWorkMissing.Hint), "approval") {
		t.Fatalf("reconcile work_missing used approval language: %q", reconcileWorkMissing.Hint)
	}

	dispatchRevisionHuman := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"reason":"revision_changed","origin_kind":"human","proposed_by":"actor-maya"}}}`))
	EnrichForCommand(dispatchRevisionHuman, "pm.decisions.dispatch")
	if !strings.Contains(dispatchRevisionHuman.Hint, "This approval is stale") || !strings.Contains(dispatchRevisionHuman.Hint, "Propose it again from the board") || strings.Contains(dispatchRevisionHuman.Hint, "The PM must propose") {
		t.Fatalf("dispatch revision_changed human hint=%q", dispatchRevisionHuman.Hint)
	}

	dispatchRevisionPM := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":{"reason":"revision_changed","origin_kind":"pm_turn"}}}`))
	EnrichForCommand(dispatchRevisionPM, "pm.decisions.dispatch")
	if !strings.Contains(dispatchRevisionPM.Hint, "This approval is stale") || !strings.Contains(dispatchRevisionPM.Hint, "The PM must propose the decision again") {
		t.Fatalf("dispatch revision_changed pm_turn hint=%q", dispatchRevisionPM.Hint)
	}

	reconcileRevision := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"read-back refused","details":{"reason":"revision_changed","origin_kind":"pm_turn"}}}`))
	EnrichForCommand(reconcileRevision, "pm.actions.reconcile")
	if !strings.Contains(reconcileRevision.Hint, "This read-back is stale") || !strings.Contains(reconcileRevision.Hint, "The PM must propose the decision again") || strings.Contains(strings.ToLower(reconcileRevision.Hint), "approval") {
		t.Fatalf("reconcile revision_changed hint=%q", reconcileRevision.Hint)
	}

	dispatchTarget := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"already at target","details":{"reason":"already_at_target","origin_kind":"pm_turn"}}}`))
	EnrichForCommand(dispatchTarget, "pm.decisions.dispatch")
	if dispatchTarget.Hint != "The task is already where this proposal asks, so there is nothing to deliver." {
		t.Fatalf("dispatch already_at_target hint=%q", dispatchTarget.Hint)
	}

	reconcileTarget := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"already at target","details":{"reason":"already_at_target"}}}`))
	EnrichForCommand(reconcileTarget, "pm.actions.reconcile")
	if reconcileTarget.Hint != "The task is already where this proposal asks, so there is nothing to read back." {
		t.Fatalf("reconcile already_at_target hint=%q", reconcileTarget.Hint)
	}

	dispatchNull := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":null}}`))
	EnrichForCommand(dispatchNull, "pm.decisions.dispatch")
	if !strings.Contains(dispatchNull.Hint, "This approval is stale") || !strings.Contains(dispatchNull.Hint, "Propose the decision again") || strings.Contains(dispatchNull.Hint, "The PM must propose") {
		t.Fatalf("dispatch null details hint=%q", dispatchNull.Hint)
	}

	reconcileNull := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed","details":null}}`))
	EnrichForCommand(reconcileNull, "pm.actions.reconcile")
	if !strings.Contains(reconcileNull.Hint, "This read-back is stale") || !strings.Contains(reconcileNull.Hint, "Propose the decision again") {
		t.Fatalf("reconcile null details hint=%q", reconcileNull.Hint)
	}
	if strings.Contains(reconcileNull.Hint, "The PM must propose") || strings.Contains(strings.ToLower(reconcileNull.Hint), "approval") {
		t.Fatalf("reconcile null details asserted proposer or used approval language: %q", reconcileNull.Hint)
	}

	dispatchOmitted := FromHTTPFailure(409, []byte(`{"error":{"code":"source_revision_changed","message":"approved source revision has changed"}}`))
	EnrichForCommand(dispatchOmitted, "pm.decisions.dispatch")
	if !strings.Contains(dispatchOmitted.Hint, "Propose the decision again") || strings.Contains(dispatchOmitted.Hint, "The PM must propose") {
		t.Fatalf("dispatch omitted details hint=%q", dispatchOmitted.Hint)
	}
}

func TestEnrichForCommandPMReconcileNothingDelivered(t *testing.T) {
	t.Parallel()
	err := FromHTTPFailure(400, []byte(`{"error":{"code":"invalid_request","message":"invalid PM request: Nothing has been delivered yet, so there is nothing to read back"}}`))
	EnrichForCommand(err, "pm.actions.reconcile")
	if !strings.Contains(err.Hint, "already acknowledged") || strings.Contains(err.Hint, "deliver first or acknowledge") {
		t.Fatalf("hint=%q", err.Hint)
	}
}
