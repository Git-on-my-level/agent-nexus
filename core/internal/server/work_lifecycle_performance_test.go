package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceWorkLifecyclePrivatePrefixBudget(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	reader := seedMachinePrincipalForLockoutTest(t, ctx, raw, "lifecycle-reader", "lifecycle-reader", "lifecycle.reader", "lifecycle-token")
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO threads(id,body_json,updated_at,updated_by) VALUES('private-lifecycle','{"pm_actor_id":"owner"}','now','owner')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4096; i++ {
		id := fmt.Sprintf("private-lifecycle-%04d", i)
		if _, err := tx.Exec(`INSERT INTO cards(id,handle,thread_id,title,summary,column_key,archived_at,created_at,created_by,updated_at,updated_by) VALUES(?,?,'private-lifecycle','Hidden','','todo','now','now','owner','now','owner')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	h := NewHandler("lifecycle-budget", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
	for _, phase := range []string{"cold", "warm", "invalidated"} {
		if phase == "invalidated" {
			if _, err := raw.Exec(`UPDATE threads SET updated_by=updated_by WHERE id='private-lifecycle'`); err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(http.MethodGet, "/work?limit=20", nil)
		r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
		w := httptest.NewRecorder()
		capture.Start()
		h.ServeHTTP(w, r)
		capture.Stop()
		work := capture.Work()
		t.Logf("%s VM=%d scans=%d", phase, work.VMSteps, work.FullScanSteps)
		if w.Code != 200 || capture.WorkError() != nil {
			t.Fatalf("status=%d err=%v body=%.300s", w.Code, capture.WorkError(), w.Body.String())
		}
		var page struct {
			Work   []any    `json:"work"`
			Refs   []string `json:"archived_refs"`
			Cursor string   `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Work) != 0 || len(page.Refs) != 0 || page.Cursor != "" {
			t.Fatalf("hidden lifecycle affected response: %+v err=%v", page, err)
		}
		if work.VMSteps > 50000 {
			t.Errorf("%s exceeds unchanged 50,000 VM ceiling: %d", phase, work.VMSteps)
		}
	}
}
