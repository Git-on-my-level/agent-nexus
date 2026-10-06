package visualreport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func reviewFixture(t *testing.T, fields map[string]any) []byte {
	t.Helper()
	panel := map[string]any{"id": "note", "project_id": "workspace", "type": "explanation", "title": "Narrative", "author": "actor-author", "provenance": "reported", "observed_at": nil, "freshness": "unknown", "source_ids": []any{}, "data": map[string]any{"text": "Context"}}
	for key, value := range fields {
		panel[key] = value
	}
	root := map[string]any{"kind": Kind, "schema_version": Version, "title": "Dashboard", "summary": "Context", "generated_at": "2026-10-01T00:00:00Z", "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current", "outcome": "Ship"}}, "sources": []any{}, "panels": []any{panel}}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReviewMetadataLegacyAndWriteClock(t *testing.T) {
	raw := reviewFixture(t, nil)
	panels, err := ParseAll(string(raw))
	if err != nil || len(panels) != 1 || !panels[0].ReviewByDefaulted || panels[0].ReviewBy != "2026-10-08T00:00:00Z" {
		t.Fatalf("legacy: %#v %v", panels, err)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		valid bool
	}{{"7d", true}, {"2026-10-08", true}, {"2026-10-08T00:00:00Z", true}, {"2d", false}, {"2026-10-05", false}, {"2026-10-05T00:00:00Z", false}} {
		raw := reviewFixture(t, map[string]any{"authored_at": "2026-10-01T00:00:00Z", "review_by": tc.value})
		if !Validate(raw).Valid {
			t.Fatalf("expired stored report no longer readable: %s", tc.value)
		}
		if result := ValidateWrite(raw, now); result.Valid != tc.valid {
			t.Fatalf("write %s: %#v", tc.value, result)
		}
	}
	raw = reviewFixture(t, map[string]any{"authored_at": "2026-10-01T00:00:00Z"})
	if Validate(raw).Valid {
		t.Fatal("modern authored panel accepted missing review_by")
	}
	raw = reviewFixture(t, map[string]any{"review_by": nil})
	if Validate(raw).Valid {
		t.Fatal("null review deadline accepted")
	}
}

func TestAuthoredStatusWarningsAndLiveAlternatives(t *testing.T) {
	for _, tc := range []struct {
		kind        string
		data        any
		alternative string
	}{
		{"milestone-timeline", map[string]any{"items": []any{}}, "live-timeline"},
		{"evidence-table", map[string]any{"columns": []any{"State"}, "rows": []any{}}, "live-cards"},
		{"callout", map[string]any{"tone": "info", "text": "Current status: ready"}, "live-initiatives"},
	} {
		raw := reviewFixture(t, map[string]any{"type": tc.kind, "data": tc.data})
		if !Validate(raw).Valid {
			t.Fatalf("status warning invalidated %s", tc.kind)
		}
		warnings := Warnings(raw)
		if len(warnings) != 1 || !strings.Contains(warnings[0], tc.alternative) {
			t.Fatalf("%s: %#v", tc.kind, warnings)
		}
	}
	if warnings := Warnings(reviewFixture(t, nil)); len(warnings) != 0 {
		t.Fatalf("narrative warned: %#v", warnings)
	}
	raw := reviewFixture(t, map[string]any{"type": "evidence-table", "source": map[string]any{"series": "status"}, "data": map[string]any{}})
	if warnings := Warnings(raw); len(warnings) != 0 {
		t.Fatalf("series binding warned: %#v", warnings)
	}
}

func TestNewLivePanelsValidation(t *testing.T) {
	if _, err := ParseQuery("live-cards", []byte(`{"board_refs":["board:delivery"],"label":"release","role":"reviewer","status":"done","limit":2,"sort":"updated"}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"role":""}`, `{"limit":101}`, `{"status":null}`, `{"label":[]}`, `{"sort":"sql"}`, `{"board_refs":["board:a","board:a"]}`} {
		if _, err := ParseQuery("live-cards", []byte(raw)); err == nil {
			t.Fatalf("bad card query accepted: %s", raw)
		}
	}
	raw := reviewFixture(t, map[string]any{"type": "live-timeline", "source": map[string]any{"series": "releases", "range": "30d"}, "data": map[string]any{}})
	if result := Validate(raw); !result.Valid {
		t.Fatalf("timeline binding rejected: %#v", result.Errors)
	}
	if !IsLive("live-timeline") {
		t.Fatal("timeline is not live")
	}
	raw = reviewFixture(t, map[string]any{"type": "live-timeline", "data": map[string]any{}})
	if Validate(raw).Valid {
		t.Fatal("timeline accepted missing source")
	}
}
