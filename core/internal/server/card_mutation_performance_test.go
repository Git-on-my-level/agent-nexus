package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceCardMutationProjectionHistoryBudget(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	if err := perfguard.Seed(ctx, env.workspace.DB(), env.workspace.Layout().ArtifactContentDir, "scale-owner-actor", time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/card_mutation_projection_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budget struct {
		MaxQueries int    `json:"max_queries"`
		MaxRows    int    `json:"max_rows"`
		MaxVMSteps uint64 `json:"max_vm_steps"`
	}
	if err := json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := primitives.NewTestStore(db, "")
	for _, revisions := range []int{1, 1025} {
		if revisions > 1 {
			// Valid import rows share the existing artifact; no historical text
			// needs copying to expose accidental history materialization.
			_, err := env.workspace.DB().Exec(`WITH RECURSIVE n(v) AS (SELECT 2 UNION ALL SELECT v+1 FROM n WHERE v<1025)
				INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,thread_id,created_at,created_by)
				SELECT 'echo-history-'||v,'scale-card-1',v,'scale-artifact-1','scale-thread-1',?,'scale-owner-actor' FROM n`, perfguard.Timestamp)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, actor := range []string{"scale-owner-actor", "scale-stranger-actor"} {
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			req := httptest.NewRequest("POST", "/cards/scale-card-1/move", nil).WithContext(scope)
			cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: actor})
			opts := handlerOptions{primitiveStore: s}
			capture.Start()
			view, err := mutatedCardView(scope, req, opts, "scale-card-1")
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			if err != nil || capture.WorkError() != nil {
				t.Fatalf("projection=%v instrumentation=%v", err, capture.WorkError())
			}
			if queries > budget.MaxQueries || rows > budget.MaxRows || work.VMSteps > budget.MaxVMSteps {
				t.Errorf("history=%d actor=%s projection exceeds fixed budget: SQL=%d rows=%d VM=%d", revisions, actor, queries, rows, work.VMSteps)
			}
			indexedCurrentRow := false
			for _, statement := range statements {
				if strings.Contains(statement.SQL, "ORDER BY cr.revision_number ASC") {
					t.Error("mutation projection materialized revision history")
				}
				if !strings.Contains(statement.SQL, "AS ordered_cards") {
					continue
				}
				plan, err := perfguard.Explain(ctx, env.workspace.DB(), statement)
				if err != nil {
					t.Fatal(err)
				}
				indexedCurrentRow = strings.Contains(strings.Join(plan, "\n"), "idx_ref_edges_work_placement")
				if !indexedCurrentRow {
					t.Fatalf("current card lost indexed placement seek: %v", plan)
				}
			}
			if !indexedCurrentRow {
				t.Fatal("current-row selector was not exercised")
			}
			// Retaining the current row's fields must match GET's public card
			// shape; only the discarded history read is being removed.
			card, err := s.GetBoardCard(scope, "", "scale-card-1")
			if err != nil {
				t.Fatal(err)
			}
			if err := enrichCardsWithContext(scope, req, opts, []map[string]any{card}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(view, publicCardView(card)) {
				t.Fatal("mutation projection differs from GET card fields")
			}
			t.Logf("history=%d actor=%s SQL=%d rows=%d VM=%d", revisions, actor, queries, rows, work.VMSteps)
		}
	}
}
