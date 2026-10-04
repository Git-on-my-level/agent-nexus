package visualreport

import (
	"encoding/json"
	"os"
	"strings"
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
		GoValid           *bool `json:"go_valid"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			result := Validate([]byte(tc.Content))
			expectedValid := tc.Valid
			if tc.GoValid != nil {
				expectedValid = *tc.GoValid
			}
			if result.Recognized != tc.Recognized || result.Valid != expectedValid {
				t.Fatalf("recognized=%v valid=%v, expected %v/%v: %v", result.Recognized, result.Valid, tc.Recognized, expectedValid, result.Errors)
			}
			if result.Valid {
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

func TestURLHostAllowlist(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{name: "LDH and one trailing dot", url: "https://a-b.example./", ok: true},
		{name: "round-tripping A-label", url: "https://xn--bcher-kva.example/", ok: true},
		{name: "canonical IPv4", url: "https://192.0.2.1/", ok: true},
		{name: "bracketed IPv6", url: "https://[2001:db8::1]/", ok: true},
		{name: "invalid A-label", url: "https://xn--/", ok: false},
		{name: "empty label", url: "https://example..com/", ok: false},
		{name: "leading hyphen", url: "https://-bad.example/", ok: false},
		{name: "trailing hyphen", url: "https://bad-.example/", ok: false},
		{name: "hexadecimal numeric suffix", url: "https://example.0xFFFFFFFFFFFFFFFFF./", ok: false},
		{name: "octal numeric suffix", url: "https://example.017/", ok: false},
		{name: "non-canonical IPv4", url: "https://192.168.001.1/", ok: false},
		{name: "internationalized host", url: "https://bücher.example/", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeURL(tc.url); got != tc.ok {
				t.Fatalf("safeURL(%q) = %v, want %v", tc.url, got, tc.ok)
			}
		})
	}
}

func TestURLRejectionExplainsPunycode(t *testing.T) {
	v := validator{}
	v.url("https://bücher.example/", "source.url")
	if len(v.errors) != 1 || !strings.Contains(v.errors[0], "punycode") {
		t.Fatalf("error should tell agents to use punycode: %v", v.errors)
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
