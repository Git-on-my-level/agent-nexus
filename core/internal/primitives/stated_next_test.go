package primitives

import (
	"context"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/plans"
)

// An in-progress card whose latest note cites a decided ask is possibly_moot.
// A newer note promise replaces the ask outcome stored as next_action, and
// plan_health.reason names that promise rather than an earlier plan-step date.
func TestStatedNextFollowsTheLivePromise(t *testing.T) {
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

	makeCard := func(title, phase string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": title, "phase": phase})
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
	answer := func(card map[string]any, askID, text string) {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		if _, _, err := store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
			"type": "human_attention_responded", "thread_id": thread, "refs": []string{"event:" + askID},
			"payload": map[string]any{"outcome": "answered", "response_text": text, "request_event_ref": "event:" + askID},
		}, map[string]any{"requested": true, "target_actor_id": "agent-1", "thread_id": thread}); err != nil {
			t.Fatal(err)
		}
	}
	note := func(card map[string]any, text, what, at string) {
		t.Helper()
		thread := anyStringValue(card["thread_id"])
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": thread,
			"refs":    []string{anyStringValue(card["ref"]), "thread:" + thread},
			"payload": map[string]any{"text": text, "next": map[string]any{"at": at, "what": what}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	active := makeCard("Loop ledger", "in_progress")
	if err = store.SetCardPlan(ctx, "actor-1", anyStringValue(active["id"]), anyStringValue(active["updated_at"]), plans.Plan{Steps: []plans.Step{{
		ID: "retire", Title: "Retire finished or dead loops", Due: "2026-10-16",
	}}}); err != nil {
		t.Fatal(err)
	}
	askID := postAsk(active)
	answer(active, askID, "Go left")
	note(active, "next step waits on event:"+askID, "Check the loop ledger", "2026-10-21")

	review := makeCard("In review", "review")
	reviewAsk := postAsk(review)
	answer(review, reviewAsk, "Ship it")
	note(review, "still waiting on event:"+reviewAsk, "Confirm the review", "2026-10-18")

	blocked := makeCard("Blocked", "blocked")
	blockedAsk := postAsk(blocked)
	answer(blocked, blockedAsk, "Unblock")
	note(blocked, "waiting on event:"+blockedAsk, "Stay blocked", "2026-10-18")

	onlyNote := makeCard("Note only", "in_progress")
	note(onlyNote, "no annotation yet", "Write the note", "2026-10-20")

	answeredFirst := makeCard("Answer after the note", "in_progress")
	note(answeredFirst, "promise is already current", "Keep the draft", "2026-10-22")
	laterAsk := postAsk(answeredFirst)
	answer(answeredFirst, laterAsk, "Go left")

	if drained, err := store.RefreshBriefInitiatives(ctx, 200); err != nil || !drained {
		t.Fatalf("refresh drained=%v err=%v", drained, err)
	}

	visible := func(_, owner string) bool { return owner == "" || owner == "reader" }
	brief, err := store.Brief(ctx, BriefReader{ActorID: "reader", Kind: "human", Visible: visible}, now)
	if err != nil {
		t.Fatal(err)
	}
	moot := initiativeMoot(t, brief, "Loop ledger")
	if moot["flagged"] != true || moot["code"] != nextAskDecidedCode || moot["ask_ref"] != "event:"+askID {
		t.Fatalf("in-progress brief moot = %#v", moot)
	}
	if initiativeMoot(t, brief, "In review")["code"] != nextAskDecidedCode {
		t.Fatal("review did not flag")
	}
	if initiativeMoot(t, brief, "Blocked")["flagged"] != false {
		t.Fatalf("blocked flagged: %#v", initiativeMoot(t, brief, "Blocked"))
	}
	promise := initiativePromise(t, brief, "Loop ledger")
	if promise["source"] != "note" || promise["what"] != "Check the loop ledger" || !strings.Contains(anyStringValue(promise["at"]), "2026-10-21") {
		t.Fatalf("brief promise = %#v", promise)
	}

	read := func(card map[string]any) map[string]any {
		t.Helper()
		item, err := store.GetWork(ctx, anyStringValue(card["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if err = store.EnrichCardPlans(ctx, []map[string]any{item}, visible, now); err != nil {
			t.Fatal(err)
		}
		return item
	}
	item := read(active)
	if item["next_action"] != "Check the loop ledger" {
		t.Fatalf("next_action = %#v", item["next_action"])
	}
	health, _ := item["plan_health"].(plans.Health)
	if health.Promise == nil || health.Promise.Source != "note" || !strings.Contains(health.Reason, "Check the loop ledger") || !strings.Contains(health.Reason, "2026-10-21") || strings.Contains(health.Reason, "2026-10-16") {
		t.Fatalf("plan_health = %+v", health)
	}
	summary, _ := item["work_summary"].(*WorkSummary)
	if summary == nil || summary.Status.Reason != health.Reason || summary.Next == nil || summary.Next.Title != "Check the loop ledger" {
		t.Fatalf("summary = %+v", summary)
	}
	flag, _ := item["possibly_moot"].(PossiblyMoot)
	if !flag.Flagged || flag.Code != nextAskDecidedCode {
		t.Fatalf("card moot = %+v", flag)
	}

	reviewItem := read(review)
	reviewFlag, _ := reviewItem["possibly_moot"].(PossiblyMoot)
	if !reviewFlag.Flagged || reviewFlag.Code != nextAskDecidedCode {
		t.Fatalf("review moot = %+v", reviewFlag)
	}
	blockedItem := read(blocked)
	blockedFlag, _ := blockedItem["possibly_moot"].(PossiblyMoot)
	if blockedFlag.Flagged || blockedFlag.Code != "" || blockedFlag.AskRef != "" {
		t.Fatalf("blocked moot = %+v", blockedFlag)
	}
	if read(onlyNote)["next_action"] != nil && read(onlyNote)["next_action"] != "" {
		t.Fatalf("note-only next_action = %#v", read(onlyNote)["next_action"])
	}
	later := read(answeredFirst)
	if later["next_action"] != "Go left" {
		t.Fatalf("newer answer = %#v", later["next_action"])
	}
	laterHealth, _ := later["plan_health"].(plans.Health)
	if laterHealth.Promise == nil || laterHealth.Promise.Source != "note" || laterHealth.Promise.What != "Keep the draft" {
		t.Fatalf("newer answer must not replace the promise: %+v", laterHealth)
	}
}

func initiativePromise(t *testing.T, brief map[string]any, title string) map[string]any {
	t.Helper()
	items, _ := brief["initiatives"].([]map[string]any)
	for _, item := range items {
		if item["title"] == title {
			promise, _ := item["promise"].(map[string]any)
			if promise == nil {
				t.Fatalf("%s promise missing: %#v", title, item)
			}
			return promise
		}
	}
	t.Fatalf("initiative %q missing", title)
	return nil
}
