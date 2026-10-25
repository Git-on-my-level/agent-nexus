package storage

import (
	"context"
	"testing"
)

func TestAskDeliveryOutcomeUpgradeAndReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema upgrade and reopen")
	}
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 123)
	for _, q := range []string{
		`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('outcome-ask','human_attention_requested','2026-01-01','requester','[]','{"payload":{"title":"Ask","requester_actor_id":"requester"}}'),('outcome-retire','human_attention_retired','2026-01-02','requester','[]','{}'),('outcome-answer','human_attention_responded','2026-01-03','requester','[]','{}')`,
		`INSERT INTO ask_subscriptions(id,actor_id,ask_id,kind,label,created_at) VALUES('outcome-sub','requester','outcome-ask','bridge','resume','2026-01-01')`,
		`INSERT INTO human_attention_request_resolutions VALUES('outcome-ask','outcome-retire','withdrawn','requester','2026-01-02')`,
		`UPDATE ask_deliveries SET state='delivered' WHERE ask_id='outcome-ask'`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the old delivered record and wake; a later answer gets new identities.
	for _, q := range []string{`DELETE FROM human_attention_request_resolutions WHERE request_event_id='outcome-ask'`, `INSERT INTO human_attention_request_resolutions VALUES('outcome-ask','outcome-answer','answered','human','2026-01-03')`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ws.Close()
	ws, err = InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var count int
	for _, q := range []string{
		`SELECT count(*) FROM ask_deliveries WHERE id='outcome-sub.outcome-ask' AND resolution_event_id='' AND state='delivered'`,
		`SELECT count(*) FROM ask_deliveries WHERE id='outcome-sub.outcome-ask.outcome-answer' AND resolution_event_id='outcome-answer' AND state='pending'`,
		`SELECT count(*) FROM agent_wakeups WHERE wakeup_id='ask-delivery-outcome-sub.outcome-ask.outcome-answer' AND trigger_event_id='outcome-answer' AND target_actor_id='requester'`,
	} {
		if err = ws.DB().QueryRow(q).Scan(&count); err != nil || count != 1 {
			t.Fatal(q, count, err)
		}
	}
}
