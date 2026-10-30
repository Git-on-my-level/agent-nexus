package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

// The materialized brief facts (triggers + fold queue) and the per-request
// derivation from events produce the same folds on the same fixture.
func TestBriefMaterializedFoldsMatchPerRequestFolds(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	db, _ := testsql.Open("file:" + h.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, h.workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defaultCreated := time.Date(2026, 10, 25, 15, 0, 0, 123456789, time.UTC)
	ids := []string{}
	for i, payloads := range [][]map[string]any{
		{{"next": map[string]any{"at": "2026-01-01", "what": "a"}}, {"next": map[string]any{"at": "2026-01-03", "what": "b"}}},
		{{"next": map[string]any{"at": "30d", "what": "draft"}, "evidence_refs": []any{"https://example.test/pr/1"}}},
		{{"evidence_refs": []any{"card:does-not-exist"}}, {"next": map[string]any{"at": "bogus"}}},
		{},
	} {
		w, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Fixture %d", i), "initiative": true})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, anyString(w["id"]))
		if i == 3 {
			if _, err = db.Exec(`UPDATE cards SET created_at=? WHERE id=?`, defaultCreated.Format(time.RFC3339Nano), w["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE work_tracking SET default_at=? WHERE card_id=?`, defaultCreated.Add(12*time.Hour).Format(time.RFC3339Nano), w["id"]); err != nil {
				t.Fatal(err)
			}
		}
		for _, payload := range payloads {
			payload["text"] = "note"
			if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "message_posted", "thread_id": w["thread_id"], "refs": []string{anyString(w["ref"]), "thread:" + anyString(w["thread_id"])}, "payload": payload}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	materialized, err := store.PromiseFolds(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := store.FoldPromisesFromEvents(ctx, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(materialized) != len(ids) || !reflect.DeepEqual(materialized, direct) {
		a, _ := json.Marshal(materialized)
		b, _ := json.Marshal(direct)
		t.Fatalf("paths differ:\nmaterialized=%s\ndirect=%s", a, b)
	}
	if f := materialized[ids[0]]; f.Moves != 1 || f.Promise == nil || f.Promise.What != "b" {
		t.Fatalf("fixture 0 = %+v", f)
	}
	if f := materialized[ids[2]]; !f.LastEvidenceAt.IsZero() || f.Promise == nil || f.Promise.Source != "default" {
		t.Fatalf("unresolvable evidence and bad next must preserve the default: %+v", f)
	}
	if f := materialized[ids[3]]; f.Promise == nil || f.Promise.Source != "default" || !f.Promise.At.Equal(defaultCreated.Add(12*time.Hour)) {
		t.Fatalf("default timestamp lost subsecond precision: %+v", f)
	}
	brief, err := store.Brief(ctx, primitives.BriefReader{ActorID: "actor-1", Kind: "human"}, defaultCreated.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range brief["initiatives"].([]map[string]any) {
		if row["ref"] == "card:fixture-3" {
			found = true
			promise, _ := row["promise"].(map[string]any)
			if promise["at"] != defaultCreated.Add(12*time.Hour).Format(time.RFC3339Nano) || promise["source"] != "default" {
				t.Fatalf("brief wire lost the exact default promise: %+v", promise)
			}
		}
	}
	if !found {
		t.Fatal("default-promised card missing from brief")
	}
}
