// Event lifecycle transitions: archive, unarchive, trash, and restore over
// event rows, including recursive descendant selection for message threads.

package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// Each expansion reads at most 2002 raw indexed event-ref edges.
// Only candidate IDs bypass the relation shadow; scoped events must authorize
// each candidate before recursion or output. Filtering raw edges before the
// cap would permit workspace-sized invisible fanout scans. A high raw
// degree fails closed even if some edges point outside this message thread.
// UNION deduplicates cycles; the global cap permits at most 2002 expansions.
const eventDescendantsQuery = `WITH RECURSIVE descendants(id, overflow) AS (
 SELECT ?, 0 UNION
 SELECT events.id, 0 FROM descendants CROSS JOIN events ON events.id IN (
   SELECT CASE WHEN source_type='event' AND edge_type='ref' THEN source_id ELSE NULL END FROM main.ref_edges INDEXED BY idx_ref_edges_target
   WHERE target_type='event' AND target_id=descendants.id
   LIMIT 2002
 ) WHERE descendants.overflow=0 AND CASE WHEN events.thread_id=? AND events.type='message_posted' THEN 1 ELSE 0 END=1
 UNION SELECT NULL, 1 FROM descendants WHERE descendants.overflow=0 AND (
   SELECT count(*) FROM (SELECT CASE WHEN source_type='event' AND edge_type='ref' THEN source_id ELSE NULL END FROM main.ref_edges INDEXED BY idx_ref_edges_target
     WHERE target_type='event' AND target_id=descendants.id
     LIMIT 2002)
 ) > 2001
 LIMIT 2002) SELECT id, overflow FROM descendants`

func (s *Store) collectMessageDescendantIDs(ctx context.Context, threadID, parentID string) ([]string, error) {
	// Borrow the writer snapshot: indexed candidate checks share one current
	// epoch-admitted point proof instead of rematerializing denied identities.
	read, finish, err := s.BeginResourceRead(ctx)
	if err != nil {
		return nil, err
	}
	defer finish()
	rows, err := s.db.QueryContext(read, eventDescendantsQuery, parentID, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id sql.NullString
		var overflow int
		if err := rows.Scan(&id, &overflow); err != nil {
			return nil, err
		}
		if overflow != 0 {
			return nil, ErrConflict
		}
		if id.String != parentID {
			ids = append(ids, id.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) > 2000 {
		return nil, ErrConflict
	}
	return ids, nil
}

// EventLifecycleBase fences the mutable visibility exception to append-only events.
type EventLifecycleBase struct {
	ArchivedAt *string `json:"archived_at"`
	TrashedAt  *string `json:"trashed_at"`
}

func (s *Store) ApplyEventLifecycle(ctx context.Context, actorID, eventID, action, reason string, base *EventLifecycleBase) (map[string]any, []string, error) {
	return s.ApplyEventLifecycleSelection(ctx, actorID, eventID, action, reason, base, nil)
}

// Selected bases protect explicitly selected replies that an ancestor may cover.
// The shared snapshot is checked only for scoped members of this cascade.
func (s *Store) ApplyEventLifecycleSelection(ctx context.Context, actorID, eventID, action, reason string, base *EventLifecycleBase, selectedBases map[string]EventLifecycleBase) (map[string]any, []string, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	eventID = strings.TrimSpace(eventID)
	if actorID == "" || eventID == "" {
		return nil, nil, fmt.Errorf("actor_id and event_id are required")
	}
	var result map[string]any
	var affected []string
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE events SET archived_at=archived_at WHERE id=?`, eventID); err != nil {
			return err
		}
		current, err := s.GetEvent(ctx, eventID)
		if err != nil {
			return err
		}
		value := func(v any) string {
			if v == nil {
				return ""
			}
			return fmt.Sprint(v)
		}
		expected := func(v *string) string {
			if v == nil {
				return ""
			}
			return *v
		}
		if base != nil && (value(current["archived_at"]) != expected(base.ArchivedAt) || value(current["trashed_at"]) != expected(base.TrashedAt)) {
			return ErrConflict
		}
		if action == "archive" && value(current["trashed_at"]) != "" {
			return ErrAlreadyTrashed
		}
		if action == "unarchive" && value(current["archived_at"]) == "" {
			return ErrNotArchived
		}
		if action == "restore" && value(current["trashed_at"]) == "" {
			return ErrNotTrashed
		}
		if (action == "archive" && value(current["archived_at"]) != "") || (action == "trash" && value(current["trashed_at"]) != "") {
			result = current
			return nil
		}
		ids := []string{eventID}
		if current["type"] == "message_posted" {
			desc, err := s.collectMessageDescendantIDs(ctx, value(current["thread_id"]), eventID)
			if err != nil {
				return err
			}
			ids = append(ids, desc...)
		}
		marks := make([]string, len(ids))
		args := make([]any, 0, len(ids)+3)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		var update, guard string
		switch action {
		case "archive":
			update = "archived_at=?,archived_by=?"
			args = append(args, now, actorID)
			guard = " AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')=''"
		case "unarchive":
			update = "archived_at=NULL,archived_by=NULL"
			guard = " AND COALESCE(archived_at,'')<>''"
		case "trash":
			update = "trashed_at=?,trashed_by=?,trash_reason=?,archived_at=NULL,archived_by=NULL"
			args = append(args, now, actorID, strings.TrimSpace(reason))
			guard = " AND COALESCE(trashed_at,'')=''"
		case "restore":
			update = "trashed_at=NULL,trashed_by=NULL,trash_reason=NULL"
			guard = " AND COALESCE(trashed_at,'')<>''"
		default:
			return ErrInvalidDocumentRequest
		}
		for i, id := range ids {
			marks[i] = "?"
			args = append(args, id)
		}
		selector := " WHERE id IN (" + strings.Join(marks, ",") + ")"
		// Include unchanged rows too: a selected reply's base must match even
		// when the action guard would omit that reply from the write.
		selected, err := tx.QueryContext(ctx, "SELECT id,archived_at,trashed_at FROM events"+selector, args[len(args)-len(ids):]...)
		if err != nil {
			return err
		}
		for selected.Next() {
			var id string
			var archived, trashed sql.NullString
			if err = selected.Scan(&id, &archived, &trashed); err != nil {
				selected.Close()
				return err
			}
			if selectedBase, ok := selectedBases[id]; ok && (archived.String != expected(selectedBase.ArchivedAt) || trashed.String != expected(selectedBase.TrashedAt)) {
				selected.Close()
				return ErrConflict
			}
			changes := (action == "archive" && archived.String == "" && trashed.String == "") || (action == "unarchive" && archived.String != "") || (action == "trash" && trashed.String == "") || (action == "restore" && trashed.String != "")
			if changes {
				affected = append(affected, id)
			}
		}
		err = selected.Err()
		selected.Close()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE events SET "+update+" WHERE id IN ("+strings.Join(marks, ",")+")"+guard, args...); err != nil {
			return err
		}
		result, err = s.GetEvent(ctx, eventID)
		return err
	})
	return result, affected, err
}

func (s *Store) ArchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	result, _, err := s.ApplyEventLifecycle(ctx, actorID, eventID, "archive", "", nil)
	return result, err
}

func (s *Store) UnarchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	result, _, err := s.ApplyEventLifecycle(ctx, actorID, eventID, "unarchive", "", nil)
	return result, err
}

func (s *Store) TrashEvent(ctx context.Context, actorID, eventID, reason string) (map[string]any, error) {
	result, _, err := s.ApplyEventLifecycle(ctx, actorID, eventID, "trash", reason, nil)
	return result, err
}

func (s *Store) RestoreEvent(ctx context.Context, actorID, eventID string) (map[string]any, error) {
	result, _, err := s.ApplyEventLifecycle(ctx, actorID, eventID, "restore", "", nil)
	return result, err
}
