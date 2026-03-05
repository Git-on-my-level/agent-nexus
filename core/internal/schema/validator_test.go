package schema

import "testing"

func TestValidateEnumRejectsUnknownStrictValue(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateEnum(contract, "thread_status", "not_a_real_status")
	if err == nil {
		t.Fatal("expected strict enum error")
	}
}

func TestValidateEnumAcceptsUnknownOpenValue(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	if err := ValidateEnum(contract, "event_type", "my_custom_event"); err != nil {
		t.Fatalf("expected unknown open enum value to pass: %v", err)
	}
	if err := ValidateEnum(contract, "artifact_kind", "my_custom_artifact"); err != nil {
		t.Fatalf("expected unknown open enum value to pass: %v", err)
	}
}

func TestValidateTypedRefRejectsMissingColon(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateTypedRef(contract, "artifact123")
	if err == nil {
		t.Fatal("expected invalid typed ref error")
	}
}

func TestValidateTypedRefAllowsUnknownPrefix(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	if err := ValidateTypedRef(contract, "customprefix:abc"); err != nil {
		t.Fatalf("expected unknown prefix to be accepted: %v", err)
	}
}

func TestValidateProvenanceRejectsMissingSources(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateProvenance(contract, map[string]any{
		"notes": "missing required sources",
	})
	if err == nil {
		t.Fatal("expected provenance.sources required error")
	}
}

func TestValidateProvenanceAcceptsValidShape(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateProvenance(contract, map[string]any{
		"sources": []any{"receipt:artifact-1", "inferred"},
		"notes":   "validated",
		"by_field": map[string]any{
			"status": []any{"decision:event-1"},
		},
	})
	if err != nil {
		t.Fatalf("expected valid provenance to pass: %v", err)
	}
}
