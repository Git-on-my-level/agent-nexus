package storage

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
)

func installAskSubscriptions(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"events", "agent_wakeup_stream", "human_attention_request_resolutions"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS agent_wakeup_actor_positions(seq INTEGER PRIMARY KEY,actor_id TEXT NOT NULL,wakeup_id TEXT NOT NULL,resume_token TEXT NOT NULL DEFAULT (lower(hex(randomblob(24)))))`,
		`CREATE VIEW IF NOT EXISTS ask_wake_snapshot_positions AS SELECT target_actor_id,notification_status,created_at,wakeup_id FROM agent_wakeups`,
		`CREATE UNIQUE INDEX IF NOT EXISTS agent_wakeup_actor_token ON agent_wakeup_actor_positions(resume_token)`,
		`CREATE INDEX IF NOT EXISTS agent_wakeup_actor_seq ON agent_wakeup_actor_positions(actor_id,seq)`,
		`CREATE TRIGGER IF NOT EXISTS agent_wakeup_actor_append AFTER INSERT ON agent_wakeup_stream BEGIN INSERT INTO agent_wakeup_actor_positions(seq,actor_id,wakeup_id) SELECT NEW.seq,target_actor_id,NEW.wakeup_id FROM agent_wakeups WHERE wakeup_id=NEW.wakeup_id; END`,
		`CREATE TABLE IF NOT EXISTS ask_subscriptions(id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,ask_id TEXT NOT NULL DEFAULT '',kind TEXT NOT NULL CHECK(kind IN ('await','bridge','webhook')),label TEXT NOT NULL,endpoint TEXT NOT NULL DEFAULT '',secret BLOB,nonce BLOB,created_at TEXT NOT NULL,enabled INTEGER NOT NULL DEFAULT 1)`,
		`CREATE INDEX IF NOT EXISTS ask_subscriptions_ask ON ask_subscriptions(ask_id,enabled,id)`,
		`CREATE INDEX IF NOT EXISTS ask_subscriptions_actor ON ask_subscriptions(actor_id,ask_id,enabled,id)`,
		`CREATE TABLE IF NOT EXISTS ask_deliveries(id TEXT PRIMARY KEY,subscription_id TEXT NOT NULL,ask_id TEXT NOT NULL,kind TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'pending',attempts INTEGER NOT NULL DEFAULT 0,next_at TEXT NOT NULL,last_at TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',status_code INTEGER NOT NULL DEFAULT 0,latency_ms INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX IF NOT EXISTS ask_deliveries_due ON ask_deliveries(kind,state,next_at,id)`,
		`CREATE INDEX IF NOT EXISTS ask_deliveries_ask ON ask_deliveries(ask_id,id)`,
		`CREATE TRIGGER IF NOT EXISTS ask_deliveries_resolved AFTER INSERT ON human_attention_request_resolutions BEGIN
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id,s.id,NEW.request_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.ask_id=NEW.request_event_id AND s.enabled=1;
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id,s.id,NEW.request_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.actor_id=(SELECT json_extract(payload_json,'$.payload.requester_actor_id') FROM events WHERE id=NEW.request_event_id) AND s.ask_id='' AND s.enabled=1;
 END`,
		`CREATE TABLE IF NOT EXISTS ask_delivery_attempts(delivery_id TEXT NOT NULL,attempt INTEGER NOT NULL,status_code INTEGER NOT NULL,latency_ms INTEGER NOT NULL,at TEXT NOT NULL,PRIMARY KEY(delivery_id,attempt))`,
		`CREATE TRIGGER IF NOT EXISTS ask_bridge_wake AFTER INSERT ON ask_deliveries WHEN NEW.kind='bridge' BEGIN
 INSERT OR IGNORE INTO agent_wakeups(wakeup_id,status,target_handle,target_actor_id,thread_id,trigger_event_id,trigger_created_at,refs_json,created_at,updated_at)
 SELECT 'ask-delivery-'||NEW.id,'requested',s.actor_id,s.actor_id,COALESCE(e.thread_id,''),r.resolution_event_id,NEW.next_at,json_array('event:'||NEW.ask_id,'event:'||r.resolution_event_id),NEW.next_at,NEW.next_at
 FROM ask_subscriptions s JOIN events e ON e.id=NEW.ask_id JOIN human_attention_request_resolutions r ON r.request_event_id=NEW.ask_id WHERE s.id=NEW.subscription_id;
 END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{{Table: "ask_subscriptions", Kind: "filter/ask_subscriptions", ID: "id", Columns: []string{"label", "endpoint"}}, {Table: "ask_deliveries", Kind: "filter/ask_deliveries", ID: "id", Columns: []string{"reason"}}})
}
