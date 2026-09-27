package output

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
				Result: map[string]any{
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
					Code:      "invalid_request",
					Message:   "path is required",
					Retryable: true,
					ExitCode:  2,
					Details:   map[string]any{"flag": "--path"},
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

func TestCardsListTextProjection(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	doc := Envelope{OK: true, Command: "cards list", Result: map[string]any{"cards": []any{map[string]any{"ref": "card:launch", "title": "Launch plan", "assignee_refs": []any{"actor:alice"}, "thread_id": "thread-1", "rank": "a"}}}}
	if err := WriteEnvelopeText(&buf, doc); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, `card ref=card:launch title="Launch plan" assignees=actor:alice`) {
		t.Fatalf("missing card row: %s", got)
	}
	if strings.Contains(got, "thread=") || strings.Contains(got, "rank=") || strings.Contains(got, "::") {
		t.Fatalf("old card fields: %s", got)
	}
}
