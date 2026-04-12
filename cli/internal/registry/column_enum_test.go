package registry

import "testing"

func TestColumnKeyEnumValuesNonEmpty(t *testing.T) {
	t.Parallel()

	vals := ColumnKeyEnumValues()
	if len(vals) == 0 {
		t.Fatal("expected non-empty column_key enum_values from embedded commands.json")
	}
	seen := map[string]bool{}
	for _, v := range vals {
		if seen[v] {
			t.Fatalf("duplicate value %q", v)
		}
		seen[v] = true
	}
}
