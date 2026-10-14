package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func TestDashboardHTTPOriginalReceiptAndFreshAuthority(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.dashboard")
	stranger := seedNotificationTestAgent(t, env, "other.dashboard")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Dashboard","summary":"Launch","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	env.workspace.DB().SetMaxOpenConns(1)
	opts := handlerOptions{primitiveStore: store, actorRegistry: env.registry, authStore: env.authStore, workspaceID: "ws_main"}
	call := func(actor string, body map[string]any) *httptest.ResponseRecorder {
		scoped, cancel := context.WithTimeout(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor}), 5*time.Second)
		defer cancel()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/workspace/dashboard", strings.NewReader(string(raw))).WithContext(scoped)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: actor})
		out := httptest.NewRecorder()
		handleSetWorkspaceDashboard(out, req, opts)
		return out
	}
	body := map[string]any{"document_ref": "document:" + anyString(doc["id"]), "request_key": "original", "if_updated_at": nil}
	first := call(owner.ActorID, body)
	if first.Code != 200 {
		t.Fatalf("first=%d %s", first.Code, first.Body.String())
	}
	var receipt map[string]any
	if err = json.Unmarshal(first.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["ok"] != true || receipt["updated_at"] == nil {
		t.Fatalf("receipt=%v", receipt)
	}
	if err = store.SetWorkspaceDashboard(ctx, owner.ActorID, ""); err != nil {
		t.Fatal(err)
	}
	replay := call(owner.ActorID, body)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	hidden, err := store.PatchThread(ctx, owner.ActorID, anyString(doc["thread_id"]), map[string]any{"pm_actor_id": stranger.ActorID}, nil)
	if err != nil || hidden.Thread == nil {
		t.Fatalf("ownership=%v", err)
	}
	if out := call(owner.ActorID, body); out.Code != 404 {
		t.Fatalf("replay ignored new denial: %d %s", out.Code, out.Body.String())
	}
	if err = store.SetWorkspaceDashboard(ctx, stranger.ActorID, anyString(doc["id"])); err != nil {
		t.Fatal(err)
	}
	if out := call(owner.ActorID, map[string]any{"document_ref": nil, "request_key": "hidden-clear", "if_updated_at": nil}); out.Code != 409 {
		t.Fatalf("hidden singleton overwritten: %d %s", out.Code, out.Body.String())
	}
	if out := call(owner.ActorID, map[string]any{"document_ref": nil, "request_key": "missing-base"}); out.Code != 400 {
		t.Fatalf("missing base admitted: %d", out.Code)
	}
	for _, key := range []any{nil, 1, " ", strings.Repeat("x", 257)} {
		if out := call(owner.ActorID, map[string]any{"document_ref": nil, "request_key": key, "if_updated_at": nil}); out.Code != 400 {
			t.Fatalf("invalid key silently used legacy path: %v status=%d", key, out.Code)
		}
	}

}
