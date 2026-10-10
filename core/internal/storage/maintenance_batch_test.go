package storage_test

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"modernc.org/sqlite"
)

var maintenanceDelayID atomic.Uint64

// Delaying projection writes inside SQLite makes an oversized chunk exceed its
// real deadline without relying on payload parsing speed on the test machine.
func TestMaintenanceDeadlineEventuallyCommitsCursor(t *testing.T) {
	for _, kind := range []string{"asks", "inbox"} {
		t.Run(kind, func(t *testing.T) {
			function := fmt.Sprintf("maintenance_delay_%d", maintenanceDelayID.Add(1))
			if err := sqlite.RegisterScalarFunction(function, 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
				time.Sleep(20 * time.Millisecond)
				return int64(0), nil
			}); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := ws.DB().ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			var run func(context.Context) (bool, error)
			var job, target string
			if kind == "asks" {
				// Simulate asks written before the projection trigger was installed.
				exec(`DROP TRIGGER ask_subjects_insert`)
				for i := 0; i < 8; i++ {
					id := fmt.Sprintf("legacy-%02d", i)
					raw, _ := json.Marshal(map[string]any{"id": id, "type": "human_attention_requested", "actor_id": "owner", "payload": map[string]any{"requester_actor_id": "owner"}})
					exec(`INSERT INTO events(id,type,ts,actor_id,payload_json) VALUES(?,'human_attention_requested','2026-10-10T00:00:00Z','owner',?)`, id, string(raw))
				}
				exec(`UPDATE ask_subjects_job SET cursor='',done=0`)
				exec(`CREATE TRIGGER slow_maintenance BEFORE INSERT ON ask_subjects BEGIN SELECT ` + function + `(); END`)
				run = ws.MaintainAskSubjectsBatch
				job, target = "ask_subjects_job", "ask_subjects"
			} else {
				store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
				thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "Legacy"})
				if err != nil {
					t.Fatal(err)
				}
				id := thread.Thread["id"].(string)
				items := []primitives.DerivedInboxItem{}
				for i := 0; i < 8; i++ {
					items = append(items, primitives.DerivedInboxItem{ID: fmt.Sprintf("legacy-%02d", i), ThreadID: id, Category: "agent_wake", TriggerAt: "2026-10-10T00:00:00Z", Data: map[string]any{"related_refs": []string{"thread:" + id}}})
				}
				if err := store.ReplaceDerivedInboxItems(ctx, id, items); err != nil {
					t.Fatal(err)
				}
				exec(`DELETE FROM inbox_lifecycle_refs`)
				exec(`UPDATE derived_inbox_items SET lifecycle_ready=0`)
				exec(`UPDATE inbox_lifecycle_job SET phase=5,cursor='',owners_ready=1,done=0`)
				exec(`CREATE TRIGGER slow_maintenance BEFORE INSERT ON inbox_lifecycle_refs BEGIN SELECT ` + function + `(); END`)
				run = func(ctx context.Context) (bool, error) { return ws.MaintainInboxLifecycleBatch(ctx, 200) }
				job, target = "inbox_lifecycle_job", "inbox_lifecycle_refs"
			}
			failures, progress := 0, false
			for attempt := 0; attempt < 20; attempt++ {
				done, err := run(ctx)
				var cursor string
				if e := ws.DB().QueryRowContext(ctx, `SELECT cursor FROM `+job).Scan(&cursor); e != nil {
					t.Fatal(e)
				}
				if err != nil {
					failures++
					// Before the first commit every failed attempt must roll back both
					// projection rows and the cursor, then learn a smaller cap.
					if !progress {
						var count int
						if e := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+target).Scan(&count); e != nil {
							t.Fatal(e)
						}
						if cursor != "" || count != 0 {
							t.Fatalf("failed chunk committed cursor=%q rows=%d", cursor, count)
						}
					}
					continue
				}
				if cursor != "" {
					progress = true
				}
				if done {
					var count int
					if e := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM `+target).Scan(&count); e != nil {
						t.Fatal(e)
					}
					if failures == 0 || !progress || count != 8 {
						t.Fatalf("failures=%d progress=%v rows=%d", failures, progress, count)
					}
					return
				}
			}
			t.Fatal("deadline-exceeding maintenance never completed")
		})
	}
}
