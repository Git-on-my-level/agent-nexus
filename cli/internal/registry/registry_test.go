package registry

import "testing"

func TestEmbeddedRegistryIsConsistent(t *testing.T) {
	t.Parallel()

	meta, err := LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded registry: %v", err)
	}
	if meta.CommandCount == 0 {
		t.Fatal("expected non-empty command registry")
	}
	specs := CommandSpecs()
	if len(specs) != meta.CommandCount {
		t.Fatalf("command count mismatch between generated go registry and embedded json: go=%d json=%d", len(specs), meta.CommandCount)
	}

	helpMeta, err := LoadEmbeddedHelp()
	if err != nil {
		t.Fatalf("load help metadata: %v", err)
	}
	if helpMeta.CommandCount != meta.CommandCount {
		t.Fatalf("help command count mismatch: help=%d meta=%d", helpMeta.CommandCount, meta.CommandCount)
	}
	if helpMeta.GroupCount == 0 {
		t.Fatal("expected non-empty help groups")
	}

	conceptsMeta, err := LoadEmbeddedConcepts()
	if err != nil {
		t.Fatalf("load embedded concepts metadata: %v", err)
	}
	if conceptsMeta.ConceptCount == 0 {
		t.Fatal("expected non-empty concepts metadata")
	}
}

func TestEmbeddedEventRefRules(t *testing.T) {
	t.Parallel()

	rules, err := LoadEmbeddedEventRefRules()
	if err != nil {
		t.Fatalf("load embedded event ref rules: %v", err)
	}
	if rules.RuleCount == 0 {
		t.Fatal("expected non-empty event ref rules")
	}

	commitmentStatusChanged, ok := rules.RuleForEventType("commitment_status_changed")
	if !ok {
		t.Fatal("expected commitment_status_changed rule to be loaded")
	}
	if commitmentStatusChanged.ThreadID != "required" {
		t.Fatalf("unexpected thread_id for commitment_status_changed: %q", commitmentStatusChanged.ThreadID)
	}
	if len(commitmentStatusChanged.ConditionalRefs) != 2 {
		t.Fatalf("expected 2 conditional refs for commitment_status_changed, got %d", len(commitmentStatusChanged.ConditionalRefs))
	}

	_, ok = rules.RuleForEventType("unknown_event_type")
	if ok {
		t.Fatal("expected unknown event type to not have a rule")
	}
}
