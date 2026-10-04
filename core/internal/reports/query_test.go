package reports

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLiveQueryConformance(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/fixtures/live-report-queries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string          `json:"name"`
		Type  string          `json:"type"`
		Data  json.RawMessage `json:"data"`
		Valid bool            `json:"valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			_, err := ParseQuery(tc.Type, tc.Data)
			if (err == nil) != tc.Valid {
				t.Fatalf("valid=%v err=%v", tc.Valid, err)
			}
		})
	}
}

func TestChecklistSummary(t *testing.T) {
	first, progress, needs := Summary("# Shipping the launch\n\n- [x] Reviewed\n  - [X] Nested\n- [ ] Deliver\nNeeds David: pick the date\n```markdown\n- [x] Example\n```\nNeeds operator: approve")
	if first != "Shipping the launch" || progress.Done != 2 || progress.Total != 3 || len(needs) != 2 {
		t.Fatalf("%q %#v %#v", first, progress, needs)
	}
}

func TestReportEnvelopeAndStructuredContent(t *testing.T) {
	content := `{"kind":"anx.visual-report","schema_version":1,"panels":[{"id":"static","type":"explanation"},{"id":"live","type":"live-asks","data":{}}]}`
	var structured any
	_ = json.Unmarshal([]byte(content), &structured)
	for _, body := range []any{content, structured} {
		panels, err := Parse(body)
		if err != nil || len(panels) != 1 || panels[0].Query.Limit != 10 {
			t.Fatalf("%#v %v", panels, err)
		}
	}
	for _, bad := range []string{strings.Replace(content, `"schema_version":1`, `"schema_version":2`, 1), strings.Replace(content, `"id":"live"`, `"id":"static"`, 1), strings.Replace(content, `"data":{}`, `"data":{"fetch":"https://example.test"}`, 1)} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
