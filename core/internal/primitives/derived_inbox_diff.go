package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// replaceThreadInboxRows writes a thread's derived inbox rows as a diff.
// Deleting and re-inserting every row on every refresh fired each row's
// insert and delete triggers (access edges, lifecycle refs, work summaries,
// scope inbox) and bumped the access epoch, which made every cached
// readability check stale and kept the escalation loop busy. A row whose
// stored content is unchanged, and whose ask has not been answered since,
// is now left alone; every other row is replaced exactly as before.
func replaceThreadInboxRows(ctx context.Context, tx interface {
	eventExec
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, threadID string, items []DerivedInboxItem) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, category, trigger_at, COALESCE(due_at,''), has_due_at, COALESCE(source_event_id,''), COALESCE(source_card_id,''), data_json, COALESCE(source_hash,''),
			EXISTS(SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=COALESCE(d.source_event_id,''))
		FROM derived_inbox_items d WHERE thread_id = ? AND COALESCE(json_extract(data_json,'$.subtype'),'') <> 'report_review_due'`, threadID)
	if err != nil {
		return fmt.Errorf("read derived inbox items: %w", err)
	}
	existing := map[string]string{}
	for rows.Next() {
		var id, category, triggerAt, dueAt, sourceEvent, sourceCard, data, hash string
		var hasDue, resolved int
		if err := rows.Scan(&id, &category, &triggerAt, &dueAt, &hasDue, &sourceEvent, &sourceCard, &data, &hash, &resolved); err != nil {
			rows.Close()
			return fmt.Errorf("read derived inbox item: %w", err)
		}
		if resolved == 0 {
			existing[id] = inboxRowSignature(category, triggerAt, dueAt, hasDue, sourceEvent, sourceCard, data, hash)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read derived inbox items: %w", err)
	}
	keep := []string{}
	changed := make([]DerivedInboxItem, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if sig, ok := existing[id]; ok && sig == newInboxRowSignature(item) {
			keep = append(keep, id)
			continue
		}
		changed = append(changed, item)
	}
	kept, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE thread_id = ? AND COALESCE(json_extract(data_json,'$.subtype'),'') <> 'report_review_due'
		AND id NOT IN (SELECT value FROM json_each(?))`, threadID, string(kept)); err != nil {
		return fmt.Errorf("delete derived inbox items: %w", err)
	}
	for _, item := range changed {
		if err := insertDerivedInboxItem(ctx, tx, threadID, item); err != nil {
			return err
		}
	}
	return nil
}

func inboxRowSignature(category, triggerAt, dueAt string, hasDue int, sourceEvent, sourceCard, data, hash string) string {
	return strings.Join([]string{category, triggerAt, dueAt, fmt.Sprint(hasDue), sourceEvent, sourceCard, data, hash}, "\x00")
}

// newInboxRowSignature is the signature insertDerivedInboxItem would store;
// an item it would reject never matches, so it is inserted and reports its
// error there.
func newInboxRowSignature(item DerivedInboxItem) string {
	category, triggerAt := strings.TrimSpace(item.Category), strings.TrimSpace(item.TriggerAt)
	if strings.TrimSpace(item.ID) == "" || category == "" || triggerAt == "" {
		return "\x01invalid"
	}
	data, hash, err := marshalDerivedJSON(stripInboxDataForStore(item), item.SourceHash)
	if err != nil {
		return "\x01invalid"
	}
	return inboxRowSignature(category, triggerAt, nullableString(item.DueAt).String, boolToInt(item.HasDueAt),
		nullableString(item.SourceEventID).String, nullableString(item.SourceCardID).String, data, hash)
}
