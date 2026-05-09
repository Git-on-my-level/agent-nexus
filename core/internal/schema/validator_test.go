package schema

import (
	"strings"
	"testing"
)

func TestValidateEnumRejectsUnknownStrictValue(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateEnum(contract, "priority", "not_a_real_priority")
	if err == nil {
		t.Fatal("expected strict enum error")
	}
}

func TestValidateEnumRejectsUnknownEventAndArtifactKinds(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	if err := ValidateEnum(contract, "event_type", "my_custom_event"); err == nil {
		t.Fatal("expected unknown event type to be rejected")
	}
	if err := ValidateEnum(contract, "artifact_kind", "my_custom_artifact"); err == nil {
		t.Fatal("expected unknown artifact kind to be rejected")
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

func TestValidateTypedRefAllowsKnownContractPrefixes(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	for _, ref := range []string{
		"actor:actor-1",
		"artifact:artifact-1",
		"event:event-1",
		"thread:thread-1",
		"topic:topic-1",
		"document:document-1",
		"document_revision:document-revision-1",
		"board:board-1",
		"card:card-1",
		"card_revision:card-revision-1",
		"inbox:inbox-1",
		"url:https://example.test/item",
	} {
		if err := ValidateTypedRef(contract, ref); err != nil {
			t.Fatalf("expected known prefix ref %q to be accepted: %v", ref, err)
		}
	}
}

func TestValidateTypedRefRejectsUnknownContractPrefix(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	err := ValidateTypedRef(contract, "customprefix:abc")
	if err == nil {
		t.Fatal("expected unknown prefix to be rejected")
	}
	if !strings.Contains(err.Error(), "unknown prefix") {
		t.Fatalf("expected unknown prefix error, got %v", err)
	}
}

func TestValidateTypedRefAllowsUnknownPrefixWithoutContract(t *testing.T) {
	t.Parallel()

	if err := ValidateTypedRef(nil, "customprefix:abc"); err != nil {
		t.Fatalf("expected nil-contract validation to check shape only: %v", err)
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
		"sources": []any{"event:event-1", "inferred"},
		"notes":   "validated",
		"by_field": map[string]any{
			"status": []any{"event:event-1"},
		},
	})
	if err != nil {
		t.Fatalf("expected valid provenance to pass: %v", err)
	}
}
