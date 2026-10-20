package storage

import (
	"context"
	"database/sql"
	"errors"

	"agent-nexus-core/internal/sqliteutil"
)

// inboxLifecycleRefBudget bounds the ref array elements one item-phase chunk
// handles inside its write transaction. The 200-item cap alone does not bound
// work: one item may carry a huge ref array.
const inboxLifecycleRefBudget = 2000

// inboxLifecycleItemRefs is an upper bound on the elements an item's refs SQL
// expands (whichever of related_refs or legacy refs it uses).
const inboxLifecycleItemRefs = `max(COALESCE(json_array_length(data_json,'$.related_refs'),0),COALESCE(json_array_length(data_json,'$.refs'),0))`

// inboxLifecycleInlineBytes is the largest item (by data_json bytes) the
// write chunk expands inline. Larger items take the split path even when
// they have few refs, so no write transaction parses a large document.
const inboxLifecycleInlineBytes = 64 << 10

// inboxLifecycleCandidate is an item sized by a read before the write chunk.
type inboxLifecycleCandidate struct {
	id    string
	bytes int64
	refs  int
	ready bool
}

// classifyInboxLifecycleItems sizes the next candidates in one read
// statement that holds no write lock. octet_length comes from the record
// header; json_array_length parses the document, outside the lock.
func (w *Workspace) classifyInboxLifecycleItems(ctx context.Context, cursor string, limit int) ([]inboxLifecycleCandidate, error) {
	rows, err := w.db.QueryContext(context.WithoutCancel(ctx), `SELECT id,octet_length(data_json),`+inboxLifecycleItemRefs+`,lifecycle_ready FROM derived_inbox_items WHERE id>? ORDER BY id LIMIT ?`, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	candidates := []inboxLifecycleCandidate{}
	for rows.Next() {
		var c inboxLifecycleCandidate
		var bytes sql.NullInt64
		if err = rows.Scan(&c.id, &bytes, &c.refs, &c.ready); err != nil {
			return nil, err
		}
		c.bytes = bytes.Int64
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}

// maintainInboxLifecycleItems indexes the planned candidates that fit the
// budget in the caller's write transaction, with the trigger's SQL. Each is
// re-checked first by its data_json byte length (O(1), from the record
// header); a changed or missing item ends the chunk so it is re-planned. It
// returns the new cursor (the last complete item) and whether the phase
// finished.
//
// A first candidate too large for inline expansion becomes the job's split
// item instead (split_id); splitInboxLifecycleItem then indexes it slice by
// slice. That costs one write of the one-row job table, not of the item.
func maintainInboxLifecycleItems(ctx context.Context, tx *sql.Tx, cursor string, candidates []inboxLifecycleCandidate, limit, budget int) (string, bool, error) {
	remaining := budget
	for i, c := range candidates {
		var bytes sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT octet_length(data_json) FROM derived_inbox_items WHERE id=?`, c.id).Scan(&bytes)
		if errors.Is(err, sql.ErrNoRows) {
			cursor = c.id // deleted since planning: nothing to index
			continue
		}
		if err != nil {
			return cursor, false, err
		}
		if bytes.Int64 != c.bytes {
			return cursor, false, nil // rewritten since planning; its trigger indexed it
		}
		left := max(c.refs, 1)
		if left > budget || c.bytes > inboxLifecycleInlineBytes {
			if i > 0 {
				return cursor, false, nil // the next chunk starts with it
			}
			if c.ready {
				// Its own trigger fully indexed it after the owner phases.
				cursor = c.id
				continue
			}
			_, err = tx.ExecContext(ctx, `UPDATE inbox_lifecycle_job SET split_id=?,split_offset=0,split_hidden=0 WHERE singleton=1`, c.id)
			return cursor, false, err
		}
		if left > remaining {
			return cursor, false, nil
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO inbox_lifecycle_refs `+inboxLifecycleRefsSQL("i.id=?"), c.id, c.id, c.id); err != nil {
			return cursor, false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE derived_inbox_items SET lifecycle_hidden=`+inboxLifecycleHiddenSQL()+`,lifecycle_ready=1 WHERE id=?`, c.id); err != nil {
			return cursor, false, err
		}
		remaining -= left
		cursor = c.id
	}
	return cursor, len(candidates) < limit, nil
}

// inboxLifecycleSlice is one budget-sized slice of an oversized item's refs,
// read in a statement snapshot outside any write transaction.
type inboxLifecycleSlice struct {
	found, explicit, hideable bool
	total                     int
	refs                      string // JSON array of the slice's raw elements
}

// splitInboxLifecycleItem indexes the next slice of the job's split item.
//
// Expanding the array (json_each walks all of it) happens in a read
// snapshot that holds no write lock; the write transaction only inserts the
// bounded slice and updates the one-row job. The item row is written once,
// by the final slice. While split, the item stays lifecycle_ready=0, so
// readers use the canonical lifecycle read; owner triggers cannot mark it
// ready. Any write of the item reindexes it fully and clears split_id, so a
// slice read before that write is discarded. Lifecycle never grants
// authorization: ownership is checked independently by every reader.
func (w *Workspace) splitInboxLifecycleItem(ctx context.Context, id string, budget int) error {
	sqlCtx := context.WithoutCancel(ctx)
	var offset int
	if err := w.db.QueryRowContext(sqlCtx, `SELECT split_offset FROM inbox_lifecycle_job WHERE singleton=1 AND split_id=?`, id).Scan(&offset); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // the split ended since the chunk read it
		}
		return err
	}
	slice, err := w.readInboxLifecycleSlice(sqlCtx, id, offset, offset+budget)
	if err != nil {
		return err
	}
	return w.writeInboxLifecycleSlice(sqlCtx, id, offset, budget, slice)
}

// writeInboxLifecycleSlice applies one slice in a short write transaction
// whose work is bounded by the budget, not by the item's array size.
func (w *Workspace) writeInboxLifecycleSlice(sqlCtx context.Context, id string, offset, budget int, slice inboxLifecycleSlice) error {
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(sqlCtx, w.db)
	if err != nil {
		return err
	}
	defer cleanup()
	var current int
	err = tx.QueryRowContext(sqlCtx, `SELECT split_offset FROM inbox_lifecycle_job WHERE singleton=1 AND split_id=?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && current != offset) {
		return nil // rewritten, deleted or advanced since the snapshot
	}
	if err != nil {
		return err
	}
	finish := func() error {
		if _, err := tx.ExecContext(sqlCtx, `UPDATE inbox_lifecycle_job SET cursor=?,split_id='',split_offset=0,split_hidden=0 WHERE singleton=1`, id); err != nil {
			return err
		}
		return tx.Commit()
	}
	// The id may have changed since the snapshot even if a trigger missed
	// the split: never insert refs for an id that no longer exists.
	var exists bool
	if err = tx.QueryRowContext(sqlCtx, `SELECT EXISTS(SELECT 1 FROM derived_inbox_items WHERE id=?)`, id).Scan(&exists); err != nil {
		return err
	}
	if !slice.found || !exists {
		return finish()
	}
	norm := `SELECT CASE WHEN ? THEN value ELSE anx_unicode_trim(value) END AS ref FROM json_each(?)`
	if _, err = tx.ExecContext(sqlCtx, `INSERT OR IGNORE INTO inbox_lifecycle_refs SELECT ?,ref FROM (`+norm+`) WHERE ref<>''`, id, slice.explicit, slice.refs); err != nil {
		return err
	}
	// split_hidden means "an indexed ref names a hidden subject"; owner
	// triggers keep it current as subjects change, each slice adds its refs.
	if _, err = tx.ExecContext(sqlCtx, `UPDATE inbox_lifecycle_job SET split_offset=?,split_hidden=(split_hidden OR EXISTS(SELECT 1 FROM (`+norm+`) s JOIN inbox_hidden_subject_refs h ON h.ref=s.ref)) WHERE singleton=1`, offset+budget, slice.explicit, slice.refs); err != nil {
		return err
	}
	if offset+budget < slice.total {
		return tx.Commit()
	}
	if !slice.explicit {
		if _, err = tx.ExecContext(sqlCtx, `INSERT OR IGNORE INTO inbox_lifecycle_refs SELECT id,'thread:'||anx_unicode_trim(thread_id) FROM derived_inbox_items WHERE id=? AND thread_id IS NOT NULL`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(sqlCtx, `UPDATE inbox_lifecycle_job SET split_hidden=(split_hidden OR EXISTS(SELECT 1 FROM derived_inbox_items i JOIN inbox_hidden_subject_refs h ON h.ref='thread:'||anx_unicode_trim(i.thread_id) WHERE i.id=?)) WHERE singleton=1`, id); err != nil {
			return err
		}
	}
	// The single write of the item row.
	if _, err = tx.ExecContext(sqlCtx, `UPDATE derived_inbox_items SET lifecycle_hidden=(? AND (SELECT split_hidden FROM inbox_lifecycle_job WHERE singleton=1)),lifecycle_ready=(SELECT owners_ready FROM inbox_lifecycle_job WHERE singleton=1) WHERE id=?`, slice.hideable, id); err != nil {
		return err
	}
	return finish()
}

// readInboxLifecycleSlice reads elements [start,end) of an item's ref array
// in one statement snapshot. It may walk the whole array but holds no write
// lock.
func (w *Workspace) readInboxLifecycleSlice(ctx context.Context, id string, start, end int) (inboxLifecycleSlice, error) {
	var s inboxLifecycleSlice
	array := inboxLifecycleArraySQL()
	err := w.db.QueryRowContext(ctx, `SELECT COALESCE((`+inboxLifecycleExplicitSQL()+`),0),`+inboxLifecycleHideableSQL("i.")+`,json_array_length(`+array+`),
 (SELECT json_group_array(j.value) FROM json_each(`+array+`) j WHERE j.type='text' AND j.key>=? AND j.key<?)
 FROM derived_inbox_items i WHERE i.id=?`, start, end, id).Scan(&s.explicit, &s.hideable, &s.total, &s.refs)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.found = true
	return s, nil
}
