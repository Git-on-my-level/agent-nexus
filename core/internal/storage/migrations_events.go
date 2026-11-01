// Migration 13: dropping events.body_json in favor of the event payload
// wrapper format.

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func applyMigration13DropEventsBodyJSON(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "events")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasBody, err := sqliteTableHasColumn(ctx, tx, "events", "body_json")
	if err != nil {
		return fmt.Errorf("migration 13 pragma events.body_json: %w", err)
	}
	if !hasBody {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, refs_json, payload_json, body_json FROM events`)
	if err != nil {
		return fmt.Errorf("migration 13 select events: %w", err)
	}
	defer rows.Close()

	type rowData struct {
		id, refsJSON, payloadJSON string
		bodyJSON                  sql.NullString
	}
	var batch []rowData
	for rows.Next() {
		var r rowData
		if err := rows.Scan(&r.id, &r.refsJSON, &r.payloadJSON, &r.bodyJSON); err != nil {
			return fmt.Errorf("migration 13 scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migration 13 rows: %w", err)
	}

	for _, r := range batch {
		newRefs := r.refsJSON
		newPayload := r.payloadJSON

		bodyStr := ""
		if r.bodyJSON.Valid {
			bodyStr = strings.TrimSpace(r.bodyJSON.String)
		}
		if bodyStr != "" && bodyStr != "{}" {
			var body map[string]any
			if err := json.Unmarshal([]byte(bodyStr), &body); err != nil || body == nil {
				body = map[string]any{}
			}
			if raw, ok := body["refs"]; ok {
				if b, err := json.Marshal(raw); err == nil {
					newRefs = string(b)
				}
			}
			wrapper := migration13EventPayloadWrapperFromBodyMap(body)
			b, err := json.Marshal(wrapper)
			if err != nil {
				return fmt.Errorf("migration 13 marshal payload for %s: %w", r.id, err)
			}
			newPayload = string(b)
		} else {
			var pl map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(r.payloadJSON)), &pl); err != nil || pl == nil {
				pl = map[string]any{}
			}
			if _, has := pl["payload"]; !has {
				wrap := map[string]any{"payload": pl}
				b, err := json.Marshal(wrap)
				if err != nil {
					return fmt.Errorf("migration 13 wrap payload for %s: %w", r.id, err)
				}
				newPayload = string(b)
			}
		}

		if newRefs != r.refsJSON || newPayload != r.payloadJSON {
			if _, err := tx.ExecContext(ctx, `UPDATE events SET refs_json = ?, payload_json = ? WHERE id = ?`, newRefs, newPayload, r.id); err != nil {
				return fmt.Errorf("migration 13 update event %s: %w", r.id, err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `ALTER TABLE events DROP COLUMN body_json`); err != nil {
		return fmt.Errorf("migration 13 drop body_json: %w", err)
	}
	return nil
}

// migration13EventPayloadWrapperFromBodyMap matches primitives.EventPayloadWrapperFromBodyMap (duplicated
// here to avoid an import cycle: this package is imported by internal primitives tests).
func migration13EventPayloadWrapperFromBodyMap(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{"payload": map[string]any{}}
	}
	wrapper := map[string]any{}
	if raw, ok := body["payload"]; ok && raw != nil {
		p, ok := raw.(map[string]any)
		if !ok {
			wrapper["payload"] = map[string]any{}
		} else {
			inner := make(map[string]any, len(p))
			for k, v := range p {
				inner[k] = v
			}
			wrapper["payload"] = inner
		}
	} else {
		wrapper["payload"] = map[string]any{}
	}
	envelope := map[string]struct{}{
		"id": {}, "type": {}, "ts": {}, "actor_id": {}, "thread_id": {}, "refs": {}, "payload": {},
	}
	for k, v := range body {
		if _, skip := envelope[k]; skip {
			continue
		}
		switch k {
		case "archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason":
			continue
		}
		wrapper[k] = v
	}
	return wrapper
}
