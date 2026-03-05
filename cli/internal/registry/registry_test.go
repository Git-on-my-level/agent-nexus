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
}
