//go:build integration

package integration

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestPublishedReportIsOverviewDashboard(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "report-publisher")
	topicID := createReportTopic(t, h, "overview-"+runToken())
	schema := h.runCLIExpectOK(t, "report-publisher", nil, "report", "schema")
	var report map[string]any
	raw, _ := json.Marshal(schema.Payload["result"].(map[string]any)["example"])
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"explanation", "live-initiatives"} {
		t.Run(kind, func(t *testing.T) {
			report["title"] = "Published Overview " + kind
			if kind == "live-initiatives" {
				panel := report["panels"].([]any)[0].(map[string]any)
				panel["type"], panel["data"] = kind, map[string]any{"limit": 7}
			}
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "dashboard.json")
			writeReportFixture(t, file, raw)
			published := runReportPublish(t, h, "report-publisher", file, topicID)
			reportAction(t, published)
			ref := mustStringPath(t, published.Payload, "result.doc_ref")
			overview := h.runCLIExpectOK(t, "report-publisher", nil, "overview")
			reports := firstSlicePath(t, overview.Payload, "result.dashboard.reports", "result.body.dashboard.reports")
			if len(reports) != 1 || reports[0].(map[string]any)["ref"] != ref {
				t.Fatalf("published report missing from newest dashboard: %s", overview.Stdout)
			}
			selected := reports[0].(map[string]any)["report"].(map[string]any)
			if selected["title"] != report["title"] {
				t.Fatalf("Overview changed published report: %v", selected)
			}
			h.runCLIExpectOK(t, "report-publisher", nil, "workspace", "dashboard", "set", ref)
			pinned := h.runCLIExpectOK(t, "report-publisher", nil, "overview")
			if firstStringPath(t, pinned.Payload, "result.dashboard.pinned_ref", "result.body.dashboard.pinned_ref") != ref {
				t.Fatalf("published report pin missing: %s", pinned.Stdout)
			}
			if kind == "live-initiatives" {
				h.runCLIExpectOK(t, "report-publisher", nil, "report", "render", ref)
			}
			h.runCLIExpectOK(t, "report-publisher", nil, "workspace", "dashboard", "set", "none")
		})
	}
}
