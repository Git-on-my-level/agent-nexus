package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

func (s *Store) applyAskStaleness(out map[string]any, activity string, now time.Time) {
	out["is_stale"] = false
	delete(out, "stale_at")
	delete(out, "stale_reason")
	delete(out, "stale_since")
	threshold := s.askStaleAfter
	if threshold <= 0 {
		threshold = 7 * 24 * time.Hour
	}
	changed, err := time.Parse(time.RFC3339Nano, activity)
	if err != nil {
		return
	}
	out["stale_at"] = changed.Add(threshold).UTC().Format(time.RFC3339Nano)
	if now.Sub(changed) > threshold {
		out["is_stale"] = true
		out["stale_reason"] = "subject_inactive"
		out["stale_since"] = changed.Add(threshold).UTC().Format(time.RFC3339Nano)
	}
}

// EnrichInboxAskStaleness consumes only an already authorized inbox response
// page. Each batch admits at most 200 subject refs and uses card ID, unique
// handle and the unique identity origin index. No history or workspace
// projection is loaded, and there are no per-row SQL calls. Archived subjects
// remain stale until the lifecycle worker durably withdraws their open asks.
func (s *Store) EnrichInboxAskStaleness(ctx context.Context, items []map[string]any, now time.Time) error {
	refs := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		delete(item, "stale_at")
		delete(item, "stale_reason")
		delete(item, "stale_since")
		item["is_stale"] = false
		kind := strings.TrimSpace(strings.ToLower(anyStringValue(item["kind"])))
		if (kind != "ask" && kind != "review" && kind != "escalate") || inboxAttentionCompleted(item) {
			continue
		}
		ref := strings.TrimSpace(anyStringValue(item["subject_ref"]))
		if strings.HasPrefix(ref, "card:") && !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	if len(refs) == 0 {
		return nil
	}
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return err
	}
	defer closeRead()
	activity := map[string]string{}
	for start := 0; start < len(refs); start += 200 {
		end := min(start+200, len(refs))
		encoded, err := json.Marshal(refs[start:end])
		if err != nil {
			return err
		}
		// Match resolveResourceRef's handle, alias, then ID precedence. This
		// also covers legacy asks before ask_subjects backfill has finished.
		// Alias identities are write-maintained routing keys, not grants.
		// The card hydration retains canonical scope; no alias reason text
		// is loaded or inspected on this bounded convenience projection.
		rows, err := s.db.QueryContext(ctx, `SELECT page.value,c.updated_at
 FROM json_each(?) page JOIN cards c ON c.id=COALESCE(
 (SELECT id FROM cards INDEXED BY idx_cards_handle_unique WHERE handle IS NOT NULL AND trim(handle)<>'' AND handle=anx_normalize_handle(trim(substr(page.value,6)))),
 (SELECT resource_id FROM resource_access_identities WHERE origin=? AND origin_id=json_array('card',anx_normalize_handle(trim(substr(page.value,6)))) AND kind='card' LIMIT 1),
 trim(substr(page.value,6)))`, string(encoded), "resource_handle_aliases")
		if err != nil {
			return err
		}
		for rows.Next() {
			var ref, at string
			if err = rows.Scan(&ref, &at); err != nil {
				rows.Close()
				return err
			}
			activity[ref] = at
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for _, item := range items {
		kind := strings.TrimSpace(strings.ToLower(anyStringValue(item["kind"])))
		if (kind == "ask" || kind == "review" || kind == "escalate") && !inboxAttentionCompleted(item) {
			s.applyAskStaleness(item, activity[strings.TrimSpace(anyStringValue(item["subject_ref"]))], now)
		}
	}
	return nil
}

func inboxAttentionCompleted(item map[string]any) bool {
	status := anyStringValue(item["status"])
	return (status != "" && status != "open") || anyStringValue(item["responded_at"]) != "" || anyStringValue(item["completed_at"]) != ""
}
