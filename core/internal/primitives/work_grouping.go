package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// The work tree stores parentage: cards.parent_card_id is the only parent
// edge. A card is an initiative exactly when it has no parent and is not
// waiting in the Unsorted tray (needs_home=1). Every read uses these helpers.
const workTreeMaxDepth = 3

// workIsInitiativeSQL is the one initiative predicate (see work_tree.go). A
// parent hidden from the reader counts as no parent, so a visible child of a
// private initiative reads as top-level work and never reveals that parent
// (reads resolve unqualified cards through the reader's scoped relation).
func workIsInitiativeSQL(card string) string {
	return `(` + card + `.needs_home=0 AND ` + workIsRootSQL(card) + `)`
}

// workIsRootSQL: the card has no parent the reader can see (tray included).
func workIsRootSQL(card string) string {
	return `(` + card + `.parent_card_id IS NULL OR NOT EXISTS(SELECT 1 FROM cards work_parent WHERE work_parent.id=` + card + `.parent_card_id))`
}

// workParentSQL yields the stored parent's ID while that parent is not trashed.
// Callers hydrate it through the scoped cards relation, so a hidden parent
// reads as absent.
func workParentSQL(child string) string {
	return `(SELECT parent.id FROM cards parent WHERE parent.id=` + child + `.parent_card_id AND parent.trashed_at IS NULL)`
}

func workHasParentSQL(child string) string {
	return `(` + child + `.parent_card_id IS NOT NULL)`
}

type WorkChild struct {
	Ref         string      `json:"ref"`
	Title       string      `json:"title"`
	StepID      string      `json:"step_id,omitempty"`
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
	rows, err := q.QueryContext(ctx, `SELECT requested.value,c.id,COALESCE((SELECT 'card:'||COALESCE(NULLIF(p.handle,''),p.id) FROM cards p WHERE p.id=`+workParentSQL("c")+`),''),`+workIsInitiativeSQL("c")+`,c.needs_home
	 FROM json_each(?) requested CROSS JOIN cards c ON c.id=requested.value OR (c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle=requested.value)`, string(encoded))
	if err != nil {
		return err
	}
	canonical := map[string]map[string]any{}
	for rows.Next() {
		var key, id, parent string
		var initiative, needsHome bool
		if err = rows.Scan(&key, &id, &parent, &initiative, &needsHome); err != nil {
			rows.Close()
			return err
		}
		c := byID[key]
		canonical[id] = c
		// Reader-relative: a hidden parent reads as no parent.
		c["is_initiative"], c["needs_home"] = initiative, needsHome
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
	streams := `EXISTS(SELECT 1 FROM cards grandchild WHERE grandchild.parent_card_id=c.id AND grandchild.trashed_at IS NULL)`
	if !expand {
		streams = "0"
	}
	// Only a routing identity is selected before canonical event hydration.
	// Unary + suppresses the outer TEXT affinity so SQLite seeks the JSON
	// expression index instead of scanning every completion for every child.
	doneAt := `CASE WHEN c.column_key='done' THEN (SELECT e.ts FROM events e
 WHERE e.id=(SELECT position.id FROM work_done_event_positions position WHERE position.card_id=+c.id ORDER BY anx_timestamp_key(position.ts) DESC,position.id DESC LIMIT 1)) END`
	// Children are cards whose stored parent is the requested card, oldest
	// first, at most 200 per parent through the parent index.
	rows, err = q.QueryContext(ctx, `SELECT requested.parent_id,COALESCE(NULLIF(c.handle,''),c.id),c.title,'',c.column_key,
	 `+needs+`,`+streams+`,c.id,c.assignee,
 bi.promise_at,bi.open,`+doneAt+`
	 FROM (SELECT requested.value AS parent_id,c.id AS child_id,ROW_NUMBER() OVER (PARTITION BY requested.value ORDER BY c.created_at,c.id) AS position
	  FROM json_each(?) requested CROSS JOIN cards c ON c.parent_card_id=requested.value WHERE c.trashed_at IS NULL) requested
	 CROSS JOIN cards c ON c.id=requested.child_id
	 LEFT JOIN brief_initiatives bi ON bi.card_id=c.id
	 WHERE requested.position<=200
	 ORDER BY requested.parent_id,c.created_at,c.id`, stripActor(viewer), "actor:"+stripActor(viewer), human, stripActor(viewer), "actor:"+stripActor(viewer), string(encoded))
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

// groupWorkTx is used by BOTH create paths, fold and work.group, inside the
// card's own transaction. It stores the parent; it never edits a plan.
// A current initiative must be moved explicitly (MoveWork with a reason).
func (s *Store) groupWorkTx(ctx context.Context, tx *accessTx, actor string, child boardCardRow, under, after, due string) (string, error) {
	if err := refuseInitiativeGrouping(ctx, tx, child); err != nil {
		return "", err
	}
	return s.moveWorkTx(ctx, tx, actor, child, under)
}

// moveWorkTx stores child's parent (under=="" files it in the Unsorted tray).
// It enforces one parent, no cycles, depth <= workTreeMaxDepth and an open
// parent. Structural probes read main.cards: a hidden ancestor still counts
// toward depth and cycles, but no hidden identity is ever returned.
func (s *Store) moveWorkTx(ctx context.Context, tx *accessTx, actor string, child boardCardRow, under string) (string, error) {
	parentID, parentRef := "", ""
	if under != "" {
		parent, err := s.loadGroupingCard(ctx, tx, under)
		if err != nil {
			return "", err
		}
		if parent.CardID == child.CardID {
			return "", workInvalid("a card cannot be moved under itself")
		}
		if plans.Status(parent.ColumnKey) == "done" || parent.ColumnKey == "cancelled" {
			return "", workInvalid("parent must be open")
		}
		chain, err := workAncestorChain(ctx, tx, parent.CardID)
		if err != nil {
			return "", err
		}
		for _, id := range chain {
			if id == child.CardID {
				return "", workInvalid("moving a card under its own descendant would create a cycle")
			}
		}
		height, err := workSubtreeHeight(ctx, tx, child.CardID)
		if err != nil {
			return "", err
		}
		// chain runs root..parent, so the child would sit at depth len(chain).
		if len(chain)+height >= workTreeMaxDepth {
			legal := workTreeMaxDepth - 1 - height - 1
			if legal >= 0 && legal < len(chain) {
				var ref string
				if err = tx.QueryRowContext(ctx, `SELECT 'card:'||COALESCE(NULLIF(handle,''),id) FROM cards WHERE id=?`, chain[legal]).Scan(&ref); err == nil {
					return "", workInvalid("the work tree is limited to %d levels (initiative, workstream, task); move it under %s instead", workTreeMaxDepth, ref)
				}
			}
			return "", workInvalid("the work tree is limited to %d levels (initiative, workstream, task)", workTreeMaxDepth)
		}
		parentID = parent.CardID
		parentRef = "card:" + firstNonEmptyString(parent.Handle.String, parent.CardID)
	}
	var current sql.NullString
	var needsHome bool
	if err := tx.QueryRowContext(ctx, `SELECT parent_card_id,needs_home FROM main.cards WHERE id=?`, child.CardID).Scan(&current, &needsHome); err != nil {
		return "", err
	}
	tray := parentID == ""
	if current.String == parentID && needsHome == tray {
		return parentRef, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=?,needs_home=?,updated_at=?,updated_by=? WHERE id=?`, nullableString(parentID), tray, now, actor, child.CardID); err != nil {
		return "", err
	}
	if err := markBoardCardProjectionDirtyTx(ctx, tx, child); err != nil {
		return "", err
	}
	card, err := child.toMap()
	if err != nil {
		return "", err
	}
	var parentValue any
	if parentRef != "" {
		parentValue = parentRef
	}
	return parentRef, insertWorkEvent(ctx, tx, actor, card, "card_updated", "Card moved in the work tree", map[string]any{"changed_fields": []string{"parent"}, "parent_ref": parentValue, "needs_home": tray})
}

// releaseWorkTreeChildrenTx sends a closing card's children to the Unsorted
// tray: archived, trashed or purged work is never a home. Already-trashed
// children are released too, so restoring one never revives a closed edge.
func releaseWorkTreeChildrenTx(ctx context.Context, tx *accessTx, id, actor, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=NULL,needs_home=1,updated_at=?,updated_by=? WHERE parent_card_id=?`, now, actor, id)
	return err
}

// workAncestorChain returns parent IDs from the root down to id (inclusive).
func workAncestorChain(ctx context.Context, tx *accessTx, id string) ([]string, error) {
	chain := []string{id}
	for at := id; len(chain) <= 32; {
		var parent sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT parent_card_id FROM main.cards WHERE id=?`, at).Scan(&parent); err != nil {
			return nil, err
		}
		if !parent.Valid || parent.String == "" {
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}
			return chain, nil
		}
		for _, seen := range chain {
			if seen == parent.String {
				return nil, workInvalid("work tree contains a cycle")
			}
		}
		chain = append(chain, parent.String)
		at = parent.String
	}
	return nil, workInvalid("hierarchy exceeds safe depth")
}

// workSubtreeHeight is how many levels sit below id, capped past the limit.
// Each level is one exact EXISTS probe through the parent index, so a wide
// branch can never hide a deeper one.
func workSubtreeHeight(ctx context.Context, tx *accessTx, id string) (int, error) {
	from := `main.cards l1`
	where := `l1.parent_card_id=? AND l1.trashed_at IS NULL`
	for height := 1; height <= workTreeMaxDepth; height++ {
		var deeper bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+from+` WHERE `+where+`)`, id).Scan(&deeper); err != nil {
			return 0, err
		}
		if !deeper {
			return height - 1, nil
		}
		next := fmt.Sprintf("l%d", height+1)
		from += fmt.Sprintf(` CROSS JOIN main.cards %s ON %s.parent_card_id=l%d.id`, next, next, height)
		where += ` AND ` + next + `.trashed_at IS NULL`
	}
	return workTreeMaxDepth, nil
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
