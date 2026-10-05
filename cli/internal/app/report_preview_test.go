package app

import (
	"strings"
	"testing"
)

func TestReportPreviewSandboxFallbackKeepsTextSummaryAndReason(t *testing.T) {
	reason, ok := reportRendererFallbackReason([]byte(`{"rendered":false,"reason":"sandbox_unavailable"}`))
	if !ok || reason != "sandbox_unavailable" {
		t.Fatalf("expected sandbox fallback reason, got reason=%q ok=%t", reason, ok)
	}

	text := reportPreviewText(
		map[string]any{"title": "Release report"},
		"release-report.png",
		false,
		reason,
		[]any{map[string]any{
			"title":     "Launch movement",
			"what":      "Recent workspace movement",
			"source":    "live-activity {}",
			"freshness": "ok as of 2026-10-05T00:00:00Z",
		}},
	)
	for _, expected := range []string{
		"Report preview: Release report",
		"PNG: unavailable (Chromium sandbox unavailable; text summary follows)",
		"Launch movement | what: Recent workspace movement",
		"source: live-activity {} | freshness: ok as of 2026-10-05T00:00:00Z",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("fallback text omitted %q: %s", expected, text)
		}
	}
}

func TestReportPreviewDoesNotTreatUnknownRendererOutputAsFallback(t *testing.T) {
	if reason, ok := reportRendererFallbackReason([]byte(`{"rendered":false,"reason":"unknown"}`)); ok || reason != "" {
		t.Fatalf("unknown renderer reason was accepted as fallback: reason=%q ok=%t", reason, ok)
	}
}
