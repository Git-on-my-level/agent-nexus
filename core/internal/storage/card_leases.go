package storage

import (
	"context"
	"database/sql"
)

// Card work leases (migration 117).
//
// card_leases holds at most one lease row per card: the current or last
// holder, its epoch (the fencing token, incremented whenever a different
// tenure starts), the TTL and the expiry instant. Rows inherit the card's
// visibility. Lease state is computed from expires_at and released_at on
// read, so correctness never depends on the expiry clock; the clock only
// records one card_lease_expired event per epoch (expired_event_id) and wakes
// the card's agent assignee. Timestamps are fixed-width UTC so lexical order
// equals time order for the partial due index.
func installCardLeases(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS card_leases (
			card_id TEXT PRIMARY KEY,
			owner TEXT NOT NULL,
			epoch INTEGER NOT NULL CHECK(epoch >= 1),
			holder_actor_id TEXT NOT NULL,
			ttl_seconds INTEGER NOT NULL,
			acquired_at TEXT NOT NULL,
			renewed_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			released_at TEXT NOT NULL DEFAULT '',
			expired_event_id TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		// Expiry clock seek: unreleased leases whose expiry is not yet recorded.
		`CREATE INDEX IF NOT EXISTS idx_card_leases_due ON card_leases(expires_at, card_id) WHERE released_at='' AND expired_event_id=''`,
		`CREATE TRIGGER IF NOT EXISTS trg_card_leases_card_delete AFTER DELETE ON cards BEGIN DELETE FROM card_leases WHERE card_id=OLD.id; END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
