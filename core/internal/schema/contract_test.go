package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExtractsCoreSchemaRules(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	if contract.Version != "0.2.2" {
		t.Fatalf("unexpected schema version: got %q", contract.Version)
	}

	threadStatus, ok := contract.Enums["thread_status"]
	if !ok {
		t.Fatal("thread_status enum was not loaded")
	}
	if threadStatus.Policy != EnumPolicyStrict {
		t.Fatalf("unexpected thread_status policy: got %q", threadStatus.Policy)
	}

	eventType, ok := contract.Enums["event_type"]
	if !ok {
		t.Fatal("event_type enum was not loaded")
	}
	if eventType.Policy != EnumPolicyOpen {
		t.Fatalf("unexpected event_type policy: got %q", eventType.Policy)
	}

	if !contract.HasKnownTypedRefPrefix("artifact") {
		t.Fatal("expected typed ref prefix artifact to be loaded")
	}
	if !contract.HasKnownTypedRefPrefix("url") {
		t.Fatal("expected typed ref prefix url to be loaded")
	}

	sources, ok := contract.Provenance.Fields["sources"]
	if !ok {
		t.Fatal("provenance.sources field was not loaded")
	}
	if !sources.Required {
		t.Fatal("expected provenance.sources to be required")
	}
	if sources.Type != "list<string>" {
		t.Fatalf("unexpected provenance.sources type: got %q", sources.Type)
	}

	receipt, ok := contract.Packets["receipt"]
	if !ok {
		t.Fatal("receipt packet schema was not loaded")
	}
	outputs, ok := receipt.Fields["outputs"]
	if !ok {
		t.Fatal("receipt.outputs field was not loaded")
	}
	if outputs.MinItems == nil || *outputs.MinItems != 1 {
		t.Fatalf("expected receipt.outputs min_items=1, got %#v", outputs.MinItems)
	}
	verification, ok := receipt.Fields["verification_evidence"]
	if !ok {
		t.Fatal("receipt.verification_evidence field was not loaded")
	}
	if verification.MinItems == nil || *verification.MinItems != 1 {
		t.Fatalf("expected receipt.verification_evidence min_items=1, got %#v", verification.MinItems)
	}
	artifactRefRule := contract.ArtifactRefRules["receipt"]
	if len(artifactRefRule) != 2 {
		t.Fatalf("expected 2 receipt artifact ref rules, got %#v", artifactRefRule)
	}
	receiptEventRule, ok := contract.EventRefRules["receipt_added"]
	if !ok {
		t.Fatal("receipt_added event ref rule was not loaded")
	}
	if len(receiptEventRule.RefsMustInclude) != 2 {
		t.Fatalf("expected receipt_added refs_must_include length=2, got %#v", receiptEventRule.RefsMustInclude)
	}

	threadSnapshot, ok := contract.Snapshots["thread"]
	if !ok {
		t.Fatal("thread snapshot schema was not loaded")
	}
	openCommitments, ok := threadSnapshot.Fields["open_commitments"]
	if !ok {
		t.Fatal("thread.open_commitments field was not loaded")
	}
	if !openCommitments.Required {
		t.Fatal("expected thread.open_commitments to be required")
	}
}

func TestLoadMissingVersion(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "schema.yaml")
	if err := os.WriteFile(path, []byte("enums: {}\n"), 0o644); err != nil {
		t.Fatalf("failed to write test schema: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing schema version")
	}
}

func loadContract(t *testing.T) *Contract {
	t.Helper()

	path := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	return contract
}
