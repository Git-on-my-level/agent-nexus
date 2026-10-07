package primitives

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestOverviewVisitValidationUsesFreshWriteGraph(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	other := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	control, err := other.CreateWork(ctx, "owner", "", map[string]any{"title": "Private control"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.PatchThread(ctx, "owner", anyStringValue(control["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	work, err := other.CreateWork(ctx, "owner", "", map[string]any{"title": "Initially public"})
	if err != nil {
		t.Fatal(err)
	}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	if !s.CanAccessResource(request, "card", anyStringValue(work["id"])) {
		t.Fatal("public work missing")
	}
	snapshot := denialSnapshotFrom(request)
	if snapshot == nil {
		t.Fatal("request denial was not prepared")
	}
	now := time.Now().UTC()
	policy, _ := resourceaccess.PolicyFrom(request)
	policyError := errors.New("custom write policy")
	policy.Check = func(context.Context, resourceaccess.QueryRower, any) error { return policyError }
	if err = s.RecordOverviewVisit(resourceaccess.WithPolicy(request, policy), "human:reader", []map[string]any{work}, now); !errors.Is(err, policyError) {
		t.Fatalf("visit replaced the caller's policy: %v", err)
	}
	counter.Reset()
	if err = s.RecordOverviewVisit(request, "human:reader", []map[string]any{work}, now); err != nil {
		t.Fatal(err)
	}
	fresh := false
	for _, statement := range counter.Statements() {
		if strings.Contains(statement.SQL, "json_group_array(json_array(kind,id))") {
			t.Fatal("visit rebuilt the prepared denial")
		}
		if strings.Contains(statement.SQL, "CROSS JOIN _anx_denied") {
			fresh = strings.Contains(statement.SQL, "main.threads") && strings.Contains(statement.SQL, "_anx_fresh_denied") && strings.Contains(statement.SQL, "main.resource_access_epoch") && len(statement.Args) == 7
		}
	}
	if !fresh {
		t.Fatal("visit validation did not read the current transaction graph")
	}
	// A different connection makes work private after the read. The write must
	// use its own transaction epoch and reject the now-inaccessible snapshot.
	if _, err = other.PatchThread(ctx, "owner", anyStringValue(work["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordOverviewVisit(request, "human:reader", []map[string]any{work}, now.Add(time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale visit accepted private work: %v", err)
	}
	var visited string
	if err = ws.DB().QueryRow(`SELECT visited_at FROM overview_visits WHERE principal_id='human:reader'`).Scan(&visited); err != nil || visited != now.Format("2006-01-02T15:04:05.000000000Z") {
		t.Fatalf("rejected visit changed state: %q %v", visited, err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM resource_access_epoch`); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordOverviewVisit(request, "human:reader", []map[string]any{work}, now.Add(2*time.Second)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing epoch accepted private work: %v", err)
	}
	if denialSnapshotFrom(request) != snapshot {
		t.Fatal("visit replaced the immutable request snapshot")
	}
	// Business writes also retain ordinary transaction validation.
	if _, err = s.CreateArtifact(request, "reader", map[string]any{"kind": "note", "refs": []string{anyStringValue(work["ref"])}}, "body", "text"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("visit policy escaped into business mutation: %v", err)
	}
}
