package app

import "testing"

func TestFindSecretByNameOrID(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"secrets": []any{
			map[string]any{"id": "sec_1", "name": "ALPHA"},
			map[string]any{"id": "sec_2", "name": "BETA"},
		},
	}
	if got := findSecretByNameOrID(body, "ALPHA"); got == nil || got["id"] != "sec_1" {
		t.Fatalf("by name: got %#v", got)
	}
	if got := findSecretByNameOrID(body, "sec_2"); got == nil || got["name"] != "BETA" {
		t.Fatalf("by id: got %#v", got)
	}
	if findSecretByNameOrID(body, "missing") != nil {
		t.Fatal("expected nil for missing secret")
	}
	if findSecretByNameOrID("not-a-map", "x") != nil {
		t.Fatal("expected nil for non-object body")
	}
}

func TestExtractSecretEnvPairs(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"secrets": []any{
			map[string]any{"name": "K1", "value": "v1"},
			map[string]any{"name": "", "value": "skip"},
		},
	}
	pairs := extractSecretEnvPairs(body)
	if len(pairs) != 1 || pairs["K1"] != "v1" {
		t.Fatalf("got %#v", pairs)
	}
	if extractSecretEnvPairs(map[string]any{}) != nil {
		t.Fatal("expected nil when secrets key missing")
	}
}

func TestFormatSecretListText(t *testing.T) {
	t.Parallel()

	out := formatSecretListText(map[string]any{
		"secrets": []any{
			map[string]any{"name": "N", "description": "d", "updated_at": "t"},
		},
	})
	if out == "" || out == "No secrets." {
		t.Fatalf("unexpected output: %q", out)
	}
}
