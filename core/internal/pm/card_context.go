package pm

import (
	"context"
	"encoding/json"
	"strings"
)

// Only decisions owned by the requesting reader belong in their overview.
// Sixteen indexed candidates are admitted before canonical body hydration.
func (s *Service) attachReaderDecisions(ctx context.Context, p Principal, page ContextPage) (ContextPage, error) {
	rows, err := s.store.database().QueryContext(ctx, `SELECT r.body FROM (
 SELECT id FROM pm_reader_decision_positions WHERE kind='decision' AND workspace_id=? AND actor_id=? AND status='awaiting_answer' ORDER BY id LIMIT 16
 ) candidates CROSS JOIN pm_records r ON r.kind='decision' AND r.id=candidates.id LIMIT 15`, p.WorkspaceID, p.ActorID)
	if err != nil {
		return ContextPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return ContextPage{}, err
		}
		var d Decision
		if err = json.Unmarshal(raw, &d); err != nil {
			return ContextPage{}, err
		}
		page.Decisions = append(page.Decisions, map[string]any{"ref": "decision:" + d.ID, "card_ref": d.WorkRef, "reason": d.Instruction, "scope": d.Scope, "status": d.Status})
	}
	if err = rows.Err(); err != nil {
		return ContextPage{}, err
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 128*1024 {
		return ContextPage{}, ErrInvalid
	}
	return page, nil
}

// Each branch admits at most six rows using pm_records_card_awaiting before
// hydration. No workspace-wide decision list or per-card SQL round trip.
func (s *Service) attachPinnedDecisions(ctx context.Context, p Principal, page ContextPage) (ContextPage, error) {
	branches := []string{}
	args := []any{}
	cards := map[string]map[string]any{}
	for _, item := range page.Items {
		card, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref, _ := card["ref"].(string)
		if ref == "" {
			continue
		}
		card["decisions"] = []any{}
		cards[ref] = card
		branches = append(branches, `SELECT * FROM (SELECT r.body FROM (SELECT id FROM pm_card_decision_positions WHERE kind='decision' AND workspace_id=? AND card_ref=? AND status='awaiting_answer' ORDER BY id LIMIT 6) candidates JOIN pm_records r ON r.kind='decision' AND r.id=candidates.id)`)
		args = append(args, p.WorkspaceID, ref)
	}
	if len(branches) == 0 {
		return page, nil
	}
	rows, err := s.store.database().QueryContext(ctx, strings.Join(branches, " UNION ALL "), args...)
	if err != nil {
		return ContextPage{}, err
	}
	ds := []Decision{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return ContextPage{}, err
		}
		var d Decision
		if err = json.Unmarshal(raw, &d); err != nil {
			rows.Close()
			return ContextPage{}, err
		}
		ds = append(ds, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ContextPage{}, err
	}
	// The pm_records shadow authorizes the complete decision (including private
	// evidence) under the pinned requesting-reader snapshot, before hydration.
	for _, d := range ds {
		card := cards[d.WorkRef]
		items := card["decisions"].([]any)
		if len(items) == 5 {
			card["decisions_partial"] = true
			continue
		}
		card["decisions"] = append(items, map[string]any{"ref": "decision:" + d.ID, "reason": d.Instruction, "scope": d.Scope, "status": d.Status, "requester": d.ActorID})
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 128*1024 {
		return ContextPage{}, ErrInvalid
	}
	return page, nil
}
