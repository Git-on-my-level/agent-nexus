package server

import (
	"context"
	"fmt"
	"reflect"
	"testing"

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
	ids := []string{}
	for i, payloads := range [][]map[string]any{
		{{"next": map[string]any{"at": "2026-01-01", "what": "a"}}, {"next": map[string]any{"at": "2026-01-03", "what": "b"}}},
		{{"next": map[string]any{"at": "30d", "what": "draft"}, "evidence_refs": []any{"https://example.test/pr/1"}}},
		{{"evidence_refs": []any{"card:does-not-exist"}}, {"next": map[string]any{"at": "bogus"}}},
		{},
	} {
		w, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Fixture %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, anyString(w["id"]))
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
		t.Fatalf("paths differ:\nmaterialized=%+v\ndirect=%+v", materialized, direct)
	}
	if f := materialized[ids[0]]; f.Moves != 1 || f.Promise == nil || f.Promise.What != "b" {
		t.Fatalf("fixture 0 = %+v", f)
	}
	if f := materialized[ids[2]]; !f.LastEvidenceAt.IsZero() || f.Promise != nil {
		t.Fatalf("unresolvable evidence and bad next must not count: %+v", f)
	}
}
