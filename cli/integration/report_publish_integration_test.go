//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestReportPublishArchivedDocumentNeedsExplicitUnarchive(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "report-publisher")

	topicID := createReportTopic(t, h, "archived-"+runToken())
	reportPath := filepath.Join(t.TempDir(), "report.json")
	writeReportFixture(t, reportPath, reportFixture(t, "Archived lifecycle report", "The report is still visible after archive recovery."))

	first := runReportPublish(t, h, "report-publisher", reportPath, topicID)
	if got := reportAction(t, first); got != "created" {
		t.Fatalf("first publish action = %q, want created: %s", got, first.Stdout)
	}
	firstDocs := reportTopicDocuments(t, h, "report-publisher", topicID)
	if len(firstDocs) != 1 {
		t.Fatalf("first publish linked %d documents, want 1", len(firstDocs))
	}
	archivedDoc, _ := firstDocs[0].(map[string]any)
	archivedRef := mustStringPath(t, first.Payload, "result.doc_ref")
	if ref, ok := archivedDoc["ref"].(string); ok && ref != "" {
		archivedRef = ref
	}
	h.runCLIExpectOK(t, "report-publisher", nil, "docs", "archive", archivedRef)

	explicit := h.runCLI(t, "report-publisher", nil, "report", "publish", reportPath, "--topic", "topic:"+topicID, "--doc", archivedRef)
	if explicit.ExitCode == 0 || explicit.Payload["ok"] != false {
		t.Fatalf("explicit archived publish unexpectedly succeeded: %s", explicit.Stdout)
	}
	if got := mustStringPath(t, explicit.Payload, "error.code"); got != "archived_report_document" {
		t.Fatalf("explicit archived publish code = %q, want archived_report_document: %s", got, explicit.Stdout)
	}
	command := "anx docs unarchive " + archivedRef
	if !strings.Contains(mustStringPath(t, explicit.Payload, "error.message"), command) {
		t.Fatalf("archived-doc error omitted repair command %q: %s", command, explicit.Stdout)
	}
	actions, _ := getPathValue(explicit.Payload, "error.next_actions")
	actionRows, _ := actions.([]any)
	if len(actionRows) != 1 {
		t.Fatalf("expected one unarchive repair action: %s", explicit.Stdout)
	}
	actionRow, _ := actionRows[0].(map[string]any)
	argv, _ := actionRow["argv"].([]any)
	argvText := make([]string, 0, len(argv))
	for _, arg := range argv {
		argText, _ := arg.(string)
		argvText = append(argvText, argText)
	}
	if got := strings.Join(argvText, " "); got != command {
		t.Fatalf("repair next action = %q, want %q: %s", got, command, explicit.Stdout)
	}

	second := runReportPublish(t, h, "report-publisher", reportPath, topicID)
	if got := reportAction(t, second); got != "created" {
		t.Fatalf("automatic publish after archive action = %q, want created: %s", got, second.Stdout)
	}
	secondRef := mustStringPath(t, second.Payload, "result.doc_ref")
	if secondRef == archivedRef {
		t.Fatalf("automatic publish reused archived document %q", archivedRef)
	}

	activeDocs := reportTopicDocuments(t, h, "report-publisher", topicID)
	activeCount := 0
	archivedFound := false
	for _, raw := range activeDocs {
		doc, _ := raw.(map[string]any)
		ref, _ := doc["ref"].(string)
		state, _ := doc["state"].(string)
		if ref == archivedRef && state == "archived" {
			archivedFound = true
		}
		if state == "active" {
			activeCount++
			if ref != secondRef {
				t.Fatalf("unexpected active report ref %q, want %q", ref, secondRef)
			}
		}
	}
	if !archivedFound || activeCount != 1 {
		t.Fatalf("expected archived original plus one active replacement, found %d topic docs: %#v", activeCount, activeDocs)
	}
	retry := runReportPublish(t, h, "report-publisher", reportPath, topicID)
	if got := reportAction(t, retry); got != "revised" {
		t.Fatalf("retry after replacement action = %q, want revised: %s", got, retry.Stdout)
	}
	if got := mustStringPath(t, retry.Payload, "result.doc_ref"); got != secondRef {
		t.Fatalf("retry selected %q, want active replacement %q", got, secondRef)
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
