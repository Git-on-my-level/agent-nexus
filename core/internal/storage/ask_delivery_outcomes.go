package storage

import (
	"context"
	"database/sql"
)

// Each outcome owns its deliveries, including their webhook idempotency keys
// and bridge wake IDs. An undone retirement must not occupy a later answer's
// slot. Released legacy deliveries keep their empty outcome and original ID.
func installAskDeliveryOutcomes(ctx context.Context, tx *sql.Tx) error {
	hasOutcome, err := sqliteTableHasColumn(ctx, tx, "ask_deliveries", "resolution_event_id")
	if err != nil {
		return err
	}
	if !hasOutcome {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE ask_deliveries ADD COLUMN resolution_event_id TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	for _, q := range []string{
		`CREATE INDEX IF NOT EXISTS ask_deliveries_outcome ON ask_deliveries(ask_id,resolution_event_id,id)`,
		`DROP TRIGGER IF EXISTS deliveries_webhook_insert`,
		`CREATE TRIGGER deliveries_webhook_insert AFTER INSERT ON ask_deliveries WHEN NEW.kind='webhook' BEGIN
 INSERT INTO deliveries(delivery_id,idempotency_key,principal_id,channel,kind,status,payload_json,source_ref,attempts,max_attempts,next_attempt_at,created_at,updated_at)
 SELECT 'dlv_webhook_'||NEW.id,'webhook:'||NEW.id,s.actor_id,'webhook','webhook','pending',json_object('ask_delivery_id',NEW.id),
 'event:'||CASE WHEN NEW.resolution_event_id<>'' THEN NEW.resolution_event_id ELSE NEW.ask_id END,NEW.attempts,5,NEW.next_at,NEW.next_at,NEW.next_at
 FROM ask_subscriptions s WHERE s.id=NEW.subscription_id ON CONFLICT DO NOTHING;
 END`,
		`DROP TRIGGER IF EXISTS ask_deliveries_resolved`,
		`CREATE TRIGGER ask_deliveries_resolved AFTER INSERT ON human_attention_request_resolutions BEGIN
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,resolution_event_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id||'.'||NEW.resolution_event_id,s.id,NEW.request_event_id,NEW.resolution_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.ask_id=NEW.request_event_id AND s.enabled=1;
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,resolution_event_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id||'.'||NEW.resolution_event_id,s.id,NEW.request_event_id,NEW.resolution_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.actor_id=(SELECT json_extract(payload_json,'$.payload.requester_actor_id') FROM events WHERE id=NEW.request_event_id) AND s.ask_id='' AND s.enabled=1;
 END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
