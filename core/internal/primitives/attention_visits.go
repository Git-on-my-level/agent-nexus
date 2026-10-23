package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"
)

type AttentionState struct {
	Title     string `json:"title"`
	State     any    `json:"state,omitempty"`
	Plan      any    `json:"plan,omitempty"`
	Metric    any    `json:"metric,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}
type AttentionSnapshot map[string]AttentionState
type AttentionChange struct {
	Ref   string `json:"ref"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	From  any    `json:"from,omitempty"`
	To    any    `json:"to,omitempty"`
}
type AttentionChanges struct {
	Since       *string           `json:"since"`
	Items       []AttentionChange `json:"items"`
	ChangedRefs []string          `json:"changed_refs"`
	Truncated   bool              `json:"truncated"`
}

func (s *Store) AttentionWorkSnapshot(ctx context.Context, now time.Time) (AttentionSnapshot, bool, error) {
	page, err := s.ListReportWork(ctx, ReportWorkFilter{Limit: 50, IncludeClosed: true, WorkList: &WorkListFilter{}, skipOwnerContext: true, overviewSummary: true})
	if err != nil {
		return nil, false, err
	}
	work, truncated := page.Work, page.Truncated
	if err = s.EnrichCardPlans(ctx, work, nil, now); err != nil {
		return nil, false, err
	}
	statesAtVisit := overviewSnapshot(work)
	out := AttentionSnapshot{}
	for _, w := range work {
		// Snapshot explicit statuses, never timer-derived ages or prose labels.
		out[anyStringValue(w["ref"])] = AttentionState{Title: anyStringValue(w["title"]), State: map[string]any{"phase": w["phase"], "health": initiativeHealthState(w)}, Plan: statesAtVisit[anyStringValue(w["id"])].Steps, UpdatedAt: anyStringValue(w["updated_at"])}
	}
	for start := 0; start < len(work); start += 50 {
		end := min(start+50, len(work))
		ids := []string{}
		for _, w := range work[start:end] {
			if len(workMap(w["goal"])) > 0 || len(workMap(w["cadence"])) > 0 {
				ids = append(ids, anyStringValue(w["id"]))
			}
		}
		if len(ids) == 0 {
			continue
		}
		states, _, err := s.loadLoopStates(ctx, ids, now)
		if err != nil {
			return nil, false, err
		}
		for _, w := range work[start:end] {
			state, ok := states[anyStringValue(w["id"])]
			if !ok {
				continue
			}
			ref := anyStringValue(w["ref"])
			v := out[ref]
			v.Metric = workMap(state["goal"])["current"]
			if v.Metric == nil {
				v.Metric = workMap(state["goal"])["latest"]
			}
			out[ref] = v
		}
	}
	return out, truncated, nil
}

func (s *Store) RecordAttentionVisit(ctx context.Context, principal, scope string, snapshot AttentionSnapshot, now time.Time) error {
	if principal == "" || len(snapshot) > 100 {
		return workInvalid("authenticated principal and at most 100 snapshot subjects required")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, overviewVisitValidationKey{}, true)
	_, err = s.db.ExecContext(ctx, `INSERT INTO overview_visits(principal_id,scope_ref,visited_at,snapshot_json) VALUES(?,?,?,?) ON CONFLICT(principal_id,scope_ref) DO UPDATE SET visited_at=excluded.visited_at,snapshot_json=excluded.snapshot_json WHERE excluded.visited_at>overview_visits.visited_at`, principal, scope, now.UTC().Format("2006-01-02T15:04:05.000000000Z"), string(raw))
	return err
}
func (s *Store) AttentionVisitChanges(ctx context.Context, principal, scope string, current AttentionSnapshot, truncated bool) (AttentionChanges, error) {
	out := AttentionChanges{Items: []AttentionChange{}, ChangedRefs: []string{}, Truncated: truncated}
	var since, raw string
	err := s.db.QueryRowContext(ctx, `SELECT visited_at,snapshot_json FROM overview_visits WHERE principal_id=? AND scope_ref=?`, principal, scope).Scan(&since, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Since = &since
	before := AttentionSnapshot{}
	if err = json.Unmarshal([]byte(raw), &before); err != nil {
		return out, err
	}
	// Normalize typed plan structs and decoded JSON before comparing.
	encoded, err := json.Marshal(current)
	if err != nil {
		return out, err
	}
	current = AttentionSnapshot{}
	if err = json.Unmarshal(encoded, &current); err != nil {
		return out, err
	}
	refs := []string{}
	for ref := range current {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		old, known := before[ref]
		v := current[ref]
		changed := false
		if !known {
			at, _ := time.Parse(time.RFC3339Nano, v.UpdatedAt)
			last, _ := time.Parse(time.RFC3339Nano, since)
			if at.After(last) {
				out.Items = append(out.Items, AttentionChange{Ref: ref, Kind: "new_item", Title: v.Title})
				changed = true
			}
		} else {
			for _, field := range []struct {
				kind     string
				from, to any
			}{{"state_changed", old.State, v.State}, {"plan_changed", old.Plan, v.Plan}, {"metric_moved", old.Metric, v.Metric}} {
				if !reflect.DeepEqual(field.from, field.to) {
					out.Items = append(out.Items, AttentionChange{Ref: ref, Kind: field.kind, Title: v.Title, From: field.from, To: field.to})
					changed = true
				}
			}
		}
		if changed {
			out.ChangedRefs = append(out.ChangedRefs, ref)
		}
	}
	if len(out.Items) > MaxOverviewChanges {
		out.Items = out.Items[:MaxOverviewChanges]
		out.Truncated = true
		out.ChangedRefs = []string{}
		seen := map[string]bool{}
		for _, i := range out.Items {
			if !seen[i.Ref] {
				out.ChangedRefs = append(out.ChangedRefs, i.Ref)
				seen[i.Ref] = true
			}
		}
	}
	return out, nil
}
