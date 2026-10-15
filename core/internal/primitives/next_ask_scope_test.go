package primitives

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

// A signed-in read rewrites events into a relation shadow with no physical
// indexes. The ask lookup must still succeed, flag a visible decided ask,
// and neither flag nor name an ask the scope hides.
func TestNextAskDecidedUnderAccessScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyStringValue(board["id"])
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	makeCard := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	postAsk := func(card map[string]any) string {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		ask, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": thread, "summary": "Which way?",
			"refs": []string{"thread:" + thread},
			"payload": map[string]any{
				"kind": "ask", "title": "Which way?", "requester_actor_id": "agent-1",
				"subject_ref": anyStringValue(card["ref"]), "response_proposals": []string{"Proceed"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(ask["id"])
	}
	answer := func(card map[string]any, askID string) {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		if _, _, err := store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": thread, "refs": []string{"event:" + askID},
			"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + askID},
		}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": thread}); err != nil {
			t.Fatal(err)
		}
	}
	note := func(card map[string]any, text string) {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": thread,
			"refs":    []string{anyStringValue(card["ref"]), "thread:" + thread},
			"payload": map[string]any{"text": text, "next": map[string]any{"at": "2d", "what": "hear back"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	visibleCard := makeCard("Visible ask")
	visibleAsk := postAsk(visibleCard)
	answer(visibleCard, visibleAsk)
	note(visibleCard, "next step waits on event:"+visibleAsk)

	hiddenCard := makeCard("Hidden ask thread")
	if _, err := store.PatchThread(ctx, "actor-1", anyStringValue(hiddenCard["thread_id"]), map[string]any{"pm_actor_id": "other"}, nil); err != nil {
		t.Fatal(err)
	}
	hiddenAsk := postAsk(hiddenCard)
	answer(hiddenCard, hiddenAsk)
	citing := makeCard("Cites a hidden ask")
	note(citing, "next step waits on event:"+hiddenAsk)

	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}

	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	allow := func(string, string) bool { return true }
	brief, err := store.Brief(scoped, BriefReader{ActorID: "reader", Kind: "human", Visible: allow}, now)
	if err != nil {
		t.Fatal(err)
	}
	shown := initiativeMoot(t, brief, "Visible ask")
	if shown["flagged"] != true || shown["code"] != nextAskDecidedCode || shown["ask_ref"] != "event:"+visibleAsk {
		t.Fatalf("visible brief moot = %#v", shown)
	}
	hidden := initiativeMoot(t, brief, "Cites a hidden ask")
	if hidden["flagged"] != false || hidden["code"] != nil || hidden["ask_ref"] != nil || strings.Contains(anyStringValue(hidden["reason"]), hiddenAsk) {
		t.Fatalf("hidden brief moot = %#v", hidden)
	}

	read := func(card map[string]any) PossiblyMoot {
		t.Helper()
		item, err := store.GetWork(ctx, anyStringValue(card["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if err = store.EnrichCardPlans(scoped, []map[string]any{item}, allow, now); err != nil {
			t.Fatal(err)
		}
		moot, ok := item["possibly_moot"].(PossiblyMoot)
		if !ok {
			t.Fatalf("possibly_moot = %#v", item["possibly_moot"])
		}
		return moot
	}
	if moot := read(visibleCard); !moot.Flagged || moot.Code != nextAskDecidedCode || moot.AskRef != "event:"+visibleAsk {
		t.Fatalf("visible card = %+v", moot)
	}
	if moot := read(citing); moot.Flagged || moot.Code != "" || moot.AskRef != "" || strings.Contains(moot.Reason, hiddenAsk) {
		t.Fatalf("hidden card = %+v", moot)
	}

	previews, err := store.ResolveRefs(scoped, []string{anyStringValue(visibleCard["ref"]), anyStringValue(citing["ref"])}, allow, now)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]PossiblyMoot{}
	for _, row := range previews {
		if row.PossiblyMoot == nil {
			t.Fatalf("missing possibly_moot on %s", row.Ref)
		}
		byRef[row.Ref] = *row.PossiblyMoot
	}
	if moot := byRef[anyStringValue(visibleCard["ref"])]; !moot.Flagged || moot.AskRef != "event:"+visibleAsk {
		t.Fatalf("visible ref = %+v", moot)
	}
	if moot := byRef[anyStringValue(citing["ref"])]; moot.Flagged || moot.AskRef != "" || strings.Contains(moot.Reason, hiddenAsk) {
		t.Fatalf("hidden ref = %+v", moot)
	}
}

func TestBothMootRulesFlagTogether(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyStringValue(board["id"])
	done, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": "Finished source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workspace.DB().ExecContext(ctx, `UPDATE cards SET column_key='done' WHERE id=?`, anyStringValue(done["id"])); err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{
		"title": "Both rules", "related_refs": []any{anyStringValue(done["ref"])},
	})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyStringValue(card["thread_id"])
	ask, err := store.AppendEvent(ctx, "agent-1", map[string]any{
		"type": "human_attention_requested", "thread_id": thread, "summary": "Which way?",
		"refs": []string{"thread:" + thread},
		"payload": map[string]any{
			"kind": "ask", "title": "Which way?", "requester_actor_id": "agent-1",
			"subject_ref": anyStringValue(card["ref"]), "response_proposals": []string{"Proceed"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	askID := anyStringValue(ask["id"])
	if _, _, err = store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
		"type": "human_attention_responded", "thread_id": thread, "refs": []string{"event:" + askID},
		"payload": map[string]any{"outcome": "answered", "response_text": "Proceed", "request_event_ref": "event:" + askID},
	}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": thread}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{
		"type": "message_posted", "thread_id": thread,
		"refs":    []string{anyStringValue(card["ref"]), "thread:" + thread},
		"payload": map[string]any{"text": "next step waits on event:" + askID, "next": map[string]any{"at": "2d", "what": "hear back"}},
	}); err != nil {
		t.Fatal(err)
	}
	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	item, err := store.GetWork(ctx, anyStringValue(card["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.EnrichCardPlans(ctx, []map[string]any{item}, nil, now); err != nil {
		t.Fatal(err)
	}
	moot, _ := item["possibly_moot"].(PossiblyMoot)
	if !moot.Flagged || moot.Sources != 1 || !strings.Contains(moot.Reason, "linked source") || moot.Code != nextAskDecidedCode || moot.AskRef != "event:"+askID {
		t.Fatalf("both rules = %+v", moot)
	}
}

func TestCitedAskHandleLookupUsesHandleIndex(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	rows, err := workspace.DB().QueryContext(ctx, `EXPLAIN QUERY PLAN
		SELECT ev.id FROM events ev
		WHERE ev.handle IN (SELECT value FROM json_each(?)) AND ev.handle IS NOT NULL AND trim(ev.handle)<>''`, `["event-handle"]`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "idx_events_handle_unique") {
		t.Fatalf("handle lookup lost its index:\n%s", plan)
	}
}
