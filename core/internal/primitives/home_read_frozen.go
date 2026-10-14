package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/schema"
	"context"
	"fmt"
	"strings"
	"time"
)

// CheckHomeReadCursors admits only bounded, currently visible event snapshots.
// No home lookup/projection or newest-at-send scan is needed. A keyed read marks
// exactly what the reader saw, leaving later arrivals unread.
func (s *Store) CheckHomeReadCursors(ctx context.Context, refs []string, expected map[string]EventCursor) error {
	// This is a read-only proof phase, including when borrowed by a writer.
	// Capture current authority once, then pin it in that same SQLite snapshot;
	// the following business writes retain the caller's ordinary write policy.
	if scope, ok := accessScopeFrom(ctx); ok {
		if _, requested := ctx.Value(denialRequestKey{}).(*denialRequestState); !requested {
			ctx = WithRequestAccessScope(ctx, scope)
		}
	}
	if len(refs) == 0 || len(refs) > 100 {
		return ErrInvalidBoardRequest
	}
	ids := make([]any, 0, len(refs))
	seen := map[string]string{}
	for _, ref := range refs {
		c := expected[ref]
		if strings.TrimSpace(ref) == "" || c.ID == "" || c.TS == "" {
			return ErrInvalidBoardRequest
		}
		if _, e := time.Parse(time.RFC3339Nano, c.TS); e != nil {
			return ErrInvalidBoardRequest
		}
		if ts, ok := seen[c.ID]; ok && ts != c.TS {
			return ErrInvalidBoardRequest
		}
		if _, ok := seen[c.ID]; !ok {
			ids = append(ids, c.ID)
			seen[c.ID] = c.TS
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,ts FROM events WHERE id IN (`+strings.TrimRight(strings.Repeat("?,", len(ids)), ",")+`)`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ts string
		if err = rows.Scan(&id, &ts); err != nil {
			return err
		}
		if seen[id] != ts {
			return ErrConflict
		}
		delete(seen, id)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(seen) != 0 {
		return ErrNotFound
	}
	rows.Close()
	next, close, err := s.BeginResourceRead(ctx)
	if err != nil {
		return err
	}
	defer close()
	return s.checkHomeReadMembership(next, refs, expected)
}

// Membership uses existing resource handle/ID indexes and the covering source
// ref index. Raw group candidates are exact ID/handle lookups; the scoped
// group EXISTS below independently admits their current visibility in the
// same statement before any match is returned. The raw edge EXISTS is a point membership proof between the
// scoped event and scoped group above: it cannot enumerate hidden edges or
// return their data. Like lifecycle's raw bounded adjacency, this avoids the
// workspace-wide ref_edges visibility shadow. It never loads a home lookup
// or scans stored event JSON.
func (s *Store) checkHomeReadMembership(ctx context.Context, refs []string, expected map[string]EventCursor) error {
	args := make([]any, 0, len(refs)*3)
	unique := dedupeStrings(refs)
	for _, ref := range unique {
		kind, id, err := schema.SplitTypedRef(ref)
		if err != nil || !map[string]bool{"topic": true, "board": true, "thread": true, "card": true}[kind] {
			return ErrInvalidBoardRequest
		}
		args = append(args, kind, id, expected[ref].ID)
	}
	values := strings.TrimRight(strings.Repeat("(?,?,?),", len(unique)), ",")
	query := `WITH requested(kind,ref,event_id) AS (VALUES ` + values + `), groups(kind,id,handle,thread_id) AS (
  SELECT 'topic',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.topics g WHERE r.kind='topic' AND g.id=r.ref
 UNION ALL SELECT 'topic',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.topics g WHERE r.kind='topic' AND g.handle IS NOT NULL AND trim(g.handle)<>'' AND g.handle=r.ref
 UNION ALL SELECT 'board',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.boards g WHERE r.kind='board' AND g.id=r.ref
 UNION ALL SELECT 'board',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.boards g WHERE r.kind='board' AND g.handle IS NOT NULL AND trim(g.handle)<>'' AND g.handle=r.ref
 UNION ALL SELECT 'thread',g.id,g.handle,g.id FROM requested r CROSS JOIN main.threads g WHERE r.kind='thread' AND g.id=r.ref
 UNION ALL SELECT 'thread',g.id,g.handle,g.id FROM requested r CROSS JOIN main.threads g WHERE r.kind='thread' AND g.handle IS NOT NULL AND trim(g.handle)<>'' AND g.handle=r.ref
 UNION ALL SELECT 'card',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.cards g WHERE r.kind='card' AND g.id=r.ref
 UNION ALL SELECT 'card',g.id,g.handle,g.thread_id FROM requested r CROSS JOIN main.cards g WHERE r.kind='card' AND g.handle IS NOT NULL AND trim(g.handle)<>'' AND g.handle=r.ref
 ) SELECT DISTINCT r.kind,r.ref FROM requested r JOIN groups g ON g.kind=r.kind AND (g.id=r.ref OR g.handle=r.ref)
 JOIN events e ON e.id=r.event_id
 WHERE ((g.kind='topic' AND EXISTS(SELECT 1 FROM topics visible WHERE visible.id=g.id AND COALESCE(visible.archived_at,'')='' AND COALESCE(visible.trashed_at,'')='')) OR (g.kind='board' AND EXISTS(SELECT 1 FROM boards visible WHERE visible.id=g.id AND COALESCE(visible.archived_at,'')='' AND COALESCE(visible.trashed_at,'')='')) OR (g.kind='thread' AND EXISTS(SELECT 1 FROM threads visible WHERE visible.id=g.id)) OR (g.kind='card' AND EXISTS(SELECT 1 FROM cards visible WHERE visible.id=g.id AND COALESCE(visible.archived_at,'')='' AND COALESCE(visible.trashed_at,'')=''))) AND (e.thread_id=g.thread_id OR EXISTS (
  SELECT 1 FROM main.ref_edges x INDEXED BY idx_ref_edges_access_cover
  WHERE x.target_type=g.kind AND x.target_id COLLATE NOCASE IN(g.id,g.handle)
   AND x.edge_type='ref' AND x.source_type='event' AND x.source_id=e.id
 ) OR EXISTS (
  SELECT 1 FROM main.ref_edges x INDEXED BY idx_ref_edges_access_cover
  WHERE x.target_type='thread' AND x.target_id COLLATE NOCASE=g.thread_id
   AND x.edge_type='ref' AND x.source_type='event' AND x.source_id=e.id
 ))`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			return err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if count != len(unique) {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkHomeReadFrozen(ctx context.Context, reader string, refs []string, expected map[string]EventCursor) error {
	return s.db.WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		if err := s.CheckHomeReadCursors(ctx, refs, expected); err != nil {
			return err
		}
		for _, ref := range dedupeStrings(refs) {
			c := expected[ref]
			_, err := tx.ExecContext(ctx, `INSERT INTO home_read_cursors(reader_id,group_ref,last_read_event_ts,last_read_event_id,updated_at) VALUES(?,?,?,?,?)
    ON CONFLICT(reader_id,group_ref) DO UPDATE SET last_read_event_ts=excluded.last_read_event_ts,last_read_event_id=excluded.last_read_event_id,updated_at=excluded.updated_at
    WHERE excluded.last_read_event_ts>home_read_cursors.last_read_event_ts OR (excluded.last_read_event_ts=home_read_cursors.last_read_event_ts AND excluded.last_read_event_id>home_read_cursors.last_read_event_id)`, reader, ref, c.TS, c.ID, time.Now().UTC().Format(time.RFC3339Nano))
			if err != nil {
				return fmt.Errorf("advance frozen cursor: %w", err)
			}
		}
		return nil
	})
}
