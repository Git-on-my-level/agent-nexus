package server

import (
	"testing"

	contractenums "agent-nexus-contracts-go-client/enums"
)

// TestContractEnumConformance checks core's board column validators and
// priority naming against the generated schema enums.
// scripts/check-enum-conformance.mjs runs it beside the CLI and web-ui checks.
func TestContractEnumConformance(t *testing.T) {
	for _, column := range contractenums.BoardColumnKeyValues {
		if err := validateBoardCardCreateRequest("", "", "", column, "", "", "", "", nil); err != nil {
			t.Errorf("card create column %q rejected: %v", column, err)
		}
		if err := validateBoardPlacementRequest(column, "", "", nil); err != nil {
			t.Errorf("placement column %q rejected: %v", column, err)
		}
	}
	if validateBoardCardCreateRequest("", "", "", "doing", "", "", "", "", nil) == nil || validateBoardPlacementRequest("doing", "", "", nil) == nil {
		t.Error("a column outside the contract was accepted")
	}
	names := map[string]string{contractenums.PriorityP0: "critical", contractenums.PriorityP1: "high", contractenums.PriorityP2: "medium", contractenums.PriorityP3: "low"}
	if len(names) != len(contractenums.PriorityValues) {
		t.Fatalf("priority names cover %d values, contract has %d", len(names), len(contractenums.PriorityValues))
	}
	for alias, canonical := range contractenums.PriorityAliases {
		if got := briefPriorityName(alias); got != names[canonical] {
			t.Errorf("briefPriorityName(%q)=%q, want %q", alias, got, names[canonical])
		}
	}
	for canonical, want := range names {
		if got := briefPriorityName(canonical); got != want {
			t.Errorf("briefPriorityName(%q)=%q, want %q", canonical, got, want)
		}
	}
}
