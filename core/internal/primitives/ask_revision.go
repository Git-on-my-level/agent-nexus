package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/askpresentation"
	"agent-nexus-core/internal/auth"
)

const AskRevisedEventType = "human_attention_revised"

var revisableAskKeys = map[string]bool{"title": true, "situation": true, "impact": true, "recommendation": true, "alternatives": true, "if_no_answer": true, "details": true}

// ValidateAskRevision rejects unknown keys, including nested keys. A new API
// field must be reviewed explicitly before becoming mutable here.
func ValidateAskRevision(p map[string]any) error {
	if len(p) == 0 {
		return workInvalid("presentation must contain at least one field")
	}
	for k, v := range p {
		if !revisableAskKeys[k] {
			return workInvalid("immutable or unknown presentation field: " + k)
		}
		if k == "title" {
			title, ok := v.(string)
			if !ok || strings.TrimSpace(title) == "" || strings.ContainsAny(title, "\r\n") || utf8.RuneCountInString(title) > 160 {
				return workInvalid("title must be a non-empty single line of at most 160 characters")
			}
		}
		if v == nil {
			continue
		}
		if k == "recommendation" {
			if obj, ok := v.(map[string]any); ok {
				for key := range obj {
					if key != "action" && key != "why" {
						return workInvalid("unknown recommendation field: " + key)
					}
				}
			}
		}
		if k == "alternatives" {
			if list, ok := v.([]any); ok {
				for _, val := range list {
					if obj, ok := val.(map[string]any); ok {
						for key := range obj {
							if key != "action" && key != "consequence" {
								return workInvalid("unknown alternative field: " + key)
							}
						}
					}
				}
			}
		}
	}
	nonnull := map[string]any{}
	for k, v := range p {
		if v != nil {
			nonnull[k] = v
		}
	}
	if err := askpresentation.Validate(nonnull); err != nil {
		return workInvalid(err.Error())
	}
	return nil
}
func askPresentation(payload map[string]any) map[string]any {
	p := map[string]any{}
	for k := range revisableAskKeys {
		if v, ok := payload[k]; ok && v != nil {
			p[k] = v
		}
	}
	return p
}
func applyAskPresentation(dst, p map[string]any) {
	for k := range revisableAskKeys {
		delete(dst, k)
	}
	for k, v := range p {
		if revisableAskKeys[k] {
			dst[k] = v
		}
	}
}

// AskPresentationStates hydrates one current, scoped audit event per admitted
// ask. The head and event primary keys bound reads independently of history.
// Callers supply already-authorized asks; this does not authorize ask contents.
func (s *Store) AskPresentationStates(ctx context.Context, asks []map[string]any) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	for start := 0; start < len(asks); start += 200 {
		end := min(start+200, len(asks))
		ids := []string{}
		for _, ask := range asks[start:end] {
			id := anyStringValue(ask["id"])
			ids = append(ids, id)
			out[id] = map[string]any{"ask_id": "event:" + id, "presentation_revision": 0, "presentation": askPresentation(asMapValue(ask["payload"]))}
		}
		if len(ids) == 0 {
			continue
		}
		raw, _ := json.Marshal(ids)
		rows, err := s.db.QueryContext(ctx, `SELECT h.ask_id,h.revision,e.id,e.actor_id,e.ts,e.payload_json FROM ask_presentation_heads h JOIN events e ON e.id=h.event_id WHERE h.ask_id IN (SELECT value FROM json_each(?))`, string(raw))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, event, actor, at, body string
			var revision int
			if err = rows.Scan(&id, &revision, &event, &actor, &at, &body); err != nil {
				rows.Close()
				return nil, err
			}
			var wrapper map[string]any
			if err = json.Unmarshal([]byte(body), &wrapper); err != nil {
				rows.Close()
				return nil, err
			}
			p := asMapValue(wrapper["payload"])
			out[id] = map[string]any{"ask_id": "event:" + id, "presentation_revision": revision, "presentation": p["presentation"], "presentation_rewrite": map[string]any{"actor_id": actor, "at": at, "event_ref": "event:" + event, "previous": p["previous"]}}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) GetAskPresentation(ctx context.Context, ref string) (map[string]any, error) {
	resolved, err := resolveResourceRef(ctx, s.db, ResourceRefInput{Type: "event", Ref: ref})
	if err != nil {
		return nil, err
	}
	ask, err := s.GetEvent(ctx, resolved.ID)
	if err != nil {
		return nil, err
	}
	if ask["type"] != "human_attention_requested" {
		return nil, ErrNotFound
	}
	states, err := s.AskPresentationStates(ctx, []map[string]any{ask})
	return states[resolved.ID], err
}

// ReviseAskPresentation leaves the original event byte-for-byte intact. Its
// counter, audit event and replay receipt commit in the same transaction.
func (s *Store) ReviseAskPresentation(ctx context.Context, principal auth.Principal, ref string, revision int, key, hash string, patch map[string]any, undo bool) (map[string]any, error) {
	if revision < 0 || key == "" || len(key) > 128 || strings.TrimSpace(key) != key || hash == "" {
		return nil, workInvalid("if_revision and request_key are required")
	}
	if undo {
		if patch != nil {
			return nil, workInvalid("undo and presentation are mutually exclusive")
		}
	} else if err := ValidateAskRevision(patch); err != nil {
		return nil, err
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	var result map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		var err error
		result, err = s.reviseAskPresentationTx(ctx, tx, principal, ref, revision, key, hash, patch, undo)
		return err
	})
	return result, err
}

func (s *Store) reviseAskPresentationTx(ctx context.Context, tx *accessTx, principal auth.Principal, ref string, revision int, key, hash string, patch map[string]any, undo bool) (map[string]any, error) {
	var err error
	// Acquire the SQLite write snapshot before reads, as ExecuteWithReplay does.
	// Concurrent retries then observe the committed head/receipt rather than
	// failing to upgrade an old read snapshot.
	if _, err = tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope='asks.revise' AND actor_id=? AND request_key=?`, principal.ActorID, key); err != nil {
		return nil, err
	}
	resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "event", Ref: ref})
	if err != nil {
		return nil, err
	}
	var raw, thread, requester string
	err = tx.QueryRowContext(ctx, `SELECT payload_json,COALESCE(thread_id,''),actor_id FROM events WHERE id=? AND type='human_attention_requested' AND COALESCE(trashed_at,'')=''`, resolved.ID).Scan(&raw, &thread, &requester)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var wrapper map[string]any
	if err = json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}
	payload := asMapValue(wrapper["payload"])
	if id := strings.TrimPrefix(anyStringValue(payload["requester_actor_id"]), "actor:"); id != "" {
		requester = id
	}
	if err = auth.RequireAskCuratorTx(ctx, tx, principal, requester); err != nil {
		return nil, err
	}
	// Resolve the subject in the same transaction even when an old imported ask
	// lacks a materialized subject edge. Admin authority never bypasses visibility.
	if subject := anyStringValue(payload["subject_ref"]); subject != "" {
		kind, _, ok := normalizeTypedRef(subject)
		if !ok || kind != "card" {
			return nil, workInvalid("ask must have a card subject")
		}
		if _, err = resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: subject}); err != nil {
			return nil, err
		}
	}
	var savedHash, saved string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_replays WHERE scope='asks.revise' AND actor_id=? AND request_key=?`, principal.ActorID, key).Scan(&savedHash, &saved)
	if err == nil {
		if savedHash != hash {
			return nil, ErrConflict
		}
		var result map[string]any
		err = json.Unmarshal([]byte(saved), &result)
		return result, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var closed int
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM human_attention_request_resolutions WHERE request_event_id=?) OR EXISTS(SELECT 1 FROM ask_subjects WHERE ask_id=? AND (open=0 OR due_at<=julianday('now')))`, resolved.ID, resolved.ID).Scan(&closed); err != nil {
		return nil, err
	}
	if closed != 0 {
		return nil, ErrHumanAttentionAlreadyResponded
	}
	current := askPresentation(payload)
	previous := map[string]any{}
	currentRevision := 0
	var headRaw string
	err = tx.QueryRowContext(ctx, `SELECT h.revision,e.payload_json FROM ask_presentation_heads h JOIN events e ON e.id=h.event_id WHERE h.ask_id=?`, resolved.ID).Scan(&currentRevision, &headRaw)
	if err == nil {
		var head map[string]any
		if err = json.Unmarshal([]byte(headRaw), &head); err != nil {
			return nil, err
		}
		p := asMapValue(head["payload"])
		current = asMapValue(p["presentation"])
		previous = asMapValue(p["previous"])
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if currentRevision != revision {
		return nil, ErrConflict
	}
	next := cloneMap(current)
	if undo {
		if currentRevision == 0 {
			return nil, ErrConflict
		}
		next = askPresentation(previous)
	} else {
		for k, v := range patch {
			if v == nil {
				delete(next, k)
			} else {
				next[k] = v
			}
		}
	}
	event, err := prepareEventForInsert(principal.ActorID, map[string]any{"type": AskRevisedEventType, "thread_id": thread, "summary": "Ask rewritten for clarity", "refs": []string{"event:" + resolved.ID, anyStringValue(payload["subject_ref"]), "thread:" + thread}, "payload": map[string]any{"request_event_id": resolved.ID, "presentation_revision": revision + 1, "previous": current, "presentation": next, "undo": undo}, "provenance": map[string]any{"sources": []string{"event:" + resolved.ID}}})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"ask_id": "event:" + resolved.ID, "presentation_revision": revision + 1, "presentation": next, "presentation_rewrite": map[string]any{"actor_id": principal.ActorID, "at": event.Body["ts"], "event_ref": "event:" + anyStringValue(event.Body["id"]), "previous": current}}
	encoded, _ := json.Marshal(result)
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(event.PayloadJSON) + len(event.RefsJSON) + len(encoded) + 1024)}, blobLedgerWritePlan{}); err != nil {
		return nil, err
	}
	if err = insertPreparedEvent(ctx, tx, event); err != nil {
		return nil, err
	}
	if currentRevision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO ask_presentation_heads(ask_id,revision,event_id) VALUES(?,?,?)`, resolved.ID, revision+1, event.Body["id"])
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE ask_presentation_heads SET revision=?,event_id=? WHERE ask_id=? AND revision=?`, revision+1, event.Body["id"], resolved.ID, revision)
		if err == nil {
			n, e := result.RowsAffected()
			if e != nil {
				return nil, e
			}
			if n != 1 {
				return nil, ErrConflict
			}
		}
	}
	if err != nil {
		return nil, askRevisionConflict(err)
	}

	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('asks.revise',?,?,?,200,?)`, principal.ActorID, key, hash, string(encoded)); err != nil {
		return nil, askRevisionConflict(err)
	}
	return result, nil
}

// ApplyAskPresentationState changes copy only, never answer or routing fields.
func ApplyAskPresentationState(dst, state map[string]any) {
	applyAskPresentation(dst, asMapValue(state["presentation"]))
	dst["presentation_revision"] = state["presentation_revision"]
	delete(dst, "presentation_rewrite")
	if v := state["presentation_rewrite"]; v != nil {
		dst["presentation_rewrite"] = v
	}
}

func askRevisionConflict(err error) error {
	var constraint *sqlite.Error
	if errors.As(err, &constraint) && (constraint.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || constraint.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return ErrConflict
	}
	return err
}
