//go:build integration

package integration

import (
	"reflect"
	"testing"
)

func TestGenericSessionParticipationAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "participant")
	created := h.runCLIExpectOK(t, "participant", map[string]any{"title": "Generic session fixture"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	before := h.runCLIExpectOK(t, "participant", nil, "work", "get", ref)
	registration := map[string]any{
		"provider": "unlisted-provider", "native_session_id": "synthetic-conversation",
		"capabilities": map[string]any{"resume": "unknown", "history": "unsupported", "logs": "unknown"},
		"activity":     "active", "sequence": 1,
	}
	registered := h.runCLIExpectOK(t, "participant", registration, "sessions", "register", "--from-file", "-")
	id := mustStringPath(t, registered.Payload, "result.session.session_id")
	replay := h.runCLIExpectOK(t, "participant", registration, "sessions", "register", "--from-file", "-")
	if got := mustStringPath(t, replay.Payload, "result.session.session_id"); got != id {
		t.Fatalf("registration did not converge: %s", replay.Stdout)
	}
	if got := mustStringPath(t, replay.Payload, "result.session.last_seen_at"); got != mustStringPath(t, registered.Payload, "result.session.last_seen_at") {
		t.Fatal("retry renewed session activity")
	}
	h.runCLIExpectOK(t, "participant", nil, "sessions", "get", id)
	participation := map[string]any{"session_id": id, "activity": "active", "sequence": 1}
	joined := h.runCLIExpectOK(t, "participant", participation, "work", "participants", "register", ref, "--from-file", "-")
	participantID := mustStringPath(t, joined.Payload, "result.participant.participant_id")
	joinedAgain := h.runCLIExpectOK(t, "participant", participation, "work", "participants", "register", ref, "--from-file", "-")
	if got := mustStringPath(t, joinedAgain.Payload, "result.participant.participant_id"); got != participantID {
		t.Fatal("participation did not converge")
	}
	listed := h.runCLIExpectOK(t, "participant", nil, "work", "participants", "list", ref, "--limit", "1")
	firstParticipant := func(result cliResult) map[string]any {
		t.Helper()
		raw, ok := getPathValue(result.Payload, "result.participants")
		rows, isList := raw.([]any)
		if !ok || !isList || len(rows) != 1 {
			t.Fatalf("expected one participant: %s", result.Stdout)
		}
		row, ok := rows[0].(map[string]any)
		if !ok {
			t.Fatalf("invalid participant: %s", result.Stdout)
		}
		return row
	}
	if got := firstParticipant(listed)["participant_id"]; got != participantID {
		t.Fatalf("participant missing: %s", listed.Stdout)
	}
	registration["activity"], registration["sequence"] = "closed", 2
	h.runCLIExpectOK(t, "participant", registration, "sessions", "register", "--from-file", "-")
	listed = h.runCLIExpectOK(t, "participant", nil, "work", "participants", "list", ref)
	if got := firstParticipant(listed)["activity"]; got != "closed" {
		t.Fatalf("closed session appeared active: %s", listed.Stdout)
	}
	after := h.runCLIExpectOK(t, "participant", nil, "work", "get", ref)
	beforeWork, _ := getPathValue(before.Payload, "result.work")
	afterWork, _ := getPathValue(after.Payload, "result.work")
	if !reflect.DeepEqual(beforeWork, afterWork) {
		t.Fatalf("participation changed task: before=%s after=%s", before.Stdout, after.Stdout)
	}
}
