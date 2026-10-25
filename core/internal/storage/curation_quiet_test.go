package storage

import (
	"context"
	"testing"
)

func TestCurationUpgradeWithdrawsLegacyAsks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 118)
	// One ordinary ask, one open proposal, one already-answered proposal and
	// one orphaned proposal whose dedupe-row insertion failed.
	for _, id := range []string{"ordinary", "proposal", "answered", "orphan"} {
		curation := `{}`
		if id != "ordinary" {
			curation = `{"curation":{"action":"batch"}}`
		}
		if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'human_attention_requested','2026-10-25T00:00:00Z','system','[]',json_object('payload',json(?)))`, id, curation); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('answer','human_attention_responded','2026-10-25T00:01:00Z','human','[]','{}');
 INSERT INTO human_attention_request_resolutions VALUES('answered','answer','answered','human','2026-10-25T00:01:00Z');
 INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,created_at) VALUES('k','proposal','batch','formula','open','2026-10-25T00:00:00Z');
 INSERT INTO deliveries(delivery_id,idempotency_key,principal_id,channel,kind,status,payload_json,source_ref,next_attempt_at,created_at,updated_at) VALUES('push','push','human','test','push','pending','{}','event:proposal','now','now','now');`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	for i := 0; i < 2; i++ {
		for _, id := range []string{"proposal", "orphan"} {
			var kind, reason string
			if err = db.QueryRow(`SELECT r.resolution_type,json_extract(e.payload_json,'$.payload.reason') FROM human_attention_request_resolutions r JOIN events e ON e.id=r.resolution_event_id WHERE r.request_event_id=?`, id).Scan(&kind, &reason); err != nil {
				t.Fatal(id, err)
			}
			if kind != "withdrawn" || reason == "" {
				t.Fatal(id, kind, reason)
			}
		}
		var ordinary, withdrawals int
		var answered, status, delivery string
		if err = db.QueryRow(`SELECT count(*) FROM human_attention_request_resolutions WHERE request_event_id='ordinary'`).Scan(&ordinary); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT count(*) FROM events WHERE type='human_attention_withdrawn'`).Scan(&withdrawals); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT resolution_event_id FROM human_attention_request_resolutions WHERE request_event_id='answered'`).Scan(&answered); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT status FROM curation_proposals WHERE key='k'`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRow(`SELECT status FROM deliveries WHERE delivery_id='push'`).Scan(&delivery); err != nil {
			t.Fatal(err)
		}
		if ordinary != 0 || withdrawals != 2 || answered != "answer" || status != "dismissed" || delivery != "canceled" {
			t.Fatal(ordinary, withdrawals, answered, status, delivery)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = quietCurationSuggestions(ctx, tx); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}
