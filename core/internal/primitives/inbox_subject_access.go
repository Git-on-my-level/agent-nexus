package primitives

import "context"

// InboxThreadAccessOwners resolves inherited privacy from canonical ownership,
// including archived cards/boards and legacy parent threads. Mutable thread
// subject_ref metadata is insufficient authority for this check. This local
// Check owners against canonical tables: reader-filtered shadows must not hide
// a private legacy parent when deciding access. These identities stay internal
// to authorization and are never returned as resource data.
func (s *Store) InboxThreadAccessOwners(ctx context.Context, threadID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(json_extract(t.body_json, '$.pm_actor_id'), ''),
		       COALESCE(json_extract(bt.body_json, '$.pm_actor_id'), '')
		FROM main.cards c
		LEFT JOIN main.threads t ON t.id=COALESCE(NULLIF(trim(c.thread_id), ''), trim(c.parent_thread_id))
		LEFT JOIN main.boards b ON b.id=c.board_id
		LEFT JOIN main.threads bt ON bt.id=b.thread_id
		WHERE c.thread_id=? OR (COALESCE(trim(c.thread_id), '')='' AND c.parent_thread_id=?)`, threadID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := []string{}
	for rows.Next() {
		var cardOwner, boardOwner string
		if err := rows.Scan(&cardOwner, &boardOwner); err != nil {
			return nil, err
		}
		owners = append(owners, cardOwner, boardOwner)
	}
	return owners, rows.Err()
}
