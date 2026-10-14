package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
)

func TestEscalationPushesOverdueAskAndWakesPM(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)

	ask, err := store.AppendEvent(ctx, "series-owner-actor", map[string]any{
		"type": "human_attention_requested", "thread_id": "thread-escalate", "refs": []string{"thread:thread-escalate"},
		"payload": map[string]any{
			"kind": "ask", "title": "Merge battery-v2?", "requester_actor_id": "series-owner-actor",
			"subject_ref": "thread:thread-escalate", "response_proposals": []string{"Merge"}, "by": "1h",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	askID := anyString(ask["id"])
	if _, err := db.Exec(`INSERT INTO notification_channel_settings(principal_id,channel,address,push_enabled,digest_enabled,updated_at) VALUES('series-human','email','admin@example.com',1,1,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// A private ask: its subject card sits on a board whose thread is owned.
	board, err := store.CreateBoard(ctx, "series-owner-actor", map[string]any{"title": "private board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateBoardCard(ctx, "series-owner-actor", anyString(board["id"]), primitives.AddBoardCardInput{Title: "private card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchThread(ctx, "series-owner-actor", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "series-owner-actor"}, nil); err != nil {
		t.Fatal(err)
	}
	privateRef := "card:" + anyString(card.Card["id"])
	privateAsk, err := store.AppendEvent(ctx, "series-owner-actor", map[string]any{
		"type": "human_attention_requested", "thread_id": "thread-private", "refs": []string{"thread:thread-private", privateRef},
		"payload": map[string]any{
			"kind": "ask", "title": "Confidential private ask", "requester_actor_id": "series-owner-actor",
			"subject_ref": privateRef, "related_refs": []string{privateRef}, "response_proposals": []string{"Yes"}, "by": "30m",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	privateAskID := anyString(privateAsk["id"])

	readCheck := NewEscalationReadCheck(store)
	if readCheck(ctx, "series-human", privateAskID) {
		t.Fatal("private ask must not be readable by another principal")
	}
	if !readCheck(ctx, "series-human", askID) {
		t.Fatal("public ask should be readable by the workspace human")
	}
	if readCheck(ctx, "series-human", "missing-event") || readCheck(ctx, "", askID) {
		t.Fatal("read check must fail closed")
	}
	pmWaker := NewEscalationPMWaker(db, store, env.authStore, func() string { return "series-owner-actor" }, readCheck, "ws_test", "test")
	if pmWaker == nil {
		t.Fatal("PM waker not constructed")
	}
	escalator := &delivery.Escalator{Store: delivery.Store{DB: db}, WorkspaceName: "test", CanRead: readCheck, AskPM: pmWaker, DigestHourUTC: 23}
	now := time.Now().UTC().Add(2 * time.Hour)
	if now.Hour() >= 23 {
		escalator.DigestHourUTC = 0
	}
	if err := escalator.Step(ctx, now); err != nil {
		t.Fatal(err)
	}
	var title, principal string
	if err := db.QueryRow(`SELECT json_extract(payload_json,'$.title'), principal_id FROM deliveries WHERE kind='push' AND source_ref=?`, "event:"+askID).Scan(&title, &principal); err != nil {
		t.Fatalf("push delivery missing: %v", err)
	}
	if title != "Merge battery-v2?" || principal != "series-human" {
		t.Fatalf("push payload: %q %q", title, principal)
	}
	var leaked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE kind IN ('push','digest') AND (source_ref=? OR payload_json LIKE '%Confidential%')`, "event:"+privateAskID).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("private ask leaked into a delivery: %d %v", leaked, err)
	}
	var wakes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE kind='wake' AND channel='bridge' AND principal_id='series-owner-actor' AND source_ref=?`, "event:"+askID).Scan(&wakes); err != nil || wakes != 1 {
		t.Fatalf("PM wake not queued through the delivery queue: %d %v", wakes, err)
	}
	// A second pass is idempotent.
	if err := escalator.Step(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE source_ref=?`, "event:"+askID).Scan(&total); err != nil || total != 2 {
		t.Fatalf("escalation repeated: %d %v", total, err)
	}

	// Answering the ask removes it from the ledger and the heartbeat counts.
	summary, err := (delivery.Store{DB: db}).AttentionSummary(ctx, now)
	if err != nil || summary["open_decisions"] != 2 || summary["overdue_decisions"] != 2 {
		t.Fatalf("summary before answer: %#v %v", summary, err)
	}
	if _, err := db.Exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,?,?,?)`, askID, "answer-1", "answered", "series-human", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	summary, err = (delivery.Store{DB: db}).AttentionSummary(ctx, now)
	if err != nil || summary["open_decisions"] != 1 {
		t.Fatalf("summary after answer: %#v %v", summary, err)
	}
}
