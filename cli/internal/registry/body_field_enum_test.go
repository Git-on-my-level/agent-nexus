package registry

import (
	"slices"
	"testing"
)

func TestBodyFieldEnumTopicsPatchType(t *testing.T) {
	t.Parallel()

	vals, ok := BodyFieldEnum("topics patch", "patch.type")
	if !ok || len(vals) == 0 {
		t.Fatalf("expected patch.type enums, got ok=%v vals=%#v", ok, vals)
	}
	if !slices.Contains(vals, "incident") || !slices.Contains(vals, "initiative") {
		t.Fatalf("unexpected values: %#v", vals)
	}
}

func TestBodyFieldEnumCardsPatchRisk(t *testing.T) {
	t.Parallel()

	vals, ok := BodyFieldEnum("cards patch", "patch.risk")
	if !ok {
		t.Fatal("expected patch.risk enums")
	}
	if !slices.Contains(vals, "medium") {
		t.Fatalf("unexpected values: %#v", vals)
	}
}
