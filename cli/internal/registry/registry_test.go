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
		t.Fatalf("load embedded help metadata: %v", err)
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
