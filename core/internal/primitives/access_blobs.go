package primitives

import (
	"context"
	"errors"
	"fmt"

	"agent-nexus-core/internal/resourceaccess"
)

// BackfillArtifactAccess runs before a store is handed to readers and works with
// every blob backend. Unknown or unavailable old blobs remain fail-closed in the
// access graph; a later startup can retry them. Rows and reference edges publish
// atomically, and content hashes make the scan independent of mutable metadata.
func (s *Store) BackfillArtifactAccess(ctx context.Context) error {
	if s.db == nil || s.blob == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,content_hash FROM artifacts WHERE content_refs_json IS NULL`)
	if err != nil {
		return err
	}
	type entry struct{ id, hash string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.hash); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var failures []error
	for _, e := range entries {
		body, err := s.blob.Read(ctx, e.hash)
		if err != nil {
			failures = append(failures, fmt.Errorf("index artifact content %s: %w", e.id, err))
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=? WHERE id=? AND content_hash=? AND content_refs_json IS NULL`, resourceaccess.ReferenceAtomsJSON(string(body)), e.id, e.hash); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}
