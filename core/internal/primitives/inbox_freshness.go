package primitives

import "context"

// Preserve workspace freshness (including dirty threads with no inbox rows yet)
// while returning one aggregate, rather than hydrating every thread and view.
func (s *Store) InboxFreshnessSummary(ctx context.Context) (int, int, error) {
	var count, rank int
	err := s.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(CASE
 WHEN r.desired_generation>r.materialized_generation AND trim(COALESCE(r.last_error,''))<>'' THEN 3
 WHEN r.desired_generation>r.materialized_generation OR r.in_progress_generation IS NOT NULL THEN 2
 WHEN v.thread_id IS NOT NULL OR r.materialized_generation>0 THEN 0 ELSE 1 END),0)
 FROM threads t LEFT JOIN derived_topic_views v ON v.thread_id=t.id LEFT JOIN topic_projection_refresh_status r ON r.thread_id=t.id
 WHERE COALESCE(t.archived_at,'')='' AND COALESCE(t.trashed_at,'')=''`).Scan(&count, &rank)
	return count, rank, err
}

// InboxThreadIDs samples projection details without hydrating unrelated public
// thread references. The aggregate above still covers every visible thread.
func (s *Store) InboxThreadIDs(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM threads WHERE COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')='' ORDER BY updated_at DESC,id ASC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
