// Board column rank allocation, rebalancing, and ordered card loading for a
// column.

package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
)

func (s *Store) loadOrderedBoardCards(ctx context.Context, q queryRower, boardID string, columnKey string) ([]boardCardRow, error) {
	db, ok := q.(interface {
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	})
	if !ok {
		return nil, fmt.Errorf("board card query does not support row iteration")
	}

	query := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
			  FROM ref_edges re
			  LEFT JOIN boards b ON b.id = re.source_id
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND c.archived_at IS NULL AND c.trashed_at IS NULL`
	args := []any{boardDefaultColumn, refEdgeTypeBoardCard, boardID}
	if strings.TrimSpace(columnKey) != "" {
		query += ` AND COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) = ?`
		args = append(args, boardDefaultColumn, columnKey)
	}
	query += `
		) AS ordered_cards
		ORDER BY ` + boardColumnOrderSQL(`column_key`) + `, rank ASC, card_id ASC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query board cards: %w", err)
	}
	defer rows.Close()

	out := make([]boardCardRow, 0)
	for rows.Next() {
		row, err := scanBoardCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board cards: %w", err)
	}
	return out, nil
}

func (s *Store) allocateBoardCardRank(ctx context.Context, tx *accessTx, boardID, columnKey, beforeCardID, afterCardID, excludeCardID string) (string, error) {
	cards, err := s.loadOrderedBoardCards(ctx, tx, boardID, columnKey)
	if err != nil {
		return "", err
	}
	filtered := make([]boardCardRow, 0, len(cards))
	for _, card := range cards {
		if card.CardID == excludeCardID {
			continue
		}
		filtered = append(filtered, card)
	}

	insertIndex, err := boardInsertIndex(filtered, beforeCardID, afterCardID)
	if err != nil {
		return "", err
	}
	prevRank := ""
	if insertIndex > 0 {
		prevRank = filtered[insertIndex-1].Rank
	}
	nextRank := ""
	if insertIndex < len(filtered) {
		nextRank = filtered[insertIndex].Rank
	}

	rank, ok := allocateBoardRankBetween(prevRank, nextRank)
	if ok {
		return rank, nil
	}
	if err := rebalanceBoardColumnRanks(ctx, tx, boardID, columnKey, excludeCardID); err != nil {
		return "", err
	}
	cards, err = s.loadOrderedBoardCards(ctx, tx, boardID, columnKey)
	if err != nil {
		return "", err
	}
	filtered = filtered[:0]
	for _, card := range cards {
		if card.CardID == excludeCardID {
			continue
		}
		filtered = append(filtered, card)
	}
	insertIndex, err = boardInsertIndex(filtered, beforeCardID, afterCardID)
	if err != nil {
		return "", err
	}
	prevRank = ""
	if insertIndex > 0 {
		prevRank = filtered[insertIndex-1].Rank
	}
	nextRank = ""
	if insertIndex < len(filtered) {
		nextRank = filtered[insertIndex].Rank
	}
	rank, ok = allocateBoardRankBetween(prevRank, nextRank)
	if !ok {
		return "", fmt.Errorf("failed to allocate board rank")
	}
	return rank, nil
}

func boardInsertIndex(cards []boardCardRow, beforeCardID, afterCardID string) (int, error) {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
	if beforeCardID == "" && afterCardID == "" {
		return len(cards), nil
	}
	if beforeCardID != "" && afterCardID != "" {
		return 0, invalidBoardRequest("before_card_id and after_card_id are mutually exclusive")
	}
	for i, card := range cards {
		if beforeCardID != "" && card.CardID == beforeCardID {
			return i, nil
		}
		if afterCardID != "" && card.CardID == afterCardID {
			return i + 1, nil
		}
	}
	if beforeCardID != "" {
		return 0, invalidBoardRequest("before_card_id must reference a card already on the board")
	}
	return 0, invalidBoardRequest("after_card_id must reference a card already on the board")
}

func allocateBoardRankBetween(prevRank, nextRank string) (string, bool) {
	prevValue, ok := parseBoardRank(prevRank)
	if prevRank != "" && !ok {
		return "", false
	}
	nextValue, ok := parseBoardRank(nextRank)
	if nextRank != "" && !ok {
		return "", false
	}

	switch {
	case prevRank == "" && nextRank == "":
		return formatBoardRank(boardRankStep), true
	case prevRank == "":
		if nextValue <= 1 {
			return "", false
		}
		candidate := nextValue / 2
		if candidate == 0 || candidate >= nextValue {
			return "", false
		}
		return formatBoardRank(candidate), true
	case nextRank == "":
		if prevValue > math.MaxUint64-boardRankStep {
			return "", false
		}
		return formatBoardRank(prevValue + boardRankStep), true
	default:
		if nextValue <= prevValue+1 {
			return "", false
		}
		candidate := prevValue + ((nextValue - prevValue) / 2)
		if candidate <= prevValue || candidate >= nextValue {
			return "", false
		}
		return formatBoardRank(candidate), true
	}
}

func rebalanceBoardColumnRanks(ctx context.Context, tx *accessTx, boardID, columnKey, excludeCardID string) error {
	rows, err := loadBoardCardsForColumn(ctx, tx, boardID, columnKey)
	if err != nil {
		return err
	}
	nextRankValue := boardRankStep
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range rows {
		if row.CardID == excludeCardID {
			continue
		}
		rankStr := formatBoardRank(nextRankValue)
		if err := upsertBoardCardRefEdge(ctx, tx, boardID, row.CardID, columnKey, rankStr); err != nil {
			return fmt.Errorf("rebalance board card rank: %w", err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE cards SET rank = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			rankStr,
			now,
			actors.SystemActorID,
			row.CardID,
		); err != nil {
			return fmt.Errorf("rebalance board card rank: %w", err)
		}
		nextRankValue += boardRankStep
	}
	return nil
}

func loadBoardCardsForColumn(ctx context.Context, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, boardID, columnKey string) ([]boardCardRow, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
		        COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
		        COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
		   FROM ref_edges re
		   LEFT JOIN boards b ON b.id = re.source_id
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND re.source_id = ?
		    AND COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) = ?
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
		  ORDER BY rank ASC, card_id ASC`,
		boardDefaultColumn,
		refEdgeTypeBoardCard,
		boardID,
		boardDefaultColumn,
		columnKey,
	)
	if err != nil {
		return nil, fmt.Errorf("query board cards for column: %w", err)
	}
	defer rows.Close()

	out := make([]boardCardRow, 0)
	for rows.Next() {
		row, err := scanBoardCardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board cards for column: %w", err)
	}
	return out, nil
}

func formatBoardRank(value uint64) string {
	return fmt.Sprintf("%0*d", boardRankWidth, value)
}

func parseBoardRank(raw string) (uint64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
