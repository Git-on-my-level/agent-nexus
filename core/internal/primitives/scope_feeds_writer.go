package primitives

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/resourceaccess"
)

// ScopeInboxCapture is supplied by A's trusted canonical adapter. It must
// validate the source/RID tuple, advance its canonical version and apply the
// detached B feed/payload/counter changes in the SAME transaction. It must not
// start another transaction, certify parity, or commit. This is not a callback
// accepted from HTTP; constructors/dispatch remain unwired.
type ScopeInboxCapture func(context.Context, *resourceaccess.Tx, *DerivedInboxItem, DerivedInboxItem) error

// WriteScopeInboxItem writes one canonical derived row with its required
// projection capture. Success leaves the caller's transaction open. Every
// failure makes it uncommittable, including ignored capture/validation errors.
// The existing bulk replacement API is unchanged: integrating its bounded
// foreground fence and durable rebuild is a separate A/D wiring requirement.
func WriteScopeInboxItem(ctx context.Context, tx *resourceaccess.Tx, item DerivedInboxItem, capture ScopeInboxCapture) error {
	if tx == nil {
		return ErrScopeInboxProjection
	}
	success := false
	defer func() {
		if !success {
			_ = tx.Rollback()
		}
	}()
	if ctx == nil || capture == nil {
		return ErrScopeInboxProjection
	}
	item.ID = strings.TrimSpace(item.ID)
	item.ThreadID = strings.TrimSpace(item.ThreadID)
	item.Category = strings.TrimSpace(item.Category)
	item.TriggerAt = strings.TrimSpace(item.TriggerAt)
	if !scopeInboxText(item.ID, 512) || !scopeInboxText(item.ThreadID, 512) || !scopeInboxText(item.Category, 128) || !scopeInboxText(item.TriggerAt, 128) {
		return ErrScopeInboxProjection
	}
	if item.GeneratedAt == "" {
		item.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if !validScopeInboxItem(item) {
		return ErrScopeInboxProjection
	}
	// Freeze nested caller-owned data before the first write, using exactly the
	// existing column-authoritative store shape and hash behavior.
	data, hash, err := marshalDerivedJSON(stripInboxDataForStore(item), item.SourceHash)
	if err != nil || len(data) > MaxScopeInboxPayloadBytes {
		return ErrScopeInboxProjection
	}
	item.SourceHash = hash
	var frozen map[string]any
	d := json.NewDecoder(bytes.NewBufferString(data))
	d.UseNumber()
	if err := d.Decode(&frozen); err != nil {
		return ErrScopeInboxProjection
	}
	item.Data = frozen
	old, err := scanScopeInboxSource(tx.QueryRowContext(ctx, `SELECT id,thread_id,category,trigger_at,due_at,has_due_at,source_event_id,source_card_id,generated_at,
CASE WHEN length(CAST(data_json AS BLOB))<=16384 THEN data_json END,source_hash FROM derived_inbox_items WHERE id=?`, item.ID))
	var before *DerivedInboxItem
	if err == nil {
		before = &old
	} else if err != sql.ErrNoRows {
		return err
	}
	if before != nil {
		result, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE id=?`, item.ID)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrScopeInboxProjection
		}
	}
	if err := insertDerivedInboxItem(ctx, tx, item.ThreadID, item); err != nil {
		return err
	}
	// Rehydrate only after persistence: adding a default kind before inserting
	// would alter the stored JSON after its canonical hash was calculated.
	rehydrateDerivedInboxDataFromColumns(&item)
	if err := capture(ctx, tx, before, item); err != nil {
		return err
	}
	success = true
	return nil
}

// Preserve JSON numbers in the captured before image. The legacy general
// decoder uses float64; reusing it here would change integers above 2^53 before
// projecting/hashing the source. This bounded scanner does no extra SQL.
func scanScopeInboxSource(row scanDerivedInboxItemRower) (DerivedInboxItem, error) {
	var item DerivedInboxItem
	var due, event, card, hash sql.NullString
	var hasDue int
	var raw string
	if err := row.Scan(&item.ID, &item.ThreadID, &item.Category, &item.TriggerAt, &due, &hasDue, &event, &card, &item.GeneratedAt, &raw, &hash); err != nil {
		return DerivedInboxItem{}, err
	}
	item.DueAt = due.String
	item.HasDueAt = hasDue != 0
	item.SourceEventID = event.String
	item.SourceCardID = card.String
	item.SourceHash = hash.String
	if !utf8.ValidString(raw) || !json.Valid([]byte(raw)) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.UseNumber()
	if err := d.Decode(&item.Data); err != nil {
		return DerivedInboxItem{}, err
	}
	if !validScopeInboxItem(item) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	rehydrateDerivedInboxDataFromColumns(&item)
	return item, nil
}
