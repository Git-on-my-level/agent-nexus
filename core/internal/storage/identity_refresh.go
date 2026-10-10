package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Replace trigger definitions only: existing identity/mention rows remain valid.
// Timestamps, plans and lifecycle changes cannot alter an identity spelling.
// Ownership/content triggers and epoch invalidation remain fully conservative.
func boundIdentityRefreshes(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"boards", "cards"} {
		name := "mention_identity_" + table + "_update"
		var definition string
		err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='trigger' AND name=?`, name).Scan(&definition)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		old := "AFTER UPDATE ON " + table + " BEGIN"
		replacement := "AFTER UPDATE OF id,handle ON " + table + " WHEN OLD.id IS NOT NEW.id OR OLD.handle IS NOT NEW.handle BEGIN"
		if strings.Contains(definition, replacement) {
			continue
		}
		if !strings.Contains(definition, old) {
			return fmt.Errorf("unexpected identity trigger definition: %s", name)
		}
		if _, err = tx.ExecContext(ctx, "DROP TRIGGER "+name); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, strings.Replace(definition, old, replacement, 1)); err != nil {
			return err
		}
	}
	return nil
}
