package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

// An open backlog initiative is possibly_moot when its newest live note cites
// an inbox ask the reader can see that is already answered or withdrawn. An
// open ask, prose without an event ref, and an ask the reader cannot see do
// not flag. A later note that drops the ref clears the flag.
func TestNextAskDecidedOnBriefAndCard(t *testing.T) {
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

	type cardAsk struct {
		card  map[string]any
		askID string
	}
	makeCard := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	postAsk := func(card map[string]any, title string) string {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		ask, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": thread, "summary": title,
			"refs": []string{"thread:" + thread},
			"payload": map[string]any{
				"kind": "ask", "title": title, "requester_actor_id": "agent-1",
				"subject_ref": anyStringValue(card["ref"]), "response_proposals": []string{"Proceed"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(ask["id"])
	}
	answer := func(card map[string]any, askID, outcome string) {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		if _, _, err := store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": thread, "refs": []string{"event:" + askID},
			"payload": map[string]any{"outcome": outcome, "response_text": "Proceed", "request_event_ref": "event:" + askID},
		}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": thread}); err != nil {
			t.Fatal(err)
		}
	}
	note := func(card map[string]any, text string, next bool) string {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		payload := map[string]any{"text": text}
		if next {
			payload["next"] = map[string]any{"at": "2d", "what": "hear back"}
		}
		ev, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": thread,
			"refs":    []string{anyStringValue(card["ref"]), "thread:" + thread},
			"payload": payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		return anyStringValue(ev["id"])
	}

	answered := makeCard("Answered ask")
	answeredID := postAsk(answered, "Which way?")
	answer(answered, answeredID, "answered")
	note(answered, "next step waits on event:"+answeredID, true)

	withdrawn := makeCard("Withdrawn ask")
	withdrawnID := postAsk(withdrawn, "Still needed?")
	if _, err := store.AppendHumanAttentionWithdrawal(ctx, "agent-1", withdrawnID, map[string]any{
		"type": "human_attention_withdrawn", "summary": "Ask withdrawn",
		"refs":    []string{"event:" + withdrawnID},
		"payload": map[string]any{"reason": "no longer needed"},
	}); err != nil {
		t.Fatal(err)
	}
	note(withdrawn, "still pointing at event:"+withdrawnID, true)

	open := makeCard("Open ask")
	openID := postAsk(open, "Still open")
	note(open, "next step waits on event:"+openID, true)

	plain := makeCard("No ref")
	note(plain, "waiting on the budget ask David already answered", true)

	hiddenCard := makeCard("Hidden ask thread")
	if _, err := store.PatchThread(ctx, "actor-1", anyStringValue(hiddenCard["thread_id"]), map[string]any{"pm_actor_id": "other"}, nil); err != nil {
		t.Fatal(err)
	}
	hiddenID := postAsk(hiddenCard, "Private decision")
	answer(hiddenCard, hiddenID, "answered")
	citing := makeCard("Cites a hidden ask")
	note(citing, "next step waits on event:"+hiddenID, true)

	cleared := makeCard("Newer note clears it")
	clearedAsk := postAsk(cleared, "Old question")
	answer(cleared, clearedAsk, "answered")
	first := note(cleared, "next step waits on event:"+clearedAsk, true)
	second := note(cleared, "draft is up", false)
	if _, err := workspace.DB().ExecContext(ctx, `UPDATE brief_notes SET ts=? WHERE event_id=?`, "2026-10-01T00:00:00Z", first); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.DB().ExecContext(ctx, `UPDATE brief_notes SET ts=? WHERE event_id=?`, "2026-10-02T00:00:00Z", second); err != nil {
		t.Fatal(err)
	}

	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}

	hide := func(_, owner string) bool { return owner == "" || owner == "reader" }
	show := func(_, _ string) bool { return true }
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	hiddenBrief, err := store.Brief(ctx, BriefReader{ActorID: "reader", Kind: "human", Visible: hide}, now)
	if err != nil {
		t.Fatal(err)
	}
	shownBrief, err := store.Brief(ctx, BriefReader{ActorID: "reader", Kind: "human", Visible: show}, now)
	if err != nil {
		t.Fatal(err)
	}

	assertFlagged := func(brief map[string]any, title, askID string) {
		t.Helper()
		moot := initiativeMoot(t, brief, title)
		if moot["flagged"] != true || moot["code"] != nextAskDecidedCode || moot["reason"] != nextAskDecidedCode {
			t.Fatalf("%s moot = %#v", title, moot)
		}
		if moot["ask_ref"] != "event:"+askID || moot["decided_at"] == "" {
			t.Fatalf("%s evidence = %#v", title, moot)
		}
		if _, err := time.Parse(time.RFC3339, anyStringValue(moot["decided_at"])); err != nil {
			t.Fatal(err)
		}
	}
	assertClear := func(brief map[string]any, title string) {
		t.Helper()
		moot := initiativeMoot(t, brief, title)
		if moot["flagged"] != false || moot["code"] != nil || moot["ask_ref"] != nil || moot["reason"] != nil || moot["decided_at"] != nil {
			t.Fatalf("%s should not flag: %#v", title, moot)
		}
	}

	assertFlagged(hiddenBrief, "Answered ask", answeredID)
	assertFlagged(hiddenBrief, "Withdrawn ask", withdrawnID)
	assertClear(hiddenBrief, "Open ask")
	assertClear(hiddenBrief, "No ref")
	assertClear(hiddenBrief, "Cites a hidden ask")
	assertClear(hiddenBrief, "Newer note clears it")
	assertFlagged(shownBrief, "Cites a hidden ask", hiddenID)

	assertCard := func(card map[string]any, visible func(string, string) bool, flagged bool, askID string) {
		t.Helper()
		row := map[string]any{"id": card["id"]}
		if err := store.EnrichCardPlans(ctx, []map[string]any{row}, visible, now); err != nil {
			t.Fatal(err)
		}
		moot, ok := row["possibly_moot"].(PossiblyMoot)
		if !ok {
			t.Fatalf("possibly_moot = %#v", row["possibly_moot"])
		}
		if moot.Flagged != flagged || (flagged && (moot.Code != nextAskDecidedCode || moot.AskRef != "event:"+askID || moot.DecidedAt == "")) {
			t.Fatalf("card %v flagged=%v moot=%+v", card["title"], flagged, moot)
		}
		if !flagged && (moot.Code != "" || moot.AskRef != "" || moot.DecidedAt != "" || moot.Reason != "") {
			t.Fatalf("clear card leaked evidence: %+v", moot)
		}
	}
	assertCard(answered, hide, true, answeredID)
	assertCard(withdrawn, hide, true, withdrawnID)
	assertCard(open, hide, false, "")
	assertCard(plain, hide, false, "")
	assertCard(citing, hide, false, "")
	assertCard(citing, show, true, hiddenID)
	assertCard(cleared, hide, false, "")
}

func initiativeMoot(t *testing.T, brief map[string]any, title string) map[string]any {
	t.Helper()
	items, _ := brief["initiatives"].([]map[string]any)
	for _, item := range items {
		if item["title"] == title {
			moot, _ := item["possibly_moot"].(map[string]any)
			if moot == nil {
				t.Fatalf("%s possibly_moot missing: %#v", title, item)
			}
			return moot
		}
	}
	t.Fatalf("initiative %q missing from %#v", title, brief["initiatives"])
	return nil
}
