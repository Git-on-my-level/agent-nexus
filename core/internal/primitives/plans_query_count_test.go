package primitives_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestPlanAndReportReadsHaveBoundedQueryCounts(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	base, err := s.CreateWork(ctx, "actor-1", board["id"].(string), map[string]any{"title": "Initiative", "handle": "batch-0", "source": map[string]any{"authority": "github", "connection_id": "count-fixture", "native_id": "item-0"}})
	if err != nil {
		t.Fatal(err)
	}
	baseID := base["id"].(string)
	observed, err := s.SubmitWorkObservation(ctx, "actor-1", baseID, map[string]any{"idempotency_key": "read", "reader_id": "github", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "in_progress"}, "evidence": []any{map[string]any{"url": "https://example.test/evidence"}}})
	if err != nil {
		t.Fatal(err)
	}
	observationID := observed["observation"].(map[string]any)["id"]
	ids, refs := []string{baseID}, []string{base["ref"].(string)}
	// Seed canonical rows directly so a 2,000-row cost regression test does not
	// spend most of its time exercising unrelated event-producing mutations.
	for _, size := range []int{1, 200, 2000} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := len(ids); i < size; i++ {
			id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
			boardID, handle := "board-"+id, fmt.Sprintf("batch-%d", i)
			if _, err = tx.Exec(`INSERT INTO boards(id,handle,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by)
			 SELECT ?,?,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by FROM boards WHERE id=?`, boardID, handle, board["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO cards(id,handle,board_id,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json)
			 SELECT ?,?,?,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json FROM cards WHERE id=?`, id, handle, boardID, baseID); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json)
			 SELECT ?,source_type,?,target_type,?,edge_type,created_at,metadata_json FROM ref_edges WHERE source_type='board' AND edge_type='board_card' AND target_id=?`, "edge-"+id, boardID, id, baseID); err != nil {
				t.Fatal(err)
			}
			obsID := "observation-" + id
			if _, err = tx.Exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,source_sequence,status,body_json)
			 SELECT ?,?,idempotency_key,digest,observed_at,received_at,source_sequence,status,json_set(body_json,'$.id',?,'$.card_id',?,'$.card_ref',?) FROM work_observations WHERE id=?`, obsID, id, obsID, id, "card:"+handle, observationID); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO work_metadata(card_id,authority,connection_id,native_id,metadata_json,version,latest_observation_id,latest_attempt_id,refresh_json,updated_at,updated_by)
			 SELECT ?,authority,connection_id,?,json_set(metadata_json,'$.source.native_id',?),version,?,?,refresh_json,updated_at,updated_by FROM work_metadata WHERE card_id=?`, id, handle, handle, obsID, obsID, baseID); err != nil {
				t.Fatal(err)
			}
			ids, refs = append(ids, id), append(refs, "card:"+handle)
		}
		for i, id := range ids {
			p, _ := json.Marshal(plans.Plan{Steps: []plans.Step{{ID: "linked", Title: "Linked work", Ref: refs[i], Status: "done", After: []string{}}}})
			if _, err = tx.Exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?) ON CONFLICT(card_id) DO UPDATE SET body_json=excluded.body_json`, id, string(p), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			counter.Reset()
			page, err := s.ListReportWork(ctx, primitives.ReportWorkFilter{})
			if err != nil || len(page.Work) != size || page.Truncated {
				t.Fatalf("report rows=%d truncated=%v error=%v", len(page.Work), page.Truncated, err)
			}
			if got := counter.Count(); got != 1 {
				t.Fatalf("report hydration: %d queries for %d cards", got, size)
			}
			counter.Reset()
			if err = s.EnrichCardPlans(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "selected-pm", PMActorID: "selected-pm"}), page.Work, func(string, string) bool { return true }, time.Now(), 0); err != nil {
				t.Fatal(err)
			}
			if got := counter.Count(); got != int64(1+2*((size+199)/200)+size/200) {
				t.Fatalf("plan roll-up: %d queries for %d distinct linked refs", got, size)
			}
			for _, work := range page.Work {
				state := work["plan_state"].(plans.State)
				if state.Progress.Done != 0 || !state.Steps[0].Resolvable {
					t.Fatalf("source phase must override fallback: %+v", state)
				}
			}
			n := size
			if n > 200 {
				n = 200
			}
			counter.Reset()
			items, err := s.ResolveRefs(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "selected-pm", PMActorID: "selected-pm"}), refs[:n], func(string, string) bool { return true }, time.Now(), 0)
			if err != nil || len(items) != n {
				t.Fatalf("batch=%d error=%v", len(items), err)
			}
			if got := counter.Count(); got != int64(3+2*(n/200)) {
				t.Fatalf("batch resolve: %d queries for %d refs", got, n)
			}
		})
	}
}

func TestReportBatchProjectionMatchesCanonicalWork(t *testing.T) {
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	native, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Native", "priority": "p1", "phase": "review", "summary": "Evidence"})
	if err != nil {
		t.Fatal(err)
	}
	external := registerWork(t, s, board)
	id := external["id"].(string)
	for i, status := range []string{"reported", "error"} {
		_, err = s.SubmitWorkObservation(ctx, "actor-1", id, map[string]any{"idempotency_key": status, "reader_id": "github", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": i + 1, "status": status, "facts": map[string]any{"title": "Observed title", "phase": "blocked", "owner": "actor:source", "native_status": "pending"}, "error": "read failed", "evidence": []any{map[string]any{"url": "https://example.test/evidence"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListReportWork(ctx, primitives.ReportWorkFilter{BoardIDs: []string{board}})
	if err != nil || len(page.Work) != 2 {
		t.Fatalf("%+v %v", page, err)
	}
	for _, work := range page.Work {
		want, err := s.GetWork(ctx, work["id"].(string))
		if err != nil || !reflect.DeepEqual(work, want) {
			t.Fatalf("batch projection differs: got=%+v want=%+v error=%v", work, want, err)
		}
	}
	if native["id"] == external["id"] {
		t.Fatal("fixture must exercise both authorities")
	}
}
