package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// The legacy unpaged GET /cards returns every visible card, so its cost grows
// with the visible list. Plan/movement enrichment must share the 200-card
// statement batches of access and hierarchy enrichment (not 50-card ones),
// and private cards of an unrelated owner must add no statements.
func TestCardsListBatchesEnrichmentAndIgnoresHiddenCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("seeds a multi-batch card corpus")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	reader := seedMachinePrincipalForLockoutTest(t, ctx, raw, "cards-list-reader", "cards-list-reader", "cards.list.reader", "cards-list-token")
	const visible = 450 // three 200-card batches; nine 50-card batches before
	insertCards(t, raw, "cards-list-public", `{}`, "cards-list-visible", visible)

	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	h := NewHandler("cards-list-budget", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
	measure := func(label string) (int, perfguard.WorkStats) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/cards", nil)
		r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
		w := httptest.NewRecorder()
		capture.Start()
		h.ServeHTTP(w, r)
		statements, queries, _ := capture.Stop()
		work := capture.Work()
		if w.Code != http.StatusOK || capture.WorkError() != nil {
			t.Fatalf("%s: status=%d err=%v body=%.300s", label, w.Code, capture.WorkError(), w.Body.String())
		}
		var body struct {
			Cards []map[string]any `json:"cards"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Cards) != visible {
			t.Fatalf("%s: cards=%d err=%v", label, len(body.Cards), err)
		}
		for _, card := range body.Cards {
			if !strings.HasPrefix(anyString(card["id"]), "cards-list-visible-") {
				t.Fatalf("%s: hidden card leaked: %v", label, card["id"])
			}
		}
		planBatches := 0
		for _, s := range statements {
			if strings.Contains(s.SQL, "LEFT JOIN card_plans p ON p.card_id=c.id") {
				planBatches++
			}
		}
		if planBatches != 3 {
			t.Errorf("%s: plan/movement statements=%d, want 3 (200-card batches)", label, planBatches)
		}
		t.Logf("%s queries=%d VM=%d scans=%d", label, queries, work.VMSteps, work.FullScanSteps)
		return queries, work
	}
	baseQueries, baseWork := measure("visible-only")
	// 50-card plan batches issued 35 statements here (18 for plans and notes);
	// 200-card batches issue 23.
	if baseQueries > 25 {
		t.Errorf("GET /cards with %d visible cards issued %d statements, want <= 25", visible, baseQueries)
	}

	insertCards(t, raw, "cards-list-private", `{"pm_actor_id":"cards-list-stranger"}`, "cards-list-hidden", 4096)
	queries, work := measure("with-hidden-corpus")
	if queries != baseQueries {
		t.Errorf("hidden corpus changed statement count: %d -> %d", baseQueries, queries)
	}
	// Only the scoped list selector visits the hidden rows; enrichment never
	// sees them. Allow that selector's per-row cost, nothing per-card beyond.
	if limit := baseWork.VMSteps + 4096*60; work.VMSteps > limit {
		t.Errorf("hidden corpus VM steps %d exceed %d (visible-only %d)", work.VMSteps, limit, baseWork.VMSteps)
	}
}

func insertCards(t *testing.T, db *sql.DB, threadID, threadBody, prefix string, n int) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO threads(id,body_json,updated_at,updated_by) VALUES(?,?,'2026-01-01T00:00:00Z','owner')`, threadID, threadBody); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO boards(id,handle,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Board',?,'[{"key":"ready","title":"Ready"}]','2026-01-01T00:00:00Z','owner','2026-01-01T00:00:00Z','owner')`, threadID, threadID, threadID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s-%04d", prefix, i)
		if _, err := tx.Exec(`INSERT INTO cards(id,handle,board_id,thread_id,head_revision_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,?,?,'Card','','ready','2026-01-01T00:00:00Z','owner','2026-01-01T00:00:00Z','owner')`, id, id, threadID, threadID, id+"-rev"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
