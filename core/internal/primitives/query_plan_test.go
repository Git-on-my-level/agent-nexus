package primitives

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
)

func TestWorkspaceListQueriesUseIndexedPlans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

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
	threadID, _ := threadResult.Thread["id"].(string)

	if _, err := store.CreateArtifact(ctx, "actor-1", map[string]any{
		"id":   "artifact-plan-1",
		"kind": "receipt",
		"refs": []string{"thread:" + threadID},
	}, "artifact content", "text/plain"); err != nil {
		t.Fatalf("create artifact: %v", err)
	}

	if _, _, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"id":        "doc-plan-1",
		"thread_id": threadID,
		"title":     "Plan doc",
	}, "doc content", "text", []string{"thread:" + threadID}); err != nil {
		t.Fatalf("create document: %v", err)
	}

	threadQuery, threadArgs := buildListThreadsQuery(ThreadListFilter{Status: "active"})
	threadPlan := explainQueryPlan(t, workspace.DB(), threadQuery, threadArgs...)
	assertPlanUsesIndex(t, "threads", threadPlan, "idx_threads_status_updated_at")

	cardsQuery := `SELECT id FROM cards WHERE parent_thread_id = ? AND archived_at IS NULL`
	cardsPlan := explainQueryPlan(t, workspace.DB(), cardsQuery, threadID)
	assertPlanUsesIndex(t, "cards", cardsPlan, "idx_cards_parent_thread_id")

	artifactQuery, artifactArgs := buildListArtifactsQuery(ArtifactListFilter{
		ThreadID: threadID,
		Kind:     "receipt",
	})
	artifactPlan := explainQueryPlan(t, workspace.DB(), artifactQuery, artifactArgs...)
	assertPlanUsesIndex(t, "artifacts", artifactPlan, "idx_artifacts_thread_kind_trashed_created_at")

	documentQuery, documentArgs := buildListDocumentsQuery(DocumentListFilter{ThreadID: threadID})
	documentPlan := explainQueryPlan(t, workspace.DB(), documentQuery, documentArgs...)
	assertPlanUsesIndex(t, "documents", documentPlan, "idx_documents_thread_trashed_updated_at")
}

func TestBuildListThreadsQueryAddsWhereBeforeOptionalAndFilters(t *testing.T) {
	t.Parallel()

	stale := true
	query, args := buildListThreadsQuery(ThreadListFilter{
		IncludeArchived: true,
		IncludeTrashed:  true,
		Tag:             "ops",
		Cadences:        []string{"daily"},
		Query:           "plan",
		Stale:           &stale,
	})

	if !strings.Contains(query, "FROM threads LEFT JOIN derived_topic_views ON derived_topic_views.thread_id = threads.id WHERE EXISTS") {
		t.Fatalf("expected optional filters to start a WHERE clause, got query:\n%s", query)
	}
	if strings.Contains(query, "FROM threads LEFT JOIN derived_topic_views ON derived_topic_views.thread_id = threads.id AND") {
		t.Fatalf("query appended AND without WHERE:\n%s", query)
	}
	if len(args) != 5 {
		t.Fatalf("expected 5 query args, got %d (%#v)", len(args), args)
	}
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
