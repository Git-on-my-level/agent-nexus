package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEventLifecycleAtomicReceiptAndCascade(t *testing.T) {
	requireIntegrationTest(t)
	for _, action := range []string{"archive", "unarchive", "trash", "restore"} {
		t.Run(action, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			owner := seedNotificationTestAgent(t, env, "owner.lifecycle-event")
			store := env.primitiveStore.(*primitives.Store)
			ctx := context.Background()
			topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Private lifecycle", "summary": "Original description"})
			if err != nil {
				t.Fatal(err)
			}
			thread := anyString(topic.Topic["thread_id"])
			if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			peer, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Independent reply source", "summary": "Synthetic"})
			if err != nil {
				t.Fatal(err)
			}
			peerThread := anyString(peer.Topic["thread_id"])
			appendMessage := func(id, parent string) map[string]any {
				refs := []any{"thread:" + thread}
				if parent != "" {
					refs = append(refs, "event:"+parent, "thread:"+peerThread)
				}
				event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": id, "type": "message_posted", "thread_id": thread, "refs": refs, "payload": map[string]any{"text": "PRIVATE message"}})
				if err != nil {
					t.Fatal(err)
				}
				return event
			}
			parent := appendMessage("lifecycle-parent", "")
			child := appendMessage("lifecycle-child", "lifecycle-parent")
			if action == "unarchive" {
				parent, err = store.ArchiveEvent(ctx, owner.ActorID, "lifecycle-parent")
			}
			if action == "restore" {
				parent, err = store.TrashEvent(ctx, owner.ActorID, "lifecycle-parent", "prepare")
			}
			if err != nil {
				t.Fatal(err)
			}
			_ = child
			env.workspace.DB().SetMaxOpenConns(1)
			primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1 << 30, MaxArtifacts: 1000})(store)
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
			opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
			body := map[string]any{"actor_id": owner.ActorID, "request_key": "original-event-action", "reason": "Original reason", "if_lifecycle": map[string]any{"archived_at": parent["archived_at"], "trashed_at": parent["trashed_at"]}}
			call := func(body map[string]any, id string) *httptest.ResponseRecorder {
				bounded, cancel := context.WithTimeout(scope, 10*time.Second)
				defer cancel()
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/events/"+id+"/"+action, strings.NewReader(string(raw))).WithContext(bounded)
				cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
				out := httptest.NewRecorder()
				handleEventLifecycle(out, req, opts, id, action)
				return out
			}
			snapshot := func() string {
				var rows []string
				for _, id := range []string{"lifecycle-parent", "lifecycle-child"} {
					event, err := store.GetEvent(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := json.Marshal(event)
					rows = append(rows, string(raw))
				}
				var receipts int
				if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays`).Scan(&receipts); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal([]any{rows, receipts})
				return string(raw)
			}
			for _, trigger := range []string{`CREATE TRIGGER reject_action BEFORE UPDATE ON events WHEN NEW.id='lifecycle-child' BEGIN SELECT RAISE(ABORT,'child rejected'); END`, `CREATE TRIGGER reject_action BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt rejected'); END`} {
				if _, err = env.workspace.DB().Exec(trigger); err != nil {
					t.Fatal(err)
				}
				before := snapshot()
				if out := call(body, "lifecycle-parent"); out.Code != 500 || snapshot() != before {
					t.Fatal("partial cascade/receipt commit", out.Code, out.Body.String())
				}
				if _, err = env.workspace.DB().Exec(`DROP TRIGGER reject_action`); err != nil {
					t.Fatal(err)
				}
			}
			results := make(chan *httptest.ResponseRecorder, 8)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); results <- call(body, "lifecycle-parent") }()
			}
			wg.Wait()
			close(results)
			original := ""
			for out := range results {
				if out.Code != 200 {
					t.Fatal(out.Code, out.Body.String())
				}
				if original == "" {
					original = out.Body.String()
				}
				if out.Body.String() != original {
					t.Fatal("receipt changed")
				}
			}
			var receipt struct {
				Affected []string `json:"affected_event_ids"`
			}
			if err = json.Unmarshal([]byte(original), &receipt); err != nil || len(receipt.Affected) != 2 {
				t.Fatal("missing cascade coverage", original, err)
			}
			changed := map[string]any{}
			for k, v := range body {
				changed[k] = v
			}
			changed["reason"] = "Changed intent"
			if out := call(changed, "lifecycle-parent"); out.Code != 409 {
				t.Fatal("changed body accepted", out.Code)
			}
			if out := call(body, "lifecycle-child"); out.Code != 409 {
				t.Fatal("changed target accepted", out.Code)
			}
			changed["reason"] = body["reason"]
			changed["actor_id"] = "replacement"
			if out := call(changed, "lifecycle-parent"); out.Code != 403 {
				t.Fatal("actor substitution accepted", out.Code)
			}
			// Later canonical changes cannot replace the original result or reapply a cascade.
			if action == "archive" {
				_, err = store.UnarchiveEvent(ctx, owner.ActorID, "lifecycle-parent")
			}
			if action == "unarchive" {
				_, err = store.ArchiveEvent(ctx, owner.ActorID, "lifecycle-parent")
			}
			if action == "trash" {
				_, err = store.RestoreEvent(ctx, owner.ActorID, "lifecycle-parent")
			}
			if action == "restore" {
				_, err = store.TrashEvent(ctx, owner.ActorID, "lifecycle-parent", "later")
			}
			if err != nil {
				t.Fatal(err)
			}
			before := snapshot()
			if out := call(body, "lifecycle-parent"); out.Code != 200 || out.Body.String() != original || snapshot() != before {
				t.Fatal("recovery mutated later state", out.Code, out.Body.String())
			}
			changed["actor_id"] = owner.ActorID
			changed["request_key"] = "fresh"
			changed["if_lifecycle"] = map[string]any{"archived_at": "stale", "trashed_at": nil}
			if out := call(changed, "lifecycle-parent"); out.Code != 409 {
				t.Fatal("stale base accepted", out.Code)
			}
			delete(changed, "if_lifecycle")
			if out := call(changed, "lifecycle-parent"); out.Code != 400 {
				t.Fatal("missing base accepted", out.Code)
			}
			changed["if_lifecycle"] = map[string]any{}
			if out := call(changed, "lifecycle-parent"); out.Code != 400 {
				t.Fatal("incomplete base accepted", out.Code)
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, peerThread, map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = store.GetEvent(scope, "lifecycle-parent"); err != nil {
				t.Fatal("parent must stay visible", err)
			}
			revokedBefore := snapshot()
			// Receipt relation policy can hide the original row; re-entry then
			// fails CAS or rolls back on its occupied key. No content or changed
			// state may escape even when only the descendant became private.
			if out := call(body, "lifecycle-parent"); (out.Code != 403 && out.Code != 404 && out.Code != 409) || snapshot() != revokedBefore || strings.Contains(out.Body.String(), "lifecycle-child") || strings.Contains(out.Body.String(), "PRIVATE") {
				t.Fatal("independently revoked child receipt exposed", out.Code, out.Body.String())
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
				t.Fatal(err)
			}
			if out := call(body, "lifecycle-parent"); (out.Code != 403 && out.Code != 404) || strings.Contains(out.Body.String(), "PRIVATE") {
				t.Fatal("revoked receipt exposed", out.Code, out.Body.String())
			}
		})
	}
}

func TestEventLifecycleLegacyConflictCodes(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.legacy-event")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "custom", "refs": []string{}, "summary": "Original"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyString(event["id"])
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry}
	for _, tc := range []struct{ action, code string }{{"unarchive", "not_archived"}, {"restore", "not_trashed"}, {"archive", "already_trashed"}} {
		if tc.action == "archive" {
			if _, err = store.TrashEvent(ctx, owner.ActorID, id, ""); err != nil {
				t.Fatal(err)
			}
		}
		raw, _ := json.Marshal(map[string]any{"actor_id": owner.ActorID})
		req := httptest.NewRequest(http.MethodPost, "/events/"+id+"/"+tc.action, strings.NewReader(string(raw)))
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handleEventLifecycle(out, req, opts, id, tc.action)
		if out.Code != 409 || !strings.Contains(out.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatal(tc, out.Code, out.Body.String())
		}
	}
}
