package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"agent-nexus-core/internal/primitives"
)

/*
The brief must not change when the client asks for compact work rows.

`work_view=summary` (SCA-681) narrows two different things, and only one of
them is near the brief. `compactOverviewWork` rewrites `work.items` in the
response, after the visit snapshot and every derived section are already
built — the brief reads the snapshot, not the response, so that pass cannot
reach it. The SQL-level trim is the one that could: it drops
definition_of_done_json, resolution_refs_json, refs_json and provenance_json
from the projection.

The brief's inputs survive because they come from somewhere else. `relations`
and `blockers` — the two that carry the top-ranked "blocks N cards" signal
and the at-risk reasons — are parsed out of metadata_json, which the trim
keeps, as are priority and due_at.

That is a true statement about today's trim, not a guarantee. This test
pins it: if a later trim takes metadata_json's relations with it, the brief
would quietly start reporting "blocks 0 cards" and rank the wrong thing
first, with every other test still green. Comparing the two views is the only
assertion that fails loudly when that happens.
*/
func TestBriefIsIdenticalUnderSummaryWorkView(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"executive","display_name":"Alex","created_at":"2026-10-04T12:00:00Z","tags":["human"]}}`, 201).Body.Close()
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "executive", map[string]any{"title": "Initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateBoard(ctx, "executive", anyString(board["id"]), map[string]any{"role": "initiatives"}, nil); err != nil {
		t.Fatal(err)
	}
	// A gate two open cards depend on, a blocked card with recorded blockers,
	// and a card with a due date: between them they exercise every signal the
	// ranking and the at-risk reasons read out of metadata_json.
	gate, err := store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": "Approve the pricing change", "next_actor": "human", "next_action": "Pick a price", "priority": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Build the page", "Wire the API"} {
		if _, err = store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": title, "relations": []any{map[string]any{"kind": "depends_on", "ref": anyString(gate["ref"])}}}); err != nil {
			t.Fatal(err)
		}
	}
	stuck, err := store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": "Restore the replica", "blockers": []any{"waiting on vendor access"}, "next_actor": "human", "due_at": "2026-10-04T12:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.MoveBoardCard(ctx, "executive", anyString(board["id"]), anyString(stuck["id"]), primitives.MoveBoardCardInput{ColumnKey: "blocked"}); err != nil {
		t.Fatal(err)
	}

	read := func(path string) map[string]any {
		t.Helper()
		resp, err := http.Get(h.baseURL + path)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		err = json.NewDecoder(resp.Body).Decode(&payload)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: status=%d error=%v", path, resp.StatusCode, err)
		}
		brief, ok := payload["brief"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no brief: %v", path, payload)
		}
		// generated_at moves between the two reads by construction; so does the
		// digest baseline, because reading /overview records a visit.
		delete(brief, "generated_at")
		if digest, ok := brief["since_last_look"].(map[string]any); ok {
			delete(digest, "since")
		}
		return map[string]any{"brief": brief, "work": payload["work"]}
	}
	encode := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	full := read("/overview?work_view=full")
	summary := read("/overview?work_view=summary")

	if encode(full["brief"]) != encode(summary["brief"]) {
		t.Fatalf("the brief changed with the work view:\n full=%s\n summary=%s", encode(full["brief"]), encode(summary["brief"]))
	}

	// Guard the guard: if the summary view stopped actually compacting the
	// response, the comparison above would pass for the wrong reason.
	if encode(full["work"]) == encode(summary["work"]) {
		t.Fatal("summary work rows are identical to full rows; this test is no longer proving anything")
	}
	brief := full["brief"].(map[string]any)
	top := brief["decisions"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if top["signals"].(map[string]any)["blocks"] != float64(2) {
		t.Fatalf("the dependency signal this test exists to protect is not present: %v", top)
	}
	risk := brief["at_risk"].(map[string]any)["items"].([]any)
	if len(risk) == 0 || risk[0].(map[string]any)["reason"] != "Blocked on waiting on vendor access" {
		t.Fatalf("recorded blockers are not reaching the brief's reasons: %v", brief["at_risk"])
	}
}
