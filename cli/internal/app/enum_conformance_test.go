package app

import (
	"testing"
	"time"

	contractenums "agent-nexus-contracts-go-client/enums"
)

// TestContractEnumConformance checks the CLI's input edges against the
// generated schema enums. scripts/check-enum-conformance.mjs runs it beside the
// core and web-ui checks so the three cannot drift apart.
func TestContractEnumConformance(t *testing.T) {
	for _, outcome := range contractenums.HumanAttentionResponseOutcomeValues {
		if errs := validateDraftInboxRespond(map[string]any{"inbox_item_id": "inbox:1", "response_text": "ok", "outcome": outcome}); len(errs) != 0 {
			t.Errorf("ask outcome %q rejected: %v", outcome, errs)
		}
	}
	for _, column := range contractenums.BoardColumnKeyValues {
		if _, err := parsePMProposal([]string{"card:launch", "--status", column, "--why", "moved", "--evidence", "event:proof"}); err != nil {
			t.Errorf("pm propose --status %q rejected: %v", column, err)
		}
	}
	if _, err := parsePMProposal([]string{"card:launch", "--status", "doing", "--why", "moved"}); err == nil {
		t.Error("pm propose accepted a column outside the contract")
	}
	priorities := map[string]string{}
	for _, value := range contractenums.PriorityValues {
		priorities[value] = value
	}
	for alias, canonical := range contractenums.PriorityAliases {
		priorities[alias] = canonical
	}
	for in, want := range priorities {
		got, err := parseAskDecisionFlags(trackedString{}, nil, trackedString{value: in, set: true}, time.Now())
		if err != nil || got.priority != want {
			t.Errorf("ask --priority %q = %q, %v; want %q", in, got.priority, err, want)
		}
	}
}
