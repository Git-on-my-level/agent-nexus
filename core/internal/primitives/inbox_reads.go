package primitives

import (
	"context"
	"fmt"
)

type InboxReadOptions struct {
	AsksOnly bool
	Limit    *int // nil requests the full authorized list; zero counts only.
	ThreadID string
	ID       string
}

func inboxReadSQL(ctx context.Context) string {
	subject := `COALESCE(NULLIF(trim(json_extract(i.data_json,'$.subject_ref')),''),'thread:'||i.thread_id)`
	refs := `json_extract(i.data_json,'$.related_refs')`
	legacyRefs := `json_extract(i.data_json,'$.refs')`
	// The handle filters ownership before counts and pages. Requests retain
	// their own lifecycle; ordinary notifications require active context.
	return `(lower(trim(COALESCE(NULLIF(trim(json_extract(i.data_json,'$.kind')),''),i.category))) IN ('ask','review','escalate') OR (` + inboxSubjectLifecycleSQL(ctx, subject, refs, legacyRefs, true) + `))`
}

func inboxSubjectLifecycleSQL(ctx context.Context, subject, refs, legacyRefs string, active bool) string {
	cardAllow := "1=1"
	if active {
		cardAllow += ` AND COALESCE(inbox_card.archived_at,'')='' AND COALESCE(inbox_card.trashed_at,'')='' AND NOT EXISTS (SELECT 1 FROM boards inbox_board WHERE inbox_board.id=inbox_card.board_id AND (COALESCE(inbox_board.archived_at,'')<>'' OR COALESCE(inbox_board.trashed_at,'')<>''))`
	}
	// Compile the generic ref matcher once for this subject set. Repeating its
	// resource/alias joins for each input makes SQLite preparation dominate even
	// an empty digest. The scoped relations still filter access before paging.
	inputs := `SELECT ` + subject + ` AS value UNION ALL SELECT 'thread:'||i.thread_id UNION ALL SELECT 'card:'||COALESCE(i.source_card_id,'') UNION ALL SELECT value FROM json_each(` + refs + `) UNION ALL SELECT value FROM json_each(` + legacyRefs + `)`
	return `NOT EXISTS (SELECT 1 FROM cards inbox_card WHERE COALESCE(NULLIF(trim(inbox_card.thread_id),''),trim(inbox_card.parent_thread_id))=i.thread_id AND NOT (` + cardAllow + `))
 AND NOT EXISTS (SELECT 1 FROM (` + inputs + `) inbox_ref WHERE NOT (` + referenceLifecycleSQL(ctx, `inbox_ref.value`, active) + `))`
}

// ReadInbox counts and pages the centrally scoped inbox relation.
// Summary counts in SQLite and decodes only the requested, authorized page.
func (s *Store) ReadInbox(ctx context.Context, options InboxReadOptions) ([]DerivedInboxItem, int, error) {
	if options.Limit != nil && (*options.Limit < 0 || *options.Limit > 50) {
		return nil, 0, fmt.Errorf("inbox summary limit must be 0..50")
	}
	where := "1=1"
	if !options.AsksOnly {
		where = inboxReadSQL(ctx)
	}
	args := []any{}
	if options.ThreadID != "" {
		where += ` AND i.thread_id=?`
		args = append(args, options.ThreadID)
	}
	if options.ID != "" {
		where += ` AND i.id=?`
		args = append(args, options.ID)
	}
	if options.AsksOnly {
		where += ` AND COALESCE(NULLIF(json_extract(i.data_json,'$.kind'),''),i.category)='ask'`
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM derived_inbox_items i WHERE `+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	items := []DerivedInboxItem{}
	if options.Limit != nil && *options.Limit == 0 {
		return items, count, nil
	}
	query := `SELECT i.id,i.thread_id,i.category,i.trigger_at,i.due_at,i.has_due_at,i.source_event_id,i.source_card_id,i.generated_at,i.data_json,i.source_hash,i.lifecycle_ready FROM derived_inbox_items i WHERE ` + where
	if options.AsksOnly {
		query += ` ORDER BY CASE lower(trim(COALESCE(NULLIF(trim(json_extract(i.data_json,'$.priority')),''),json_extract(i.data_json,'$.severity'),''))) WHEN 'urgent' THEN 0 WHEN 'p0' THEN 0 WHEN 'critical' THEN 0 WHEN 'high' THEN 1 WHEN 'p1' THEN 1 WHEN 'normal' THEN 2 WHEN 'medium' THEN 2 WHEN 'p2' THEN 2 WHEN 'low' THEN 3 WHEN 'p3' THEN 3 ELSE 4 END,i.trigger_at ASC,i.id ASC`
	} else {
		query += ` ORDER BY CASE i.category WHEN 'escalate' THEN 0 WHEN 'ask' THEN 1 WHEN 'review' THEN 2 ELSE 99 END,i.trigger_at DESC,i.id ASC`
	}
	if options.Limit != nil {
		query += ` LIMIT ?`
		args = append(args, *options.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanDerivedInboxItem(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, count, rows.Err()
}

// Inbox freshness is principal-scoped without changing generic thread reads.
// Filter backing card/board access before loading any projection metadata.
func (s *Store) ListInboxThreadIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT threads.id FROM threads WHERE COALESCE(threads.archived_at,'')='' AND COALESCE(threads.trashed_at,'')='' AND `+backingThreadLifecycleSQL(ctx, `threads.id`, true)+` ORDER BY threads.id`)
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
