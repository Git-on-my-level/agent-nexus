package storage

import (
	"context"
	"database/sql"
)

// migration is one ordered schema step. The runner owns the ledger write; a
// chunk only reports whether its bounded work is complete.
type migration struct {
	Version    int
	Statements []string
	// AfterApply runs in the same transaction after Statements (optional).
	AfterApply func(ctx context.Context, tx *sql.Tx) error
	// ApplyChunk executes one resumable chunk in the runner's transaction.
	// The runner alone verifies completion, records Version and updates its ledger.
	ApplyChunk func(ctx context.Context, tx *sql.Tx) (done bool, err error)
	// BetweenChunks runs after an incomplete chunk commits, outside any
	// transaction. It lets a large migration checkpoint its WAL or release
	// resources before the next bounded phase.
	BetweenChunks func(ctx context.Context, db *sql.DB) error
	Verify        func(ctx context.Context, tx *sql.Tx) error
}
