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
	if !rules.EventTypeOpenEnum {
		t.Fatal("expected event_type enum policy to be open in generated metadata")
	}

	cardMoved, ok := rules.RuleForEventType("card_moved")
	if !ok {
		t.Fatal("expected card_moved rule to be loaded")
	}
	if len(cardMoved.RefsMustInclude) != 2 {
		t.Fatalf("expected 2 required refs for card_moved, got %d", len(cardMoved.RefsMustInclude))
	}
	if len(cardMoved.PayloadMustInclude) != 1 || cardMoved.PayloadMustInclude[0] != "column_key" {
		t.Fatalf("unexpected payload rules for card_moved: %#v", cardMoved)
	}

	topicCreated, ok := rules.RuleForEventType("topic_created")
	if !ok {
		t.Fatal("expected topic_created rule to be loaded")
	}
	if len(topicCreated.RefsMustInclude) != 1 || topicCreated.RefsMustInclude[0] != "topic:<topic_id>" {
		t.Fatalf("unexpected refs for topic_created: %#v", topicCreated)
	}

	_, ok = rules.RuleForEventType("unknown_event_type")
	if ok {
		t.Fatal("expected unknown event type to not have a rule")
	}
}
