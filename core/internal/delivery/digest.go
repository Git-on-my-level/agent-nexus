package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

type digestQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// resolveDigest runs after bounded queue admission, in the claim transaction
// or after closing the list cursor. Never serialize the persisted aggregate:
// even its source event can inherit a contributor that is now denied.
func (s Store) resolveDigest(ctx context.Context, q digestQueryer, d *Delivery) (bool, error) {
	if len(d.Payload) == 0 {
		return false, nil
	}
	if s.AccessContext == nil || strings.TrimSpace(d.PrincipalID) == "" {
		return false, nil
	}
	ctx = s.AccessContext(ctx, d.PrincipalID)
	// Use the same recipient-bound, statement-validated policy inside a claim
	// transaction. Falling back to Policy.Read would rebuild workspace ownership
	// instead of authorizing only these admitted identities.
	if policy, ok := resourceaccess.PolicyFrom(ctx); ok && policy.TransactionRead == nil && policy.ReadOnDB != nil {
		policy.TransactionRead = policy.ReadOnDB
		ctx = resourceaccess.WithPolicy(ctx, policy)
	}
	raw, _ := json.Marshal(d.Payload)
	var payload struct {
		Type      string   `json:"type"`
		Workspace string   `json:"workspace"`
		Day       string   `json:"day"`
		CardRefs  []string `json:"card_refs"`
		Cards     []struct {
			Ref string `json:"ref"`
		} `json:"cards"`
		Items []struct {
			Ref      string `json:"ref"`
			Link     string `json:"link"`
			DecideBy string `json:"decide_by"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false, nil
	}
	tracking := payload.Type == "work_tracking_digest"
	refs := []string{}
	if tracking {
		refs = payload.CardRefs
		if refs == nil {
			for _, card := range payload.Cards {
				refs = append(refs, card.Ref)
			}
		}
	} else if payload.Type == "digest" {
		for _, item := range payload.Items {
			refs = append(refs, item.Ref)
		}
	} else {
		return false, nil
	}
	if len(refs) == 0 || len(refs) > 50 {
		return false, nil
	}
	prefix := "event:"
	if tracking {
		prefix = "card:"
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !strings.HasPrefix(ref, prefix) {
			return false, nil
		}
		ids = append(ids, strings.TrimPrefix(ref, prefix))
	}
	encoded, _ := json.Marshal(ids)
	query := `SELECT id,COALESCE(json_extract(payload_json,'$.payload.title'),''),'','' FROM events WHERE id IN(SELECT value FROM json_each(?))`
	args := []any{string(encoded)}
	if tracking {
		// Two indexed point-lookup sets support legacy handle snapshots as well
		// as the canonical IDs stored by new queues. No workspace inventory scan.
		query = `SELECT c.id, c.title, COALESCE(NULLIF(c.handle,''),c.id),CASE WHEN t.orphaned_at<>'' OR COALESCE(c.assignee,'')='' THEN 'awaiting_owner' ELSE 'default_promise_lapsed' END FROM cards c LEFT JOIN work_tracking t ON t.card_id=c.id WHERE c.id IN(SELECT value FROM json_each(?))
   UNION SELECT c.id,c.title,COALESCE(NULLIF(c.handle,''),c.id),CASE WHEN t.orphaned_at<>'' OR COALESCE(c.assignee,'')='' THEN 'awaiting_owner' ELSE 'default_promise_lapsed' END FROM cards c LEFT JOIN work_tracking t ON t.card_id=c.id WHERE c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle IN(SELECT value FROM json_each(?))`
		args = append(args, string(encoded))
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	type fact struct{ id, title, ref, reason string }
	facts := map[string]fact{}
	for rows.Next() {
		var f fact
		if err = rows.Scan(&f.id, &f.title, &f.ref, &f.reason); err != nil {
			rows.Close()
			return false, err
		}
		facts[f.id] = f
		if tracking {
			facts[f.ref] = f
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return false, err
	}
	items := []any{}
	text := ""
	for i, id := range ids {
		f, ok := facts[id]
		if !ok {
			continue
		}
		if tracking {
			ref := "card:" + f.ref
			items = append(items, map[string]any{"ref": ref, "title": f.title, "reason": f.reason})
			text += "\n" + ref + ": " + f.title + " (" + f.reason + ")"
		} else {
			items = append(items, map[string]any{"ref": "event:" + f.id, "title": f.title, "link": payload.Items[i].Link, "decide_by": payload.Items[i].DecideBy})
		}
	}
	if len(items) == 0 {
		return false, nil
	}
	d.SourceRef = ""
	d.LastError = ""
	d.Payload = map[string]any{"type": payload.Type, "workspace": payload.Workspace}
	if tracking {
		d.Payload["title"] = "Daily work tracking digest"
		d.Payload["text"] = fmt.Sprintf("Daily work tracking digest: %d card(s) need ownership or an explicit promise. This is a bounded preview; use anx work list for the full inventory.", len(items)) + text
		d.Payload["cards"] = items
		d.Payload["has_more"] = true
	} else {
		d.Payload["day"] = payload.Day
		d.Payload["items"] = items
		// Stored workspace counts may include newly denied contributors.
		d.Payload["open"] = len(items)
		d.Payload["overdue"] = len(items)
	}
	return true, nil
}
