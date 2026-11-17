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
}
