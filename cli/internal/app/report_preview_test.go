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

func TestReportPreviewActivityUsesSubjectActionAndDetail(t *testing.T) {
	report := map[string]any{"title": "Changes", "panels": []any{map[string]any{"id": "movement", "type": "live-activity", "title": "What changed", "data": map[string]any{"limit": 15, "min_impact": "high"}}}}
	observations := []any{map[string]any{"id": "movement", "status": "ok", "observed_at": "2026-10-16T12:00:00Z", "data": map[string]any{"items": []any{map[string]any{"actor_label": "Alex", "actor_id": "actor-123", "verb": "moved", "subject": map[string]any{"ref": "card:launch", "title": "Launch search", "kind": "initiative"}, "detail": "Ready → In progress", "impact": "high", "ts": "2026-10-16T11:55:00Z"}, map[string]any{"summary": "Legacy activity"}}}}}
	text := reportPreviewText(report, "changes.png", true, "", summarizeReportPanels(report, observations))
	for _, want := range []string{"Alex moved Launch search [card:launch]", "Ready → In progress", "high", "Legacy activity"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %s", want, text)
		}
	}
	if strings.Contains(text, "actor-123") {
		t.Fatalf("display name replaced by ID: %s", text)
	}
}
