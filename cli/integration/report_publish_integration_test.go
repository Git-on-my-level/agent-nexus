//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReportPublishUsesTopicRefsAndIsRetrySafe(t *testing.T) {
	h := newLiveCoreHarness(t)
	token := runToken()
	h.enrollHost(t, "report-publisher")

	firstTopicID := createReportTopic(t, h, "sequential-"+token)
	reportPath := filepath.Join(t.TempDir(), "report.json")
	firstBody := reportFixture(t, "Integration report "+token, "Initial report content")
	writeReportFixture(t, reportPath, firstBody)

	first := runReportPublish(t, h, "report-publisher", reportPath, firstTopicID)
	if got := reportAction(t, first); got != "created" {
		t.Fatalf("first publish action = %q, want created: %s", got, first.Stdout)
	}
	firstDocs := reportTopicDocuments(t, h, "report-publisher", firstTopicID)
	if len(firstDocs) != 1 {
		t.Fatalf("first publish linked %d documents, want 1: %s", len(firstDocs), first.Stdout)
	}
	firstDocID := mustStringPath(t, firstDocs[0].(map[string]any), "id")

	sequentialRetry := runReportPublish(t, h, "report-publisher", reportPath, firstTopicID)
	if got := reportAction(t, sequentialRetry); got != "revised" {
		t.Fatalf("sequential retry action = %q, want revised: %s", got, sequentialRetry.Stdout)
	}
	if docs := reportTopicDocuments(t, h, "report-publisher", firstTopicID); len(docs) != 1 {
		t.Fatalf("sequential retry linked %d documents, want 1", len(docs))
	}
	beforeRepublish := reportHistory(t, h, "report-publisher", firstDocID)

	concurrentTopicID := createReportTopic(t, h, "concurrent-"+token)
	concurrentPath := filepath.Join(t.TempDir(), "concurrent-report.json")
	writeReportFixture(t, concurrentPath, reportFixture(t, "Concurrent report "+token, "Shared retry content"))
	start := make(chan struct{})
	results := make(chan cliResult, 2)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- runReportPublish(t, h, "report-publisher", concurrentPath, concurrentTopicID)
		}()
	}
	close(start)
	for i := 0; i < cap(results); i++ {
		result := <-results
		if result.ExitCode != 0 || result.Payload["ok"] != true {
			t.Fatalf("concurrent publish failed (exit=%d): %s\n%s", result.ExitCode, result.Stdout, result.Stderr)
		}
	}
	concurrentDocs := reportTopicDocuments(t, h, "report-publisher", concurrentTopicID)
	if len(concurrentDocs) != 1 {
		t.Fatalf("concurrent retries linked %d documents, want exactly 1", len(concurrentDocs))
	}

	updatedBody := reportFixture(t, "Integration report "+token, "Updated report content")
	writeReportFixture(t, reportPath, updatedBody)
	republished := runReportPublish(t, h, "report-publisher", reportPath, firstTopicID)
	if got := reportAction(t, republished); got != "revised" {
		t.Fatalf("republish action = %q, want revised: %s", got, republished.Stdout)
	}
	afterRepublish := reportHistory(t, h, "report-publisher", firstDocID)
	if len(afterRepublish) != len(beforeRepublish)+1 {
		t.Fatalf("republish history changed from %d to %d revisions, want one new revision", len(beforeRepublish), len(afterRepublish))
	}
}

func reportHistory(t *testing.T, h *liveCoreHarness, agent, documentID string) []any {
	t.Helper()
	history := h.runCLIExpectOK(t, agent, nil, "docs", "history", documentID)
	return firstSlicePath(t, history.Payload, "result.revisions", "result.body.revisions")
}

func createReportTopic(t *testing.T, h *liveCoreHarness, suffix string) string {
	t.Helper()
	created := h.runCLIExpectOK(t, "report-publisher", map[string]any{
		"topic": map[string]any{
			"title":         "Report publish " + suffix,
			"summary":       "Topic for real-core report publishing integration coverage.",
			"owner_refs":    []any{},
			"document_refs": []any{},
			"board_refs":    []any{},
			"related_refs":  []any{},
			"provenance":    map[string]any{"sources": []any{"inferred"}},
		},
	}, "topics", "create")
	return mustStringPath(t, created.Payload, "result.topic.id")
}

func runReportPublish(t *testing.T, h *liveCoreHarness, agent, file, topicID string) cliResult {
	t.Helper()
	return h.runCLI(t, agent, nil, "report", "publish", file, "--topic", "topic:"+topicID)
}

func reportAction(t *testing.T, result cliResult) string {
	t.Helper()
	if result.ExitCode != 0 || result.Payload["ok"] != true {
		t.Fatalf("report publish failed (exit=%d): %s\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	return mustStringPath(t, result.Payload, "result.action")
}

func reportTopicDocuments(t *testing.T, h *liveCoreHarness, agent, topicID string) []any {
	t.Helper()
	workspace := h.runCLIExpectOK(t, agent, nil, "topics", "workspace", "--topic-id", topicID)
	if docs, ok := getPathValue(workspace.Payload, "result.documents"); ok {
		if rows, ok := docs.([]any); ok {
			return rows
		}
	}
	if docs, ok := getPathValue(workspace.Payload, "result.body.documents"); ok {
		if rows, ok := docs.([]any); ok {
			return rows
		}
	}
	t.Fatalf("topic workspace omitted documents: %s", workspace.Stdout)
	return nil
}

func reportFixture(t *testing.T, title, summary string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"kind":           "anx.visual-report",
		"schema_version": 1,
		"title":          title,
		"summary":        summary,
		"generated_at":   "2026-10-05T00:00:00Z",
		"projects": []any{map[string]any{
			"id": "project-a", "title": "Project A", "summary": "Evidence is bounded.", "outcome": "Qualification unknown",
		}},
		"sources": []any{},
		"panels": []any{map[string]any{
			"id": "finding", "project_id": "project-a", "type": "explanation", "title": "Finding",
			"author": "integration", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{},
			"data": map[string]any{"text": summary},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func writeReportFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
