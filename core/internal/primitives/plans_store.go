package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

type PreviewBoard struct {
	Ref   string `json:"ref"`
	Title string `json:"title"`
}
type PreviewStep = plans.NextStep

// RefPreview contains only the bounded data needed by chips and plan derivation.
// Internal fields support principal-scoped reads and never appear on the wire.
type RefPreview struct {
	Ref                     string          `json:"ref"`
	Kind                    string          `json:"kind,omitempty"`
	Title                   string          `json:"title,omitempty"`
	Status                  string          `json:"status,omitempty"`
	Authority               string          `json:"authority,omitempty"`
	NativeID                string          `json:"native_id,omitempty"`
	ConnectionID            string          `json:"connection_id,omitempty"`
	ObservedAt              string          `json:"observed_at,omitempty"`
	Source                  string          `json:"source,omitempty"`
	PlanHealth              *plans.Health   `json:"plan_health,omitempty"`
	PlanResolutionTruncated bool            `json:"plan_resolution_truncated,omitempty"`
	StatusMismatch          bool            `json:"status_mismatch,omitempty"`
	CreatedAt               time.Time       `json:"-"`
	DueAt                   string          `json:"-"`
	Phase                   string          `json:"phase,omitempty"`
	Owner                   string          `json:"owner,omitempty"`
	OwnerDisplay            string          `json:"owner_display,omitempty"`
	Board                   *PreviewBoard   `json:"board,omitempty"`
	Priority                string          `json:"priority,omitempty"`
	LastMovedAt             string          `json:"last_moved_at,omitempty"`
	NextStep                *PreviewStep    `json:"next_step,omitempty"`
	Progress                *plans.Progress `json:"progress,omitempty"`
	URL                     string          `json:"url,omitempty"`
	Resolvable              bool            `json:"resolvable"`
	ID                      string          `json:"-"`
	MovementAt              time.Time       `json:"-"`
}

const maxPlanRefBudget = 4000

// readRefFacts accepts bounded batches of at most 200 refs. It never
// fetches arbitrary URLs, nor interprets missing evidence as completed work.
func (s *Store) readRefFacts(ctx context.Context, refs []string, visible func(string, string) bool) ([]RefPreview, error) {
	if visible != nil {
		if _, scoped := accessScopeFrom(ctx); !scoped {
			return nil, invalidBoardRequest("principal read access scope is required for visibility-filtered resolution")
		}
	}
	if len(refs) > 200 {
		return nil, invalidBoardRequest("refs must contain at most 200 strings per batch")
	}
	out := make([]RefPreview, len(refs))
	for i, ref := range refs {
		out[i] = RefPreview{Ref: ref}
	}
	groups := map[string][]string{}
	for _, ref := range refs {
		kind, value, ok := strings.Cut(ref, ":")
		if kind == "doc" {
			kind = "document"
		}
		if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
			kind, value, ok = "card", ref, true
		}
		if ok && value != "" && (kind == "card" || kind == "document" || kind == "topic" || kind == "board") {
			groups[kind] = append(groups[kind], value)
		}
	}
	for _, kind := range []string{"card", "document", "topic", "board"} {
		values := uniqueSortedStrings(groups[kind])
		if len(values) == 0 {
			continue
		}
		encoded, err := json.Marshal(values)
		if err != nil {
			return nil, err
		}
		marks := "SELECT value FROM json_each(?)"
		args := []any{}
		for j := 0; j < 2; j++ {
			args = append(args, string(encoded))
		}
		var query string
		switch kind {
		case "card":
			args = []any{string(encoded), "", string(encoded), "", string(encoded), ""}
			query = `WITH requested_cards AS (
 SELECT cards.id FROM cards INDEXED BY sqlite_autoindex_cards_1 WHERE id IN (SELECT value FROM json_each(?)) AND id>? AND cards.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=cards.board_id AND lifecycle_board.trashed_at IS NOT NULL)
 UNION SELECT id FROM cards INDEXED BY idx_cards_handle_unique WHERE handle IS NOT NULL AND trim(handle) <> '' AND handle IN (SELECT value FROM json_each(?)) AND id>? AND cards.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=cards.board_id AND lifecycle_board.trashed_at IS NOT NULL)
 UNION SELECT bounded.value AS id FROM json_each(?) requested CROSS JOIN json_each((SELECT json_group_array(card_id) FROM (SELECT m.card_id FROM work_metadata m INDEXED BY idx_work_source_url JOIN cards access_card ON access_card.id=m.card_id WHERE json_extract(m.metadata_json,'$.source.url')=requested.value AND m.authority!='nexus' AND access_card.trashed_at IS NULL AND NOT EXISTS (SELECT 1 FROM boards lifecycle_board WHERE lifecycle_board.id=access_card.board_id AND lifecycle_board.trashed_at IS NOT NULL) ORDER BY m.card_id LIMIT 2))) bounded WHERE bounded.value>?
 ORDER BY id LIMIT 200
 ) SELECT c.id,COALESCE(c.handle,''),CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.title ELSE COALESCE(json_extract(o.body_json,'$.facts.title'),json_extract(m.metadata_json,'$.title'),c.title) END,
			 CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.column_key ELSE COALESCE(json_extract(o.body_json,'$.facts.phase'),json_extract(m.metadata_json,'$.phase'),'unknown') END,
			 CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN CASE WHEN COALESCE(c.assignee,'')='' THEN '' WHEN c.assignee LIKE '%:%' THEN c.assignee ELSE 'actor:'||c.assignee END ELSE COALESCE(json_extract(o.body_json,'$.facts.owner'),json_extract(m.metadata_json,'$.owner'),'') END,
			 COALESCE(json_extract(m.metadata_json,'$.source.url'),''),COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id),''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id))),''),
			 ` + effectiveCardActivitySQL + `,
 COALESCE(json_extract(m.metadata_json,'$.priority'),'none'),
 COALESCE(NULLIF(b.handle,''),b.id,''),COALESCE(b.title,''),COALESCE(b.thread_id,''),COALESCE(json_extract(bt.body_json,'$.pm_actor_id'),''),
 COALESCE((SELECT display_name FROM actors WHERE id=CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN replace(c.assignee,'actor:','') ELSE replace(COALESCE(json_extract(o.body_json,'$.facts.owner'),json_extract(m.metadata_json,'$.owner'),''),'actor:','') END),'')
			 , c.created_at, ` + effectiveCardDueSQL + `, COALESCE((SELECT e.ts FROM events e WHERE e.thread_id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id)) AND e.type='message_posted' AND e.trashed_at IS NULL AND e.archived_at IS NULL ORDER BY e.ts DESC LIMIT 1),''),COALESCE((SELECT p.updated_at FROM card_plans p WHERE p.card_id=c.id),''),COALESCE(c.trashed_at,''),COALESCE(b.trashed_at,'')
 FROM requested_cards r JOIN cards c ON c.id=r.id LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id
 LEFT JOIN boards b ON b.id=c.board_id
 LEFT JOIN threads bt ON bt.id=b.thread_id
			 ORDER BY c.id`
		case "document":
			query = `SELECT id,handle,COALESCE(title,''),CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,'','',COALESCE(thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=documents.thread_id),''),updated_at,'','','','','','',created_at,'','','','','' FROM documents WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		case "topic":
			query = `SELECT id,handle,title,CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,COALESCE(json_extract(extensions_json,'$.owner_refs[0]'),''),'',thread_id,COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=topics.thread_id),''),updated_at,'','','','','',COALESCE((SELECT display_name FROM actors WHERE id=replace(json_extract(topics.extensions_json,'$.owner_refs[0]'),'actor:','')),''),created_at,'','','','','' FROM topics WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		case "board":
			query = `SELECT id,handle,title,CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,COALESCE(json_extract(owners_json,'$[0]'),''),'',thread_id,COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=boards.thread_id),''),updated_at,'','','','','',COALESCE((SELECT display_name FROM actors WHERE id=replace(json_extract(boards.owners_json,'$[0]'),'actor:','')),''),created_at,'','','','','' FROM boards WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		}
		candidates := map[string][]RefPreview{}
		wanted := map[string]bool{}
		for _, value := range values {
			wanted[value] = true
		}
		for {
			rows, err := s.db.QueryContext(ctx, query, args...)
			if err != nil {
				return nil, err
			}
			type row struct{ id, handle, title, phase, owner, external, thread, privateOwner, at, priority, boardHandle, boardTitle, boardThread, boardOwner, ownerDisplay, created, due, message, planAt, trashed, boardTrashed string }
			items := []row{}
			for rows.Next() {
				var item row
				if err = rows.Scan(&item.id, &item.handle, &item.title, &item.phase, &item.owner, &item.external, &item.thread, &item.privateOwner, &item.at, &item.priority, &item.boardHandle, &item.boardTitle, &item.boardThread, &item.boardOwner, &item.ownerDisplay, &item.created, &item.due, &item.message, &item.planAt, &item.trashed, &item.boardTrashed); err != nil {
					rows.Close()
					return nil, err
				}
				items = append(items, item)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			// Access checks run after closing rows, including on one-connection SQLite.
			for _, item := range items {
				needed := false
				for _, key := range []string{item.id, item.handle, item.external} {
					if wanted[key] && len(candidates[key]) < 2 {
						needed = true
					}
				}
				if !needed {
					continue
				}
				if item.trashed != "" || item.boardTrashed != "" {
					continue
				}
				if visible != nil && (!visible(item.thread, item.privateOwner) || (kind == "card" && !visible(item.boardThread, item.boardOwner))) {
					continue
				}
				preview := RefPreview{ID: item.id, Kind: kind, Title: item.title, Status: item.phase, Owner: item.owner, Resolvable: true}
				// Topics and boards have no current UI detail surface. Do not return
				// fabricated paths that would turn a resolvable chip into a 404.
				if pathKind := map[string]string{"card": "tasks", "document": "docs"}[kind]; pathKind != "" {
					preview.URL = "/" + pathKind + "/" + url.PathEscape(item.handle)
				}
				if kind == "card" {
					preview.Phase = item.phase
				}
				preview.OwnerDisplay = firstNonEmptyString(item.ownerDisplay, item.owner)
				if kind == "card" {
					preview.Priority = item.priority
					if item.boardHandle != "" {
						preview.Board = &PreviewBoard{Ref: "board:" + item.boardHandle, Title: item.boardTitle}
					}
				}
				preview.CreatedAt, _ = time.Parse(time.RFC3339Nano, item.created)
				preview.DueAt = item.due
				preview.LastMovedAt = item.at
				preview.MovementAt = latestCardActivity(item.at, item.planAt, item.message)
				if !preview.MovementAt.IsZero() {
					preview.LastMovedAt = preview.MovementAt.UTC().Format(time.RFC3339Nano)
				}
				keys := uniqueSortedStrings([]string{item.id, item.handle, item.external})
				for _, key := range keys {
					if wanted[key] && len(candidates[key]) < 2 {
						candidates[key] = append(candidates[key], preview)
					}
				}
			}
			if kind != "card" || len(items) < 200 {
				break
			}
			cursor := items[len(items)-1].id
			args[1], args[3], args[5] = cursor, cursor, cursor
		}

		for i, ref := range refs {
			prefix, value, _ := strings.Cut(ref, ":")
			if prefix == "doc" {
				prefix = "document"
			}
			if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
				prefix, value = "card", ref
			}
			if prefix != kind || len(candidates[value]) != 1 {
				continue
			}
			out[i] = candidates[value][0]
			out[i].Ref = ref
			if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
				out[i].URL = ref
			}
		}
	}
	if err := s.readExternalRefFacts(ctx, refs, out, visible); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) loadPlans(ctx context.Context, ids []string) (map[string]plans.Plan, map[string]cardHealthInput, map[string]any, error) {
	sourceRefs := map[string]any{}
	out, movement := map[string]plans.Plan{}, map[string]cardHealthInput{}
	ids = uniqueSortedStrings(ids)
	if len(ids) == 0 {
		return out, movement, sourceRefs, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,COALESCE(p.body_json,''),`+effectiveCardActivitySQL+`,COALESCE(p.updated_at,''),COALESCE((SELECT e.ts FROM events e WHERE e.thread_id=COALESCE(NULLIF(trim(c.thread_id),''),trim(c.parent_thread_id)) AND e.type='message_posted' AND e.trashed_at IS NULL AND e.archived_at IS NULL ORDER BY e.ts DESC LIMIT 1),''),COALESCE(json_extract(m.metadata_json,'$.source_refs'),'[]'),c.created_at,`+effectiveCardDueSQL+` FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN card_plans p ON p.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw, at, planAt, messageAt, refsRaw, created, due string
		if err = rows.Scan(&id, &raw, &at, &planAt, &messageAt, &refsRaw, &created, &due); err != nil {
			return nil, nil, nil, err
		}
		var entries []any
		if err = json.Unmarshal([]byte(refsRaw), &entries); err != nil {
			return nil, nil, nil, err
		}
		sourceRefs[id] = entries
		if raw != "" {
			var p plans.Plan
			if err = json.Unmarshal([]byte(raw), &p); err != nil {
				return nil, nil, nil, err
			}
			out[id] = p
		}
		input := cardHealthInput{Due: due}
		input.Created, _ = time.Parse(time.RFC3339Nano, created)
		input.Activity = latestCardActivity(at, planAt, messageAt)
		movement[id] = input
	}
	return out, movement, sourceRefs, rows.Err()
}

func (s *Store) planFacts(ctx context.Context, ps map[string]plans.Plan, visible func(string, string) bool) (map[string]plans.Fact, error) {
	refs, seen := []string{}, map[string]bool{}
	ids := []string{}
	for id := range ps {
		ids = append(ids, id)
	}
	ids = uniqueSortedStrings(ids)
	// Round-robin plans so a large plan cannot starve every other initiative.
	for stepIndex := 0; stepIndex < plans.MaxSteps && len(refs) < maxPlanRefBudget; stepIndex++ {
		for _, id := range ids {
			if stepIndex >= len(ps[id].Steps) {
				continue
			}
			ref := ps[id].Steps[stepIndex].Ref
			if ref != "" && !seen[ref] && len(refs) < maxPlanRefBudget {
				refs = append(refs, ref)
				seen[ref] = true
			}
		}
	}
	facts := map[string]plans.Fact{}
	for start := 0; start < len(refs); start += 200 {
		end := start + 200
		if end > len(refs) {
			end = len(refs)
		}
		rows, err := s.readRefFacts(ctx, refs[start:end], visible)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			facts[row.Ref] = plans.Fact{Known: row.Resolvable, Status: row.Phase, MovementAt: row.MovementAt}
		}
	}
	return facts, nil
}

func (s *Store) ResolveRefs(ctx context.Context, refs []string, visible func(string, string) bool, now time.Time, threshold time.Duration) ([]RefPreview, error) {
	if refs == nil || len(refs) > 200 {
		return nil, invalidBoardRequest("refs must be an array of at most 200 strings")
	}
	out, err := s.readRefFacts(ctx, refs, visible)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, row := range out {
		if row.Resolvable && row.Kind == "card" {
			ids = append(ids, row.ID)
		}
	}
	ps, movement, _, err := s.loadPlans(ctx, ids)
	if err != nil {
		return nil, err
	}
	facts, err := s.planFacts(ctx, ps, visible)
	if err != nil {
		return nil, err
	}
	for i, row := range out {
		if row.Kind != "card" || !row.Resolvable {
			continue
		}
		input := movement[row.ID]
		at := input.Activity
		if row.MovementAt.After(at) {
			at = row.MovementAt
		}
		var p *plans.Plan
		var state plans.State
		if value, ok := ps[row.ID]; ok {
			p = &value
			state = plans.Compute(value, facts, at, now, threshold)
			at, _ = time.Parse(time.RFC3339Nano, state.LastMovementAt)
			out[i].PlanResolutionTruncated = planRefsTruncated(value, facts)
			out[i].Progress = &state.Progress
			out[i].NextStep = plans.ReadyStep(value, state, func(step plans.Step) bool { return step.Ref == "" || facts[step.Ref].Known })
			out[i].StatusMismatch = row.Phase == "backlog" && state.Progress.Done > 0
		}
		health := plans.HealthFor(p, state, at, input.Created, now, threshold, input.Due)
		out[i].PlanHealth = &health
	}

	return out, nil
}

// EnrichCardPlans batches both plans and referenced state for an entire read.
func (s *Store) EnrichCardPlans(ctx context.Context, cards []map[string]any, visible func(string, string) bool, now time.Time, threshold time.Duration) error {
	allowed, err := s.FilterCardAccess(ctx, cards, visible)
	if err != nil {
		return err
	}
	if len(allowed) != len(cards) {
		return ErrNotFound
	}
	ids := []string{}
	for _, card := range cards {
		ids = append(ids, workString(card["id"]))
	}
	ps, movement, sourceRefs, err := s.loadPlans(ctx, ids)
	if err != nil {
		return err
	}
	facts, err := s.planFacts(ctx, ps, visible)
	if err != nil {
		return err
	}
	for _, card := range cards {
		id := workString(card["id"])
		card["source_refs"] = sourceRefs[id]
		input := movement[id]
		at := input.Activity
		var p *plans.Plan
		var state plans.State
		card["next_step"] = nil
		card["status_mismatch"] = false
		card["plan_resolution_truncated"] = false
		card["plan_step_digest"] = nil
		if value, ok := ps[id]; ok {
			p = &value
			state = plans.Compute(value, facts, at, now, threshold)
			at, _ = time.Parse(time.RFC3339Nano, state.LastMovementAt)
			card["plan_resolution_truncated"] = planRefsTruncated(value, facts)
			card["plan"], card["plan_state"] = value, state
			card["next_step"] = plans.ReadyStep(value, state, func(step plans.Step) bool { return step.Ref == "" || facts[step.Ref].Known })
			// Three bounded lists over the same steps and the same facts: no
			// extra query, and no list longer than the digest limit.
			card["plan_step_digest"] = plans.Digest(value, state, facts, now, plans.StepDigestWindow, plans.StepDigestLimit)
			card["status_mismatch"] = firstNonEmptyString(workString(card["phase"]), workString(card["column_key"])) == "backlog" && state.Progress.Done > 0
		}
		health := plans.HealthFor(p, state, at, input.Created, now, threshold, input.Due)
		card["plan_health"] = health
		if p != nil {
			state.HealthState = health.State
			state.Health = plans.LegacyHealth(health.State)
			card["plan_state"] = state
		}
	}

	return nil
}

// SetCardPlan commits the graph and its event together. A stale editor cannot
// overwrite another edit. Identical retries do not create misleading movement.
func (s *Store) SetCardPlan(ctx context.Context, actor, id, ifUpdatedAt string, p plans.Plan) error {
	if err := plans.Validate(p); err != nil {
		return invalidBoardRequestError(err)
	}
	if actor == "" || ifUpdatedAt == "" {
		return invalidBoardRequest("actor and if_updated_at are required")
	}
	for i := range p.Steps {
		if p.Steps[i].After == nil {
			p.Steps[i].After = []string{}
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	row, err := s.loadBoardCardByGlobalID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if err = ensureBoardCardMutable(row); err != nil {
		return err
	}
	var oldRaw string
	err = tx.QueryRowContext(ctx, `SELECT body_json FROM card_plans WHERE card_id=?`, row.CardID).Scan(&oldRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = ensureUpdatedAtMatches(row.UpdatedAt, &ifUpdatedAt); err != nil {
		return err
	}
	if oldRaw == string(data) {
		return nil
	}
	var old any
	if oldRaw != "" {
		if err = json.Unmarshal([]byte(oldRaw), &old); err != nil {
			return fmt.Errorf("decode stored plan: %w", err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?) ON CONFLICT(card_id) DO UPDATE SET body_json=excluded.body_json,updated_at=excluded.updated_at`, row.CardID, string(data), now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET updated_at=?,updated_by=? WHERE id=?`, now, actor, row.CardID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE work_metadata SET version=version+1 WHERE card_id=?`, row.CardID); err != nil {
		return err
	}
	if row.BoardID != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE boards SET updated_at=?,updated_by=? WHERE id=?`, now, actor, row.BoardID); err != nil {
			return err
		}
	}
	card, err := row.toMap()
	if err != nil {
		return err
	}
	if err = insertWorkEvent(ctx, tx, actor, card, "card_updated", "Initiative plan updated", map[string]any{"changed_fields": []string{"plan"}, "before_plan": old, "plan": p}); err != nil {
		return err
	}
	return tx.Commit()
}

func planRefsTruncated(p plans.Plan, facts map[string]plans.Fact) bool {
	for _, step := range p.Steps {
		if step.Ref != "" {
			if _, ok := facts[step.Ref]; !ok {
				return true
			}
		}
	}
	return false
}

// SetCardPlan commits the graph and its event together. A stale editor cannot
// overwrite another edit. Identical retries do not create misleading movement.
