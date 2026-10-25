package storage

import (
	"context"
	"testing"
)

func TestAskRetirementDeliveryPolicyUpgradeAndReopen(t *testing.T) {
	if testing.Short() {
		t.Skip("released-schema upgrade and reopen")
	}
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 123)
	db.Close()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('retire-ask','human_attention_requested','2026-01-01','requester','[]','{"payload":{"title":"Ask","requester_actor_id":"requester"}}'),('retire-event','human_attention_retired','2026-01-02','requester','[]','{}'),('answer-event','human_attention_responded','2026-01-03','human','[]','{}')`,
		`INSERT INTO ask_subscriptions(id,actor_id,ask_id,kind,label,created_at) VALUES('resume-sub','requester','retire-ask','bridge','resume','2026-01-01')`,
		`INSERT INTO human_attention_request_resolutions VALUES('retire-ask','retire-event','withdrawn','requester','2026-01-02')`,
	} {
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
	for _, q := range []string{`SELECT count(*) FROM ask_deliveries`, `SELECT count(*) FROM agent_wakeups`} {
		var n int
		if err = ws.DB().QueryRow(q).Scan(&n); err != nil || n != 0 {
			t.Fatal(q, n, err)
		}
	}
	for _, q := range []string{`DELETE FROM human_attention_request_resolutions WHERE request_event_id='retire-ask'`, `INSERT INTO human_attention_request_resolutions VALUES('retire-ask','answer-event','answered','human','2026-01-03')`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM agent_wakeups WHERE wakeup_id='ask-delivery-resume-sub.retire-ask' AND trigger_event_id='answer-event' AND target_actor_id='requester'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
