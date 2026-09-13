package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestRound4WorkDecisionRevisionAndContextPagination(t *testing.T) {
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r4-human", "r4-human-actor", "r4-human", "r4-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Round 4"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		source := map[string]any{"authority": "nexus", "revision": "ignore-native-source"}
		want := "1"
		if i%3 != 0 {
			source = map[string]any{"authority": "github", "connection_id": "fixture", "native_id": fmt.Sprint(i)}
			if i%3 == 1 {
				source["revision"] = "sha"
				want = "sha"
			}
		}
		w, err := store.CreateWork(ctx, human.ActorID, asString(board["id"]), map[string]any{"title": fmt.Sprintf("Round4 %02d", i), "source": source})
		if err != nil {
			t.Fatal(err)
		}
		ref := asString(w["ref"])
		revision, err := currentWorkDecisionRevision(ctx, store, ref)
		if err != nil || revision != want || w["decision_revision"] != want || publicWork(w)["decision_revision"] != want {
			t.Fatalf("%+v revision=%s err=%v", w, revision, err)
		}
		// Version changes affect only the fallback; known external source fences remain stable.
		w, err = store.PatchWork(ctx, human.ActorID, ref, 1, map[string]any{"next_action": "review"})
		if err != nil {
			t.Fatal(err)
		}
		if want == "1" {
			want = "2"
		}
		revision, err = currentWorkDecisionRevision(ctx, store, ref)
		if err != nil || revision != want || publicWork(w)["decision_revision"] != want {
			t.Fatalf("updated revision %s want %s: %v", revision, want, err)
		}
		if _, err = store.PatchWork(ctx, human.ActorID, ref, 2, map[string]any{"decision_revision": "forged"}); err == nil {
			t.Fatal("read-only fence accepted")
		}
	}
	rt, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	read := func(params string, status int) pm.ContextPage {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/pm/context"+params, nil)
		req.Header.Set("Authorization", "Bearer "+human.AccessToken)
		rt.ServeHTTP(rr, req)
		if rr.Code != status {
			t.Fatalf("%s: %d %s", params, rr.Code, rr.Body)
		}
		var page pm.ContextPage
		if status == 200 {
			if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	first := read("?query=Round4&limit=50", 200)
	if len(first.Items) != 50 || first.NextCursor == "" || len(first.Limitations) != 0 {
		t.Fatalf("first=%+v", first)
	}
	second := read("?query=Round4&limit=50&cursor="+url.QueryEscape(first.NextCursor), 200)
	if len(second.Items) != 10 || second.NextCursor != "" || len(second.Limitations) != 0 {
		t.Fatalf("second=%+v", second)
	}
	seen := map[string]bool{}
	for _, item := range append(first.Items, second.Items...) {
		w := item.(map[string]any)
		ref := asString(w["ref"])
		if seen[ref] || w["decision_revision"] == "" || w["decision_revision"] == nil {
			t.Fatalf("invalid work %v", w)
		}
		seen[ref] = true
	}
	if len(read("?limit=1", 200).Items) != 1 {
		t.Fatal("limit 1 ignored")
	}
	read("?limit=0", 400)
	read("?limit=51", 400)
	read("?cursor=invalid", 400)
	for ref := range seen {
		read("?work_ref="+url.QueryEscape(ref)+"&cursor="+url.QueryEscape(first.NextCursor), 400)
		break
	}
	// Warm the runtime lookup, revoke in durable auth, then recheck in the
	// same service without relying on HTTP token authentication to reject it.
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: human.ActorID, Human: true}
	if _, err := rt.Service.ListDecisions(ctx, p); err != nil {
		t.Fatal(err)
	}
	seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r4-admin", "r4-admin-actor", "r4-admin", "r4-admin-token")
	_, err = env.authStore.RevokeAgent(ctx, human.AgentID, auth.RevokeAgentInput{Actor: auth.Principal{AgentID: human.AgentID, ActorID: human.ActorID}, Mode: auth.RevocationModeSelf})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Service.ListDecisions(ctx, p); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("revoked cached identity authorized: %v", err)
	}
}

type round4PrincipalStore struct {
	principal   auth.AuthPrincipalSummary
	lists, gets int
}

func (s *round4PrincipalStore) ListPrincipals(context.Context, auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error) {
	s.lists++
	return []auth.AuthPrincipalSummary{s.principal}, "", nil
}
func (s *round4PrincipalStore) GetPrincipalSummary(context.Context, string) (auth.AuthPrincipalSummary, error) {
	s.gets++
	return s.principal, nil
}
func TestRound4PrincipalIdentityCacheFreshAuthority(t *testing.T) {
	ctx := context.Background()
	store := &round4PrincipalStore{principal: auth.AuthPrincipalSummary{ActorID: "actor", AgentID: "agent", PrincipalKind: "human"}}
	lookup := newPMPrincipalLookup(store)
	now := time.Now()
	lookup.now = func() time.Time { return now }
	for i := 0; i < 101; i++ {
		if _, err := lookup.find(ctx, "actor"); err != nil {
			t.Fatal(err)
		}
	}
	if store.lists != 1 || store.gets != 100 {
		t.Fatalf("lists=%d gets=%d", store.lists, store.gets)
	}
	store.principal.PrincipalKind = "agent"
	got, err := lookup.find(ctx, "actor")
	if err != nil || got.PrincipalKind != "agent" {
		t.Fatalf("stale authority %+v %v", got, err)
	}
	now = now.Add(31 * time.Second)
	if _, err := lookup.find(ctx, "actor"); err != nil || store.lists != 2 {
		t.Fatalf("TTL: %v lists=%d", err, store.lists)
	}
	store.principal.Revoked = true
	if _, err := lookup.find(ctx, "actor"); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("revoked: %v", err)
	}
	if _, ok := lookup.identities["actor"]; ok {
		t.Fatal("revoked identity retained")
	}
	store.principal = auth.AuthPrincipalSummary{ActorID: "actor", AgentID: "replacement"}
	got, err = lookup.find(ctx, "actor")
	if err != nil || got.AgentID != "replacement" {
		t.Fatalf("replacement: %+v %v", got, err)
	}
}

func TestRound4RevisionIgnoresFreshnessAndInjectedProjection(t *testing.T) {
	w := map[string]any{"source": map[string]any{"authority": "github"}, "freshness": map[string]any{"source_revision": "stale"}, "version": int64(7), "decision_revision": "forged"}
	if publicWork(w)["decision_revision"] != "7" {
		t.Fatal(publicWork(w))
	}
	w["source"].(map[string]any)["revision"] = " opaque revision "
	if publicWork(w)["decision_revision"] != " opaque revision " {
		t.Fatal("source revision was not preserved verbatim")
	}
}
