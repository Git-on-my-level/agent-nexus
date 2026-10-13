package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func TestCardPatchReplayCommitsAssignmentEventWakeAndReceipt(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.cardhost")
	target := seedNotificationTestAgent(t, env, "target.cardhost")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	work, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Durable assignment"})
	if err != nil {
		t.Fatal(err)
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30})(store)
	env.workspace.DB().SetMaxOpenConns(1)
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
	body := map[string]any{"actor_id": owner.ActorID, "request_key": "original", "if_updated_at": work["updated_at"], "patch": map[string]any{"assignee_refs": []string{"actor:" + target.ActorID}}}
	write := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPatch, "/cards/"+anyString(work["ref"]), strings.NewReader(string(raw))).WithContext(scope)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handlePatchCard(out, req, opts, anyString(work["ref"]))
		return out
	}
	counts := func() string {
		out := ""
		for _, table := range []string{"events", "artifacts", "agent_wakeups", "blob_usage_ledger", "idempotency_replays"} {
			var count int
			if err := env.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			out += fmt.Sprintf("%s=%d;", table, count)
		}
		card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
		if err != nil {
			t.Fatal(err)
		}
		return out + fmt.Sprint(card["updated_at"], card["assignee"], card["version"])
	}
	before := counts()
	for _, trigger := range []string{
		`CREATE TRIGGER reject_card_write BEFORE INSERT ON idempotency_replays WHEN NEW.scope='cards.patch' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
		`CREATE TRIGGER reject_card_write BEFORE INSERT ON events WHEN NEW.type='card_updated' BEGIN SELECT RAISE(ABORT,'event failed'); END`,
		`CREATE TRIGGER reject_card_write BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT,'wake packet failed'); END`,
		`CREATE TRIGGER reject_card_write BEFORE INSERT ON agent_wakeups BEGIN SELECT RAISE(ABORT,'wake row failed'); END`,
	} {
		if _, err := env.workspace.DB().Exec(trigger); err != nil {
			t.Fatal(err)
		}
		out := write(body)
		if out.Code == 200 {
			t.Fatalf("failed write was confirmed (%s): %s", trigger, out.Body.String())
		}
		if after := counts(); after != before {
			t.Fatalf("partial assignment survived: before %s after %s response %s", before, after, out.Body.String())
		}
		if _, err := env.workspace.DB().Exec(`DROP TRIGGER reject_card_write`); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- write(body) }()
	}
	close(start)
	first := <-results
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next.Code != 200 || next.Body.String() != first.Body.String() {
			t.Fatalf("replay changed: %d %s", next.Code, next.Body.String())
		}
	}
	var wakes, receipts, packets int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM agent_wakeups WHERE target_actor_id=?`, target.ActorID).Scan(&wakes); err != nil {
		t.Fatal(err)
	}
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE scope='cards.patch'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE kind='agent_wake'`).Scan(&packets); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 || receipts != 1 || packets != 1 {
		t.Fatalf("duplicate wake/receipt %d/%d packets %d", wakes, receipts, packets)
	}
	var original map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	later := map[string]any{"actor_id": owner.ActorID, "request_key": "later", "if_updated_at": original["card"].(map[string]any)["updated_at"], "patch": map[string]any{"risk": "high"}}
	if out := write(later); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := write(body); out.Code != 200 || out.Body.String() != first.Body.String() {
		t.Fatal("replay returned a newer card projection", out.Code, out.Body.String())
	}
	body["patch"] = map[string]any{"assignee_refs": []string{"actor:" + owner.ActorID}}
	if out := write(body); out.Code != 409 {
		t.Fatal("changed body accepted", out.Code, out.Body.String())
	}
	// Revoke the resource after successful replay; an old receipt must not bypass it.
	if _, err := store.PatchThread(ctx, owner.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	body["patch"] = map[string]any{"assignee_refs": []string{"actor:" + target.ActorID}}
	if out := write(body); out.Code != 404 {
		t.Fatal("revoked replay accepted", out.Code, out.Body.String())
	}
}
