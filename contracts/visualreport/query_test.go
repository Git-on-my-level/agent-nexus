package visualreport

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLiveQueryConformance(t *testing.T) {
	raw, err := os.ReadFile("../fixtures/visual-reports/queries.json")
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

func TestFleetHealthQueryIsEmptyAndLive(t *testing.T) {
	if !IsLive("live-fleet-health") {
		t.Fatal("fleet health panel is not recognized as a live query")
	}
	if _, err := ParseQuery("live-fleet-health", []byte("{}")); err != nil {
		t.Fatalf("empty fleet health query rejected: %v", err)
	}
	if _, err := ParseQuery("live-fleet-health", []byte("{\"card_ref\":\"card:launch\"}")); err == nil {
		t.Fatal("fleet health query accepted an unsupported field")
	}
}

func TestChecklistSummary(t *testing.T) {
	first, progress, needs := Summary("# Shipping the launch\n\n- [x] Reviewed\n  - [X] Nested\n- [ ] Deliver\nNeeds Alex: pick the date\n```markdown\n- [x] Example\n```\nNeeds operator: approve")
	if first != "Shipping the launch" || progress.Done != 2 || progress.Total != 3 || len(needs) != 2 {
		t.Fatalf("%q %#v %#v", first, progress, needs)
	}
}
