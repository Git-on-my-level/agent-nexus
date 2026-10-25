package askpresentation

import (
	"strings"
	"testing"
)

func TestValidateAdditiveAskPresentation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload map[string]any
		invalid bool
	}{
		{"legacy", map[string]any{"body": "Old ask"}, false},
		{"complete", map[string]any{"situation": "Plain words", "recommendation": map[string]any{"action": "Approve", "why": "Saves time", "future": "kept"}, "alternatives": []any{map[string]any{"action": "Wait", "consequence": "Slower"}}}, false},
		{"type", map[string]any{"situation": 123}, true},
		{"null", map[string]any{"impact": nil}, true},
		{"partial", map[string]any{"recommendation": map[string]any{"action": "Approve"}}, true},
		{"blank", map[string]any{"recommendation": map[string]any{"action": " ", "why": "Useful"}}, true},
		{"long", map[string]any{"details": strings.Repeat("界", 32001)}, true},
		{"alternatives type", map[string]any{"alternatives": "Wait"}, true},
		{"consequence", map[string]any{"alternatives": []any{map[string]any{"action": "Wait"}}}, true},
		{"cap", map[string]any{"alternatives": make([]any, 6)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.payload)
			if (err != nil) != tc.invalid {
				t.Fatalf("%v", err)
			}
		})
	}
}
