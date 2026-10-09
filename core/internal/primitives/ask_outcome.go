package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// WithAskDeliveryPolicy configures generic routing and staleness. Empty order
// uses owner, requester, board role. Unknown entries are ignored.
func WithAskDeliveryPolicy(order []string, staleAfter time.Duration) Option {
	return func(s *Store) {
		for _, entry := range order {
			if entry = strings.TrimSpace(entry); entry == "owner" || entry == "requester" || entry == "board_role" {
				s.askNextActorOrder = append(s.askNextActorOrder, entry)
			}
		}
		s.askStaleAfter = staleAfter
	}
}

// AskOutcome performs indexed point lookups only. No projection is refreshed,
// no history is materialized, and each statement retains current authorization.
func (s *Store) AskOutcome(ctx context.Context, ref string) (map[string]any, error) {
	resolved, err := resolveResourceRef(ctx, s.db, ResourceRefInput{Type: "event", Ref: ref})
	if err != nil {
		return nil, err
	}
	var id, raw string
	err = s.db.QueryRowContext(ctx, `SELECT id,payload_json FROM events WHERE id=? AND type='human_attention_requested' AND COALESCE(trashed_at,'')=''`, resolved.ID).Scan(&id, &raw)
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
	ask := asMapValue(wrapper["payload"])
	out := map[string]any{"ask_id": "event:" + id, "subject_ref": ask["subject_ref"], "status": "open", "response": nil, "delivery": []any{}, "is_stale": false}
	var resolutionID, resolutionType string
	err = s.db.QueryRowContext(ctx, `SELECT resolution_event_id,resolution_type FROM human_attention_request_resolutions WHERE request_event_id=?`, id).Scan(&resolutionID, &resolutionType)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		var responseRaw, at string
		if err = s.db.QueryRowContext(ctx, `SELECT payload_json,ts FROM events WHERE id=? AND COALESCE(trashed_at,'')=''`, resolutionID).Scan(&responseRaw, &at); errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		var responseWrapper map[string]any
		if err = json.Unmarshal([]byte(responseRaw), &responseWrapper); err != nil {
			return nil, err
		}
		response := asMapValue(responseWrapper["payload"])
		response["response_event_id"] = resolutionID
		response["at"] = at
		out["response"] = response
		out["status"] = resolutionType
		if response["outcome"] == "needs_context" {
			out["status"] = "needs_context"
		}
		if response["reason"] == "expired" {
			out["status"] = "expired"
		}
		var resultRaw string
		if e := s.db.QueryRowContext(ctx, `SELECT response_json FROM human_attention_response_claims WHERE request_event_id=?`, id).Scan(&resultRaw); e == nil {
			var result map[string]any
			if e = json.Unmarshal([]byte(resultRaw), &result); e != nil {
				return nil, e
			}
			if result["task_outcome"] != nil {
				out["task_outcome"] = result["task_outcome"]
			}
		} else if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
	}
	if out["status"] == "open" {
		// Terminal expiry comes from the durable withdrawal, so subscribers
		// can acknowledge an existing delivery before leaving their stream.
		subject := anyStringValue(ask["subject_ref"])
		if strings.HasPrefix(subject, "card:") {
			card, resolveErr := resolveResourceRef(ctx, s.db, ResourceRefInput{Type: "card", Ref: subject})
			if resolveErr != nil && !errors.Is(resolveErr, ErrNotFound) {
				return nil, resolveErr
			}
			var at string
			err = s.db.QueryRowContext(ctx, `SELECT updated_at FROM cards WHERE id=?`, card.ID).Scan(&at)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			s.applyAskStaleness(out, at, time.Now().UTC())
		}
	}
	deliveries, err := s.askDeliveries(ctx, id)
	if err != nil {
		return nil, err
	}
	out["delivery"] = deliveries
	return out, nil
}
