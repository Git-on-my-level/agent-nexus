package visualreport

import (
	"strings"
	"testing"
)

func TestWatchQueryValidation(t *testing.T) {
	for kind, raw := range map[string]string{"live-collection": `{"collection":"loops","status":["failing","flat"],"group_by":"project","limit":48}`, "live-activity": `{"subject_refs":["card:ship"],"collection":"initiatives"}`} {
		if _, err := ParseQuery(kind, []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"collection":"sql"}`, `{"collection":"loops","status":"failing"}`, `{"collection":"loops","status":["on_track"]}`, `{"collection":"agents","status":["working","working"]}`, `{"collection":"loops","limit":49}`, `{"collection":"loops","group_by":"board"}`} {
		if _, err := ParseQuery("live-collection", []byte(raw)); err == nil {
			t.Fatal(raw)
		}
	}
	for kind, want := range map[string]string{"live-collection": "primary", "live-loops": "primary", "chart": "primary", "live-activity": "secondary", "explanation": "context"} {
		if PanelRole(kind, "") != want || PanelRole(kind, "context") != "context" {
			t.Fatal(kind)
		}
	}
}

func TestWatchPanelQueryAliasAndRole(t *testing.T) {
	content := `{"kind":"anx.visual-report","schema_version":1,"title":"Watch","summary":"Work","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"work","title":"Work","summary":"Work","outcome":"Ship"}],"sources":[],"panels":[{"id":"loops","project_id":"work","type":"live-collection","role":"secondary","title":"Loops","author":"ANX","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"query":{"collection":"loops","status":["failing"]}}]}`
	panels, err := ParseAll(content)
	if err != nil || len(panels) != 1 {
		t.Fatal(panels, err)
	}
	if panels[0].Role != "secondary" || panels[0].Query.Collection != "loops" || len(panels[0].Query.StatusKeys) != 1 {
		t.Fatal(panels)
	}
	result := Validate([]byte(content))
	root := result.Report.(map[string]any)
	panel := root["panels"].([]any)[0].(map[string]any)
	if _, injected := panel["data"]; injected {
		t.Fatal("validation changed the authored report")
	}
	invalid := strings.Replace(content, `"role":"secondary"`, `"role":"invalid"`, 1)
	if Validate([]byte(invalid)).Valid {
		t.Fatal("invalid panel role accepted")
	}
}
