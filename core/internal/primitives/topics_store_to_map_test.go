package primitives

import (
	"database/sql"
	"testing"
)

func TestTopicRowToMapJSONNullBodyDoesNotPanic(t *testing.T) {
	t.Parallel()

	row := topicRow{
		ID:             "top-1",
		Type:           sqlNullString("incident"),
		Status:         sqlNullString("active"),
		Title:          sqlNullString("T"),
		BodyJSON:       "null",
		ProvenanceJSON: "{}",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap()
	if err != nil {
		t.Fatalf("toMap: %v", err)
	}
	if out["id"] != "top-1" {
		t.Fatalf("id: got %#v", out["id"])
	}
	if out["summary"] != "" {
		t.Fatalf("summary: got %#v", out["summary"])
	}
}

func TestTopicRowToMapMalformedBodyJSONDegrades(t *testing.T) {
	t.Parallel()

	row := topicRow{
		ID:             "top-bad-body",
		Type:           sqlNullString("incident"),
		Status:         sqlNullString("active"),
		Title:          sqlNullString("Still Listed"),
		BodyJSON:       `not-json`,
		ProvenanceJSON: "{}",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap()
	if err != nil {
		t.Fatalf("toMap: %v", err)
	}
	if out["title"] != "Still Listed" {
		t.Fatalf("title: got %#v", out["title"])
	}
	if out["summary"] != "" {
		t.Fatalf("summary: got %#v", out["summary"])
	}
}

func TestTopicRowToMapJSONNullProvenanceDoesNotPanic(t *testing.T) {
	t.Parallel()

	row := topicRow{
		ID:             "top-2",
		Type:           sqlNullString("incident"),
		Status:         sqlNullString("active"),
		Title:          sqlNullString("T"),
		BodyJSON:       `{"summary":"s"}`,
		ProvenanceJSON: "null",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap()
	if err != nil {
		t.Fatalf("toMap: %v", err)
	}
	if _, ok := out["provenance"].(map[string]any); !ok {
		t.Fatalf("provenance: got %#v", out["provenance"])
	}
}

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
