package registry

import (
	"slices"
	"testing"
)

func TestBodyFieldEnumTopicsPatchNoTopicTypeEnum(t *testing.T) {
	t.Parallel()

	_, ok := BodyFieldEnum("topics patch", "patch.type")
	if ok {
		t.Fatal("patch.type should not be a registered enum on topics patch")
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
