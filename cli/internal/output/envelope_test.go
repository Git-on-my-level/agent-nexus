package output

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteEnvelopeJSONGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope Envelope
		golden   string
	}{
		{
			name: "success",
			envelope: Envelope{
				OK:      true,
				Command: "version",
				Data: map[string]any{
					"cli_version": "dev",
					"base_url":    "http://127.0.0.1:8000",
				},
			},
			golden: "success.golden.json",
		},
		{
			name: "error",
			envelope: Envelope{
				OK:      false,
				Command: "api call",
				Error: &ErrorPayload{
					Code:        "invalid_request",
					Message:     "path is required",
					Recoverable: true,
					Hint:        "Run `anx help` for supported flags and usage.",
					Details:     map[string]any{"flag": "--path"},
				},
			},
			golden: "error.golden.json",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WriteEnvelopeJSON(&buf, tc.envelope); err != nil {
				t.Fatalf("write envelope: %v", err)
			}
			goldenPath := filepath.Join("testdata", tc.golden)
			expected, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden %s: %v", goldenPath, err)
			}
			if buf.String() != string(expected) {
				t.Fatalf("unexpected envelope output\n--- got ---\n%s\n--- want ---\n%s", buf.String(), string(expected))
			}
		})
	}
}
