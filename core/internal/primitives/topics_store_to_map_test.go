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
		Title:          sqlNullString("T"),
		Summary:        sql.NullString{},
		ExtensionsJSON: "null",
		ProvenanceJSON: "{}",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap(emptyTopicRefBuckets())
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
		Title:          sqlNullString("Still Listed"),
		Summary:        sql.NullString{},
		ExtensionsJSON: `not-json`,
		ProvenanceJSON: "{}",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap(emptyTopicRefBuckets())
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
		Title:          sqlNullString("T"),
		Summary:        sqlNullString("s"),
		ExtensionsJSON: `{}`,
		ProvenanceJSON: "null",
		CreatedAt:      "2026-01-01T00:00:00Z",
		CreatedBy:      "actor-1",
		UpdatedAt:      "2026-01-01T00:00:00Z",
		UpdatedBy:      "actor-1",
	}
	out, err := row.toMap(emptyTopicRefBuckets())
	if err != nil {
		t.Fatalf("toMap: %v", err)
	}
	if _, ok := out["provenance"].(map[string]any); !ok {
		t.Fatalf("provenance: got %#v", out["provenance"])
	}
	if out["summary"] != "s" {
		t.Fatalf("summary: got %#v", out["summary"])
	}
}

func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}
