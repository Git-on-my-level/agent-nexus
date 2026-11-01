// Board read paths: get, summary, and the filtered board list query.

package primitives

import (
	"context"
	"fmt"
	"strings"
)

type BoardListFilter struct {
	States []string

	Owner  string
	Owners []string
	Query  string
	Limit  *int
	Cursor string
}

type BoardListItem struct {
	Board   map[string]any
	Summary map[string]any
}

func (s *Store) GetBoard(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
}

func (s *Store) GetBoardSummary(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	summaries, err := s.computeBoardSummaries(ctx, []boardRow{row}, nil)
	if err != nil {
		return nil, err
	}
	summary, ok := summaries[row.ID]
	if !ok {
		return map[string]any{}, nil
	}
	return summary, nil
}

func (s *Store) ListBoards(ctx context.Context, filter BoardListFilter) ([]BoardListItem, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListBoardsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query boards: %w", err)
	}
	defer rows.Close()

	boardRows := make([]boardRow, 0)
	for rows.Next() {
		row, err := scanBoardRow(rows)
		if err != nil {
			return nil, "", err
		}
		boardRows = append(boardRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate boards: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(boardRows) > *filter.Limit {
		boardRows = boardRows[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	if len(boardRows) == 0 {
		return []BoardListItem{}, nextCursor, nil
	}

	boardIDs := make([]string, 0, len(boardRows))
	for _, row := range boardRows {
		boardIDs = append(boardIDs, row.ID)
	}
	typedByBoard, err := loadBoardTypedRefStringsByBoardIDs(ctx, s.db, boardIDs)
	if err != nil {
		return nil, "", err
	}
	cardByBoard, err := loadBoardCardRefStringsByBoardIDs(ctx, s.db, boardIDs)
	if err != nil {
		return nil, "", err
	}

	summaries, err := s.computeBoardSummaries(ctx, boardRows, typedByBoard)
	if err != nil {
		return nil, "", err
	}

	out := make([]BoardListItem, 0, len(boardRows))
	for _, row := range boardRows {
		typedRefs := typedByBoard[row.ID]
		if len(typedRefs) == 0 {
			typedRefs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
			if err != nil {
				return nil, "", err
			}
		}
		cardRefs := cardByBoard[row.ID]
		if cardRefs == nil {
			cardRefs = []string{}
		}
		board, err := row.boardToMapWithRefData(typedRefs, cardRefs)
		if err != nil {
			return nil, "", err
		}
		out = append(out, BoardListItem{
			Board:   board,
			Summary: summaries[row.ID],
		})
	}
	return out, nextCursor, nil
}

func buildListBoardsQuery(filter BoardListFilter) (string, []any) {
	query := `SELECT id, handle, title, summary, role, initiative_target, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM boards
		WHERE 1=1`
	args := make([]any, 0, 8)
	query += ` AND ` + LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)

	ownerFilters := uniqueNormalizedStrings(append([]string{filter.Owner}, filter.Owners...))
	if len(ownerFilters) > 0 {
		parts := make([]string, 0, len(ownerFilters))
		for _, owner := range ownerFilters {
			parts = append(parts, `EXISTS (SELECT 1 FROM json_each(owners_json) WHERE value = ?)`)
			args = append(args, owner)
		}
		query += ` AND (` + strings.Join(parts, ` OR `) + `)`
	}

	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(id) LIKE ? OR LOWER(title) LIKE ? OR LOWER(summary) LIKE ?)`
		args = append(args, searchPattern, searchPattern, searchPattern)
	}

	query += ` ORDER BY updated_at DESC, id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
				query += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}
	return query, args
}
