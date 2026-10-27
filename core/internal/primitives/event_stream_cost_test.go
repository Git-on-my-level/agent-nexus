package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestEventStreamWarmHiddenPageCostIsIndependentOfDenialHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	// Cost is measured as SQLite VM steps, which do not vary
	// with machine load; wall-clock latency belongs to the performance suite.
	ticks := map[int][]eventStreamTickCost{}
	for _, total := range []int{1000, 10000} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if _, err := ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`); err != nil {
				t.Fatal(err)
			}
			if _, err := ws.DB().Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
				INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json)
				SELECT printf('%06d',i),'message_posted','2026-01-01T00:00:00Z','owner','private','[]','{}' FROM n`, total); err != nil {
				t.Fatal(err)
			}
			dsn := "file:" + ws.Layout().DatabasePath
			workDB, work, err := perfguard.Open(dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer workDB.Close()
			db, counter := testsql.OpenDriver(dsn, workDB.Driver())
			defer db.Close()
			s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
			// Cold and warm pages both admit only their selected candidates.
			if _, err := s.ListEventStreamPage(scope, primitives.EventListFilter{}, primitives.EventCursor{}); err != nil {
				t.Fatal(err)
			}
			cursor := primitives.EventCursor{}
			for tick := 0; tick < 3; tick++ {
				counter.Reset()
				work.Start()
				page, err := s.ListEventStreamPage(scope, primitives.EventListFilter{}, cursor)
				work.Stop()
				if err != nil || len(page.Events) != 0 || !page.HasMore || page.Cursor.ID != fmt.Sprintf("%06d", (tick+1)*200) {
					t.Fatalf("hidden traversal: %+v %v", page, err)
				}
				cost := eventStreamTickCost{work: work.Work()}
				if work.WorkError() != nil || counter.Count() != 3 || counter.ReturnedRows() != 202 {
					t.Fatalf("hidden tick: statements=%d rows=%d work=%+v err=%v", counter.Count(), counter.ReturnedRows(), cost.work, work.WorkError())
				}
				ticks[total] = append(ticks[total], cost)
				bound := false
				for _, statement := range counter.Statements() {
					if !strings.Contains(statement.SQL, "FROM events e WHERE") || !strings.Contains(statement.SQL, "_anx_admission_ledger") {
						continue
					}
					var candidates []string
					if err := json.Unmarshal([]byte(statement.Args[0].(string)), &candidates); err != nil || len(candidates) != 200 {
						t.Fatalf("candidate binding includes history: %d %v", len(candidates), err)
					}
					if strings.Contains(statement.SQL, "json_group_array(json_array(kind,id))") {
						t.Fatal("whole denial capture returned")
					}
					bound = true
				}
				if !bound {
					t.Fatal("did not exercise bounded candidate binding")
				}
				t.Logf("hidden=%d tick=%d statements=%d rows=%d vm_steps=%d candidate_bindings=200", total, tick, counter.Count(), counter.ReturnedRows(), cost.work.VMSteps)
				cursor = page.Cursor
			}
		})
	}
	// Ten times the denial history must not add per-tick work: each tick binds
	// and probes the same 200 page rows, so allow only constant-size noise.
	for tick, small := range ticks[1000] {
		large := ticks[10000][tick]
		grew := func(small, large uint64) bool { return large > small+small/10 }
		if grew(small.work.VMSteps, large.work.VMSteps) || grew(small.work.FullScanSteps, large.work.FullScanSteps) || grew(small.work.AutoIndexRows, large.work.AutoIndexRows) {
			t.Fatalf("tick %d cost grew with denial history: 1000=%+v 10000=%+v", tick, small, large)
		}
	}
}

type eventStreamTickCost struct {
	work perfguard.WorkStats
}
