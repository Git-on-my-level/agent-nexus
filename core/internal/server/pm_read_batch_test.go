package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func TestPMReadBatchKeepsFreshAuthorityAndEpochPrivacy(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "batch-reader", "batch-actor", "batch-reader", "batch-token")
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	defer db.Close()
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "Batch control"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newOnboardedPMRuntime(t, db, store, auth.NewStore(db), PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		work, err := store.CreateWork(ctx, reader.ActorID, anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Public %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		d := pm.Decision{ID: fmt.Sprintf("batch-%d", i), WorkspaceID: "ws_main", ActorID: reader.ActorID, WorkRef: anyString(work["ref"]), Status: pm.AwaitingAnswer, TargetRevision: "1.1", CreatedAt: time.Now()}
		body, _ := json.Marshal(d)
		if _, err = env.workspace.DB().Exec(`INSERT INTO pm_records VALUES('decision',?,'ws_main',?,'',1,?)`, d.ID, reader.ActorID, body); err != nil {
			t.Fatal(err)
		}
	}
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
	p := pm.Principal{WorkspaceID: "ws_main", ActorID: reader.ActorID, Human: true}
	counter.Reset()
	ds, _, partial, err := runtime.Service.OverviewDecisions(scope, p)
	if err != nil || len(ds) != 100 || !partial || counter.Count() > 20 {
		t.Fatalf("bounded batch: len=%d partial=%v queries=%d err=%v", len(ds), partial, counter.Count(), err)
	}
	for _, d := range ds {
		if !d.CanAnswer || d.WorkMissing {
			t.Fatalf("public decision unavailable: %+v", d)
		}
	}
	// Explain the captured candidate SQL, including its real scope wrapper.
	// Owner/status selection must avoid sorting the matching history before
	// either arm's limit; only the bounded union needs a final sort.
	foundPlan := false
	for _, query := range counter.Statements() {
		if !strings.Contains(query.SQL, "d.rowid AS position") {
			continue
		}
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query.SQL, query.Args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "pm_records_overview_owner") || !strings.Contains(plan, "pm_records_status") || strings.Count(plan, "USE TEMP B-TREE FOR ORDER BY") != 1 {
			t.Fatalf("candidate arms lost indexed order before LIMIT: %s", plan)
		}
		foundPlan = true
	}
	if !foundPlan {
		t.Fatal("overview candidate query was not captured")
	}
	counter.Reset()
	page, err := runtime.Service.DecisionPage(scope, p, 20, "")
	if err != nil || len(page.Items) != 20 || !page.HasMore || counter.Count() > 15 {
		t.Fatalf("decision page batch: len=%d more=%v queries=%d err=%v", len(page.Items), page.HasMore, counter.Count(), err)
	}
	// A different connection changes authority while routing remains cached.
	if _, err = env.workspace.DB().Exec(`UPDATE agents SET metadata_json='{"principal_kind":"agent"}' WHERE id=?`, reader.AgentID); err != nil {
		t.Fatal(err)
	}
	ds, _, _, err = runtime.Service.OverviewDecisions(scope, p)
	if err != nil || len(ds) != 100 {
		t.Fatalf("fresh kind: len=%d err=%v", len(ds), err)
	}
	for _, d := range ds {
		if d.CanAnswer {
			t.Fatal("cached human authority survived a kind change")
		}
	}
	if _, err = store.PatchThread(ctx, reader.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	ds, _, _, err = runtime.Service.OverviewDecisions(scope, p)
	if err != nil || len(ds) != 0 {
		t.Fatalf("epoch-invalidated private work visible: %d %v", len(ds), err)
	}
	page, err = runtime.Service.DecisionPage(scope, p, 20, "")
	if err != nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("private decision page disclosed candidates: %+v %v", page, err)
	}
	if _, err = env.workspace.DB().Exec(`UPDATE agents SET revoked_at='now' WHERE id=?`, reader.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = runtime.Service.OverviewDecisions(scope, p); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("revocation ignored: %v", err)
	}
}
