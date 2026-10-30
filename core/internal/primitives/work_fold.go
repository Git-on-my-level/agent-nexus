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

// Fold snapshots contain only the fields this operation owns. Unfold preserves
// unrelated later edits and rejects conflicts in transferred fields or the plan.
// The snapshot lives in ordinary scoped metadata, so its refs inherit privacy.
const foldSnapshotKey = "_anx_fold"

var foldFields = []string{"source_refs", "related_refs", "relations", sourceEvidenceKey, sourceEvidenceRefsKey}

type foldSnapshot struct {
	Parent       string         `json:"parent"`
	ParentBefore map[string]any `json:"parent_before"`
	ChildBefore  map[string]any `json:"child_before"`
	ParentAfter  map[string]any `json:"parent_after"`
	ChildAfter   map[string]any `json:"child_after"`
	PlanBefore   plans.Plan     `json:"plan_before"`
	PlanAfter    plans.Plan     `json:"plan_after"`
	ArchivedAt   string         `json:"archived_at"`
	// The child's parent before the fold; unfold restores it. Fold refuses
	// initiatives, so no parent (including legacy snapshots) means the tray.
	TreeParentBefore string `json:"tree_parent_before,omitempty"`
}

func groupingMetadata(ctx context.Context, tx *accessTx, id string) (map[string]any, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT metadata_json FROM work_metadata WHERE card_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		if err = requireGroupingFieldReadable(ctx, tx, id, "metadata"); err != nil {
			return nil, err
		}
		return map[string]any{"source": map[string]any{"authority": "nexus"}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal([]byte(raw), &m)
	return m, err
}
func foldOwned(row boardCardRow, m map[string]any) map[string]any {
	out := map[string]any{"refs_json": row.RefsJSON, "resolution_refs_json": row.ResolutionRefsJSON}
	for _, key := range foldFields {
		if value, ok := m[key]; ok {
			out[key] = value
		}
	}
	return workClone(out)
}
func restoreFoldOwned(m, owned map[string]any) {
	for _, key := range foldFields {
		delete(m, key)
		if value, ok := owned[key]; ok {
			m[key] = value
		}
	}
}
func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func saveGroupingMetadata(ctx context.Context, tx *accessTx, id, actor string, m map[string]any) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,?,?,?,?,?)
	 ON CONFLICT(card_id) DO UPDATE SET metadata_json=excluded.metadata_json,version=work_metadata.version+1,updated_at=excluded.updated_at,updated_by=excluded.updated_by`, id, firstNonEmptyString(workString(workMap(m["source"])["authority"]), "nexus"), workJSON(m), 1, now, actor)
	return err
}
func saveFoldOwned(ctx context.Context, tx *accessTx, id, actor string, m, owned map[string]any) error {
	restoreFoldOwned(m, owned)
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET refs_json=?,resolution_refs_json=?,updated_at=?,updated_by=? WHERE id=?`, workString(owned["refs_json"]), workString(owned["resolution_refs_json"]), time.Now().UTC().Format(time.RFC3339Nano), actor, id); err != nil {
		return err
	}
	return saveGroupingMetadata(ctx, tx, id, actor, m)
}

func mergeFoldSources(a, b any) ([]map[string]any, error) {
	out := []map[string]any{}
	seen := map[[3]string]map[string]any{}
	for _, raw := range []any{a, b} {
		data, _ := json.Marshal(raw)
		var sources []map[string]any
		_ = json.Unmarshal(data, &sources)
		for _, source := range sources {
			key := [3]string{workString(source["authority"]), workString(source["connection_id"]), workString(source["native_id"])}
			if previous := seen[key]; previous != nil {
				if !jsonEqual(previous, source) {
					return nil, workInvalid("conflicting source evidence; reconcile before folding")
				}
				continue
			}
			seen[key] = source
			out = append(out, source)
		}
	}
	return out, validateSourceRefs(out)
}
func mergedRefJSON(raw ...string) string {
	refs := []string{}
	for _, r := range raw {
		var values []string
		_ = json.Unmarshal([]byte(r), &values)
		refs = append(refs, values...)
	}
	data, _ := json.Marshal(uniqueSortedStrings(refs))
	return string(data)
}

func (s *Store) FoldWork(ctx context.Context, actor, id, into, after string) (map[string]any, error) {
	if actor == "" || into == "" {
		return nil, workInvalid("actor and into are required")
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
		if err := refuseInitiativeGrouping(ctx, tx, child); err != nil {
			return err
		}
		parent, err := s.loadGroupingCard(ctx, tx, into)
		if err != nil {
			return err
		}
		if child.CardID == parent.CardID {
			return workInvalid("cannot fold a card into itself")
		}
		pm, err := groupingMetadata(ctx, tx, parent.CardID)
		if err != nil {
			return err
		}
		cm, err := groupingMetadata(ctx, tx, child.CardID)
		if err != nil {
			return err
		}
		if cm[foldSnapshotKey] != nil {
			return ErrConflict
		}
		before, err := loadGroupingPlan(ctx, tx, parent.CardID)
		if err != nil {
			return err
		}
		inputs, err := briefPromiseInputs(ctx, tx, []string{child.CardID}, func(ids []string) (map[string][]plans.PromiseNote, error) { return promiseNotesFor(ctx, tx, ids) })
		if err != nil {
			return err
		}
		due := ""
		if len(inputs) > 0 {
			if p := plans.FoldPromise(inputs[0].in).Promise; p != nil {
				due = p.At.UTC().Format(time.RFC3339)
			}
		}
		var treeParent sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT parent_card_id FROM main.cards WHERE id=?`, child.CardID).Scan(&treeParent); err != nil {
			return err
		}
		pref, err := s.groupWorkTx(ctx, tx, actor, child, into, after, due)
		if err != nil {
			return err
		}
		afterPlan, err := loadGroupingPlan(ctx, tx, parent.CardID)
		if err != nil {
			return err
		}
		snapshot := foldSnapshot{Parent: pref, ParentBefore: foldOwned(parent, pm), ChildBefore: foldOwned(child, cm), PlanBefore: before, PlanAfter: afterPlan, ArchivedAt: time.Now().UTC().Format(time.RFC3339Nano), TreeParentBefore: treeParent.String}
		sources, err := mergeFoldSources(pm["source_refs"], cm["source_refs"])
		if err != nil {
			return err
		}
		pm["source_refs"] = sources
		cm["source_refs"] = []any{}
		if len(sources) > 0 {
			pm[sourceEvidenceKey] = snapshot.ArchivedAt
			pm[sourceEvidenceRefsKey] = sources
		}
		// Canonical discussion, asks and observations are append-only and stay
		// attached to C. The plan and related child ref keep all of them
		// reachable from P, even while C is archived.
		cref := "card:" + firstNonEmptyString(child.Handle.String, child.CardID)
		refs := []string{cref}
		for r := range stringSet(json.RawMessage(child.RefsJSON)) {
			if r != pref && r != "card:"+parent.CardID && !strings.HasPrefix(r, "board:") && !strings.HasPrefix(r, "thread:") {
				refs = append(refs, r)
			}
		}
		data, _ := json.Marshal(refs)
		parent.RefsJSON = mergedRefJSON(parent.RefsJSON, string(data))
		parent.ResolutionRefsJSON = mergedRefJSON(parent.ResolutionRefsJSON, child.ResolutionRefsJSON)
		kept := []string{}
		for r := range stringSet(json.RawMessage(child.RefsJSON)) {
			if strings.HasPrefix(r, "board:") || strings.HasPrefix(r, "thread:") {
				kept = append(kept, r)
			}
		}
		data, _ = json.Marshal(uniqueSortedStrings(kept))
		child.RefsJSON = string(data)
		child.ResolutionRefsJSON = "[]"
		related := []string{}
		for r := range stringSet(json.RawMessage(parent.RefsJSON)) {
			if !strings.HasPrefix(r, "board:") && !strings.HasPrefix(r, "thread:") {
				related = append(related, r)
			}
		}
		pm["related_refs"] = uniqueSortedStrings(related)
		cm["related_refs"] = []any{}
		var relations []map[string]any
		data, _ = json.Marshal(cm["relations"])
		_ = json.Unmarshal(data, &relations)
		relations = append(relations, map[string]any{"kind": "merged_into", "ref": pref})
		cm["relations"] = relations
		if err = validateWorkLocal(map[string]any{"relations": relations}); err != nil {
			return err
		}
		snapshot.ParentAfter = foldOwned(parent, pm)
		snapshot.ChildAfter = foldOwned(child, cm)
		cm[foldSnapshotKey] = snapshot
		if err = saveFoldOwned(ctx, tx, parent.CardID, actor, pm, snapshot.ParentAfter); err != nil {
			return err
		}
		if err = saveFoldOwned(ctx, tx, child.CardID, actor, cm, snapshot.ChildAfter); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET archived_at=?,archived_by=? WHERE id=?`, snapshot.ArchivedAt, actor, child.CardID); err != nil {
			return err
		}
		card, err := child.toMap()
		if err != nil {
			return err
		}
		if err = insertWorkEvent(ctx, tx, actor, card, "card_archived", "Card folded into "+pref, map[string]any{"reason": "folded into " + pref, "merged_into": pref}); err != nil {
			return err
		}
		out, err = s.GetWork(ctx, child.CardID)
		return err
	})
	return out, err
}

func (s *Store) UnfoldWork(ctx context.Context, actor, id string) (map[string]any, error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	var out map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		child, err := s.loadGroupingCard(ctx, tx, id)
		if err != nil {
			return err
		}
		cm, err := groupingMetadata(ctx, tx, child.CardID)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(cm[foldSnapshotKey])
		var snapshot foldSnapshot
		if json.Unmarshal(data, &snapshot) != nil || snapshot.Parent == "" {
			return workInvalid("card has no fold to reverse")
		}
		parent, err := s.loadGroupingCard(ctx, tx, snapshot.Parent)
		if err != nil {
			return err
		}
		pm, err := groupingMetadata(ctx, tx, parent.CardID)
		if err != nil {
			return err
		}
		plan, err := loadGroupingPlan(ctx, tx, parent.CardID)
		if err != nil {
			return err
		}
		if child.TrashedAt.Valid || child.ArchivedAt.String != snapshot.ArchivedAt || !jsonEqual(foldOwned(child, cm), snapshot.ChildAfter) || !jsonEqual(foldOwned(parent, pm), snapshot.ParentAfter) || !jsonEqual(plan, snapshot.PlanAfter) {
			return ErrConflict
		}
		delete(cm, foldSnapshotKey)
		if err = saveFoldOwned(ctx, tx, parent.CardID, actor, pm, snapshot.ParentBefore); err != nil {
			return err
		}
		if err = saveFoldOwned(ctx, tx, child.CardID, actor, cm, snapshot.ChildBefore); err != nil {
			return err
		}
		if err = s.saveGroupingPlan(ctx, tx, actor, parent, snapshot.PlanBefore); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET archived_at=NULL,archived_by=NULL,parent_card_id=?,needs_home=? WHERE id=?`, nullableString(snapshot.TreeParentBefore), snapshot.TreeParentBefore == "", child.CardID); err != nil {
			return err
		}
		card, err := child.toMap()
		if err != nil {
			return err
		}
		if err = insertWorkEvent(ctx, tx, actor, card, "card_updated", "Card unfolded", map[string]any{"changed_fields": []string{"fold"}, "unfolded_from": snapshot.Parent}); err != nil {
			return err
		}
		out, err = s.GetWork(ctx, child.CardID)
		return err
	})
	return out, err
}
