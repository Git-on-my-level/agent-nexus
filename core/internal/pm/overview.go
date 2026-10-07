package pm

import (
	"context"
	"encoding/json"
)

// OverviewDecisions excludes completed history in SQL before hydration. The
// bounded candidate result retains normal PM permission and delivery checks.
func (s *Service) OverviewDecisions(ctx context.Context, p Principal) ([]Decision, map[string][]Action, bool, error) {
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return nil, nil, false, err
	}
	rows, err := s.store.database().QueryContext(ctx, `SELECT body FROM (
 SELECT * FROM (SELECT d.body,d.rowid AS position FROM pm_records d
 WHERE d.kind='decision' AND d.workspace_id=? AND json_extract(d.body,'$.status')='awaiting_answer' AND d.actor_id=? ORDER BY d.rowid DESC LIMIT 101)
 UNION ALL
 SELECT * FROM (SELECT d.body,d.rowid AS position FROM pm_records d
 WHERE d.kind='decision' AND d.workspace_id=? AND json_extract(d.body,'$.status')='answered'
 AND EXISTS(SELECT 1 FROM pm_records a WHERE a.kind='action' AND a.workspace_id=d.workspace_id AND a.parent_id=d.id AND (json_extract(a.body,'$.status')='failed' OR (a.actor_id=? AND json_extract(a.body,'$.status') IN ('pending_delivery','pending')))) ORDER BY d.rowid DESC LIMIT 101)
 ) ORDER BY position DESC LIMIT 101`, p.WorkspaceID, p.ActorID, p.WorkspaceID, p.ActorID)
	if err != nil {
		return nil, nil, false, err
	}
	ds, err := scanBodies[Decision](rows)
	if err != nil {
		return nil, nil, false, err
	}
	truncated := len(ds) > 100
	if truncated {
		ds = ds[:100]
	}
	ids := []string{}
	refs := []string{}
	for _, d := range ds {
		refs = append(refs, d.WorkRef)
	}
	readable, err := s.authorizeReadBatch(ctx, p, "pm.read", refs)
	if err != nil {
		return nil, nil, false, err
	}
	out := []Decision{}
	for _, d := range ds {
		if readable[d.WorkRef] {
			out = append(out, d)
			ids = append(ids, d.ID)
		}
	}
	out, err = s.decisionsForReader(ctx, p, out)
	if err != nil {
		return nil, nil, false, err
	}
	if len(ids) == 0 {
		return out, map[string][]Action{}, truncated, nil
	}
	raw, _ := json.Marshal(ids)
	rows, err = s.store.database().QueryContext(ctx, `SELECT body FROM pm_records WHERE kind='action' AND workspace_id=? AND parent_id IN (SELECT value FROM json_each(?)) AND (json_extract(body,'$.status')='failed' OR (actor_id=? AND json_extract(body,'$.status') IN ('pending_delivery','pending'))) LIMIT 201`, p.WorkspaceID, string(raw), p.ActorID)
	if err != nil {
		return nil, nil, false, err
	}
	actions, err := scanBodies[Action](rows)
	if err != nil {
		return nil, nil, false, err
	}
	if len(actions) > 200 {
		truncated = true
		actions = actions[:200]
	}
	refs = nil
	for _, a := range actions {
		refs = append(refs, a.WorkRef)
	}
	readable, err = s.authorizeReadBatch(ctx, p, "pm.read", refs)
	if err != nil {
		return nil, nil, false, err
	}
	byDecision := map[string][]Action{}
	for _, a := range actions {
		if readable[a.WorkRef] {
			byDecision[a.DecisionID] = append(byDecision[a.DecisionID], s.actionForReader(ctx, a))
		}
	}
	return out, byDecision, truncated, nil
}
