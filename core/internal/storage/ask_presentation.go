package storage

import (
	"context"
	"database/sql"
)

func installAskPresentationHeads(ctx context.Context, tx *sql.Tx) error {
	// Only pointers and a counter live here. Authored content remains in immutable
	// canonical events and inherits the original ask and subject card.
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS ask_presentation_heads(
 ask_id TEXT PRIMARY KEY REFERENCES events(id),
 revision INTEGER NOT NULL CHECK(revision>0),
 event_id TEXT NOT NULL UNIQUE REFERENCES events(id));`)
	return err
}
