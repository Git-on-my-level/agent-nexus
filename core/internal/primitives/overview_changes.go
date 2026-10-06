package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

const MaxOverviewChanges = 100
const maxOverviewEventCandidates = 200
const maxOverviewSubjectRefs = 4000

type OverviewChange struct {
	Kind   string `json:"kind"`
	KindV2 string `json:"kind_v2,omitempty"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	StepID string `json:"step_id,omitempty"`
	TS     string `json:"ts,omitempty"`
}
type OverviewChanges struct {
	Since       *string          `json:"since"`
	GeneratedAt string           `json:"generated_at"`
	Items       []OverviewChange `json:"items"`
	Truncated   bool             `json:"truncated"`
}

func (d *OverviewChanges) Add(item OverviewChange) {
	if len(d.Items) == MaxOverviewChanges {
		d.Truncated = true
		return
	}
	if item.Kind == "initiative_stale" {
		item.KindV2 = item.Kind
		item.Kind = "initiative_stalled"
	}
	d.Items = append(d.Items, item)
}

type visitState struct {
	Health string            `json:"health"`
	Steps  map[string]string `json:"steps"`
}

func overviewSnapshot(work []map[string]any) map[string]visitState {
	out := map[string]visitState{}
	for _, w := range work {
		v := visitState{Health: anyStringValue(initiativeHealth(w)["status"]), Steps: map[string]string{}}
		if state, ok := w["plan_state"].(plans.State); ok {
			p := w["plan"].(plans.Plan)
			for i, step := range state.Steps {
				if p.Steps[i].Ref == "" || step.Resolvable {
					v.Steps[step.ID] = step.Status
				}
			}
		}
		out[anyStringValue(w["id"])] = v
	}
	return out
}

// The database is already workspace scoped. Only authenticated principal IDs
// are supplied by HTTP; no caller-controlled actor or timestamp is accepted.
func (s *Store) RecordOverviewVisit(ctx context.Context, principal string, work []map[string]any, now time.Time) error {
	if principal == "" {
		return nil
	}
	raw, err := json.Marshal(overviewSnapshot(work))
	if err != nil {
		return err
	}
	// A slower old request cannot replace a newer visit or its snapshot.
	_, err = s.db.ExecContext(ctx, `INSERT INTO overview_visits(principal_id,visited_at,snapshot_json) VALUES(?,?,?)
 ON CONFLICT(principal_id) DO UPDATE SET visited_at=excluded.visited_at,snapshot_json=excluded.snapshot_json
 WHERE excluded.visited_at>overview_visits.visited_at`, principal, now.UTC().Format("2006-01-02T15:04:05.000000000Z"), string(raw))
	return err
}

func (s *Store) OverviewChanges(ctx context.Context, principal string, work []map[string]any, truncated bool, visible func(string, string) bool, now time.Time) (OverviewChanges, error) {
	out := OverviewChanges{GeneratedAt: now.UTC().Format(time.RFC3339Nano), Items: []OverviewChange{}, Truncated: truncated}
	if principal == "" {
		return out, nil
	}
	var since, raw string
	err := s.db.QueryRowContext(ctx, `SELECT visited_at,snapshot_json FROM overview_visits WHERE principal_id=?`, principal).Scan(&since, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Since = &since
	before := map[string]visitState{}
	if err = json.Unmarshal([]byte(raw), &before); err != nil {
		return out, err
	}
	for _, w := range work {
		old, known := before[anyStringValue(w["id"])]
		if !known {
			continue
		}
		ref, title := anyStringValue(w["ref"]), anyStringValue(w["title"])
		health := anyStringValue(initiativeHealth(w)["status"])
		if health == "stalled" {
			health = "stale"
		}
		if old.Health == "stalled" {
			old.Health = "stale"
		} // Saved visits from older cores.
		if health != old.Health && (health == "stale" || health == "blocked") && w["phase"] != "done" && w["phase"] != "cancelled" {
			out.Add(OverviewChange{Kind: "initiative_" + health, Ref: ref, Title: title})
		}
		if state, ok := w["plan_state"].(plans.State); ok {
			p := w["plan"].(plans.Plan)
			for i, step := range state.Steps {
				prior, existed := old.Steps[step.ID]
				if !existed || prior == "done" || step.Status != "done" {
					continue
				}
				if p.Steps[i].Ref != "" && !step.Resolvable {
					continue
				}
				out.Add(OverviewChange{Kind: "step_completed", Ref: ref, Title: p.Steps[i].Title, StepID: step.ID})
			}
		}
	}
	err = s.overviewAnsweredAsks(ctx, &out, visible, now)
	return out, err
}

// Read one accessible event page, then resolve subjects in bounded batches. Answer
// text is deliberately absent: a compact digest needs only the existence of an
// answer and a safe navigation ref. Inbox subject refs are folded into the same
// batch and both the event and inbox backing threads must be readable.
func (s *Store) overviewAnsweredAsks(ctx context.Context, out *OverviewChanges, visible func(string, string) bool, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.ts,e.refs_json,COALESCE(t.id,''),COALESCE(json_extract(t.body_json,'$.pm_actor_id'),''),
 COALESCE(i.thread_id,''),COALESCE(json_extract(it.body_json,'$.pm_actor_id'),''),COALESCE(json_extract(i.data_json,'$.related_refs'),json_extract(i.data_json,'$.refs'),'[]')
 FROM events e LEFT JOIN threads t ON t.id=COALESCE(NULLIF(e.thread_id,''),(SELECT substr(value,8) FROM json_each(e.refs_json) WHERE value LIKE 'thread:%' LIMIT 1))
 LEFT JOIN derived_inbox_items i ON i.id=COALESCE(json_extract(e.payload_json,'$.payload.inbox_item_id'),json_extract(e.payload_json,'$.inbox_item_id'),(SELECT substr(value,7) FROM json_each(e.refs_json) WHERE value LIKE 'inbox:%' LIMIT 1))
 LEFT JOIN threads it ON it.id=i.thread_id
 WHERE e.type='human_attention_responded' AND e.trashed_at IS NULL AND e.archived_at IS NULL
 AND (t.id IS NULL OR (t.trashed_at IS NULL AND t.archived_at IS NULL))
 AND (it.id IS NULL OR (it.trashed_at IS NULL AND it.archived_at IS NULL))

 AND `+backingThreadLifecycleSQL(ctx, `t.id`, true)+`
 AND (i.id IS NULL OR (`+inboxReadSQL(ctx)+`))
 AND NOT EXISTS (SELECT 1 FROM json_each(e.refs_json) answer_ref WHERE NOT (`+referenceLifecycleSQL(ctx, `answer_ref.value`, true)+`))
 AND julianday(e.ts)>=julianday(?) AND julianday(e.ts)<=julianday(?)
 ORDER BY e.ts DESC,e.id DESC LIMIT ?`, *out.Since, now.Format(time.RFC3339Nano), maxOverviewEventCandidates+1)
	if err != nil {
		return err
	}
	type answer struct {
		id, ts, thread, owner, inboxThread, inboxOwner string
		refs                                           []string
	}
	since, err := time.Parse(time.RFC3339Nano, *out.Since)
	if err != nil {
		rows.Close()
		return err
	}
	answers, refs := []answer{}, []string{}
	seen := map[string]bool{}
	candidates := 0
	for rows.Next() {
		var a answer
		var raw, related string
		if err = rows.Scan(&a.id, &a.ts, &raw, &a.thread, &a.owner, &a.inboxThread, &a.inboxOwner, &related); err != nil {
			rows.Close()
			return err
		}
		candidates++
		if candidates > maxOverviewEventCandidates {
			out.Truncated = true
			continue
		}
		at, parseErr := time.Parse(time.RFC3339Nano, a.ts)
		// SQLite date math rounds sub-millisecond timestamps. Include its
		// boundary candidates, then enforce the exact interval in Go.
		if parseErr != nil || !at.After(since) || at.After(now) {
			continue
		}
		if err = json.Unmarshal([]byte(raw), &a.refs); err != nil {
			rows.Close()
			return err
		}
		var subject []string
		if err = json.Unmarshal([]byte(related), &subject); err != nil {
			rows.Close()
			return err
		}
		a.refs = append(a.refs, subject...)
		complete := true
		for _, ref := range a.refs {
			if !digestSubjectRef(ref) || seen[ref] {
				continue
			}
			if len(refs) == maxOverviewSubjectRefs {
				out.Truncated = true
				complete = false
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
		// Never infer an answer's eligibility from a partially resolved subject set.
		if complete {
			answers = append(answers, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	readable := map[string]bool{}
	refs = uniqueSortedStrings(refs)
	for start := 0; start < len(refs); start += 200 {
		end := min(start+200, len(refs))
		previews, err := s.readRefFacts(ctx, refs[start:end], visible)
		if err != nil {
			return err
		}
		for _, p := range previews {
			readable[p.Ref] = p.Resolvable
		}
	}
	for _, a := range answers {
		if visible != nil && (!visible(a.thread, a.owner) || !visible(a.inboxThread, a.inboxOwner)) {
			continue
		}
		allowed := a.thread != "" || a.inboxThread != ""
		for _, ref := range a.refs {
			if strings.HasPrefix(ref, "thread:") && ref != "thread:"+a.thread && ref != "thread:"+a.inboxThread {
				allowed = false
				break
			}

			if digestSubjectRef(ref) {
				if !readable[ref] {
					allowed = false
					break
				}
				allowed = true
			}
		}
		if allowed {
			out.Add(OverviewChange{Kind: "ask_answered", Ref: "event:" + a.id, Title: "Ask answered", TS: a.ts})
		}
	}
	return nil
}
func digestSubjectRef(ref string) bool {
	kind, _, _ := strings.Cut(ref, ":")
	return kind == "card" || kind == "board" || kind == "topic" || kind == "doc" || kind == "document"
}

func (s *Store) LoadOverviewChanges(ctx context.Context, principal string, visible func(string, string) bool, now time.Time, threshold time.Duration) (OverviewChanges, error) {
	work, truncated, err := s.overviewWork(ctx, visible, now, threshold, true)
	if err != nil {
		return OverviewChanges{}, err
	}
	return s.OverviewChanges(ctx, principal, work, truncated, visible, now)
}
