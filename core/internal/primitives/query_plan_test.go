package primitives

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"organization-autorunner-core/internal/storage"
)

func TestWorkspaceListQueriesUseIndexedPlans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"id":               "thread-plan-1",
		"title":            "Plan thread",
		"type":             "initiative",
		"status":           "active",
		"priority":         "p1",
		"tags":             []string{"ops", "backend"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-20T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []string{"step-1"},
		"key_artifacts":    []string{},
		"provenance":       map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Snapshot["id"].(string)

	if _, err := store.CreateCommitment(ctx, "actor-1", map[string]any{
		"id":                 "commitment-plan-1",
		"thread_id":          threadID,
		"title":              "Indexed commitment",
		"owner":              "actor-1",
		"due_at":             "2026-03-18T00:00:00Z",
		"status":             "open",
		"definition_of_done": []string{"done"},
		"links":              []string{"url:https://example.com"},
		"provenance":         map[string]any{"sources": []string{"inferred"}},
	}); err != nil {
		t.Fatalf("create commitment: %v", err)
	}

	if _, err := store.CreateArtifact(ctx, "actor-1", map[string]any{
		"id":   "artifact-plan-1",
		"kind": "work_order",
		"refs": []string{"thread:" + threadID},
	}, "artifact content", "text/plain"); err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	if _, _, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"id":        "doc-plan-1",
		"thread_id": threadID,
		"title":     "Plan doc",
		"status":    "active",
	}, "doc content", "text", []string{"thread:" + threadID}); err != nil {
		t.Fatalf("create document: %v", err)
	}

	threadQuery, threadArgs := buildListThreadsQuery(ThreadListFilter{Status: "active"})
	threadPlan := explainQueryPlan(t, workspace.DB(), threadQuery, threadArgs...)
	assertPlanUsesIndex(t, "threads", threadPlan, "idx_snapshots_kind_status_updated_at")

	commitmentQuery, commitmentArgs := buildListCommitmentsQuery(CommitmentListFilter{
		ThreadID:  threadID,
		Status:    "open",
		DueAfter:  "2026-03-01T00:00:00Z",
		DueBefore: "2026-03-31T00:00:00Z",
	})
	commitmentPlan := explainQueryPlan(t, workspace.DB(), commitmentQuery, commitmentArgs...)
	assertPlanUsesIndex(t, "commitments", commitmentPlan, "idx_snapshots_commitments_thread_status_due_updated_at")

	artifactQuery, artifactArgs := buildListArtifactsQuery(ArtifactListFilter{
		ThreadID: threadID,
		Kind:     "work_order",
	})
	artifactPlan := explainQueryPlan(t, workspace.DB(), artifactQuery, artifactArgs...)
	assertPlanUsesIndex(t, "artifacts", artifactPlan, "idx_artifacts_thread_kind_tombstoned_created_at")

	documentQuery, documentArgs := buildListDocumentsQuery(DocumentListFilter{ThreadID: threadID})
	documentPlan := explainQueryPlan(t, workspace.DB(), documentQuery, documentArgs...)
	assertPlanUsesIndex(t, "documents", documentPlan, "idx_documents_thread_tombstoned_updated_at")
}

func explainQueryPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain query plan for %q: %v", query, err)
	}
	defer rows.Close()

	details := make([]string, 0)
	for rows.Next() {
		var selectID int
		var order int
		var from int
		var detail string
		if err := rows.Scan(&selectID, &order, &from, &detail); err != nil {
			t.Fatalf("scan query plan row: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate query plan rows: %v", err)
	}

	plan := strings.Join(details, "\n")
	t.Logf("query plan:\n%s", plan)
	return plan
}

func assertPlanUsesIndex(t *testing.T, name string, plan string, indexName string) {
	t.Helper()
	if !strings.Contains(plan, indexName) {
		t.Fatalf("%s query plan did not use %s:\n%s", name, indexName, plan)
	}
}
