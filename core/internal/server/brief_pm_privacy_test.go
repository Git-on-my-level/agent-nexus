package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Other owners' PM-private asks and cards must not fill the brief windows,
// and their asks must not show up in blocked or waiting counts.
func TestBriefPMPrivateRowsStayOutOfLimitAndCounts(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	work := func(title, owner string) map[string]any {
		t.Helper()
		w, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": title, "initiative": true})
		if err != nil {
			t.Fatal(err)
		}
		if owner != "" {
			if _, err = store.PatchThread(ctx, "actor-1", anyString(w["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
				t.Fatal(err)
			}
		}
		return w
	}
	note := func(card map[string]any, at string) {
		t.Helper()
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "message_posted", "thread_id": card["thread_id"],
			"refs":    []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])},
			"payload": map[string]any{"text": "progress", "next": map[string]any{"at": at, "what": "ship"}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	visible := work("Visible card", "")
	own := work("Own card", "reader")
	note(visible, "2026-06-01")
	note(own, "2026-06-02")
	for i := 0; i < primitives.BriefSectionLimit+1; i++ {
		card := work(fmt.Sprintf("Hidden card %d", i), "other")
		note(card, "2026-01-01")
	}
	ask := func(thread map[string]any, title, by, subject string, blocks []any) {
		t.Helper()
		payload := map[string]any{
			"kind": "ask", "title": title, "requester_actor_id": "agent-1", "by": by,
		}
		if subject != "" {
			payload["subject_ref"] = subject
		}
		if blocks != nil {
			payload["blocks"] = blocks
		}
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": thread["thread_id"],
			"refs": []string{"thread:" + anyString(thread["thread_id"])}, "summary": title, "payload": payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	hiddenThread := work("Hidden ask thread", "other")
	ownThread := work("Own ask thread", "reader")
	for i := 0; i < 60; i++ {
		ask(hiddenThread, fmt.Sprintf("Hidden ask %d", i), "2026-01-02T00:00:00Z", anyString(visible["ref"]), nil)
	}
	ask(hiddenThread, "Hidden block", "2026-01-03T00:00:00Z", "thread:unrelated", []any{visible["ref"]})
	ask(visible, "Visible ask", "2026-06-01T00:00:00Z", anyString(visible["ref"]), nil)
	ask(ownThread, "Own ask", "2026-06-02T00:00:00Z", anyString(own["ref"]), nil)

	for drained := false; !drained; {
		var err error
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	body, err := store.Brief(ctx, primitives.BriefReader{
		ActorID: "reader", Kind: "human",
		Visible: func(_, owner string) bool { return owner == "" || owner == "reader" },
	}, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	titles := map[string]bool{}
	for _, raw := range body["decisions"].([]map[string]any) {
		titles[anyString(raw["title"])] = true
		if len(anyString(raw["title"])) >= 10 && anyString(raw["title"])[:10] == "Hidden ask" {
			t.Fatalf("hidden ask in decisions: %v", raw["title"])
		}
	}
	if !titles["Visible ask"] || !titles["Own ask"] {
		t.Fatalf("readable asks missing from decisions: %v", titles)
	}
	states := map[string]map[string]any{}
	for _, item := range body["initiatives"].([]map[string]any) {
		title := anyString(item["title"])
		states[title] = item
		if len(title) >= 6 && title[:6] == "Hidden" {
			t.Fatalf("hidden card in initiatives: %s", title)
		}
	}
	if states["Visible card"] == nil || states["Own card"] == nil {
		t.Fatalf("readable cards missing: %v", states)
	}
	card := states["Visible card"]
	if card["blocked_count"] != 1 || card["waiting_on_you_count"] != 1 {
		t.Fatalf("counts disclose or drop visible asks: blocked=%v waiting=%v", card["blocked_count"], card["waiting_on_you_count"])
	}
}
