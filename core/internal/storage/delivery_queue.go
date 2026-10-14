package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Delivery queue (migration 93).
//
// deliveries is the one durable queue for everything core hands to a
// principal outside a request: agent wakes (channel "bridge", kind "wake") and
// human notifications (channels declared by adapters, kinds "push" and
// "digest"). Rows are keyed by an idempotency key, retried with backoff and
// closed by receipts in delivery_receipts. Agent wakes keep their detailed
// packet rows in agent_wakeups and the bridge keeps claiming them there;
// triggers mirror every wake insert and status change into this queue in the
// same transaction, so the queue and its receipts cover wakes without a second
// write path. Signed ask webhooks are mirrored the same way (channel
// "webhook").
//
// ask_deadlines is the bounded, indexed open-ask ledger the escalation clock
// and the heartbeat read. Triggers add a row when an ask event is appended and
// delete it when the ask is resolved or trashed, so the sweep never scans
// events. decide_by is normalized in Go (the review_by grammar includes
// durations relative to the ask time); rows with normalized=0 are pending.
var deliveryQueueStatements = []string{
	`CREATE TABLE IF NOT EXISTS deliveries (
		delivery_id TEXT PRIMARY KEY,
		idempotency_key TEXT NOT NULL UNIQUE,
		principal_id TEXT NOT NULL,
		channel TEXT NOT NULL,
		kind TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('pending','claimed','delivered','failed','canceled')),
		payload_json TEXT NOT NULL DEFAULT '{}',
		source_ref TEXT NOT NULL DEFAULT '',
		attempts INTEGER NOT NULL DEFAULT 0,
		max_attempts INTEGER NOT NULL DEFAULT 6,
		next_attempt_at TEXT NOT NULL,
		claimed_by TEXT NOT NULL DEFAULT '',
		claim_token TEXT NOT NULL DEFAULT '',
		lease_expires_at TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		delivered_at TEXT NOT NULL DEFAULT '',
		failed_at TEXT NOT NULL DEFAULT ''
	)`,
	// Claim seek: one channel's due rows in time order. Claimed rows with an
	// expired lease are reclaimable through the same index.
	`CREATE INDEX IF NOT EXISTS idx_deliveries_claim ON deliveries(channel, status, next_attempt_at, delivery_id)`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_principal ON deliveries(principal_id, created_at DESC, delivery_id DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_deliveries_source ON deliveries(source_ref, kind) WHERE source_ref<>''`,
	`CREATE TABLE IF NOT EXISTS delivery_receipts (
		receipt_id INTEGER PRIMARY KEY AUTOINCREMENT,
		delivery_id TEXT NOT NULL,
		outcome TEXT NOT NULL CHECK(outcome IN ('notification_delivered','notification_failed')),
		attempt INTEGER NOT NULL,
		error TEXT NOT NULL DEFAULT '',
		recorded_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_delivery_receipts_delivery ON delivery_receipts(delivery_id, receipt_id)`,
	// Per-workspace channel choices of one principal. address is the channel
	// identity (an email address, a Telegram chat id); only the principal and
	// the adapter delivering to it ever see it.
	`CREATE TABLE IF NOT EXISTS notification_channel_settings (
		principal_id TEXT NOT NULL,
		channel TEXT NOT NULL,
		address TEXT NOT NULL DEFAULT '',
		push_enabled INTEGER NOT NULL DEFAULT 1,
		digest_enabled INTEGER NOT NULL DEFAULT 1,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(principal_id, channel)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_notification_channel_settings_channel ON notification_channel_settings(channel, principal_id)`,
	// Channels an adapter declaration delivers. One adapter may deliver
	// several channels; several adapters may share a channel.
	`CREATE TABLE IF NOT EXISTS adapter_channels (
		adapter TEXT NOT NULL,
		channel TEXT NOT NULL,
		PRIMARY KEY(adapter, channel)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_adapter_channels_channel ON adapter_channels(channel, adapter)`,
	`CREATE TABLE IF NOT EXISTS ask_deadlines (
		request_event_id TEXT PRIMARY KEY,
		requester_actor_id TEXT NOT NULL DEFAULT '',
		ask_ts TEXT NOT NULL,
		by_raw TEXT NOT NULL DEFAULT '',
		decide_by TEXT,
		blocks_count INTEGER NOT NULL DEFAULT 0,
		normalized INTEGER NOT NULL DEFAULT 0,
		escalated_at TEXT,
		escalation TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_ask_deadlines_due ON ask_deadlines(decide_by, request_event_id) WHERE escalated_at IS NULL AND decide_by IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_ask_deadlines_pending ON ask_deadlines(request_event_id) WHERE normalized=0`,
	`CREATE INDEX IF NOT EXISTS idx_ask_deadlines_decide_by ON ask_deadlines(decide_by)`,
}

const askDeadlineSelect = `SELECT id,
		trim(COALESCE(json_extract(payload_json,'$.payload.requester_actor_id'),'')),
		ts,
		trim(COALESCE(json_extract(payload_json,'$.payload.decide_by'), json_extract(payload_json,'$.payload.by'), '')),
		CASE WHEN json_type(payload_json,'$.payload.blocks')='array' THEN json_array_length(payload_json,'$.payload.blocks') ELSE 0 END`

// Trigger inserts use ON CONFLICT DO NOTHING rather than INSERT OR IGNORE:
// an outer statement's OR-conflict policy (for example INSERT OR REPLACE)
// overrides a trigger's own OR clause, but not an upsert clause.
var deliveryQueueEventTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS ask_deadlines_on_ask AFTER INSERT ON events
	WHEN NEW.type='human_attention_requested' AND COALESCE(NEW.trashed_at,'')='' BEGIN
		INSERT INTO ask_deadlines(request_event_id, requester_actor_id, ask_ts, by_raw, blocks_count)
		VALUES(NEW.id,
			trim(COALESCE(json_extract(NEW.payload_json,'$.payload.requester_actor_id'),'')),
			NEW.ts,
			trim(COALESCE(json_extract(NEW.payload_json,'$.payload.decide_by'), json_extract(NEW.payload_json,'$.payload.by'), '')),
			CASE WHEN json_type(NEW.payload_json,'$.payload.blocks')='array' THEN json_array_length(NEW.payload_json,'$.payload.blocks') ELSE 0 END)
		ON CONFLICT DO NOTHING;
	END`,
	`CREATE TRIGGER IF NOT EXISTS ask_deadlines_on_trash AFTER UPDATE OF trashed_at ON events
	WHEN NEW.type='human_attention_requested' AND COALESCE(NEW.trashed_at,'')<>'' BEGIN
		DELETE FROM ask_deadlines WHERE request_event_id=NEW.id;
	END`,
}

var deliveryQueueResolutionTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS ask_deadlines_on_resolution AFTER INSERT ON human_attention_request_resolutions BEGIN
		DELETE FROM ask_deadlines WHERE request_event_id=NEW.request_event_id;
		UPDATE deliveries SET status='canceled', updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE source_ref='event:'||NEW.request_event_id AND kind='push' AND status='pending';
	END`,
}

// Wake status maps onto the queue status; completed/failed append a receipt.
var deliveryQueueWakeTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS deliveries_wake_insert AFTER INSERT ON agent_wakeups BEGIN
		INSERT INTO deliveries(delivery_id, idempotency_key, principal_id, channel, kind, status, payload_json, source_ref, attempts, next_attempt_at, claimed_by, created_at, updated_at, delivered_at, failed_at)
		VALUES('dlv_wake_'||NEW.wakeup_id, 'wake:'||NEW.wakeup_id, NEW.target_actor_id, 'bridge', 'wake',
			CASE NEW.status WHEN 'claimed' THEN 'claimed' WHEN 'completed' THEN 'delivered' WHEN 'failed' THEN 'failed' ELSE 'pending' END,
			json_object('wakeup_id', NEW.wakeup_id, 'thread_id', NEW.thread_id),
			CASE WHEN NEW.trigger_event_id<>'' THEN 'event:'||NEW.trigger_event_id ELSE '' END,
			CASE WHEN NEW.status IN ('claimed','completed','failed') THEN 1 ELSE 0 END,
			NEW.created_at, NEW.bridge_instance_id, NEW.created_at, NEW.updated_at,
			CASE WHEN NEW.status='completed' THEN COALESCE(NEW.completed_at,NEW.updated_at) ELSE '' END,
			CASE WHEN NEW.status='failed' THEN COALESCE(NEW.failed_at,NEW.updated_at) ELSE '' END)
		ON CONFLICT DO NOTHING;
	END`,
	`CREATE TRIGGER IF NOT EXISTS deliveries_wake_status AFTER UPDATE OF status ON agent_wakeups
	WHEN NEW.status<>OLD.status BEGIN
		UPDATE deliveries SET
			status=CASE NEW.status WHEN 'claimed' THEN 'claimed' WHEN 'completed' THEN 'delivered' WHEN 'failed' THEN 'failed' ELSE 'pending' END,
			attempts=attempts+CASE WHEN NEW.status='claimed' THEN 1 ELSE 0 END,
			claimed_by=NEW.bridge_instance_id,
			last_error=NEW.failure_reason,
			updated_at=NEW.updated_at,
			delivered_at=CASE WHEN NEW.status='completed' THEN COALESCE(NEW.completed_at,NEW.updated_at) ELSE delivered_at END,
			failed_at=CASE WHEN NEW.status='failed' THEN COALESCE(NEW.failed_at,NEW.updated_at) ELSE failed_at END
		WHERE idempotency_key='wake:'||NEW.wakeup_id;
		INSERT INTO delivery_receipts(delivery_id, outcome, attempt, error, recorded_by, created_at)
		SELECT 'dlv_wake_'||NEW.wakeup_id,
			CASE NEW.status WHEN 'completed' THEN 'notification_delivered' ELSE 'notification_failed' END,
			MAX(1, (SELECT attempts FROM deliveries WHERE idempotency_key='wake:'||NEW.wakeup_id)),
			NEW.failure_reason, NEW.bridge_instance_id, NEW.updated_at
		WHERE NEW.status IN ('completed','failed') AND EXISTS (SELECT 1 FROM deliveries WHERE idempotency_key='wake:'||NEW.wakeup_id);
	END`,
}

// Signed ask webhooks keep their sender and attempt log in ask_deliveries;
// the queue mirrors each webhook delivery and its outcome (channel "webhook").
var deliveryQueueWebhookTriggers = []string{
	`CREATE TRIGGER IF NOT EXISTS deliveries_webhook_insert AFTER INSERT ON ask_deliveries WHEN NEW.kind='webhook' BEGIN
		INSERT INTO deliveries(delivery_id, idempotency_key, principal_id, channel, kind, status, payload_json, source_ref, attempts, max_attempts, next_attempt_at, created_at, updated_at)
		SELECT 'dlv_webhook_'||NEW.id, 'webhook:'||NEW.id, s.actor_id, 'webhook', 'webhook', 'pending',
			json_object('ask_delivery_id', NEW.id), 'event:'||NEW.ask_id, NEW.attempts, 5, NEW.next_at, NEW.next_at, NEW.next_at
		FROM ask_subscriptions s WHERE s.id=NEW.subscription_id
		ON CONFLICT DO NOTHING;
	END`,
	`CREATE TRIGGER IF NOT EXISTS deliveries_webhook_state AFTER UPDATE ON ask_deliveries
	WHEN NEW.kind='webhook' AND (NEW.state<>OLD.state OR NEW.attempts<>OLD.attempts) BEGIN
		UPDATE deliveries SET
			status=CASE NEW.state WHEN 'delivered' THEN 'delivered' WHEN 'failed' THEN 'failed' ELSE 'pending' END,
			attempts=NEW.attempts, next_attempt_at=NEW.next_at, last_error=NEW.reason,
			updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now'),
			delivered_at=CASE WHEN NEW.state='delivered' THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE delivered_at END,
			failed_at=CASE WHEN NEW.state='failed' THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE failed_at END
		WHERE idempotency_key='webhook:'||NEW.id;
		INSERT INTO delivery_receipts(delivery_id, outcome, attempt, error, recorded_by, created_at)
		SELECT 'dlv_webhook_'||NEW.id, CASE NEW.state WHEN 'delivered' THEN 'notification_delivered' ELSE 'notification_failed' END,
			MAX(1, NEW.attempts), NEW.reason, 'webhook', strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE NEW.state<>OLD.state AND NEW.state IN ('delivered','failed') AND EXISTS (SELECT 1 FROM deliveries WHERE idempotency_key='webhook:'||NEW.id);
	END`,
}

func installDeliveryQueue(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range deliveryQueueStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("install delivery queue: %w", err)
		}
	}
	// Legacy fixtures may predate some source tables; install each trigger
	// group only where its source exists.
	if ok, err := sqliteTableExists(ctx, tx, "events"); err != nil {
		return err
	} else if ok {
		for _, statement := range deliveryQueueEventTriggers {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install ask deadline triggers: %w", err)
			}
		}
		resolutions, err := sqliteTableExists(ctx, tx, "human_attention_request_resolutions")
		if err != nil {
			return err
		}
		openFilter := ""
		if resolutions {
			openFilter = ` AND NOT EXISTS (SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=events.id)`
		}
		// One-time backfill of currently open asks.
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ask_deadlines(request_event_id, requester_actor_id, ask_ts, by_raw, blocks_count) `+
			askDeadlineSelect+` FROM events WHERE type='human_attention_requested' AND COALESCE(trashed_at,'')=''`+openFilter); err != nil {
			return fmt.Errorf("backfill ask deadlines: %w", err)
		}
	}
	if ok, err := sqliteTableExists(ctx, tx, "human_attention_request_resolutions"); err != nil {
		return err
	} else if ok {
		for _, statement := range deliveryQueueResolutionTriggers {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install ask resolution triggers: %w", err)
			}
		}
	}
	if ok, err := sqliteTableExists(ctx, tx, "ask_deliveries"); err != nil {
		return err
	} else if ok {
		for _, statement := range deliveryQueueWebhookTriggers {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install webhook delivery triggers: %w", err)
			}
		}
	}
	if ok, err := sqliteTableExists(ctx, tx, "agent_wakeups"); err != nil {
		return err
	} else if ok {
		for _, statement := range deliveryQueueWakeTriggers {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("install wake delivery triggers: %w", err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO deliveries(delivery_id, idempotency_key, principal_id, channel, kind, status, payload_json, source_ref, attempts, next_attempt_at, claimed_by, created_at, updated_at, delivered_at, failed_at)
			SELECT 'dlv_wake_'||wakeup_id, 'wake:'||wakeup_id, target_actor_id, 'bridge', 'wake',
				CASE status WHEN 'claimed' THEN 'claimed' WHEN 'completed' THEN 'delivered' WHEN 'failed' THEN 'failed' ELSE 'pending' END,
				json_object('wakeup_id', wakeup_id, 'thread_id', thread_id),
				CASE WHEN trigger_event_id<>'' THEN 'event:'||trigger_event_id ELSE '' END,
				CASE WHEN status IN ('claimed','completed','failed') THEN 1 ELSE 0 END,
				created_at, bridge_instance_id, created_at, updated_at,
				CASE WHEN status='completed' THEN COALESCE(completed_at,updated_at) ELSE '' END,
				CASE WHEN status='failed' THEN COALESCE(failed_at,updated_at) ELSE '' END
			FROM agent_wakeups`); err != nil {
			return fmt.Errorf("backfill wake deliveries: %w", err)
		}
	}
	return nil
}
