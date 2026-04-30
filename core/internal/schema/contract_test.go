package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadExtractsCoreSchemaRules(t *testing.T) {
	t.Parallel()

	contract := loadContract(t)

	if contract.Version != "0.6.0" {
		t.Fatalf("unexpected schema version: got %q", contract.Version)
	}

	if _, ok := contract.Enums["topic_type"]; ok {
		t.Fatal("topic_type enum should not be present")
	}

	eventType, ok := contract.Enums["event_type"]
	if !ok {
		t.Fatal("event_type enum was not loaded")
	}
	if eventType.Policy != EnumPolicyStrict {
		t.Fatalf("unexpected event_type policy: got %q", eventType.Policy)
	}
	if len(eventType.Groups["messages"]) == 0 {
		t.Fatal("expected event_type groups to be loaded")
	}

	if !contract.HasKnownTypedRefPrefix("artifact") {
		t.Fatal("expected typed ref prefix artifact to be loaded")
	}
	if !contract.HasKnownTypedRefPrefix("url") {
		t.Fatal("expected typed ref prefix url to be loaded")
	}
	if !contract.HasKnownTypedRefPrefix("board") {
		t.Fatal("expected typed ref prefix board to be loaded")
	}
	if !contract.HasKnownTypedRefPrefix("card_revision") {
		t.Fatal("expected typed ref prefix card_revision to be loaded")
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

	if _, ok := contract.EventRefRules["receipt_added"]; ok {
		t.Fatal("receipt_added event ref rule should not be loaded")
	}
	cardCreatedRule, ok := contract.EventRefRules["card_created"]
	if !ok {
		t.Fatal("card_created event ref rule was not loaded")
	}
	if len(cardCreatedRule.RefsMustInclude) != 4 {
		t.Fatalf("expected card_created refs_must_include length=4, got %#v", cardCreatedRule.RefsMustInclude)
	}
	boardUpdatedRule, ok := contract.EventRefRules["board_updated"]
	if !ok {
		t.Fatal("board_updated event ref rule was not loaded")
	}
	if len(boardUpdatedRule.RefsMustInclude) != 1 {
		t.Fatalf("expected board_updated refs_must_include length=1, got %#v", boardUpdatedRule.RefsMustInclude)
	}

	threadSchema, ok := contract.Threads["thread"]
	if !ok {
		t.Fatal("thread primitive schema was not loaded")
	}
	topicRef, ok := threadSchema.Fields["topic_ref"]
	if !ok {
		t.Fatal("thread.topic_ref field was not loaded")
	}
	if topicRef.Required {
		t.Fatal("expected thread.topic_ref to be optional")
	}

	title, ok := threadSchema.Fields["title"]
	if !ok {
		t.Fatal("thread.title field was not loaded")
	}
	if title.Required {
		t.Fatal("expected thread.title to be optional")
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

	path := filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml")
	contract, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	return contract
}
