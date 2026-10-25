package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// Parentage is an ordinary plan reference. Choose the oldest visible open
// parent card, then its ID. Both the plan and its owner must be readable.
// The reverse covering index admits only this child's incoming links.
const workParentSources = ` FROM brief_plan_refs link INDEXED BY idx_brief_plan_refs_target
	 CROSS JOIN cards parent ON parent.id=link.card_id
	 CROSS JOIN boards parent_board ON parent_board.id=parent.board_id `

func workParentEligible(child string) string {
	return `parent.id<>` + child + `.id AND parent.archived_at IS NULL AND parent.trashed_at IS NULL AND parent.column_key NOT IN ('done','cancelled') AND parent_board.archived_at IS NULL AND parent_board.trashed_at IS NULL`
}
func workParentSQL(child string) string {
	return `(SELECT parent.id` + workParentSources + ` WHERE link.ref IN ('card:'||` + child + `.id,'card:'||` + child + `.handle) AND ` + workParentEligible(child) + ` ORDER BY parent.created_at,parent.id LIMIT 1)`
}

// Top-level admission needs existence, not the oldest parent's identity. Two
// exact probes avoid sorting and building a two-key IN set for every card.
func workHasParentSQL(child string) string {
	probe := func(key string) string {
		return `EXISTS(SELECT 1` + workParentSources + ` WHERE link.ref='card:'||` + child + `.` + key + ` AND ` + workParentEligible(child) + ` LIMIT 1)`
	}
	return `(` + probe("id") + ` OR ` + probe("handle") + `)`
}

type WorkChild struct {
	Ref         string      `json:"ref"`
	Title       string      `json:"title"`
	StepID      string      `json:"step_id"`
	State       string      `json:"state"`
	PastPromise bool        `json:"past_promise"`
	NeedsViewer bool        `json:"needs_viewer"`
	Owner       *string     `json:"owner"`
	DoneAt      *string     `json:"done_at"`
	ShortCode   string      `json:"short_code,omitempty"`
	Rollup      *WorkRollup `json:"rollup"`
}

type workHierarchyViewerKey struct{}
type workHierarchyViewer struct {
	actor string
	human bool
}

// WithWorkHierarchyViewer carries presentation identity, never read authority.
// Canonical SQL still uses the request's independent access scope.
func WithWorkHierarchyViewer(ctx context.Context, actor string, human bool) context.Context {
	return context.WithValue(ctx, workHierarchyViewerKey{}, workHierarchyViewer{actor, human})
}
func hierarchyViewer(ctx context.Context) (string, bool) {
	if viewer, ok := ctx.Value(workHierarchyViewerKey{}).(workHierarchyViewer); ok {
		return viewer.actor, viewer.human
	}
	scope, _ := accessScopeFrom(ctx)
	return scope.ActorID, false
}

type WorkRollup struct {
	Total             int            `json:"total"`
	ByState           map[string]int `json:"by_state"`
	PastPromise       int            `json:"past_promise"`
	NeedingViewer     int            `json:"needing_viewer"`
	Workstreams       []WorkChild    `json:"workstreams"`
	Children          []WorkChild    `json:"children"`
	EarliestPromiseAt *string        `json:"earliest_promise_at"`
	HasMore           bool           `json:"has_more"`
}

func emptyWorkRollup() *WorkRollup {
	return &WorkRollup{ByState: map[string]int{}, Workstreams: []WorkChild{}, Children: []WorkChild{}}
}

// EnrichWorkHierarchy reads at most 200 input cards and each plan's 200 steps.
// Hidden plans/children never enter Go, counts or continuation metadata.
func (s *Store) EnrichWorkHierarchy(ctx context.Context, cards []map[string]any, viewer string, human bool, now time.Time) error {
	ctx, closeRead, err := s.beginSummaryRead(ctx)
	if err != nil {
		return err
	}
	defer closeRead()
	return enrichWorkHierarchy(ctx, s.db, cards, viewer, human, now)
}
func enrichWorkHierarchy(ctx context.Context, q briefQueryer, cards []map[string]any, viewer string, human bool, now time.Time) error {
	return loadWorkHierarchy(ctx, q, cards, viewer, human, now, true)
}
func loadWorkHierarchy(ctx context.Context, q briefQueryer, cards []map[string]any, viewer string, human bool, now time.Time, expand bool) error {
	if len(cards) == 0 {
		return nil
	}
	if len(cards) > 200 {
		return workInvalid("hierarchy batch exceeds 200 cards")
	}
	ids := []string{}
	byID := map[string]map[string]any{}
	for _, c := range cards {
		id := workString(c["id"])
		if id == "" {
			id = strings.TrimPrefix(workString(c["ref"]), "card:")
		}
		ids = append(ids, id)
		byID[id] = c
		c["parent_ref"] = nil
		c["rollup"] = emptyWorkRollup()
	}
	encoded, _ := json.Marshal(ids)
	rows, err := q.QueryContext(ctx, `SELECT requested.value,c.id,COALESCE((SELECT 'card:'||COALESCE(NULLIF(p.handle,''),p.id) FROM cards p WHERE p.id=`+workParentSQL("c")+`),'')
	 FROM json_each(?) requested CROSS JOIN cards c ON c.id=requested.value OR (c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle=requested.value)`, string(encoded))
	if err != nil {
		return err
	}
	canonical := map[string]map[string]any{}
	for rows.Next() {
		var key, id, parent string
		if err = rows.Scan(&key, &id, &parent); err != nil {
			rows.Close()
			return err
		}
		c := byID[key]
		canonical[id] = c
		if parent != "" {
			c["parent_ref"] = parent
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	ids = ids[:0]
	for id := range canonical {
		ids = append(ids, id)
	}
	encoded, _ = json.Marshal(ids)
	// The correlated ask probe seeks a card's subject spellings. Existence is
	// enough: several open asks on one child still count as one needing viewer.
	needs := `EXISTS(SELECT 1 FROM (
	 SELECT a.ask_id,a.recipient,a.requester,a.state FROM brief_asks a INDEXED BY idx_brief_asks_subject WHERE a.subject_ref IN ('card:'||c.id,'card:'||c.handle)
	 UNION ALL SELECT a.ask_id,a.recipient,a.requester,a.state FROM brief_ask_blocks k CROSS JOIN brief_asks a ON a.ask_id=k.ask_id WHERE k.ref IN ('card:'||c.id,'card:'||c.handle)
	 ) a JOIN events e ON e.id=a.ask_id
	 WHERE a.state='open' AND COALESCE(e.trashed_at,'')=''
	 AND (a.recipient IN (?,?) OR (a.recipient='' AND ?)) AND a.requester NOT IN (?,?))`
	streams := `EXISTS(SELECT 1 FROM brief_plan_refs grandchildren
	 CROSS JOIN cards grandchild ON grandchild.id=substr(grandchildren.ref,6) OR (grandchild.handle IS NOT NULL AND trim(grandchild.handle)<>'' AND grandchild.handle=substr(grandchildren.ref,6))
	 WHERE grandchildren.card_id=c.id AND substr(grandchildren.ref,1,5)='card:' AND grandchild.trashed_at IS NULL AND ` + workParentSQL("grandchild") + `=c.id)`
	if !expand {
		streams = "0"
	}
	// Only a routing identity is selected before canonical event hydration.
	// Unary + suppresses the outer TEXT affinity so SQLite seeks the JSON
	// expression index instead of scanning every completion for every child.
	doneAt := `CASE WHEN c.column_key='done' THEN (SELECT e.ts FROM events e
 WHERE e.id=(SELECT position.id FROM work_done_event_positions position WHERE position.card_id=+c.id ORDER BY position.ts DESC,position.id DESC LIMIT 1)) END`
	rows, err = q.QueryContext(ctx, `SELECT requested.value,COALESCE(NULLIF(c.handle,''),c.id),c.title,json_extract(step.value,'$.id'),c.column_key,
	 `+needs+`,`+streams+`,c.id,c.assignee,
 bi.promise_at,bi.open,`+doneAt+`
	 FROM json_each(?) requested CROSS JOIN card_plans p ON p.card_id=requested.value
	 CROSS JOIN json_each(p.body_json,'$.steps') step
	 CROSS JOIN cards c ON c.id=substr(json_extract(step.value,'$.ref'),6) OR (c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle=substr(json_extract(step.value,'$.ref'),6))
	 LEFT JOIN brief_initiatives bi ON bi.card_id=c.id
	 WHERE CAST(step.key AS INTEGER)<200 AND substr(json_extract(step.value,'$.ref'),1,5)='card:' AND c.trashed_at IS NULL
	 AND `+workParentSQL("c")+`=requested.value
	 ORDER BY requested.value,CAST(step.key AS INTEGER)`, stripActor(viewer), "actor:"+stripActor(viewer), human, stripActor(viewer), "actor:"+stripActor(viewer), string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	streamIDs := map[string]bool{}
	childIDs := map[string]string{}
	seen := map[string]bool{}
	earliestPromises := map[string]time.Time{}
	for rows.Next() {
		var parent, ref, id string
		var assignee sql.NullString
		var promise, done sql.NullString
		var open sql.NullInt64
		var child WorkChild
		var stream bool
		if err = rows.Scan(&parent, &ref, &child.Title, &child.StepID, &child.State, &child.NeedsViewer, &stream, &id, &assignee, &promise, &open, &done); err != nil {
			return err
		}
		if seen[parent+"/"+ref] {
			continue
		}
		seen[parent+"/"+ref] = true
		child.Ref = "card:" + ref
		childIDs[child.Ref] = id
		var promiseAt time.Time
		hasPromise := false
		if promise.Valid && open.Valid && open.Int64 == 1 && child.State != "done" && child.State != "cancelled" {
			var parseErr error
			promiseAt, parseErr = time.Parse(time.RFC3339Nano, promise.String)
			hasPromise = parseErr == nil
			child.PastPromise = hasPromise && promiseAt.Before(now)
		}
		if refs := boardCardAssigneeRefs(assignee.String); len(refs) > 0 {
			child.Owner = &refs[0]
		}
		if done.Valid {
			child.DoneAt = &done.String
		}
		handle := ref
		if handle == id {
			handle = ""
		} // A canonical ID is not an authored handle.
		child.ShortCode = WorkShortCode(handle, child.Title)
		if stream {
			streamIDs[id] = true
		}
		r := canonical[parent]["rollup"].(*WorkRollup)
		r.Total++
		if previous, exists := earliestPromises[parent]; hasPromise && (!exists || promiseAt.Before(previous)) {
			earliestPromises[parent] = promiseAt
			at := promiseAt.UTC().Format(time.RFC3339Nano)
			r.EarliestPromiseAt = &at
		}
		r.ByState[child.State]++
		if child.PastPromise {
			r.PastPromise++
		}
		if child.NeedsViewer {
			r.NeedingViewer++
		}
		r.Children = append(r.Children, child)
		if stream {
			r.Workstreams = append(r.Workstreams, child)
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if !expand || len(streamIDs) == 0 {
		return nil
	}
	// One extra batch, never one query per initiative or recursive graph walk.
	streamKeys := make([]string, 0, len(streamIDs))
	for id := range streamIDs {
		streamKeys = append(streamKeys, id)
	}
	sort.Strings(streamKeys)
	expanded := map[string]map[string]any{}
	batch := []map[string]any{}
	for _, id := range streamKeys[:min(200, len(streamKeys))] {
		// A work list often already contains the workstream itself. Reuse its
		// admitted direct children instead of hydrating that plan a second time.
		if item, ok := canonical[id]; ok {
			direct := *item["rollup"].(*WorkRollup)
			direct.Children = append([]WorkChild{}, direct.Children...)
			for i := range direct.Children {
				direct.Children[i].Rollup = nil
			}
			direct.Workstreams = []WorkChild{}
			direct.HasMore = false
			expanded[id] = map[string]any{"rollup": &direct}
			continue
		}
		c := map[string]any{"id": id}
		expanded[id] = c
		batch = append(batch, c)
	}
	if err = loadWorkHierarchy(ctx, q, batch, viewer, human, now, false); err != nil {
		return err
	}
	for _, c := range canonical {
		r := c["rollup"].(*WorkRollup)
		for _, children := range [][]WorkChild{r.Children, r.Workstreams} {
			for i := range children {
				id := childIDs[children[i].Ref]
				if !streamIDs[id] {
					continue
				}
				if item, ok := expanded[id]; ok {
					children[i].Rollup = item["rollup"].(*WorkRollup)
				} else {
					r.HasMore = true
				}
			}
		}
	}
	return nil
}

// WorkShortCode reuses the grouping grammar. Labels are never identities.
func WorkShortCode(handle, title string) string {
	for _, text := range []string{handle, title} {
		if family := TitleFamily(text); family != "" && len([]rune(family)) <= 64 {
			return family
		}
	}
	return ""
}

func loadGroupingPlan(ctx context.Context, tx *accessTx, id string) (plans.Plan, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT body_json FROM card_plans WHERE card_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		if err = requireGroupingFieldReadable(ctx, tx, id, "plan"); err != nil {
			return plans.Plan{}, err
		}
		return plans.Plan{Steps: []plans.Step{}}, nil
	}
	if err != nil {
		return plans.Plan{}, err
	}
	var p plans.Plan
	err = json.Unmarshal([]byte(raw), &p)
	return p, err
}

// A scoped miss may mean either absent or unreadable. Routing presence is a
// primary-key probe only, after its card was admitted. Never replace hidden
// authored data with an empty default merely because it was not returned.
func requireGroupingFieldReadable(ctx context.Context, tx *accessTx, id, field string) error {
	var query string
	switch field {
	case "plan":
		query = `SELECT p.plan_exists,EXISTS(SELECT 1 FROM card_plans WHERE card_id=p.id) FROM work_grouping_field_positions p WHERE p.id=?`
	case "metadata":
		query = `SELECT p.metadata_exists,EXISTS(SELECT 1 FROM work_metadata WHERE card_id=p.id) FROM work_grouping_field_positions p WHERE p.id=?`
	default:
		return workInvalid("unknown grouping field")
	}
	var exists, readable bool
	if err := tx.QueryRowContext(ctx, query, id).Scan(&exists, &readable); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if exists && !readable {
		return ErrNotFound
	}
	return nil
}

func (s *Store) loadGroupingCard(ctx context.Context, tx *accessTx, ref string) (boardCardRow, error) {
	resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: ref})
	if err != nil {
		return boardCardRow{}, err
	}
	return s.loadBoardCardByGlobalID(ctx, tx, resolved.ID, true)
}

// groupWorkTx is used by BOTH create paths and work.group, inside the card's
// own transaction. It never calls a second HTTP mutation or accepts a lost link.
func (s *Store) groupWorkTx(ctx context.Context, tx *accessTx, actor string, child boardCardRow, under, after, due string) (string, error) {
	parent, err := s.loadGroupingCard(ctx, tx, under)
	if err != nil {
		return "", err
	}
	if parent.CardID == child.CardID {
		return "", workInvalid("a card cannot group under itself")
	}
	if err = ensureBoardCardMutable(parent); err != nil {
		return "", err
	}
	if plans.Status(parent.ColumnKey) == "done" || parent.ColumnKey == "cancelled" {
		return "", workInvalid("parent must be open")
	}
	board, err := loadBoardRow(ctx, tx, parent.BoardID)
	if err != nil {
		return "", err
	}
	if board.ArchivedAt.Valid || board.TrashedAt.Valid {
		return "", workInvalid("parent board must be active")
	}
	// Follow indexed incoming links to reject cycles. A fixed depth keeps
	// malicious or legacy graphs bounded; ambiguous deep graphs fail closed.
	ancestors := []string{parent.CardID}
	seen := map[string]bool{}
	for depth := 0; len(ancestors) > 0; depth++ {
		if depth >= 32 || len(seen) > 200 {
			return "", workInvalid("hierarchy exceeds safe depth")
		}
		encoded, _ := json.Marshal(ancestors)
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT p.id FROM json_each(?) requested CROSS JOIN cards c ON c.id=requested.value
		 CROSS JOIN brief_plan_refs r ON r.ref IN ('card:'||c.id,'card:'||c.handle) JOIN cards p ON p.id=r.card_id JOIN card_plans cp ON cp.card_id=p.id
		 WHERE p.archived_at IS NULL AND p.trashed_at IS NULL AND p.column_key NOT IN ('done','cancelled') LIMIT 201`, string(encoded))
		if e != nil {
			return "", e
		}
		next := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return "", e
			}
			if id == child.CardID {
				rows.Close()
				return "", workInvalid("grouping would create a cycle")
			}
			if !seen[id] {
				seen[id] = true
				next = append(next, id)
			}
		}
		rows.Close()
		if e = rows.Err(); e != nil {
			return "", e
		}
		ancestors = next
	}
	p, err := loadGroupingPlan(ctx, tx, parent.CardID)
	if err != nil {
		return "", err
	}
	ref := "card:" + firstNonEmptyString(child.Handle.String, child.CardID)
	existing := -1
	afterFound := after == ""
	for i, step := range p.Steps {
		if step.ID == after {
			afterFound = true
		}
		if step.Ref == ref || step.Ref == "card:"+child.CardID {
			existing = i
		}
	}
	if !afterFound {
		return "", workInvalid("after must name an existing parent plan step")
	}
	if existing >= 0 && due == "" {
		return "card:" + firstNonEmptyString(parent.Handle.String, parent.CardID), nil
	}
	if existing < 0 {
		sum := sha256.Sum256([]byte(child.CardID))
		step := plans.Step{ID: fmt.Sprintf("card-%x", sum[:10]), Title: child.Title, Ref: ref, After: []string{}}
		if after != "" {
			step.After = append(step.After, after)
		}
		p.Steps = append(p.Steps, step)
		existing = len(p.Steps) - 1
	}
	if due != "" {
		p.Steps[existing].Due = due
	}
	if err = plans.Validate(p); err != nil {
		return "", workInvalid("parent plan: %s", err)
	}
	if err = s.saveGroupingPlan(ctx, tx, actor, parent, p); err != nil {
		return "", err
	}
	return "card:" + firstNonEmptyString(parent.Handle.String, parent.CardID), nil
}

func (s *Store) saveGroupingPlan(ctx context.Context, tx *accessTx, actor string, parent boardCardRow, p plans.Plan) error {
	raw, _ := json.Marshal(p)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?) ON CONFLICT(card_id) DO UPDATE SET body_json=excluded.body_json,updated_at=excluded.updated_at`, parent.CardID, string(raw), now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET updated_at=?,updated_by=? WHERE id=?`, now, actor, parent.CardID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE work_metadata SET version=version+1 WHERE card_id=?`, parent.CardID); err != nil {
		return err
	}
	card, err := parent.toMap()
	if err != nil {
		return err
	}
	return insertWorkEvent(ctx, tx, actor, card, "card_updated", "Card grouped in plan", map[string]any{"changed_fields": []string{"plan"}, "plan": p})
}

func (s *Store) GroupWork(ctx context.Context, actor, id, under, after string) (map[string]any, error) {
	if actor == "" || under == "" {
		return nil, workInvalid("actor and under are required")
	}
	var out map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		child, err := s.loadGroupingCard(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = ensureBoardCardMutable(child); err != nil {
			return err
		}
		if _, err = s.groupWorkTx(ctx, tx, actor, child, under, after, ""); err != nil {
			return err
		}
		out, err = s.GetWork(ctx, child.CardID)
		if err == nil {
			viewer, human := hierarchyViewer(ctx)
			err = s.EnrichWorkHierarchy(ctx, []map[string]any{out}, viewer, human, time.Now())
		}
		return err
	})
	return out, err
}
