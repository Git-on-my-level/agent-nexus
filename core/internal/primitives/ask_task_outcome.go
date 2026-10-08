package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// applyAskTaskOutcomeTx records the decision on the canonical card in the same
// transaction as its response claim. Legacy non-card requests stay readable.
func (s *Store) applyAskTaskOutcomeTx(ctx context.Context, tx *accessTx, actor, askID string, response map[string]any) (map[string]any, error) {
	var raw, askHandle string
	if err := tx.QueryRowContext(ctx, `SELECT payload_json,COALESCE(handle,'') FROM events WHERE id=? AND type='human_attention_requested'`, askID).Scan(&raw, &askHandle); err != nil {
		return nil, err
	}
	var wrapper map[string]any
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}
	ask := asMapValue(wrapper["payload"])
	if expires := anyStringValue(ask["expires_at"]); expires != "" {
		at, e := time.Parse(time.RFC3339Nano, expires)
		if e != nil {
			return nil, fmt.Errorf("invalid expiry")
		}
		if !time.Now().Before(at) {
			return nil, ErrHumanAttentionAlreadyResponded
		}
	}

	ref := strings.TrimSpace(anyStringValue(ask["subject_ref"]))
	if !strings.HasPrefix(ref, "card:") {
		return nil, nil
	}
	resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: ref})
	if err != nil {
		return nil, err
	}
	cardID := resolved.ID
	card, err := s.loadBoardCardByGlobalID(ctx, tx, cardID, true)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if card.ArchivedAt.Valid && card.ArchivedAt.String != "" || card.TrashedAt.Valid && card.TrashedAt.String != "" || card.ColumnKey == "done" {
		return nil, ErrHumanAttentionAlreadyResponded
	}
	var metaRaw, authority string
	err = tx.QueryRowContext(ctx, `SELECT metadata_json,authority FROM work_metadata WHERE card_id=?`, card.CardID).Scan(&metaRaw, &authority)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	meta := map[string]any{}
	if metaRaw != "" {
		if err = json.Unmarshal([]byte(metaRaw), &meta); err != nil {
			return nil, err
		}
	}
	if authority == "" {
		authority = "nexus"
		meta["source"] = map[string]any{"authority": "nexus"}
	}
	payload := asMapValue(response["payload"])
	outcome := anyStringValue(payload["outcome"])
	at := anyStringValue(response["ts"])
	responseRef := "event:" + anyStringValue(response["id"])
	board, err := loadBoardRow(ctx, tx, card.BoardID)
	if err != nil {
		return nil, err
	}
	next := ""
	// A deployment may override this ordered chain with a store option. Every
	// value comes from the task, requester, or board, never a fixed identity.
	order := s.askNextActorOrder
	if len(order) == 0 {
		order = []string{"owner", "requester", "board_role"}
	}
	for _, source := range order {
		switch source {
		case "owner":
			next = card.Assignee.String
		case "requester":
			next = firstNonEmpty(anyStringValue(ask["requester_label"]), anyStringValue(ask["requester_actor_id"]))
		case "board_role":
			next = board.Role
		}
		if next != "" {
			break
		}
	}
	if outcome == "needs_context" {
		// Clarification belongs to the author/owner even when ordinary answers
		// route through a configured board role.
		next = firstNonEmpty(card.Assignee.String, anyStringValue(ask["requester_label"]), anyStringValue(ask["requester_actor_id"]))
	}
	phase := card.ColumnKey
	reason := ""
	if authority == "nexus" {
		blockers, _ := normalizeStringSlice(meta["blockers"])
		remaining := make([]string, 0, len(blockers))
		matched := false
		for _, blocker := range blockers {
			if blocker == "event:"+askID || askHandle != "" && blocker == "event:"+askHandle {
				matched = true
				continue
			}
			remaining = append(remaining, blocker)
		}
		// No inference from the phase or response prose: only an explicit blocker
		// reference proves that this ask was the sole reason the card was blocked.
		if matched {
			meta["blockers"] = remaining
			if len(remaining) == 0 && phase == "blocked" {
				phase = "ready"
			}
		}
		if outcome == "resolved" {
			phase = "done"
		}
	} else {
		reason = "source_owned"
	}
	meta["next_actor"] = next
	meta["next_action"] = anyStringValue(payload["response_text"])
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,?,?,1,?,?) ON CONFLICT(card_id) DO UPDATE SET metadata_json=excluded.metadata_json,version=work_metadata.version+1,updated_at=excluded.updated_at,updated_by=excluded.updated_by`, card.CardID, authority, workJSON(meta), at, actor); err != nil {
		return nil, err
	}
	if phase != card.ColumnKey {
		resolution := card.Resolution.String
		resolutionRefs := card.ResolutionRefsJSON
		if phase == "done" {
			resolution = "done"
			b, _ := json.Marshal([]string{responseRef})
			resolutionRefs = string(b)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET column_key=?,resolution=?,resolution_refs_json=?,updated_at=?,updated_by=? WHERE id=?`, phase, nullableString(resolution), resolutionRefs, at, actor, card.CardID); err != nil {
			return nil, err
		}
		if err = upsertBoardCardRefEdge(ctx, tx, card.BoardID, card.CardID, phase, card.Rank); err != nil {
			return nil, err
		}
		if _, err = touchBoardRow(ctx, tx, board, actor); err != nil {
			return nil, err
		}
	}
	task := map[string]any{"card_ref": resolved.CanonicalRef, "phase": phase, "next_actor": next, "reason": reason}
	decision, err := prepareEventForInsert(actor, map[string]any{"type": "card_updated", "thread_id": card.ThreadID.String, "summary": "Ask decision: " + anyStringValue(payload["response_text"]), "refs": []string{"card:" + card.CardID, "event:" + askID, responseRef}, "payload": map[string]any{"request_event_ref": "event:" + askID, "response_event_ref": responseRef, "outcome": outcome, "task_outcome": task}, "provenance": map[string]any{"sources": []string{responseRef}}})
	if err != nil {
		return nil, err
	}
	if err = insertPreparedEvent(ctx, tx, decision); err != nil {
		return nil, fmt.Errorf("record task decision: %w", err)
	}
	return task, nil
}
