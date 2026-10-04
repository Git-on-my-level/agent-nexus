package visualreport

import (
	"encoding/json"
	"os"
	"testing"
)

// The CLI's import surface must stay on the shared implementation when #247's
// schema/validate/publish command layer rebases onto it.
func TestSharedReportCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/fixtures/visual-reports/reports.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Content     string
		Recognized, Valid bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		result := Validate([]byte(tc.Content))
		if result.Recognized != tc.Recognized || result.Valid != tc.Valid {
			t.Fatalf("%s: %v", tc.Name, result.Errors)
		}
	}
}
