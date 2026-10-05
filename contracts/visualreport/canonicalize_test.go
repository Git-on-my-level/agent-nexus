package visualreport

import "testing"

func TestCanonicalizeRecognizesKindVariants(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "uppercase kind", content: `{"kind":"ANX.VISUAL-REPORT","schema_version":1}`, want: true},
		{name: "padded kind", content: `{"kind":" anx.visual-report ","schema_version":1}`, want: true},
		{name: "front matter", content: "---\nkind: anx.visual-report\n---\n{\"kind\":\"anx.visual-report\",\"schema_version\":1}\n", want: true},
		{name: "plain notes", content: "plain notes", want: false},
		{name: "other json", content: `{"kind":"note","title":"Hello"}`, want: false},
		{name: "other front matter", content: "---\nkind: note\n---\nhello\n", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := Validate(Canonicalize([]byte(tc.content)))
			if result.Recognized != tc.want {
				t.Fatalf("recognized=%v errors=%v", result.Recognized, result.Errors)
			}
			if tc.want && result.Valid {
				t.Fatal("incomplete report was accepted")
			}
			if !tc.want && len(result.Errors) != 0 {
				t.Fatalf("non-report gained errors: %v", result.Errors)
			}
		})
	}
}
