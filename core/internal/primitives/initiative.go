package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
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
	n, err := strconv.ParseInt(fmt.Sprint(v), 10, 32)
	if err != nil || n < 0 {
		return sql.NullInt64{}, invalidBoardRequest("initiative_target must be a nonnegative integer or null")
	}
	return sql.NullInt64{Int64: n, Valid: true}, nil
}

type InitiativeGroupingError struct{ Ref string }

func (e *InitiativeGroupingError) Error() string {
	return "initiative board membership declares a top-level initiative; retire it first: anx initiative retire " + e.Ref + " --reason <reason>"
}
func (e *InitiativeGroupingError) Is(target error) bool { return target == ErrInvalidWorkRequest }
func refuseInitiativeGrouping(ctx context.Context, tx *accessTx, c boardCardRow) error {
	var root bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boards b WHERE b.id=c.board_id AND b.role='initiatives' AND b.archived_at IS NULL AND b.trashed_at IS NULL) FROM cards c WHERE c.id=?`, c.CardID).Scan(&root); err != nil {
		return err
	}
	if root {
		return &InitiativeGroupingError{Ref: "card:" + firstNonEmptyString(c.Handle.String, c.CardID)}
	}
	return nil
}
func initiativeBoard(ctx context.Context, q queryRower) (boardRow, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM boards WHERE role='initiatives' AND archived_at IS NULL AND trashed_at IS NULL ORDER BY created_at,id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return boardRow{}, workInvalid("no active initiatives board; use anx boards patch <board> --role initiatives")
	}
	if err != nil {
		return boardRow{}, err
	}
	return loadBoardRow(ctx, q, id)
}

// Counts admit at most 201 physical card identities before scoped hydration.
// A scoped count is always a conservative lower bound, even with an empty window.
func (s *Store) InitiativeBudget(ctx context.Context) (map[string]any, error) {
	board, err := initiativeBoard(ctx, s.db)
	if err != nil {
		return nil, err
	}
	var count, physical int
	err = s.db.QueryRowContext(ctx, `WITH positions AS MATERIALIZED (SELECT id FROM initiative_card_positions LIMIT 201) SELECT (SELECT count(*) FROM positions),(SELECT count(*) FROM positions p CROSS JOIN cards c ON c.id=p.id LEFT JOIN brief_initiatives bi ON bi.card_id=c.id WHERE NOT `+workHasParentSQL("c")+` AND COALESCE(bi.loop_annotated,0)=0 AND COALESCE(bi.loop_last_run_at,'')<?)`, loopTS(time.Now().UTC().Add(-loopWindow))).Scan(&physical, &count)
	if err != nil {
		return nil, err
	}
	_, scoped := accessScopeFrom(ctx)
	target := 15
	if board.InitiativeTarget.Valid {
		target = int(board.InitiativeTarget.Int64)
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
func initiativeMembership(b boardRow) map[string]any {
	return map[string]any{"board_ref": "board:" + firstNonEmptyString(b.Handle.String, b.ID), "initiative": b.Role == "initiatives" && !b.ArchivedAt.Valid && !b.TrashedAt.Valid}
}
func insertInitiativeChange(ctx context.Context, tx *accessTx, actor, kind, reason string, c boardCardRow, before, after boardRow) (map[string]any, error) {
	event := map[string]any{"type": kind, "thread_id": c.ThreadID.String, "refs": []string{"card:" + c.CardID, "board:" + before.ID, "board:" + after.ID}, "summary": "Initiative membership changed: " + c.Title, "payload": map[string]any{"subject_ref": "card:" + firstNonEmptyString(c.Handle.String, c.CardID), "title": c.Title, "reason": reason, "before": initiativeMembership(before), "after": initiativeMembership(after)}}
	p, err := prepareEventForInsert(actor, event)
	if err != nil {
		return nil, err
	}
	if err = insertPreparedEvent(ctx, tx, p); err != nil {
		return nil, err
	}
	return p.Body, nil
}

// Moves primary membership and placement without changing the card's identity or thread.
func (s *Store) moveInitiativeCardTx(ctx context.Context, tx *accessTx, actor string, c boardCardRow, destination boardRow, kind, reason string) (boardCardRow, map[string]any, error) {
	before, err := loadBoardRow(ctx, tx, c.BoardID)
	if err != nil {
		return c, nil, err
	}
	if destination.ArchivedAt.Valid || destination.TrashedAt.Valid {
		return c, nil, workInvalid("destination board must be active")
	}
	if err = ensureBoardCardMutable(c); err != nil {
		return c, nil, err
	}
	if err = ensureNativeWorkMutation(ctx, tx, c.CardID); err != nil {
		return c, nil, err
	}
	if c.BoardID == destination.ID {
		return c, nil, ErrConflict
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, destination.ID, c.ColumnKey, "", "", c.CardID)
	if err != nil {
		return c, nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type='board' AND source_id=? AND target_type='card' AND target_id=? AND edge_type=?`, c.BoardID, c.CardID, refEdgeTypeBoardCard); err != nil {
		return c, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET board_id=?,rank=?,initiative_root=?,updated_at=?,updated_by=? WHERE id=?`, destination.ID, rank, destination.Role == "initiatives", time.Now().UTC().Format(time.RFC3339Nano), actor, c.CardID); err != nil {
		return c, nil, err
	}
	if err = upsertBoardCardRefEdge(ctx, tx, destination.ID, c.CardID, c.ColumnKey, rank); err != nil {
		return c, nil, err
	}
	if _, err = touchBoardRow(ctx, tx, before, actor); err != nil {
		return c, nil, err
	}
	if _, err = touchBoardRow(ctx, tx, destination, actor); err != nil {
		return c, nil, err
	}
	c.BoardID = destination.ID
	c.BoardHandle = destination.Handle
	c.Rank = rank
	if err = markBoardCardProjectionDirtyTx(ctx, tx, c); err != nil {
		return c, nil, err
	}
	var event map[string]any
	if before.Role == "initiatives" || destination.Role == "initiatives" {
		event, err = insertInitiativeChange(ctx, tx, actor, kind, reason, c, before, destination)
	}
	if err != nil {
		return c, nil, err
	}
	c, err = s.loadBoardCardByIdentifier(ctx, tx, destination.ID, c.CardID, true)
	return c, event, err
}
func (s *Store) ChangeInitiative(ctx context.Context, actor, id, verb, reason, toBoard, under string) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	if actor == "" || reason == "" || len([]rune(reason)) > 2000 {
		return nil, workInvalid("actor and reason (1..2000 characters) are required")
	}
	if verb != "promote" && verb != "retire" {
		return nil, workInvalid("unknown initiative action")
	}
	if verb == "promote" && (toBoard != "" || under != "") {
		return nil, workInvalid("to_board and under are retire-only")
	}
	var out map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		c, err := s.loadGroupingCard(ctx, tx, id)
		if err != nil {
			return err
		}
		current, err := loadBoardRow(ctx, tx, c.BoardID)
		if err != nil {
			return err
		}
		var destination boardRow
		var underID string
		if verb == "promote" {
			if c.ColumnKey == "done" || c.ColumnKey == "cancelled" {
				return workInvalid("reopen the card before promoting it")
			}
			if current.Role == "initiatives" && !current.ArchivedAt.Valid && !current.TrashedAt.Valid {
				return ErrConflict
			}
			destination, err = initiativeBoard(ctx, tx)
		} else {
			if current.Role != "initiatives" {
				return workInvalid("card is not on an initiatives board")
			}
			if under != "" {
				parent, e := s.loadGroupingCard(ctx, tx, under)
				if e != nil {
					return e
				}
				b, e := loadBoardRow(ctx, tx, parent.BoardID)
				if e != nil {
					return e
				}
				var hasParent bool
				if e = tx.QueryRowContext(ctx, `SELECT `+workHasParentSQL("c")+` FROM cards c WHERE c.id=?`, parent.CardID).Scan(&hasParent); e != nil {
					return e
				}
				underID = parent.CardID
				if hasParent || parent.ColumnKey == "done" || parent.ColumnKey == "cancelled" || parent.CardID == c.CardID || b.Role != "initiatives" || b.ArchivedAt.Valid || b.TrashedAt.Valid {
					return workInvalid("under must name another active initiative")
				}
			}
			if toBoard != "" {
				r, e := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "board", Ref: toBoard})
				if e != nil {
					return e
				}
				destination, err = loadBoardRow(ctx, tx, r.ID)
			} else {
				var boardID string
				err = tx.QueryRowContext(ctx, `SELECT id FROM boards WHERE role<>'initiatives' AND archived_at IS NULL AND trashed_at IS NULL ORDER BY created_at,id LIMIT 1`).Scan(&boardID)
				if errors.Is(err, sql.ErrNoRows) {
					b, e := s.CreateBoard(ctx, actor, map[string]any{"title": "Tasks"})
					if e != nil {
						return e
					}
					boardID = workString(b["id"])
					err = nil
				}
				if err == nil {
					destination, err = loadBoardRow(ctx, tx, boardID)
				}
			}
			if err == nil && destination.Role == "initiatives" {
				return workInvalid("retire destination must not have role initiatives")
			}
		}
		if err != nil {
			return err
		}
		c, event, err := s.moveInitiativeCardTx(ctx, tx, actor, c, destination, "initiative_"+map[string]string{"promote": "promoted", "retire": "retired"}[verb], reason)
		if err != nil {
			return err
		}
		if under != "" {
			if _, err = s.groupWorkTx(ctx, tx, actor, c, under, "", ""); err != nil {
				return err
			}
		}
		if underID != "" {
			var actual sql.NullString
			if err = tx.QueryRowContext(ctx, `SELECT `+workParentSQL("c")+` FROM cards c WHERE c.id=?`, c.CardID).Scan(&actual); err != nil {
				return err
			}
			if actual.String != underID {
				return workInvalid("existing earlier plan links would select another parent; resolve those links before retiring with --under")
			}
		}
		metadata, e := groupingMetadata(ctx, tx, c.CardID)
		if e != nil {
			return e
		}
		if e = saveGroupingMetadata(ctx, tx, c.CardID, actor, metadata); e != nil {
			return e
		}
		work, err := s.GetWork(ctx, c.CardID)
		if err != nil {
			return err
		}
		viewer, human := hierarchyViewer(ctx)
		if err = s.EnrichWorkHierarchy(ctx, []map[string]any{work}, viewer, human, time.Now()); err != nil {
			return err
		}
		out = map[string]any{"work": work, "event": event}
		budget, err := s.InitiativeBudget(ctx)
		if err != nil {
			return err
		}
		out["initiative_budget"] = budget
		if fact := initiativeOverTarget(budget, workString(work["ref"])); fact != nil {
			out["over_target"] = fact
		}
		return nil
	})
	return out, err
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

// Capture only new links, before replacing the plan. Existing grouped members
// are deliberately left alone: upgrading a workspace must not promote its old
// children. Exact target lookups and the incoming-ref index bound this write to
// the submitted plan, without a startup scan or a read-time graph rebuild.
// The keys-only canonical guard also preserves parents hidden from this writer;
// all target hydration and mutation still use the scoped cards relation.
func protectNewInitiativeLinks(ctx context.Context, tx *accessTx, parentID string, p plans.Plan) error {
	refs := []string{}
	for _, step := range p.Steps {
		if strings.HasPrefix(step.Ref, "card:") {
			refs = append(refs, step.Ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	raw, _ := json.Marshal(refs)
	rows, err := tx.QueryContext(ctx, `WITH refs AS MATERIALIZED (SELECT DISTINCT value AS ref FROM json_each(?)),
 targets AS MATERIALIZED (
 SELECT c.id,r.ref FROM refs r CROSS JOIN cards c ON c.id=substr(r.ref,6)
 UNION SELECT c.id,r.ref FROM refs r CROSS JOIN cards c ON c.handle=substr(r.ref,6))
 SELECT DISTINCT c.id FROM targets t CROSS JOIN cards c ON c.id=t.id
 WHERE c.id<>? AND c.initiative_root=0 AND c.board_id IN (SELECT id FROM boards WHERE role='initiatives' AND archived_at IS NULL AND trashed_at IS NULL)
 AND c.archived_at IS NULL AND c.trashed_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM brief_plan_refs old WHERE old.card_id=? AND old.ref=t.ref)
 AND NOT EXISTS(SELECT 1 FROM initiative_legacy_parent_positions legacy WHERE legacy.id=c.id)`, string(raw), parentID, parentID)
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return err
	}
	keys, _ := json.Marshal(ids)
	_, err = tx.ExecContext(ctx, `UPDATE cards SET initiative_root=1 WHERE id IN (SELECT value FROM json_each(?))`, string(keys))
	return err
}
