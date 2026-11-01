// Board summary projections: open-work-item counts, risk state, and activity
// rollups over a board's cards, plus board membership reads.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const boardSummaryRiskHorizon = 7 * 24 * time.Hour

type BoardMembership struct {
	Board map[string]any
	Card  map[string]any
}

func (s *Store) ListBoardMembershipsByThread(ctx context.Context, threadID string) ([]BoardMembership, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return []BoardMembership{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT b.id, b.title, b.handle, b.archived_at, b.trashed_at, re.source_id, re.target_id, json_extract(re.metadata_json, '$.column_key'), c.title, c.resolution, c.parent_thread_id, c.pinned_document_id, c.due_at, c.updated_at
		   FROM ref_edges re
		   JOIN boards b ON b.id = re.source_id
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND c.parent_thread_id = ?
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
		  ORDER BY b.updated_at DESC, b.id ASC`,
		refEdgeTypeBoardCard,
		threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("query board memberships: %w", err)
	}
	defer rows.Close()

	out := make([]BoardMembership, 0)
	for rows.Next() {
		var (
			boardID          string
			title            string
			boardHandle      sql.NullString
			boardArchivedAt  sql.NullString
			boardTrashedAt   sql.NullString
			cardBoardID      string
			cardID           string
			columnKey        sql.NullString
			cardTitle        string
			cardResolution   sql.NullString
			parentThreadID   sql.NullString
			pinnedDocumentID sql.NullString
			dueAt            sql.NullString
			updatedAt        string
		)
		if err := rows.Scan(&boardID, &title, &boardHandle, &boardArchivedAt, &boardTrashedAt, &cardBoardID, &cardID, &columnKey, &cardTitle, &cardResolution, &parentThreadID, &pinnedDocumentID, &dueAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan board membership: %w", err)
		}
		publicBoardHandle := firstNonEmpty(strings.TrimSpace(boardHandle.String), cardBoardID)
		out = append(out, BoardMembership{
			Board: map[string]any{
				"id":    boardID,
				"title": title,
				"state": canonicalLifecycleState(boardArchivedAt, boardTrashedAt),
			},
			Card: map[string]any{
				"board_id":     cardBoardID,
				"board_ref":    "board:" + publicBoardHandle,
				"id":           cardID,
				"thread_id":    nullableBoardString(parentThreadID.String),
				"title":        cardTitle,
				"resolution":   canonicalizeCardResolutionForAPI(cardResolution.String),
				"column_key":   nullableBoardString(columnKey.String),
				"related_refs": boardCardRelatedRefs(nil, parentThreadID.String),
				"document_ref": boardTypedRefOrNil("document", pinnedDocumentID.String),
				"due_at":       nullableBoardString(dueAt.String),
				"updated_at":   updatedAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board memberships: %w", err)
	}
	return out, nil
}

func (s *Store) computeBoardSummaries(ctx context.Context, boards []boardRow, typedRefsByBoard map[string][]string, visibility ...func(string, string) bool) (map[string]map[string]any, error) {
	summaries := make(map[string]map[string]any, len(boards))
	if len(boards) == 0 {
		return summaries, nil
	}

	boardIDs := make([]string, 0, len(boards))
	threadIDs := make([]string, 0, len(boards))
	for _, board := range boards {
		boardIDs = append(boardIDs, board.ID)
		threadIDs = append(threadIDs, board.ThreadID)
	}
	var err error
	if typedRefsByBoard == nil {
		typedRefsByBoard, err = loadBoardTypedRefStringsByBoardIDs(ctx, s.db, boardIDs)
		if err != nil {
			return nil, err
		}
	}

	cardsByBoard, err := s.loadBoardCardRowsByBoardIDs(ctx, boardIDs)
	if err != nil {
		return nil, err
	}

	if len(visibility) > 0 && visibility[0] != nil {
		candidates := []map[string]any{}
		for _, cards := range cardsByBoard {
			for _, card := range cards {
				candidates = append(candidates, map[string]any{"id": card.CardID})
			}
		}
		readable, err := s.FilterCardAccess(ctx, candidates, visibility[0])
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, card := range readable {
			allowed[workString(card["id"])] = true
		}
		for boardID, cards := range cardsByBoard {
			filtered := cards[:0]
			for _, card := range cards {
				if allowed[card.CardID] {
					filtered = append(filtered, card)
				}
			}
			cardsByBoard[boardID] = filtered
		}
	}
	allThreadIDs := append([]string{}, threadIDs...)
	for _, rows := range cardsByBoard {
		for _, row := range rows {
			if threadID := strings.TrimSpace(row.ParentThreadID.String); threadID != "" {
				allThreadIDs = append(allThreadIDs, threadID)
			}
		}
	}
	projections, err := s.ListDerivedTopicProjections(ctx, uniqueNormalizedStrings(allThreadIDs))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	for _, board := range boards {
		cards := cardsByBoard[board.ID]
		cardsByColumn := map[string]int{
			"backlog":     0,
			"ready":       0,
			"in_progress": 0,
			"blocked":     0,
			"review":      0,
			"done":        0,
		}
		threadSet := map[string]struct{}{board.ThreadID: {}}
		for _, card := range cards {
			cardsByColumn[card.ColumnKey]++
			if threadID := strings.TrimSpace(card.ParentThreadID.String); threadID != "" {
				threadSet[threadID] = struct{}{}
			}
		}

		unresolvedCardCount := 0
		resolvedCardCount := 0
		atRiskCardCount := 0
		dueSoonCardCount := 0
		overdueCardCount := 0
		blockedCardCount := 0
		staleCardCount := 0
		documentCount := 0
		latestActivityAt := board.UpdatedAt
		for threadID := range threadSet {
			projection, ok := projections[threadID]
			if !ok {
				continue
			}
			documentCount += projection.DocumentCount
			latestActivityAt = maxRFC3339Timestamp(latestActivityAt, projection.LastActivityAt)
		}
		for _, card := range cards {
			if boardCardRowCountsAsOpenWorkItem(card) {
				unresolvedCardCount++
				switch boardCardRowRiskState(card, now, boardSummaryRiskHorizon) {
				case "overdue":
					atRiskCardCount++
					overdueCardCount++
				case "due_soon":
					atRiskCardCount++
					dueSoonCardCount++
				case "blocked":
					atRiskCardCount++
					blockedCardCount++
				}
				if projection, ok := projections[strings.TrimSpace(card.ParentThreadID.String)]; ok && projection.Stale {
					staleCardCount++
				}
			} else {
				resolvedCardCount++
			}
			latestActivityAt = maxRFC3339Timestamp(latestActivityAt, card.UpdatedAt)
		}

		refs := typedRefsByBoard[board.ID]
		if len(refs) == 0 {
			refs, err = decodeStoredJSONList(board.RefsJSON, "board.refs")
			if err != nil {
				return nil, err
			}
		}
		summaries[board.ID] = map[string]any{
			"card_count":            len(cards),
			"cards_by_column":       cardsByColumn,
			"unresolved_card_count": unresolvedCardCount,
			"resolved_card_count":   resolvedCardCount,
			"at_risk_card_count":    atRiskCardCount,
			"due_soon_card_count":   dueSoonCardCount,
			"overdue_card_count":    overdueCardCount,
			"blocked_card_count":    blockedCardCount,
			"stale_card_count":      staleCardCount,
			"document_count":        documentCount,
			"latest_activity_at":    nullableBoardString(latestActivityAt),
			"has_document_refs":     len(boardDocumentRefsFromRefs(refs)) > 0,
		}
	}

	return summaries, nil
}

// BoardCardIsOpenWorkItem is the single definition of “open work” for board cards:
// any non-done column is open; in the done column, open until resolution is done.
func BoardCardIsOpenWorkItem(columnKey, resolution string) bool {
	if strings.TrimSpace(columnKey) != "done" {
		return true
	}
	res := canonicalizeStoredCardResolution(resolution)
	return res != "done"
}

func boardCardRowCountsAsOpenWorkItem(card boardCardRow) bool {
	if card.TrashedAt.Valid && strings.TrimSpace(card.TrashedAt.String) != "" {
		return false
	}
	if card.ArchivedAt.Valid && strings.TrimSpace(card.ArchivedAt.String) != "" {
		return false
	}
	res := ""
	if card.Resolution.Valid {
		res = card.Resolution.String
	}
	return BoardCardIsOpenWorkItem(card.ColumnKey, res)
}

func boardCardRowRiskState(card boardCardRow, now time.Time, riskHorizon time.Duration) string {
	if !boardCardRowCountsAsOpenWorkItem(card) {
		return ""
	}
	if strings.TrimSpace(card.ColumnKey) == "blocked" {
		if dueAt, ok := parseBoardCardRowDueAt(card); ok && !dueAt.After(now.Add(riskHorizon)) && dueAt.Before(now) {
			return "overdue"
		}
		return "blocked"
	}
	dueAt, ok := parseBoardCardRowDueAt(card)
	if !ok || dueAt.After(now.Add(riskHorizon)) {
		return ""
	}
	if dueAt.Before(now) {
		return "overdue"
	}
	return "due_soon"
}

func parseBoardCardRowDueAt(card boardCardRow) (time.Time, bool) {
	if !card.DueAt.Valid || strings.TrimSpace(card.DueAt.String) == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(card.DueAt.String)); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(card.DueAt.String)); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func maxRFC3339Timestamp(values ...string) string {
	var best time.Time
	bestSet := false
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			continue
		}
		if !bestSet || parsed.After(best) {
			best = parsed
			bestSet = true
		}
	}
	if !bestSet {
		return ""
	}
	return best.UTC().Format(time.RFC3339Nano)
}

// GetBoardSummariesVisible prevents public board rollups from exposing hidden
// child state. Cards and plans use the same card/board visibility predicate.
func (s *Store) GetBoardSummariesVisible(ctx context.Context, ids []string, visible func(string, string) bool) (map[string]map[string]any, error) {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,handle,title,summary,role,initiative_target,owners_json,thread_id,refs_json,column_schema_json,created_at,created_by,updated_at,updated_by,archived_at,archived_by,trashed_at,trashed_by,trash_reason FROM boards WHERE id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	boards := []boardRow{}
	for rows.Next() {
		board, err := scanBoardRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		boards = append(boards, board)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return s.computeBoardSummaries(ctx, boards, nil, visible)
}
