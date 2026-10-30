package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func nullableInitiativeTarget(v sql.NullInt64) any {
	if v.Valid {
		return v.Int64
	}
	return nil
}
func normalizeInitiativeTarget(m map[string]any, fallback sql.NullInt64) (sql.NullInt64, error) {
	v, ok := m["initiative_target"]
	if !ok {
		return fallback, nil
	}
	if v == nil {
		return sql.NullInt64{}, nil
	}
	n, valid := workInt(v)
	if !valid || n < 0 {
		return sql.NullInt64{}, invalidBoardRequest("initiative_target must be a nonnegative integer or null")
	}
	return sql.NullInt64{Int64: n, Valid: true}, nil
}

type InitiativeGroupingError struct{ Ref string }

func (e *InitiativeGroupingError) Error() string {
	return e.Ref + " is an initiative; move it explicitly: anx work move " + e.Ref + " --under <parent> --reason <reason>"
}
func (e *InitiativeGroupingError) Is(target error) bool { return target == ErrInvalidWorkRequest }

// An initiative is an explicit top-level outcome. Implicit grouping (create
// --under, group, fold) never demotes one; MoveWork with a reason does.
func refuseInitiativeGrouping(ctx context.Context, tx *accessTx, c boardCardRow) error {
	var root bool
	if err := tx.QueryRowContext(ctx, `SELECT `+workIsInitiativeSQL("c")+` AND c.column_key NOT IN ('done','cancelled') FROM main.cards c WHERE c.id=?`, c.CardID).Scan(&root); err != nil {
		return err
	}
	if root {
		return &InitiativeGroupingError{Ref: "card:" + firstNonEmptyString(c.Handle.String, c.CardID)}
	}
	return nil
}

// The initiative target lives on the oldest initiatives-role board when one
// exists (boards remain layout); a workspace without one uses the default.
func initiativeTarget(ctx context.Context, q queryRower) (sql.NullInt64, error) {
	var target sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT initiative_target FROM boards WHERE role='initiatives' AND archived_at IS NULL AND trashed_at IS NULL ORDER BY created_at,id LIMIT 1`).Scan(&target)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullInt64{}, nil
	}
	return target, err
}

// Counts admit at most 201 physical card identities before scoped hydration.
// A scoped count is always a conservative lower bound, even with an empty window.
func (s *Store) InitiativeBudget(ctx context.Context) (map[string]any, error) {
	configured, err := initiativeTarget(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var count, physical int
	err = s.db.QueryRowContext(ctx, `WITH positions AS MATERIALIZED (SELECT id FROM initiative_card_positions LIMIT 201) SELECT (SELECT count(*) FROM positions),(SELECT count(*) FROM positions p CROSS JOIN cards c ON c.id=p.id LEFT JOIN brief_initiatives bi ON bi.card_id=c.id WHERE COALESCE(bi.loop_annotated,0)=0 AND COALESCE(bi.loop_last_run_at,'')<?)`, loopTS(time.Now().UTC().Add(-loopWindow))).Scan(&physical, &count)
	if err != nil {
		return nil, err
	}
	_, scoped := accessScopeFrom(ctx)
	target := 15
	if configured.Valid {
		target = int(configured.Int64)
	}
	return map[string]any{"count": count, "target": target, "truncated": scoped || physical > 200}, nil
}
func initiativeOverTarget(budget map[string]any, ref string) map[string]any {
	if budget["count"].(int) <= budget["target"].(int) {
		return nil
	}
	argv := []string{"anx", "ask", "Initiatives exceed the target: fold this initiative or keep it?", "--subject-ref", ref, "--recommend", "Retire and fold this initiative into an existing initiative; keep it if it needs a separate outcome."}
	return map[string]any{"count": budget["count"], "target": budget["target"], "truncated": budget["truncated"], "next_actions": []map[string]any{{"command_id": "ask", "argv": argv, "mutates": true, "side_effect_class": "remote_coordination_write"}}}
}

// Retain the original board as event provenance: a later layout move must not
// publish the private historical rationale through a now-public card/thread.
func insertInitiativeChange(ctx context.Context, tx *accessTx, actor, kind, reason string, c boardCardRow, before, after map[string]any) (map[string]any, error) {
	event := map[string]any{"type": kind, "thread_id": c.ThreadID.String, "refs": []string{"card:" + c.CardID, "board:" + c.BoardID}, "summary": "Initiative membership changed: " + c.Title, "payload": map[string]any{"subject_ref": "card:" + firstNonEmptyString(c.Handle.String, c.CardID), "title": c.Title, "reason": reason, "before": before, "after": after}}
	p, err := prepareEventForInsert(actor, event)
	if err != nil {
		return nil, err
	}
	if err = insertPreparedEvent(ctx, tx, p); err != nil {
		return nil, err
	}
	return p.Body, nil
}

// moveCardBoardTx changes board placement only. Boards are layout; the work
// tree, not the board, decides initiatives.
func (s *Store) moveCardBoardTx(ctx context.Context, tx *accessTx, actor string, c boardCardRow, destination boardRow) (boardCardRow, error) {
	before, err := loadBoardRow(ctx, tx, c.BoardID)
	if err != nil {
		return c, err
	}
	if destination.ArchivedAt.Valid || destination.TrashedAt.Valid {
		return c, workInvalid("destination board must be active")
	}
	if err = ensureBoardCardMutable(c); err != nil {
		return c, err
	}
	if err = ensureNativeWorkMutation(ctx, tx, c.CardID); err != nil {
		return c, err
	}
	if c.BoardID == destination.ID {
		return c, ErrConflict
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, destination.ID, c.ColumnKey, "", "", c.CardID)
	if err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type='board' AND source_id=? AND target_type='card' AND target_id=? AND edge_type=?`, c.BoardID, c.CardID, refEdgeTypeBoardCard); err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET board_id=?,rank=?,updated_at=?,updated_by=? WHERE id=?`, destination.ID, rank, time.Now().UTC().Format(time.RFC3339Nano), actor, c.CardID); err != nil {
		return c, err
	}
	if err = upsertBoardCardRefEdge(ctx, tx, destination.ID, c.CardID, c.ColumnKey, rank); err != nil {
		return c, err
	}
	if _, err = touchBoardRow(ctx, tx, before, actor); err != nil {
		return c, err
	}
	if _, err = touchBoardRow(ctx, tx, destination, actor); err != nil {
		return c, err
	}
	c.BoardID = destination.ID
	c.BoardHandle = destination.Handle
	c.Rank = rank
	if err = markBoardCardProjectionDirtyTx(ctx, tx, c); err != nil {
		return c, err
	}
	return s.loadBoardCardByIdentifier(ctx, tx, destination.ID, c.CardID, true)
}

// MoveWorkInput names exactly one destination: a parent, the top level
// (promote to initiative) or the Unsorted tray.
type MoveWorkInput struct {
	Under    string
	Root     bool
	Unsorted bool
	Reason   string
}

// MoveWork is the one hierarchy mutation. Promote, retire, group and ungroup
// are spellings of it. Changing whether a card is an initiative requires a
// reason and records an initiative_promoted/initiative_retired event.
func (s *Store) MoveWork(ctx context.Context, actor, id string, in MoveWorkInput) (map[string]any, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	destinations := 0
	for _, set := range []bool{in.Under != "", in.Root, in.Unsorted} {
		if set {
			destinations++
		}
	}
	if actor == "" || destinations != 1 {
		return nil, workInvalid("move needs exactly one of under, root or unsorted")
	}
	if len([]rune(in.Reason)) > 2000 {
		return nil, workInvalid("reason must be at most 2000 characters")
	}
	var out map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		c, err := s.loadGroupingCard(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = ensureBoardCardMutable(c); err != nil {
			return err
		}
		if err = ensureNativeWorkMutation(ctx, tx, c.CardID); err != nil {
			return err
		}
		var wasInitiative bool
		var oldParent sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT `+workIsInitiativeSQL("c")+`,c.parent_card_id FROM main.cards c WHERE c.id=?`, c.CardID).Scan(&wasInitiative, &oldParent); err != nil {
			return err
		}
		if in.Root != wasInitiative && in.Reason == "" {
			return workInvalid("a reason is required to promote or retire an initiative")
		}
		var event map[string]any
		if in.Root {
			if c.ColumnKey == "done" || c.ColumnKey == "cancelled" {
				return workInvalid("reopen the card before promoting it")
			}
			if wasInitiative {
				return ErrConflict
			}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if _, err = tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=NULL,needs_home=0,updated_at=?,updated_by=? WHERE id=?`, now, actor, c.CardID); err != nil {
				return err
			}
			if err = markBoardCardProjectionDirtyTx(ctx, tx, c); err != nil {
				return err
			}
			card, e := c.toMap()
			if e != nil {
				return e
			}
			if err = insertWorkEvent(ctx, tx, actor, card, "card_updated", "Card moved in the work tree", map[string]any{"changed_fields": []string{"parent"}, "parent_ref": nil, "needs_home": false}); err != nil {
				return err
			}
		} else if _, err = s.moveWorkTx(ctx, tx, actor, c, in.Under); err != nil {
			return err
		}
		if in.Root != wasInitiative {
			kind := map[bool]string{true: "initiative_promoted", false: "initiative_retired"}[in.Root]
			before := map[string]any{"initiative": wasInitiative, "parent_ref": workCardRef(ctx, tx, oldParent.String)}
			after := map[string]any{"initiative": in.Root, "parent_ref": nil}
			if in.Under != "" {
				after["parent_ref"] = in.Under
			}
			if event, err = insertInitiativeChange(ctx, tx, actor, kind, in.Reason, c, before, after); err != nil {
				return err
			}
		}
		work, err := s.GetWork(ctx, c.CardID)
		if err != nil {
			return err
		}
		viewer, human := hierarchyViewer(ctx)
		if err = s.EnrichWorkHierarchy(ctx, []map[string]any{work}, viewer, human, time.Now()); err != nil {
			return err
		}
		out = map[string]any{"work": work}
		if event != nil {
			out["event"] = event
			budget, err := s.InitiativeBudget(ctx)
			if err != nil {
				return err
			}
			out["initiative_budget"] = budget
			if fact := initiativeOverTarget(budget, workString(work["ref"])); fact != nil {
				out["over_target"] = fact
			}
		}
		return nil
	})
	return out, err
}

// workCardRef spells a card ID as a ref when the caller can see it.
func workCardRef(ctx context.Context, tx *accessTx, id string) any {
	if id == "" {
		return nil
	}
	var ref string
	if err := tx.QueryRowContext(ctx, `SELECT 'card:'||COALESCE(NULLIF(handle,''),id) FROM cards WHERE id=?`, id).Scan(&ref); err != nil {
		return nil
	}
	return ref
}

// ChangeInitiative keeps the promote/retire routes as spellings of MoveWork.
// Boards no longer decide initiatives; to_board is accepted and ignored.
func (s *Store) ChangeInitiative(ctx context.Context, actor, id, verb, reason, toBoard, under string) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if actor == "" || reason == "" || len([]rune(reason)) > 2000 {
		return nil, workInvalid("actor and reason (1..2000 characters) are required")
	}
	switch verb {
	case "promote":
		if toBoard != "" || under != "" {
			return nil, workInvalid("to_board and under are retire-only")
		}
		return s.MoveWork(ctx, actor, id, MoveWorkInput{Root: true, Reason: reason})
	case "retire":
		if under == "" {
			return s.MoveWork(ctx, actor, id, MoveWorkInput{Unsorted: true, Reason: reason})
		}
		return s.MoveWork(ctx, actor, id, MoveWorkInput{Under: under, Reason: reason})
	}
	return nil, workInvalid("unknown initiative action")
}

// InitiativeChanges hydrates only scoped canonical events from a fixed routing
// window. Hidden rows never trigger refill or contribute to returned metadata.
func (s *Store) InitiativeChanges(ctx context.Context, limit int) (map[string]any, error) {
	if limit < 1 || limit > 50 {
		return nil, workInvalid("initiative changes limit must be 1..50")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.ts,e.type,e.actor_id,e.payload_json FROM (SELECT id FROM initiative_change_positions ORDER BY anx_timestamp_key(ts) DESC,id DESC LIMIT 200) p CROSS JOIN events e ON e.id=p.id WHERE e.trashed_at IS NULL ORDER BY anx_timestamp_key(e.ts) DESC,e.id DESC LIMIT ?`, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, ts, kind, actor, raw string
		if err = rows.Scan(&id, &ts, &kind, &actor, &raw); err != nil {
			return nil, err
		}
		var body map[string]any
		if err = json.Unmarshal([]byte(raw), &body); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"ref": "event:" + id, "ts": ts, "type": kind, "actor_id": actor, "payload": body["payload"]})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	_, scoped := accessScopeFrom(ctx)
	more := scoped || len(items) > limit
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"items": items, "truncated": more}, nil
}

// Implicit children cannot inherit the declaration board. Keep the parent's
// visibility through an ordinary canonical reference, even on a public Tasks board.
func (s *Store) createImplicitInitiativeChild(ctx context.Context, actor, parent string, input map[string]any) (map[string]any, error) {
	var out map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		var board string
		err := tx.QueryRowContext(ctx, `SELECT id FROM boards WHERE role<>'initiatives' AND archived_at IS NULL AND trashed_at IS NULL ORDER BY created_at,id LIMIT 1`).Scan(&board)
		if errors.Is(err, sql.ErrNoRows) {
			b, e := s.CreateBoard(ctx, actor, map[string]any{"title": "Tasks"})
			if e != nil {
				return e
			}
			board = workString(b["id"])
			err = nil
		}
		if err != nil {
			return err
		}
		next := workClone(input)
		related, err := optionalStringListField(next, "related_refs")
		if err != nil {
			return err
		}
		next["related_refs"] = uniqueNormalizedStrings(append(related, "card:"+parent))
		out, err = s.CreateWork(ctx, actor, board, next)
		return err
	})
	return out, err
}
