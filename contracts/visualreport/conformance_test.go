package visualreport

import (
	"encoding/json"
	"os"
	"testing"
)

func TestReportCorpus(t *testing.T) {
	raw, err := os.ReadFile("../fixtures/visual-reports/reports.json")
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
		t.Run(tc.Name, func(t *testing.T) {
			result := Validate([]byte(tc.Content))
			if result.Recognized != tc.Recognized || result.Valid != tc.Valid {
				t.Fatalf("recognized=%v valid=%v, expected %v/%v: %v", result.Recognized, result.Valid, tc.Recognized, tc.Valid, result.Errors)
			}
			if tc.Valid {
				if _, err := Parse(tc.Content); err != nil {
					t.Fatal(err)
				}
				var structured any
				_ = json.Unmarshal([]byte(tc.Content), &structured)
				if _, err := Parse(structured); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestSummaryCorpus(t *testing.T) {
	raw, err := os.ReadFile("../fixtures/visual-reports/summaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Markdown, Summary string
		Progress                Progress
		Needs                   []string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			summary, progress, needs := Summary(tc.Markdown)
			actual, _ := json.Marshal(needs)
			expected, _ := json.Marshal(tc.Needs)
			if summary != tc.Summary || progress != tc.Progress || string(actual) != string(expected) {
				t.Fatalf("%q %#v %#v", summary, progress, needs)
			}
		})
	}
}
