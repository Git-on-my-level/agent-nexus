package main

import (
	"strings"
	"testing"
)

func TestValidateXAnxAuthoringFailsNewRequiredGap(t *testing.T) {
	doc := openAPIDocument{
		Paths: map[string]pathItem{
			"/widgets": {
				Get: &operation{
					CommandID:  "widgets.list",
					CLIPath:    "widgets list",
					Why:        "List widgets.",
					InputMode:  "none",
					Streaming:  map[string]any{"mode": "none"},
					Output:     "Returns widgets.",
					ErrorCodes: []string{"auth_required"},
					Concepts:   []string{"widgets"},
					Stability:  "beta",
					Surface:    "canonical",
				},
			},
		},
	}

	_, err := validateXAnxAuthoring(doc, xAnxValidationBaseline{})
	if err == nil {
		t.Fatal("expected missing x-anx-agent-notes to fail")
	}
	if !strings.Contains(err.Error(), "GET /widgets widgets.list (x-anx-agent-notes): missing required field") {
		t.Fatalf("error did not include actionable operation detail:\n%v", err)
	}
}

func TestValidateXAnxAuthoringAllowsBaselinedGapAndReportsExamples(t *testing.T) {
	doc := openAPIDocument{
		Paths: map[string]pathItem{
			"/widgets": {
				Get: &operation{
					CommandID:  "widgets.list",
					CLIPath:    "widgets list",
					Why:        "List widgets.",
					InputMode:  "none",
					Streaming:  map[string]any{"mode": "none"},
					Output:     "Returns widgets.",
					ErrorCodes: []string{"auth_required"},
					Concepts:   []string{"widgets"},
					Stability:  "beta",
					Surface:    "canonical",
				},
			},
		},
	}
	baseline := xAnxValidationBaseline{
		KnownMissing: map[string][]string{
			"x-anx-agent-notes": {"widgets.list"},
		},
	}

	report, err := validateXAnxAuthoring(doc, baseline)
	if err != nil {
		t.Fatalf("expected baselined gap to pass: %v", err)
	}
	if got := report.AllowedMissingCount["x-anx-agent-notes"]; got != 1 {
		t.Fatalf("allowed missing count = %d, want 1", got)
	}
	if len(report.MissingExamples) != 1 {
		t.Fatalf("missing examples = %d, want 1", len(report.MissingExamples))
	}
}

func TestValidateXAnxAuthoringFailsInvalidEnum(t *testing.T) {
	doc := openAPIDocument{
		Paths: map[string]pathItem{
			"/widgets": {
				Get: &operation{
					CommandID:  "widgets.list",
					CLIPath:    "widgets list",
					Why:        "List widgets.",
					InputMode:  "formish",
					Streaming:  map[string]any{"mode": "none"},
					Output:     "Returns widgets.",
					ErrorCodes: []string{"auth_required"},
					Concepts:   []string{"widgets"},
					Stability:  "beta",
					Surface:    "canonical",
					AgentNotes: "Safe to retry.",
				},
			},
		},
	}

	_, err := validateXAnxAuthoring(doc, xAnxValidationBaseline{})
	if err == nil {
		t.Fatal("expected invalid input mode to fail")
	}
	if !strings.Contains(err.Error(), `invalid value "formish"`) {
		t.Fatalf("error did not include invalid enum value:\n%v", err)
	}
}

func TestValidateXAnxAuthoringFailsScalarStreamingMetadata(t *testing.T) {
	doc := openAPIDocument{
		Paths: map[string]pathItem{
			"/widgets": {
				Get: &operation{
					CommandID:  "widgets.list",
					CLIPath:    "widgets list",
					Why:        "List widgets.",
					InputMode:  "none",
					Streaming:  "sse",
					Output:     "Returns widgets.",
					ErrorCodes: []string{"auth_required"},
					Concepts:   []string{"widgets"},
					Stability:  "beta",
					Surface:    "canonical",
					AgentNotes: "Safe to retry.",
				},
			},
		},
	}

	_, err := validateXAnxAuthoring(doc, xAnxValidationBaseline{})
	if err == nil {
		t.Fatal("expected scalar streaming metadata to fail")
	}
	if !strings.Contains(err.Error(), "GET /widgets widgets.list (x-anx-streaming): must be an object with a string mode") {
		t.Fatalf("error did not include streaming shape detail:\n%v", err)
	}
}

func TestValidateXAnxAuthoringFailsStreamingMetadataMissingMode(t *testing.T) {
	doc := openAPIDocument{
		Paths: map[string]pathItem{
			"/widgets": {
				Get: &operation{
					CommandID:  "widgets.list",
					CLIPath:    "widgets list",
					Why:        "List widgets.",
					InputMode:  "none",
					Streaming:  map[string]any{},
					Output:     "Returns widgets.",
					ErrorCodes: []string{"auth_required"},
					Concepts:   []string{"widgets"},
					Stability:  "beta",
					Surface:    "canonical",
					AgentNotes: "Safe to retry.",
				},
			},
		},
	}

	_, err := validateXAnxAuthoring(doc, xAnxValidationBaseline{})
	if err == nil {
		t.Fatal("expected streaming metadata without mode to fail")
	}
	if !strings.Contains(err.Error(), "GET /widgets widgets.list (x-anx-streaming.mode): missing required string value") {
		t.Fatalf("error did not include streaming mode detail:\n%v", err)
	}
}
