package primitives

import (
	"context"
	"errors"
	"fmt"

	"agent-nexus-core/internal/resourceaccess"
)

// BackfillArtifactAccess explicitly indexes legacy content for migration probes
// and historical test fixtures. Production startup does not call it or schedule
// retries. Unknown or unavailable blobs remain fail-closed. Rows and reference
// edges publish atomically, with content hashes guarding concurrent replacement.
func (s *Store) BackfillArtifactAccess(ctx context.Context) error {
	if s.db == nil || s.blob == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,content_hash,content_type FROM artifacts WHERE content_refs_json IS NULL`)
	if err != nil {
		return err
	}
	type entry struct{ id, hash, contentType string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.hash, &e.contentType); err != nil {
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
		if _, err := s.db.ExecContext(ctx, `UPDATE artifacts SET content_refs_json=? WHERE id=? AND content_hash=? AND content_refs_json IS NULL`, resourceaccess.ContentReferenceAtomsJSON(string(body), e.contentType), e.id, e.hash); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}
