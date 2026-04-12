package registry

import (
	"slices"
	"testing"
)

func TestBodyFieldEnumBoardsPatchStatus(t *testing.T) {
	t.Parallel()

	vals, ok := BodyFieldEnum("boards patch", "patch.status")
	if !ok || len(vals) == 0 {
		t.Fatalf("expected patch.status enums, got ok=%v vals=%#v", ok, vals)
	}
	if !slices.Contains(vals, "active") || !slices.Contains(vals, "paused") {
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
