package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidBoardRequest = errors.New("invalid board request")

type BoardListFilter struct {
	Status string
	Label  string
	Labels []string
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

type AddBoardCardInput struct {
	ThreadID         string
	ColumnKey        string
	BeforeThreadID   string
	AfterThreadID    string
	PinnedDocumentID *string
	IfBoardUpdatedAt *string
}

type UpdateBoardCardInput struct {
	PinnedDocumentID *string
	IfBoardUpdatedAt *string
}

type MoveBoardCardInput struct {
	ColumnKey        string
	BeforeThreadID   string
	AfterThreadID    string
	IfBoardUpdatedAt *string
}

type RemoveBoardCardInput struct {
	IfBoardUpdatedAt *string
}

type BoardCardMutationResult struct {
	Board map[string]any
	Card  map[string]any
}

type BoardCardRemovalResult struct {
	Board           map[string]any
	RemovedThreadID string
}

type BoardMembership struct {
	Board map[string]any
	Card  map[string]any
}

type boardRow struct {
	ID               string
	Title            string
	Status           string
	LabelsJSON       string
	OwnersJSON       string
	PrimaryThreadID  string
	PrimaryDocument  sql.NullString
	ColumnSchemaJSON string
	PinnedRefsJSON   string
	CreatedAt        string
	CreatedBy        string
	UpdatedAt        string
	UpdatedBy        string
}

type boardCardRow struct {
	BoardID          string
	ThreadID         string
	ColumnKey        string
	Rank             string
	PinnedDocumentID sql.NullString
	CreatedAt        string
	CreatedBy        string
	UpdatedAt        string
	UpdatedBy        string
}

var canonicalBoardColumnOrder = []string{"backlog", "ready", "in_progress", "blocked", "review", "done"}

var canonicalBoardColumnTitles = map[string]string{
	"backlog":     "Backlog",
	"ready":       "Ready",
	"in_progress": "In Progress",
	"blocked":     "Blocked",
	"review":      "Review",
	"done":        "Done",
}

const (
	boardDefaultStatus = "active"
	boardDefaultColumn = "backlog"
	boardRankWidth     = 19
	boardRankStep      = uint64(1024)
)

func (s *Store) CreateBoard(ctx context.Context, actorID string, board map[string]any) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	if board == nil {
		return nil, invalidBoardRequest("board is required")
	}

	boardID := strings.TrimSpace(anyStringValue(board["id"]))
	if boardID == "" {
		boardID = uuid.NewString()
	}
	if err := validateBoardID(boardID); err != nil {
		return nil, invalidBoardRequestError(err)
	}
	title := strings.TrimSpace(anyStringValue(board["title"]))
	if title == "" {
		return nil, invalidBoardRequest("board.title is required")
	}
	status, err := normalizeBoardStatus(board["status"], true)
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	labels, err := normalizeOptionalStringList(board, "labels")
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	owners, err := normalizeOptionalStringList(board, "owners")
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	pinnedRefs, err := normalizeOptionalStringList(board, "pinned_refs")
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	primaryThreadID := strings.TrimSpace(anyStringValue(board["primary_thread_id"]))
	if primaryThreadID == "" {
		return nil, invalidBoardRequest("board.primary_thread_id is required")
	}
	primaryDocumentID, err := optionalStringField(board, "primary_document_id")
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	columnSchema, err := normalizeBoardColumnSchema(board["column_schema"], true)
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}

	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("marshal board labels: %w", err)
	}
	ownersJSON, err := json.Marshal(owners)
	if err != nil {
		return nil, fmt.Errorf("marshal board owners: %w", err)
	}
	pinnedRefsJSON, err := json.Marshal(pinnedRefs)
	if err != nil {
		return nil, fmt.Errorf("marshal board pinned refs: %w", err)
	}
	columnSchemaJSON, err := json.Marshal(columnSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal board column schema: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin board create transaction: %w", err)
	}

	if err := ensureThreadExists(ctx, tx, primaryThreadID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if strings.TrimSpace(primaryDocumentID) != "" {
		if err := ensureDocumentExists(ctx, tx, primaryDocumentID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO boards(
			id, title, status, labels_json, owners_json, primary_thread_id, primary_document_id,
			column_schema_json, pinned_refs_json, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		title,
		status,
		string(labelsJSON),
		string(ownersJSON),
		primaryThreadID,
		nullableString(primaryDocumentID),
		string(columnSchemaJSON),
		string(pinnedRefsJSON),
		now,
		actorID,
		now,
		actorID,
	)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert board: %w", err)
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit board create transaction: %w", err)
	}

	return map[string]any{
		"id":                  boardID,
		"title":               title,
		"status":              status,
		"labels":              labels,
		"owners":              owners,
		"primary_thread_id":   primaryThreadID,
		"primary_document_id": nullableBoardString(primaryDocumentID),
		"column_schema":       columnSchema,
		"pinned_refs":         pinnedRefs,
		"created_at":          now,
		"created_by":          actorID,
		"updated_at":          now,
		"updated_by":          actorID,
	}, nil
}

func (s *Store) GetBoard(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) UpdateBoard(ctx context.Context, actorID, boardID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	if len(patch) == 0 {
		return nil, invalidBoardRequest("patch is required")
	}

	if _, exists := patch["id"]; exists {
		return nil, invalidBoardRequest("board.id cannot be patched")
	}
	if _, exists := patch["primary_thread_id"]; exists {
		return nil, invalidBoardRequest("board.primary_thread_id cannot be patched")
	}
	for _, key := range []string{"created_at", "created_by", "updated_at", "updated_by"} {
		if _, exists := patch[key]; exists {
			return nil, invalidBoardRequest("board." + key + " is server-managed and cannot be patched")
		}
	}

	currentRow, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	current, err := currentRow.toMap()
	if err != nil {
		return nil, err
	}

	nextTitle := strings.TrimSpace(anyStringValue(current["title"]))
	if _, exists := patch["title"]; exists {
		nextTitle = strings.TrimSpace(anyStringValue(patch["title"]))
		if nextTitle == "" {
			return nil, invalidBoardRequest("board.title must not be empty")
		}
	}
	nextStatus := strings.TrimSpace(anyStringValue(current["status"]))
	if rawStatus, exists := patch["status"]; exists {
		nextStatus, err = normalizeBoardStatus(rawStatus, false)
		if err != nil {
			return nil, invalidBoardRequestError(err)
		}
	}
	nextLabels := decodeJSONListOrEmpty(currentRow.LabelsJSON)
	if rawLabels, exists := patch["labels"]; exists {
		nextLabels, err = normalizeStringSlice(rawLabels)
		if err != nil {
			return nil, invalidBoardRequest("board.labels must be a list of strings")
		}
		nextLabels = uniqueNormalizedStrings(nextLabels)
	}
	nextOwners := decodeJSONListOrEmpty(currentRow.OwnersJSON)
	if rawOwners, exists := patch["owners"]; exists {
		nextOwners, err = normalizeStringSlice(rawOwners)
		if err != nil {
			return nil, invalidBoardRequest("board.owners must be a list of strings")
		}
		nextOwners = uniqueNormalizedStrings(nextOwners)
	}
	nextPinnedRefs := decodeJSONListOrEmpty(currentRow.PinnedRefsJSON)
	if rawPinnedRefs, exists := patch["pinned_refs"]; exists {
		nextPinnedRefs, err = normalizeStringSlice(rawPinnedRefs)
		if err != nil {
			return nil, invalidBoardRequest("board.pinned_refs must be a list of strings")
		}
		nextPinnedRefs = uniqueNormalizedStrings(nextPinnedRefs)
	}
	nextColumnSchema, err := decodeBoardColumnSchema(currentRow.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	if rawColumnSchema, exists := patch["column_schema"]; exists {
		nextColumnSchema, err = normalizeBoardColumnSchema(rawColumnSchema, false)
		if err != nil {
			return nil, invalidBoardRequestError(err)
		}
	}
	nextPrimaryDocumentID := normalizeNullableString(currentRow.PrimaryDocument.String)
	if rawPrimaryDocumentID, exists := patch["primary_document_id"]; exists {
		nextPrimaryDocumentID = normalizeNullableString(rawPrimaryDocumentID)
	}

	labelsJSON, err := json.Marshal(nextLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal board labels: %w", err)
	}
	ownersJSON, err := json.Marshal(nextOwners)
	if err != nil {
		return nil, fmt.Errorf("marshal board owners: %w", err)
	}
	pinnedRefsJSON, err := json.Marshal(nextPinnedRefs)
	if err != nil {
		return nil, fmt.Errorf("marshal board pinned refs: %w", err)
	}
	columnSchemaJSON, err := json.Marshal(nextColumnSchema)
	if err != nil {
		return nil, fmt.Errorf("marshal board column schema: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin board update transaction: %w", err)
	}

	if nextPrimaryDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *nextPrimaryDocumentID); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}

	query := `UPDATE boards
		SET title = ?, status = ?, labels_json = ?, owners_json = ?, primary_document_id = ?,
		    column_schema_json = ?, pinned_refs_json = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`
	args := []any{
		nextTitle,
		nextStatus,
		string(labelsJSON),
		string(ownersJSON),
		nullableString(derefBoardString(nextPrimaryDocumentID)),
		string(columnSchemaJSON),
		string(pinnedRefsJSON),
		now,
		actorID,
		boardID,
	}
	if ifUpdatedAt != nil {
		query += ` AND updated_at = ?`
		args = append(args, strings.TrimSpace(*ifUpdatedAt))
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("update board: %w", err)
	}
	if ifUpdatedAt != nil {
		rowsAffected, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("read board update result: %w", rowsErr)
		}
		if rowsAffected == 0 {
			_ = tx.Rollback()
			return nil, ErrConflict
		}
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit board update transaction: %w", err)
	}

	return map[string]any{
		"id":                  boardID,
		"title":               nextTitle,
		"status":              nextStatus,
		"labels":              nextLabels,
		"owners":              nextOwners,
		"primary_thread_id":   currentRow.PrimaryThreadID,
		"primary_document_id": nullableBoardString(derefBoardString(nextPrimaryDocumentID)),
		"column_schema":       nextColumnSchema,
		"pinned_refs":         nextPinnedRefs,
		"created_at":          currentRow.CreatedAt,
		"created_by":          currentRow.CreatedBy,
		"updated_at":          now,
		"updated_by":          actorID,
	}, nil
}

func (s *Store) ListBoards(ctx context.Context, filter BoardListFilter) ([]BoardListItem, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
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

	summaries, err := s.computeBoardSummaries(ctx, boardRows)
	if err != nil {
		return nil, "", err
	}

	out := make([]BoardListItem, 0, len(boardRows))
	for _, row := range boardRows {
		board, err := row.toMap()
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

func (s *Store) ListBoardCards(ctx context.Context, boardID string) ([]map[string]any, error) {
	if _, err := s.getBoardRow(ctx, boardID); err != nil {
		return nil, err
	}
	rows, err := s.loadOrderedBoardCards(ctx, s.db, boardID, "")
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toMap())
	}
	return out, nil
}

func (s *Store) AddBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("thread_id is required")
	}
	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		columnKey = boardDefaultColumn
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	if err := validateBoardPlacementAnchors(input.BeforeThreadID, input.AfterThreadID); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card add transaction: %w", err)
	}

	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureThreadExists(ctx, tx, threadID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if threadID == boardRow.PrimaryThreadID {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, invalidBoardRequest("board.primary_thread_id cannot be added as a board card")
	}
	if pinnedDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *pinnedDocumentID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if err := validateBoardAnchors(ctx, tx, boardID, columnKey, strings.TrimSpace(input.BeforeThreadID), strings.TrimSpace(input.AfterThreadID), ""); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, strings.TrimSpace(input.BeforeThreadID), strings.TrimSpace(input.AfterThreadID), "")
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO board_cards(
			board_id, thread_id, column_key, rank, pinned_document_id, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		threadID,
		columnKey,
		rank,
		nullableString(derefBoardString(pinnedDocumentID)),
		now,
		actorID,
		now,
		actorID,
	)
	if err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return BoardCardMutationResult{}, ErrConflict
		}
		return BoardCardMutationResult{}, fmt.Errorf("insert board card: %w", err)
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	cardRow, err := loadBoardCardRow(ctx, tx, boardID, threadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card add transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{
		Board: boardMap,
		Card:  cardRow.toMap(),
	}, nil
}

func (s *Store) UpdateBoardCard(ctx context.Context, actorID, boardID, threadID string, input UpdateBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	if input.PinnedDocumentID == nil {
		return BoardCardMutationResult{}, invalidBoardRequest("patch.pinned_document_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card update transaction: %w", err)
	}

	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if _, err := loadBoardCardRow(ctx, tx, boardID, threadID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)
	if pinnedDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *pinnedDocumentID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE board_cards
		    SET pinned_document_id = ?, updated_at = ?, updated_by = ?
		  WHERE board_id = ? AND thread_id = ?`,
		nullableString(derefBoardString(pinnedDocumentID)),
		now,
		actorID,
		boardID,
		threadID,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("update board card: %w", err)
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow, err := loadBoardCardRow(ctx, tx, boardID, threadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card update transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardRow.toMap()}, nil
}

func (s *Store) MoveBoardCard(ctx context.Context, actorID, boardID, threadID string, input MoveBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("column_key is required")
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	if err := validateBoardPlacementAnchors(input.BeforeThreadID, input.AfterThreadID); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card move transaction: %w", err)
	}

	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow, err := loadBoardCardRow(ctx, tx, boardID, threadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := validateBoardAnchors(ctx, tx, boardID, columnKey, strings.TrimSpace(input.BeforeThreadID), strings.TrimSpace(input.AfterThreadID), threadID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, strings.TrimSpace(input.BeforeThreadID), strings.TrimSpace(input.AfterThreadID), threadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE board_cards
		    SET column_key = ?, rank = ?, updated_at = ?, updated_by = ?
		  WHERE board_id = ? AND thread_id = ?`,
		columnKey,
		rank,
		now,
		actorID,
		boardID,
		threadID,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("move board card: %w", err)
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow.ColumnKey = columnKey
	cardRow.Rank = rank
	cardRow.UpdatedAt = now
	cardRow.UpdatedBy = actorID

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card move transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardRow.toMap()}, nil
}

func (s *Store) RemoveBoardCard(ctx context.Context, actorID, boardID, threadID string, input RemoveBoardCardInput) (BoardCardRemovalResult, error) {
	if s == nil || s.db == nil {
		return BoardCardRemovalResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardRemovalResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardRemovalResult{}, fmt.Errorf("begin board card remove transaction: %w", err)
	}

	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, err
	}
	if _, err := loadBoardCardRow(ctx, tx, boardID, threadID); err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM board_cards WHERE board_id = ? AND thread_id = ?`, boardID, threadID); err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, fmt.Errorf("delete board card: %w", err)
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardRemovalResult{}, fmt.Errorf("commit board card remove transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardRemovalResult{}, err
	}
	return BoardCardRemovalResult{
		Board:           boardMap,
		RemovedThreadID: threadID,
	}, nil
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
		`SELECT b.id, b.title, b.status, bc.board_id, bc.thread_id, bc.column_key, bc.pinned_document_id
		   FROM board_cards bc
		   JOIN boards b ON b.id = bc.board_id
		  WHERE bc.thread_id = ?
		  ORDER BY b.updated_at DESC, b.id ASC`,
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
			status           string
			cardBoardID      string
			cardThreadID     string
			columnKey        string
			pinnedDocumentID sql.NullString
		)
		if err := rows.Scan(&boardID, &title, &status, &cardBoardID, &cardThreadID, &columnKey, &pinnedDocumentID); err != nil {
			return nil, fmt.Errorf("scan board membership: %w", err)
		}
		out = append(out, BoardMembership{
			Board: map[string]any{
				"id":     boardID,
				"title":  title,
				"status": status,
			},
			Card: map[string]any{
				"board_id":           cardBoardID,
				"thread_id":          cardThreadID,
				"column_key":         columnKey,
				"pinned_document_id": nullableBoardString(pinnedDocumentID.String),
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board memberships: %w", err)
	}
	return out, nil
}

func (s *Store) computeBoardSummaries(ctx context.Context, boards []boardRow) (map[string]map[string]any, error) {
	summaries := make(map[string]map[string]any, len(boards))
	if len(boards) == 0 {
		return summaries, nil
	}

	boardIDs := make([]string, 0, len(boards))
	primaryThreadIDs := make([]string, 0, len(boards))
	for _, board := range boards {
		boardIDs = append(boardIDs, board.ID)
		primaryThreadIDs = append(primaryThreadIDs, board.PrimaryThreadID)
	}

	cardsByBoard, err := s.loadBoardCardRowsByBoardIDs(ctx, boardIDs)
	if err != nil {
		return nil, err
	}

	allThreadIDs := append([]string{}, primaryThreadIDs...)
	for _, rows := range cardsByBoard {
		for _, row := range rows {
			allThreadIDs = append(allThreadIDs, row.ThreadID)
		}
	}
	projections, err := s.ListDerivedThreadProjections(ctx, uniqueNormalizedStrings(allThreadIDs))
	if err != nil {
		return nil, err
	}

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
		threadSet := map[string]struct{}{board.PrimaryThreadID: {}}
		for _, card := range cards {
			cardsByColumn[card.ColumnKey]++
			threadSet[card.ThreadID] = struct{}{}
		}

		openCommitmentCount := 0
		documentCount := 0
		latestActivityAt := board.UpdatedAt
		for threadID := range threadSet {
			projection, ok := projections[threadID]
			if !ok {
				continue
			}
			openCommitmentCount += projection.OpenCommitmentCount
			documentCount += projection.DocumentCount
			latestActivityAt = maxRFC3339Timestamp(latestActivityAt, projection.LastActivityAt)
		}

		summaries[board.ID] = map[string]any{
			"card_count":            len(cards),
			"cards_by_column":       cardsByColumn,
			"open_commitment_count": openCommitmentCount,
			"document_count":        documentCount,
			"latest_activity_at":    nullableBoardString(latestActivityAt),
			"has_primary_document":  strings.TrimSpace(board.PrimaryDocument.String) != "",
		}
	}

	return summaries, nil
}

func buildListBoardsQuery(filter BoardListFilter) (string, []any) {
	query := `SELECT id, title, status, labels_json, owners_json, primary_thread_id, primary_document_id, column_schema_json, pinned_refs_json, created_at, created_by, updated_at, updated_by
		FROM boards
		WHERE 1=1`
	args := make([]any, 0, 8)
	if status := strings.TrimSpace(filter.Status); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	labelFilters := uniqueNormalizedStrings(append([]string{filter.Label}, filter.Labels...))
	if len(labelFilters) > 0 {
		parts := make([]string, 0, len(labelFilters))
		for _, label := range labelFilters {
			parts = append(parts, `EXISTS (SELECT 1 FROM json_each(labels_json) WHERE value = ?)`)
			args = append(args, label)
		}
		query += ` AND (` + strings.Join(parts, ` OR `) + `)`
	}

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
		query += ` AND (LOWER(id) LIKE ? OR LOWER(title) LIKE ?)`
		args = append(args, searchPattern, searchPattern)
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

func (s *Store) loadBoardCardRowsByBoardIDs(ctx context.Context, boardIDs []string) (map[string][]boardCardRow, error) {
	boardIDs = uniqueNormalizedStrings(boardIDs)
	out := make(map[string][]boardCardRow, len(boardIDs))
	if len(boardIDs) == 0 {
		return out, nil
	}

	placeholders := make([]string, 0, len(boardIDs))
	args := make([]any, 0, len(boardIDs))
	for _, boardID := range boardIDs {
		placeholders = append(placeholders, "?")
		args = append(args, boardID)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT board_id, thread_id, column_key, rank, pinned_document_id, created_at, created_by, updated_at, updated_by
		   FROM board_cards
		  WHERE board_id IN (`+strings.Join(placeholders, ", ")+`)
		  ORDER BY `+boardColumnOrderSQL(`column_key`)+`, rank ASC, thread_id ASC`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query board cards by board ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		row, err := scanBoardCardRow(rows)
		if err != nil {
			return nil, err
		}
		out[row.BoardID] = append(out[row.BoardID], row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board cards by board ids: %w", err)
	}
	return out, nil
}

func (s *Store) loadOrderedBoardCards(ctx context.Context, q queryRower, boardID string, columnKey string) ([]boardCardRow, error) {
	db, ok := q.(interface {
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	})
	if !ok {
		return nil, fmt.Errorf("board card query does not support row iteration")
	}

	query := `SELECT board_id, thread_id, column_key, rank, pinned_document_id, created_at, created_by, updated_at, updated_by
		FROM board_cards
		WHERE board_id = ?`
	args := []any{boardID}
	if strings.TrimSpace(columnKey) != "" {
		query += ` AND column_key = ?`
		args = append(args, columnKey)
	}
	query += ` ORDER BY ` + boardColumnOrderSQL(`column_key`) + `, rank ASC, thread_id ASC`

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

func (s *Store) allocateBoardCardRank(ctx context.Context, tx *sql.Tx, boardID, columnKey, beforeThreadID, afterThreadID, excludeThreadID string) (string, error) {
	cards, err := s.loadOrderedBoardCards(ctx, tx, boardID, columnKey)
	if err != nil {
		return "", err
	}
	filtered := make([]boardCardRow, 0, len(cards))
	for _, card := range cards {
		if card.ThreadID == excludeThreadID {
			continue
		}
		filtered = append(filtered, card)
	}

	insertIndex, err := boardInsertIndex(filtered, beforeThreadID, afterThreadID)
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
	if err := rebalanceBoardColumnRanks(ctx, tx, boardID, columnKey, excludeThreadID); err != nil {
		return "", err
	}
	cards, err = s.loadOrderedBoardCards(ctx, tx, boardID, columnKey)
	if err != nil {
		return "", err
	}
	filtered = filtered[:0]
	for _, card := range cards {
		if card.ThreadID == excludeThreadID {
			continue
		}
		filtered = append(filtered, card)
	}
	insertIndex, err = boardInsertIndex(filtered, beforeThreadID, afterThreadID)
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

func boardInsertIndex(cards []boardCardRow, beforeThreadID, afterThreadID string) (int, error) {
	beforeThreadID = strings.TrimSpace(beforeThreadID)
	afterThreadID = strings.TrimSpace(afterThreadID)
	if beforeThreadID == "" && afterThreadID == "" {
		return len(cards), nil
	}
	if beforeThreadID != "" && afterThreadID != "" {
		return 0, invalidBoardRequest("before_thread_id and after_thread_id are mutually exclusive")
	}
	for i, card := range cards {
		if beforeThreadID != "" && card.ThreadID == beforeThreadID {
			return i, nil
		}
		if afterThreadID != "" && card.ThreadID == afterThreadID {
			return i + 1, nil
		}
	}
	if beforeThreadID != "" {
		return 0, invalidBoardRequest("before_thread_id must reference a card already on the board")
	}
	return 0, invalidBoardRequest("after_thread_id must reference a card already on the board")
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

func rebalanceBoardColumnRanks(ctx context.Context, tx *sql.Tx, boardID, columnKey, excludeThreadID string) error {
	rows, err := loadBoardCardsForColumn(ctx, tx, boardID, columnKey)
	if err != nil {
		return err
	}
	nextRankValue := boardRankStep
	for _, row := range rows {
		if row.ThreadID == excludeThreadID {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE board_cards SET rank = ? WHERE board_id = ? AND thread_id = ?`,
			formatBoardRank(nextRankValue),
			boardID,
			row.ThreadID,
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
		`SELECT board_id, thread_id, column_key, rank, pinned_document_id, created_at, created_by, updated_at, updated_by
		   FROM board_cards
		  WHERE board_id = ? AND column_key = ?
		  ORDER BY rank ASC, thread_id ASC`,
		boardID,
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

func validateBoardAnchors(ctx context.Context, tx *sql.Tx, boardID, targetColumn, beforeThreadID, afterThreadID, movingThreadID string) error {
	anchorThreadID := beforeThreadID
	if anchorThreadID == "" {
		anchorThreadID = afterThreadID
	}
	anchorThreadID = strings.TrimSpace(anchorThreadID)
	if anchorThreadID == "" {
		return nil
	}
	if anchorThreadID == strings.TrimSpace(movingThreadID) {
		return invalidBoardRequest("placement anchor cannot reference the moving card")
	}
	anchor, err := loadBoardCardRow(ctx, tx, boardID, anchorThreadID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return invalidBoardRequest("placement anchor must reference a card already on the board")
		}
		return err
	}
	if anchor.ColumnKey != targetColumn {
		return invalidBoardRequest("placement anchor must reference a card in the target column")
	}
	return nil
}

func ensureBoardUpdatedAtMatches(board boardRow, ifUpdatedAt *string) error {
	if ifUpdatedAt == nil {
		return nil
	}
	if strings.TrimSpace(*ifUpdatedAt) != board.UpdatedAt {
		return ErrConflict
	}
	return nil
}

func touchBoardRow(ctx context.Context, tx *sql.Tx, board boardRow, actorID string) (boardRow, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE boards SET updated_at = ?, updated_by = ? WHERE id = ?`,
		now,
		actorID,
		board.ID,
	); err != nil {
		return boardRow{}, fmt.Errorf("touch board row: %w", err)
	}
	board.UpdatedAt = now
	board.UpdatedBy = actorID
	return board, nil
}

func ensureThreadExists(ctx context.Context, rower queryRower, threadID string) error {
	snapshot, err := getSnapshotRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if snapshot.Kind != "thread" {
		return ErrNotFound
	}
	return nil
}

func ensureDocumentExists(ctx context.Context, rower queryRower, documentID string) error {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil
	}

	var found string
	err := rower.QueryRowContext(
		ctx,
		`SELECT id FROM documents WHERE id = ? AND tombstoned_at IS NULL`,
		documentID,
	).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query document row: %w", err)
	}
	return nil
}

func (s *Store) getBoardRow(ctx context.Context, boardID string) (boardRow, error) {
	if s == nil || s.db == nil {
		return boardRow{}, fmt.Errorf("primitives store database is not initialized")
	}
	return loadBoardRow(ctx, s.db, boardID)
}

func loadBoardRow(ctx context.Context, rower queryRower, boardID string) (boardRow, error) {
	row := boardRow{}
	err := rower.QueryRowContext(
		ctx,
		`SELECT id, title, status, labels_json, owners_json, primary_thread_id, primary_document_id, column_schema_json, pinned_refs_json, created_at, created_by, updated_at, updated_by
		   FROM boards
		  WHERE id = ?`,
		strings.TrimSpace(boardID),
	).Scan(
		&row.ID,
		&row.Title,
		&row.Status,
		&row.LabelsJSON,
		&row.OwnersJSON,
		&row.PrimaryThreadID,
		&row.PrimaryDocument,
		&row.ColumnSchemaJSON,
		&row.PinnedRefsJSON,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return boardRow{}, ErrNotFound
	}
	if err != nil {
		return boardRow{}, fmt.Errorf("query board row: %w", err)
	}
	return row, nil
}

func scanBoardRow(scanner interface{ Scan(dest ...any) error }) (boardRow, error) {
	row := boardRow{}
	if err := scanner.Scan(
		&row.ID,
		&row.Title,
		&row.Status,
		&row.LabelsJSON,
		&row.OwnersJSON,
		&row.PrimaryThreadID,
		&row.PrimaryDocument,
		&row.ColumnSchemaJSON,
		&row.PinnedRefsJSON,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
	); err != nil {
		return boardRow{}, fmt.Errorf("scan board row: %w", err)
	}
	return row, nil
}

func loadBoardCardRow(ctx context.Context, rower queryRower, boardID, threadID string) (boardCardRow, error) {
	row := boardCardRow{}
	err := rower.QueryRowContext(
		ctx,
		`SELECT board_id, thread_id, column_key, rank, pinned_document_id, created_at, created_by, updated_at, updated_by
		   FROM board_cards
		  WHERE board_id = ? AND thread_id = ?`,
		boardID,
		threadID,
	).Scan(
		&row.BoardID,
		&row.ThreadID,
		&row.ColumnKey,
		&row.Rank,
		&row.PinnedDocumentID,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return boardCardRow{}, ErrNotFound
	}
	if err != nil {
		return boardCardRow{}, fmt.Errorf("query board card row: %w", err)
	}
	return row, nil
}

func scanBoardCardRow(scanner interface{ Scan(dest ...any) error }) (boardCardRow, error) {
	row := boardCardRow{}
	if err := scanner.Scan(
		&row.BoardID,
		&row.ThreadID,
		&row.ColumnKey,
		&row.Rank,
		&row.PinnedDocumentID,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
	); err != nil {
		return boardCardRow{}, fmt.Errorf("scan board card row: %w", err)
	}
	return row, nil
}

func (r boardRow) toMap() (map[string]any, error) {
	columnSchema, err := decodeBoardColumnSchema(r.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":                  r.ID,
		"title":               r.Title,
		"status":              r.Status,
		"labels":              decodeJSONListOrEmpty(r.LabelsJSON),
		"owners":              decodeJSONListOrEmpty(r.OwnersJSON),
		"primary_thread_id":   r.PrimaryThreadID,
		"primary_document_id": nullableBoardString(r.PrimaryDocument.String),
		"column_schema":       columnSchema,
		"pinned_refs":         decodeJSONListOrEmpty(r.PinnedRefsJSON),
		"created_at":          r.CreatedAt,
		"created_by":          r.CreatedBy,
		"updated_at":          r.UpdatedAt,
		"updated_by":          r.UpdatedBy,
	}, nil
}

func (r boardCardRow) toMap() map[string]any {
	return map[string]any{
		"board_id":           r.BoardID,
		"thread_id":          r.ThreadID,
		"column_key":         r.ColumnKey,
		"rank":               r.Rank,
		"pinned_document_id": nullableBoardString(r.PinnedDocumentID.String),
		"created_at":         r.CreatedAt,
		"created_by":         r.CreatedBy,
		"updated_at":         r.UpdatedAt,
		"updated_by":         r.UpdatedBy,
	}
}

func normalizeBoardStatus(raw any, allowDefault bool) (string, error) {
	status := strings.TrimSpace(anyStringValue(raw))
	if status == "" && allowDefault {
		return boardDefaultStatus, nil
	}
	switch status {
	case "active", "paused", "closed":
		return status, nil
	default:
		return "", fmt.Errorf("board.status must be one of: active, paused, closed")
	}
}

func normalizeBoardColumnSchema(raw any, allowDefault bool) ([]map[string]any, error) {
	if raw == nil {
		if allowDefault {
			return defaultBoardColumnSchema(), nil
		}
		return nil, fmt.Errorf("board.column_schema is required")
	}

	items, ok := raw.([]any)
	if !ok {
		switch typed := raw.(type) {
		case []map[string]any:
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		default:
			return nil, fmt.Errorf("board.column_schema must be a list of objects")
		}
	}
	if len(items) != len(canonicalBoardColumnOrder) {
		return nil, fmt.Errorf("board.column_schema must contain the six canonical columns in order")
	}

	out := make([]map[string]any, 0, len(items))
	for i, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("board.column_schema must contain only objects")
		}
		expectedKey := canonicalBoardColumnOrder[i]
		key := strings.TrimSpace(anyStringValue(item["key"]))
		if key != expectedKey {
			return nil, fmt.Errorf("board.column_schema must preserve canonical key order")
		}
		title := strings.TrimSpace(anyStringValue(item["title"]))
		if title == "" {
			return nil, fmt.Errorf("board.column_schema[%d].title is required", i)
		}

		var wipLimit any
		if rawWIP, exists := item["wip_limit"]; exists && rawWIP != nil {
			limit, err := normalizeBoardWIPLimit(rawWIP)
			if err != nil {
				return nil, fmt.Errorf("board.column_schema[%d].wip_limit: %w", i, err)
			}
			wipLimit = limit
		} else {
			wipLimit = nil
		}

		out = append(out, map[string]any{
			"key":       key,
			"title":     title,
			"wip_limit": wipLimit,
		})
	}

	return out, nil
}

func normalizeBoardWIPLimit(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		if value < 0 {
			return 0, fmt.Errorf("must be non-negative")
		}
		return value, nil
	case int64:
		if value < 0 {
			return 0, fmt.Errorf("must be non-negative")
		}
		return int(value), nil
	case float64:
		if value < 0 || value != math.Trunc(value) {
			return 0, fmt.Errorf("must be a non-negative integer")
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

func defaultBoardColumnSchema() []map[string]any {
	out := make([]map[string]any, 0, len(canonicalBoardColumnOrder))
	for _, key := range canonicalBoardColumnOrder {
		out = append(out, map[string]any{
			"key":       key,
			"title":     canonicalBoardColumnTitles[key],
			"wip_limit": nil,
		})
	}
	return out
}

func decodeBoardColumnSchema(raw string) ([]map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultBoardColumnSchema(), nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode board column schema: %w", err)
	}
	if len(items) == 0 {
		return defaultBoardColumnSchema(), nil
	}
	return items, nil
}

func validateBoardColumnKey(columnKey string) error {
	columnKey = strings.TrimSpace(columnKey)
	for _, allowed := range canonicalBoardColumnOrder {
		if columnKey == allowed {
			return nil
		}
	}
	return fmt.Errorf("column_key must be one of: %s", strings.Join(canonicalBoardColumnOrder, ", "))
}

func validateBoardID(boardID string) error {
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return fmt.Errorf("board.id is required")
	}
	if strings.Contains(boardID, "/") {
		return fmt.Errorf("board.id contains invalid path characters")
	}
	return nil
}

func validateBoardPlacementAnchors(beforeThreadID, afterThreadID string) error {
	beforeThreadID = strings.TrimSpace(beforeThreadID)
	afterThreadID = strings.TrimSpace(afterThreadID)
	if beforeThreadID != "" && afterThreadID != "" {
		return fmt.Errorf("before_thread_id and after_thread_id are mutually exclusive")
	}
	return nil
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

func boardColumnOrderSQL(columnExpr string) string {
	return `CASE ` + columnExpr + `
		WHEN 'backlog' THEN 0
		WHEN 'ready' THEN 1
		WHEN 'in_progress' THEN 2
		WHEN 'blocked' THEN 3
		WHEN 'review' THEN 4
		WHEN 'done' THEN 5
		ELSE 6
	END`
}

func nullableBoardString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func normalizeNullableString(raw any) *string {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(anyStringValue(raw))
	if value == "" {
		return nil
	}
	return &value
}

func normalizeBoardOptionalPointer(raw *string) *string {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil
	}
	return &value
}

func derefBoardString(raw *string) string {
	if raw == nil {
		return ""
	}
	return strings.TrimSpace(*raw)
}

func normalizeOptionalStringList(values map[string]any, key string) ([]string, error) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return []string{}, nil
	}
	parsed, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, fmt.Errorf("board.%s must be a list of strings", key)
	}
	return uniqueNormalizedStrings(parsed), nil
}

func invalidBoardRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidBoardRequest, strings.TrimSpace(message))
}

func invalidBoardRequestError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidBoardRequest, strings.TrimSpace(err.Error()))
}
