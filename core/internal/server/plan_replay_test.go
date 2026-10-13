package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func TestPlanReplayConcurrentOriginalReceiptAndChangedRequest(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	store := h.store.(*primitives.Store)
	// The outer wrapper owns the quota lock. Plan events write through the
	// nested transaction directly; no quota writer may reacquire that lock.
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 100, MaxDocuments: 100, MaxRevisions: 100})(store)
	work, err := store.CreateWork(context.Background(), "actor-plan", "", map[string]any{"title": "Original initiative"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateWork(context.Background(), "actor-plan", "", map[string]any{"title": "Other initiative"})
	if err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-plan","display_name":"Planner","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	body := map[string]any{"actor_id": "actor-plan", "request_key": "original-plan", "if_updated_at": work["updated_at"], "plan": map[string]any{"steps": []any{map[string]any{"id": "first", "title": "Original step"}}}}
	raw, _ := json.Marshal(body)
	endpoint := h.baseURL + "/cards/" + anyString(work["ref"]) + "/plan"
	var beforeEvents int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	h.workspace.DB().SetMaxOpenConns(1)
	start := make(chan struct{})
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			req, _ := http.NewRequest(http.MethodPut, endpoint, strings.NewReader(string(raw)))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- err.Error()
				return
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			results <- fmt.Sprintf("%d %s", resp.StatusCode, data)
		}()
	}
	close(start)
	first := <-results
	if !strings.HasPrefix(first, "200 ") {
		t.Fatal(first)
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next != first {
			t.Fatalf("original receipt changed: %s vs %s", first, next)
		}
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&count); err != nil || count != beforeEvents+1 {
		t.Fatalf("duplicate plan event: %d (before %d), %v", count, beforeEvents, err)
	}
	var original map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(first, "200 ")), &original); err != nil {
		t.Fatal(err)
	}
	body["plan"] = map[string]any{"steps": []any{map[string]any{"id": "second", "title": "Newer step"}}}
	planRequest(t, http.MethodPut, endpoint, body, 409)
	body["plan"] = map[string]any{"steps": []any{map[string]any{"id": "first", "title": "Original step"}}}
	body["if_updated_at"] = original["if_updated_at"]
	planRequest(t, http.MethodPut, endpoint, body, 409)
	// A separate edit advances the live plan. Replaying the first key keeps
	// its original projection and token, rather than returning today's read.
	body["request_key"] = "later-plan"
	body["plan"] = map[string]any{"steps": []any{map[string]any{"id": "later", "title": "Later step"}}}
	planRequest(t, http.MethodPut, endpoint, body, 200)
	var replayBody map[string]any
	if err := json.Unmarshal(raw, &replayBody); err != nil {
		t.Fatal(err)
	}
	replay := planRequest(t, http.MethodPut, endpoint, replayBody, 200)
	encoded, _ := json.Marshal(replay)
	encodedOriginal, _ := json.Marshal(original)
	if string(encoded) != string(encodedOriginal) {
		t.Fatalf("replay recomputed the original receipt: %s", encoded)
	}
	for _, key := range []string{strings.Repeat("x", 257), "invalid\x00key"} {
		replayBody["request_key"] = key
		planRequest(t, http.MethodPut, endpoint, replayBody, 400)
	}
	replayBody["request_key"] = "original-plan"
	// Keys belong to the canonical target too, not merely the plan body.

	planRequest(t, http.MethodPut, h.baseURL+"/cards/"+anyString(other["ref"])+"/plan", replayBody, 409)
}

func TestAtomicMutationReceiptPreservesEncodingAndIntegerPrecision(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	store := h.store.(*primitives.Store)
	runs := 0
	run := func(context.Context) (map[string]any, error) {
		runs++
		return map[string]any{"result": struct {
			Version uint64 `json:"version"`
			Label   string `json:"label"`
		}{9007199254740993, "Original"}}, nil
	}
	first, err := store.ExecuteWithReplay(context.Background(), "test.receipt", "actor", "key", "hash", 200, run)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.ExecuteWithReplay(context.Background(), "test.receipt", "actor", "key", "hash", 200, run)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := json.Marshal(first.Response)
	two, _ := json.Marshal(replay.Response)
	if runs != 1 || string(one) != string(two) || !strings.Contains(string(one), `"version":9007199254740993`) {
		t.Fatalf("receipt changed or lost precision: runs=%d first=%s replay=%s", runs, one, two)
	}
}

func TestPlanReplayReceiptFailureRollsBackEveryWrite(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	store := h.store.(*primitives.Store)
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 100})(store)
	board, err := store.CreateBoard(context.Background(), "actor-plan", map[string]any{"title": "Plan board"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(context.Background(), "actor-plan", anyString(board["id"]), map[string]any{"title": "Atomic initiative"})
	if err != nil {
		t.Fatal(err)
	}
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-plan","display_name":"Planner","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	endpoint := h.baseURL + "/cards/" + anyString(work["ref"]) + "/plan"
	before := workGetJSON(t, endpoint, 200)
	var beforeBoardToken string
	var beforeVersion, beforeEvents int
	if err := h.workspace.DB().QueryRow(`SELECT updated_at FROM boards WHERE id=?`, board["id"]).Scan(&beforeBoardToken); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT version FROM work_metadata WHERE card_id=?`, work["id"]).Scan(&beforeVersion); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&beforeEvents); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workspace.DB().Exec(`CREATE TRIGGER reject_plan_receipt BEFORE INSERT ON idempotency_replays WHEN NEW.scope='plan.set' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"actor_id": "actor-plan", "request_key": "original", "if_updated_at": work["updated_at"], "plan": map[string]any{"steps": []any{map[string]any{"id": "first", "title": "Keep this"}}}}
	planRequest(t, http.MethodPut, endpoint, body, 500)
	after := workGetJSON(t, endpoint, 200)
	if after["plan"] != nil || after["if_updated_at"] != before["if_updated_at"] {
		t.Fatal("partial plan or token survived", after)
	}
	var boardToken string
	var version, events, plansCount, receipts int
	if err := h.workspace.DB().QueryRow(`SELECT updated_at FROM boards WHERE id=?`, board["id"]).Scan(&boardToken); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT version FROM work_metadata WHERE card_id=?`, work["id"]).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM card_plans`).Scan(&plansCount); err != nil {
		t.Fatal(err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE scope='plan.set'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if boardToken != beforeBoardToken || version != beforeVersion || events != beforeEvents || plansCount != 0 || receipts != 0 {
		t.Fatalf("partial nested write survived: board %s/%s version %d/%d events %d/%d plans %d receipts %d", boardToken, beforeBoardToken, version, beforeVersion, events, beforeEvents, plansCount, receipts)
	}
	if _, err := h.workspace.DB().Exec(`DROP TRIGGER reject_plan_receipt`); err != nil {
		t.Fatal(err)
	}
	planRequest(t, http.MethodPut, endpoint, body, 200)
}

func TestPlanReplayDeniedAfterOwnershipRevoked(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	store := h.store.(*primitives.Store)
	ctx := context.Background()
	work, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private initiative"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(work["thread_id"])
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	owner := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	body := fmt.Sprintf(`{"actor_id":"owner","request_key":"original","if_updated_at":%q,"plan":{"steps":[{"id":"first","title":"Private step"}]}}`, work["updated_at"])
	write := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/cards/"+anyString(work["ref"])+"/plan", strings.NewReader(body)).WithContext(owner)
		out := httptest.NewRecorder()
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "owner"})
		handleCardPlan(out, req, handlerOptions{primitiveStore: store}, anyString(work["ref"]))
		return out
	}
	if out := write(); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := write(); out.Code != 404 || strings.Contains(out.Body.String(), "Private step") {
		t.Fatal("revoked receipt escaped access checks", out.Code, out.Body.String())
	}
	// Even replay access below the route must enforce current ownership.
	if _, err = store.GetIdempotencyReplay(owner, "plan.set", "owner", "original"); err == nil {
		t.Fatal("private receipt remained readable")
	}
}
