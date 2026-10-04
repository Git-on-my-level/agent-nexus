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

// RefPreview contains only the bounded data needed by chips and plan derivation.
// Internal fields support principal-scoped reads and never appear on the wire.
type RefPreview struct {
	Ref        string          `json:"ref"`
	Kind       string          `json:"kind,omitempty"`
	Title      string          `json:"title,omitempty"`
	Status     string          `json:"status,omitempty"`
	Phase      string          `json:"phase,omitempty"`
	Owner      string          `json:"owner,omitempty"`
	Progress   *plans.Progress `json:"progress,omitempty"`
	URL        string          `json:"url,omitempty"`
	Resolvable bool            `json:"resolvable"`
	ID         string          `json:"-"`
	MovementAt time.Time       `json:"-"`
}

// readRefFacts uses at most four SQL queries, independent of ref count. It never
// fetches arbitrary URLs, nor interprets missing evidence as completed work.
func (s *Store) readRefFacts(ctx context.Context, refs []string, visible func(string, string) bool) ([]RefPreview, error) {
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
		marks := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
		args := []any{}
		for j := 0; j < 2; j++ {
			for _, v := range values {
				args = append(args, v)
			}
		}
		var query string
		switch kind {
		case "card":
			for _, v := range values {
				args = append(args, v)
			}
			query = `SELECT c.id,c.handle,CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.title ELSE COALESCE(json_extract(o.body_json,'$.facts.title'),json_extract(m.metadata_json,'$.title'),c.title) END,
			 CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.column_key ELSE COALESCE(json_extract(o.body_json,'$.facts.phase'),json_extract(m.metadata_json,'$.phase'),'unknown') END,
			 CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN CASE WHEN COALESCE(c.assignee,'')='' THEN '' WHEN c.assignee LIKE '%:%' THEN c.assignee ELSE 'actor:'||c.assignee END ELSE COALESCE(json_extract(o.body_json,'$.facts.owner'),json_extract(m.metadata_json,'$.owner'),'') END,
			 COALESCE(json_extract(m.metadata_json,'$.source.url'),''),COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),''),
			 CASE WHEN COALESCE(m.authority,'nexus')='nexus' THEN c.updated_at ELSE COALESCE(CASE WHEN julianday(json_extract(o.body_json,'$.source_activity_at')) > julianday(json_extract(o.body_json,'$.meaningful_progress_at')) THEN json_extract(o.body_json,'$.source_activity_at') END,json_extract(o.body_json,'$.meaningful_progress_at'),json_extract(o.body_json,'$.source_activity_at'),c.created_at) END
			 FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id
			 WHERE c.trashed_at IS NULL AND (c.id IN (` + marks + `) OR c.handle IN (` + marks + `) OR (m.authority!='nexus' AND json_extract(m.metadata_json,'$.source.url') IN (` + marks + `))) ORDER BY c.id`
		case "document":
			query = `SELECT id,handle,COALESCE(title,''),CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,'','',COALESCE(thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=documents.thread_id),''),updated_at FROM documents WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		case "topic":
			query = `SELECT id,handle,title,CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,COALESCE(json_extract(extensions_json,'$.owner_refs[0]'),''),'',thread_id,COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=topics.thread_id),''),updated_at FROM topics WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		case "board":
			query = `SELECT id,handle,title,CASE WHEN archived_at IS NULL THEN 'active' ELSE 'archived' END,COALESCE(json_extract(owners_json,'$[0]'),''),'',thread_id,COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=boards.thread_id),''),updated_at FROM boards WHERE trashed_at IS NULL AND (id IN (` + marks + `) OR handle IN (` + marks + `))`
		}
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		type row struct{ id, handle, title, phase, owner, external, thread, privateOwner, at string }
		items := []row{}
		for rows.Next() {
			var item row
			if err = rows.Scan(&item.id, &item.handle, &item.title, &item.phase, &item.owner, &item.external, &item.thread, &item.privateOwner, &item.at); err != nil {
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
		candidates := map[string][]RefPreview{}
		for _, item := range items {
			if visible != nil && !visible(item.thread, item.privateOwner) {
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
			preview.MovementAt, _ = time.Parse(time.RFC3339Nano, item.at)
			keys := uniqueSortedStrings([]string{item.id, item.handle, item.external})
			for _, key := range keys {
				if key != "" {
					candidates[key] = append(candidates[key], preview)
				}
			}
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
	return out, nil
}

func (s *Store) loadPlans(ctx context.Context, ids []string) (map[string]plans.Plan, map[string]time.Time, error) {
	out, movement := map[string]plans.Plan{}, map[string]time.Time{}
	ids = uniqueSortedStrings(ids)
	if len(ids) == 0 {
		return out, movement, nil
	}
	args := []any{}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT card_id,body_json,updated_at FROM card_plans WHERE card_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, raw, at string
		if err = rows.Scan(&id, &raw, &at); err != nil {
			return nil, nil, err
		}
		var p plans.Plan
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return nil, nil, err
		}
		out[id] = p
		movement[id], _ = time.Parse(time.RFC3339Nano, at)
	}
	return out, movement, rows.Err()
}

func (s *Store) planFacts(ctx context.Context, ps map[string]plans.Plan, visible func(string, string) bool) (map[string]plans.Fact, error) {
	refs := []string{}
	for _, p := range ps {
		for _, step := range p.Steps {
			if step.Ref != "" {
				refs = append(refs, step.Ref)
			}
		}
	}
	refs = uniqueSortedStrings(refs)
	facts := map[string]plans.Fact{}
	for len(refs) > 0 {
		n := len(refs)
		if n > 200 {
			n = 200
		}
		rows, err := s.readRefFacts(ctx, refs[:n], visible)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			facts[row.Ref] = plans.Fact{Known: row.Resolvable, Status: row.Phase, MovementAt: row.MovementAt}
		}
		refs = refs[n:]
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
	ps, movement, err := s.loadPlans(ctx, ids)
	if err != nil {
		return nil, err
	}
	facts, err := s.planFacts(ctx, ps, visible)
	if err != nil {
		return nil, err
	}
	for i, row := range out {
		if p, ok := ps[row.ID]; ok {
			state := plans.Compute(p, facts, movement[row.ID], now, threshold)
			out[i].Progress = &state.Progress
		}
	}
	return out, nil
}

// EnrichCardPlans batches both plans and referenced state for an entire read.
func (s *Store) EnrichCardPlans(ctx context.Context, cards []map[string]any, visible func(string, string) bool, now time.Time, threshold time.Duration) error {
	ids := []string{}
	for _, card := range cards {
		ids = append(ids, workString(card["id"]))
	}
	ps, movement, err := s.loadPlans(ctx, ids)
	if err != nil {
		return err
	}
	facts, err := s.planFacts(ctx, ps, visible)
	if err != nil {
		return err
	}
	for _, card := range cards {
		if p, ok := ps[workString(card["id"])]; ok {
			card["plan"] = p
			card["plan_state"] = plans.Compute(p, facts, movement[workString(card["id"])], now, threshold)
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
