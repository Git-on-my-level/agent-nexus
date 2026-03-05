package server

import (
	"reflect"
	"testing"
)

func assertActorStatementProvenance(t *testing.T, event map[string]any) {
	t.Helper()

	eventID, _ := event["id"].(string)
	if eventID == "" {
		t.Fatalf("expected event id, got %#v", event["id"])
	}

	provenance, ok := event["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance object, got %#v", event["provenance"])
	}

	sources := extractProvenanceSources(t, provenance["sources"])
	want := []string{"actor_statement:" + eventID}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("unexpected actor statement provenance: got %#v want %#v", sources, want)
	}
}

func assertInferredProvenance(t *testing.T, event map[string]any) {
	t.Helper()

	provenance, ok := event["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance object, got %#v", event["provenance"])
	}

	sources := extractProvenanceSources(t, provenance["sources"])
	want := []string{"inferred"}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("unexpected inferred provenance: got %#v want %#v", sources, want)
	}
}

func extractProvenanceSources(t *testing.T, raw any) []string {
	t.Helper()

	switch values := raw.(type) {
	case []string:
		if len(values) == 0 {
			t.Fatalf("expected non-empty provenance sources")
		}
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("unexpected non-string provenance source item: %#v", value)
			}
			out = append(out, text)
		}
		if len(out) == 0 {
			t.Fatalf("expected non-empty provenance sources")
		}
		return out
	default:
		t.Fatalf("unexpected provenance sources type: %#v", raw)
		return nil
	}
}
