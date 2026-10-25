package storage

import (
	"context"
	"database/sql"
)

// Only bookkeeping is installed at upgrade. Existing cards are admitted by
// the bounded clock cursor; upgrading never assigns a historical cadence.
func installWorkTracking(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS work_tracking (
		 card_id TEXT PRIMARY KEY,
		 default_at TEXT NOT NULL DEFAULT '',
		 reminder_event_id TEXT NOT NULL DEFAULT '',
		 reminder_sent_at TEXT NOT NULL DEFAULT '',
		 orphaned_at TEXT NOT NULL DEFAULT '',
		 fallback_owner TEXT NOT NULL DEFAULT '',
		 next_digest_at TEXT NOT NULL DEFAULT '',
		 needs_owner INTEGER NOT NULL DEFAULT 0 CHECK(needs_owner IN (0,1)),
		 updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_tracking_default ON work_tracking(default_at,card_id) WHERE default_at<>'' AND reminder_event_id=''`,
		`CREATE INDEX IF NOT EXISTS idx_work_tracking_reminders ON work_tracking(card_id) WHERE reminder_event_id<>'' AND reminder_sent_at=''`,
		`CREATE INDEX IF NOT EXISTS idx_work_tracking_digest ON work_tracking(next_digest_at,fallback_owner,card_id) WHERE next_digest_at<>''`,
		`CREATE INDEX IF NOT EXISTS idx_work_tracking_owner_digest ON work_tracking(fallback_owner,next_digest_at,card_id) WHERE next_digest_at<>''`,
		`CREATE INDEX IF NOT EXISTS idx_work_tracking_needs_owner ON work_tracking(card_id) WHERE needs_owner=1`,
		// Routing positions expose keys only. Request readers admit 200 physical
		// positions before canonical card/tracking hydration; no denied refill.
		`CREATE VIEW IF NOT EXISTS work_tracking_digest_positions AS SELECT card_id,fallback_owner,next_digest_at FROM work_tracking WHERE next_digest_at<>''`,
		`CREATE TABLE IF NOT EXISTS work_tracking_cursor (singleton INTEGER PRIMARY KEY CHECK(singleton=1), after_card_id TEXT NOT NULL DEFAULT '', complete INTEGER NOT NULL DEFAULT 0)`,
		`INSERT INTO work_tracking_cursor(singleton) VALUES(1) ON CONFLICT DO NOTHING`,
		// The canonical fold also sees linked-step/source evidence. Cancel the
		// default's pending routes when that fold meets or supersedes it.
		`CREATE TRIGGER IF NOT EXISTS trg_work_tracking_fold_insert AFTER INSERT ON brief_initiatives WHEN NEW.computed_at<>'' AND COALESCE(NEW.promise_source,'')<>'default' BEGIN UPDATE work_tracking SET default_at='',reminder_sent_at=CASE WHEN reminder_event_id<>'' AND reminder_sent_at='' THEN 'canceled' ELSE reminder_sent_at END,next_digest_at=CASE WHEN orphaned_at='' THEN '' ELSE next_digest_at END WHERE card_id=NEW.card_id AND default_at<>''; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_tracking_fold_update AFTER UPDATE OF promise_source,promise_at ON brief_initiatives WHEN NEW.computed_at<>'' AND COALESCE(NEW.promise_source,'')<>'default' BEGIN UPDATE work_tracking SET default_at='',reminder_sent_at=CASE WHEN reminder_event_id<>'' AND reminder_sent_at='' THEN 'canceled' ELSE reminder_sent_at END,next_digest_at=CASE WHEN orphaned_at='' THEN '' ELSE next_digest_at END WHERE card_id=NEW.card_id AND default_at<>''; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_tracking_delete AFTER DELETE ON cards BEGIN DELETE FROM work_tracking WHERE card_id=OLD.id; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_tracking_terminal AFTER UPDATE OF column_key,archived_at,trashed_at ON cards WHEN NEW.column_key IN ('done','cancelled') OR NEW.archived_at IS NOT NULL OR NEW.trashed_at IS NOT NULL BEGIN UPDATE work_tracking SET default_at='',next_digest_at='',needs_owner=0,reminder_sent_at=CASE WHEN reminder_event_id<>'' AND reminder_sent_at='' THEN 'canceled' ELSE reminder_sent_at END WHERE card_id=NEW.id; END`,
		`CREATE TRIGGER IF NOT EXISTS trg_work_tracking_assignment AFTER UPDATE OF assignee ON cards WHEN COALESCE(OLD.assignee,'')<>COALESCE(NEW.assignee,'') BEGIN UPDATE work_tracking SET default_at='',orphaned_at='',fallback_owner='',next_digest_at='',needs_owner=CASE WHEN COALESCE(NEW.assignee,'')='' THEN 1 ELSE 0 END,reminder_sent_at=CASE WHEN reminder_event_id<>'' AND reminder_sent_at='' THEN 'canceled' ELSE reminder_sent_at END WHERE card_id=NEW.id; END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
