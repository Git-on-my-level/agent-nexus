package storage

import (
	"context"
	"database/sql"
)

// Retirement is reversible curation, not a terminal continuation outcome.
// Keep the resolution claim for answer integrity, but never consume a resume
// registration (or schedule a webhook/runtime wake) for that claim.
func installAskRetirementDeliveryPolicy(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS ask_deliveries_resolved`,
		`CREATE TRIGGER ask_deliveries_resolved AFTER INSERT ON human_attention_request_resolutions
 WHEN NOT EXISTS(SELECT 1 FROM events WHERE id=NEW.resolution_event_id AND type='human_attention_retired') BEGIN
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id,s.id,NEW.request_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.ask_id=NEW.request_event_id AND s.enabled=1;
 INSERT OR IGNORE INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at)
 SELECT s.id||'.'||NEW.request_event_id,s.id,NEW.request_event_id,s.kind,strftime('%Y-%m-%dT%H:%M:%fZ','now')
 FROM ask_subscriptions s WHERE s.actor_id=(SELECT json_extract(payload_json,'$.payload.requester_actor_id') FROM events WHERE id=NEW.request_event_id) AND s.ask_id='' AND s.enabled=1;
 END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
