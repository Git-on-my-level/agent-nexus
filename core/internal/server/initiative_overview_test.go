package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func TestOverviewPlanBatchPrivacyAndWireFixture(t *testing.T) {
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	db, counter := testsql.Open("file:" + h.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, h.workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	var small int64
	for i := 0; i < 40; i++ {
		w, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Initiative %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		p := plans.Plan{Steps: []plans.Step{{ID: "design", Title: "Design", Status: "done", After: []string{}}, {ID: "build", Title: "Build", After: []string{"design"}}}}
		if err = store.SetCardPlan(ctx, "actor-1", anyString(w["id"]), anyString(w["updated_at"]), p); err != nil {
			t.Fatal(err)
		}
		if i != 0 && i != 39 {
			continue
		}
		counter.Reset()
		out := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/overview", nil)
		attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
		handleGetOverview(out, req, handlerOptions{primitiveStore: store})
		if out.Code != 200 {
			t.Fatal(out.Body.String())
		}
		if i == 0 {
			small = counter.Count()
		} else if counter.Count() != small {
			t.Fatalf("queries grew: %d -> %d", small, counter.Count())
		}
		var body map[string]any
		if err = json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		items := body["initiatives"].(map[string]any)["items"].([]any)
		if len(items) != i+1 {
			t.Fatal(body)
		}
		item := items[0].(map[string]any)
		// Compare real serialized fields with the shared UI corpus. Dynamic
		// resource identities and timestamps are kept outside this fixture.
		state := item["plan_state"].(map[string]any)
		state["last_movement_at"] = "2026-10-04T12:00:00Z"
		health := item["plan_health"].(map[string]any)
		if _, err = time.Parse(time.RFC3339Nano, health["since"].(string)); err != nil {
			t.Fatal(err)
		}
		health["since"] = "2026-10-04T12:00:00Z"
		item["ref"] = "card:initiative"
		item["title"] = "Initiative"
		item["updated_at"] = "2026-10-04T12:00:00Z"
		got := item
		raw, err := os.ReadFile("../../../contracts/fixtures/initiative-overview/tile.json")
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]any
		if err = json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("wire fixture differs: got=%v want=%v", got, want)
		}
	}
	if _, err = store.PatchThread(ctx, "actor-1", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	out := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/overview", nil)
	attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
	handleGetOverview(out, req, handlerOptions{primitiveStore: store})
	if out.Code != 200 || strings.Contains(out.Body.String(), "Initiative 0") || strings.Contains(out.Body.String(), "Initiative 39") {
		t.Fatal(out.Body.String())
	}
	// One denial snapshot, bounded open/closed selectors, the designated-board
	// probe, two dashboard reads and the inbox page. No plans, ref facts or
	// notification lifecycle queries run when all work is unreadable.
	if counter.Count() != 7 {
		t.Fatalf("private query count %d", counter.Count())
	}
}

func TestOverviewDigestDecisionsRespectCurrentWorkVisibility(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "digest-human", "digest-human-actor", "digest-human", "digest-token")
	store := env.primitiveStore.(*primitives.Store)
	w, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Launch readiness"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	_, err = runtime.Service.ProposeDecision(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}, pm.DecisionInput{RequestKey: "digest", WorkRef: anyString(w["ref"]), Instruction: `{"next_action":"private instruction"}`, Scope: "work.annotate", TargetRevision: "1.1"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/overview/changes", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID, AgentID: owner.AgentID, PrincipalKind: "human"})
	opts := handlerOptions{primitiveStore: store, pmRuntime: runtime}
	attachResourceAccessScope(req, opts)
	d := primitives.OverviewChanges{Since: &since, Items: []primitives.OverviewChange{}}
	if err = appendOverviewDecisions(req, opts, &d, time.Now().UTC()); err != nil || len(d.Items) != 1 {
		t.Fatalf("%+v %v", d, err)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), "private instruction") || !strings.Contains(string(raw), "Launch readiness") {
		t.Fatal(string(raw))
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(w["thread_id"]), map[string]any{"pm_actor_id": "different-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	d.Items = []primitives.OverviewChange{}
	if err = appendOverviewDecisions(req, opts, &d, time.Now().UTC()); err != nil || len(d.Items) != 0 {
		t.Fatalf("private work decision leaked: %+v %v", d, err)
	}
}

func TestOverviewDigestDistinctDecisionRefsQueryBudget(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "digest-human", "digest-human-actor", "digest-human", "digest-token")
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	runtime, err := NewPMRuntime(db, store, auth.NewStore(db), PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	req := httptest.NewRequest("GET", "/overview/changes", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID, AgentID: owner.AgentID, PrincipalKind: "human"})
	opts := handlerOptions{primitiveStore: store, pmRuntime: runtime}
	attachResourceAccessScope(req, opts)
	for i := 1; i <= 200; i++ {
		w, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": fmt.Sprintf("Initiative %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", Ref: anyString(w["ref"]), After: []string{}}}}
		if err = store.SetCardPlan(ctx, owner.ActorID, anyString(w["id"]), anyString(w["updated_at"]), p); err != nil {
			t.Fatal(err)
		}
		_, err = runtime.Service.ProposeDecision(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}, pm.DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: anyString(w["ref"]), Instruction: `{"next_action":"private instruction"}`, Scope: "work.annotate", TargetRevision: "1.1"})
		if err != nil {
			t.Fatal(err)
		}
		if i != 1 && i != 20 && i != 200 {
			continue
		}
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			counter.Reset()
			d := primitives.OverviewChanges{Since: &since, Items: []primitives.OverviewChange{}}
			if err := appendOverviewDecisions(req, opts, &d, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			if len(d.Items) != min(i, 100) || d.Truncated != (i > 100) {
				t.Fatalf("items=%d truncated=%v", len(d.Items), d.Truncated)
			}
			// Each full 200-row page needs an empty-page check for previews and plan facts.
			wantQueries := int64(5 + 2*(i/200))
			// Main captures the immutable denial snapshot on the first read only;
			// later writes use its statement-level epoch fallback without recapture.
			if i == 1 {
				wantQueries++
			}
			if got := counter.Count(); got != wantQueries {
				t.Fatalf("%d distinct decision refs used %d queries; want %d", i, got, wantQueries)
			}
		})
	}
}

func TestOverviewVisitsPersistPerPrincipalAndDigestDoesNotAdvance(t *testing.T) {
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	w, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", After: []string{}}}}
	if err = store.SetCardPlan(ctx, "actor-1", anyString(w["id"]), anyString(w["updated_at"]), p); err != nil {
		t.Fatal(err)
	}
	read := func(path, principal string) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		if principal != "" {
			cacheAuthenticatedPrincipal(req, &auth.Principal{AgentID: principal, ActorID: principal, PrincipalKind: "human"})
		}
		out := httptest.NewRecorder()
		opts := handlerOptions{primitiveStore: store}
		if path == "/overview" {
			handleGetOverview(out, req, opts)
		} else {
			handleOverviewChanges(out, req, opts)
		}
		if out.Code != 200 || out.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(out.Body.String())
		}
		var body map[string]any
		if err = json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	first := read("/overview", "alice")["since_you_last_looked"].(map[string]any)
	if first["since"] != nil || len(first["items"].([]any)) != 0 {
		t.Fatal(first)
	}
	card, err := store.GetWork(ctx, anyString(w["id"]))
	if err != nil {
		t.Fatal(err)
	}
	p.Steps[0].Status = "done"
	if err = store.SetCardPlan(ctx, "actor-1", anyString(w["id"]), anyString(card["updated_at"]), p); err != nil {
		t.Fatal(err)
	}
	// A new store instance sees the same persisted baseline.
	store = primitives.NewTestStore(h.workspace.DB(), h.workspace.Layout().ArtifactContentDir)
	for i := 0; i < 2; i++ {
		d := read("/overview/changes", "alice")
		if d["since"] == nil || len(d["items"].([]any)) != 1 || d["items"].([]any)[0].(map[string]any)["kind"] != "step_completed" {
			t.Fatal(d)
		}
	}
	for _, principal := range []string{"bob", ""} {
		if d := read("/overview/changes", principal); d["since"] != nil || len(d["items"].([]any)) != 0 {
			t.Fatal(d)
		}
	}
	d := read("/overview", "alice")["since_you_last_looked"].(map[string]any)
	if len(d["items"].([]any)) != 1 {
		t.Fatal(d)
	}
	if d = read("/overview/changes", "alice"); len(d["items"].([]any)) != 0 {
		t.Fatal(d)
	}
	var visits int
	if err = h.workspace.DB().QueryRow(`SELECT count(*) FROM overview_visits`).Scan(&visits); err != nil || visits != 1 {
		t.Fatalf("visits=%d error=%v", visits, err)
	}
	// Old concurrent requests cannot roll the baseline back.
	if err = store.RecordOverviewVisit(ctx, "human:alice", nil, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if d = read("/overview/changes", "alice"); len(d["items"].([]any)) != 0 {
		t.Fatal(d)
	}
}
