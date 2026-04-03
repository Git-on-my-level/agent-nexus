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
	Status            string
	Label             string
	Labels            []string
	Owner             string
	Owners            []string
	Query             string
	Limit             *int
	Cursor            string
	IncludeArchived   bool
	ArchivedOnly      bool
	IncludeTombstoned bool
	TombstonedOnly    bool
}

type BoardListItem struct {
	Board   map[string]any
	Summary map[string]any
}

type AddBoardCardInput struct {
	CardID           string
	Title            string
	Body             string
	ParentThreadID   string
	Assignee         *string
	Priority         *string
	Status           string
	ThreadID         string
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
	BeforeThreadID   string
	AfterThreadID    string
	PinnedDocumentID *string
	IfBoardUpdatedAt *string
}

type UpdateBoardCardInput struct {
	Title            *string
	Body             *string
	ParentThreadID   *string
	Assignee         *string
	Priority         *string
	Status           *string
	PinnedDocumentID *string
	IfBoardUpdatedAt *string
}

type MoveBoardCardInput struct {
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
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
	Card            map[string]any
	RemovedCardID   string
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
	ArchivedAt       sql.NullString
	ArchivedBy       sql.NullString
	TombstonedAt     sql.NullString
	TombstonedBy     sql.NullString
	TombstoneReason  sql.NullString
}

type boardCardRow struct {
	BoardID          string
	CardID           string
	ColumnKey        string
	Rank             string
	Title            string
	Body             string
	Version          int
	ParentThreadID   sql.NullString
	PinnedDocumentID sql.NullString
	Assignee         sql.NullString
	Priority         sql.NullString
	Status           string
	CreatedAt        string
	CreatedBy        string
	UpdatedAt        string
	UpdatedBy        string
	ProvenanceJSON   string
	ArchivedAt       sql.NullString
	ArchivedBy       sql.NullString
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
	boardTargets := appendRefEdgeTarget(nil, refEdgeTypeBoardPrimaryThread, "thread", primaryThreadID)
	boardTargets = appendRefEdgeTarget(boardTargets, refEdgeTypeBoardPrimaryDocument, "document", primaryDocumentID)
	boardTargets = append(boardTargets, typedRefEdgeTargets(refEdgeTypeBoardPinnedRef, pinnedRefs)...)
	if err := replaceRefEdges(ctx, tx, "board", boardID, boardTargets); err != nil {
		_ = tx.Rollback()
		return nil, err
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

func (s *Store) ArchiveBoard(ctx context.Context, actorID, boardID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return nil, invalidBoardRequest("board_id is required")
	}
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if row.TombstonedAt.Valid && strings.TrimSpace(row.TombstonedAt.String) != "" {
		return nil, ErrAlreadyTombstoned
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return row.toMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET archived_at = ?, archived_by = ? WHERE id = ?`,
		now, strings.TrimSpace(actorID), boardID,
	); err != nil {
		return nil, fmt.Errorf("archive board: %w", err)
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) UnarchiveBoard(ctx context.Context, actorID, boardID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return nil, invalidBoardRequest("board_id is required")
	}
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if !row.ArchivedAt.Valid || strings.TrimSpace(row.ArchivedAt.String) == "" {
		return nil, ErrNotArchived
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		boardID,
	); err != nil {
		return nil, fmt.Errorf("unarchive board: %w", err)
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) TombstoneBoard(ctx context.Context, actorID, boardID, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return nil, invalidBoardRequest("board_id is required")
	}
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if row.TombstonedAt.Valid && strings.TrimSpace(row.TombstonedAt.String) != "" {
		return row.toMap()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET tombstoned_at = ?, tombstoned_by = ?, tombstone_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, strings.TrimSpace(actorID), strings.TrimSpace(reason), boardID,
	); err != nil {
		return nil, fmt.Errorf("tombstone board: %w", err)
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) RestoreBoard(ctx context.Context, actorID, boardID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return nil, invalidBoardRequest("board_id is required")
	}
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	if !row.TombstonedAt.Valid || strings.TrimSpace(row.TombstonedAt.String) == "" {
		return nil, ErrNotTombstoned
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET tombstoned_at = NULL, tombstoned_by = NULL, tombstone_reason = NULL WHERE id = ?`,
		boardID,
	); err != nil {
		return nil, fmt.Errorf("restore board: %w", err)
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) PurgeBoard(ctx context.Context, boardID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return invalidBoardRequest("board_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge board transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var foundID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM boards WHERE id = ? AND tombstoned_at IS NOT NULL`,
		boardID,
	).Scan(&foundID)
	if errors.Is(err, sql.ErrNoRows) {
		var one int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM boards WHERE id = ?`, boardID).Scan(&one)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check board existence: %w", err2)
		}
		return ErrNotTombstoned
	}
	if err != nil {
		return fmt.Errorf("select tombstoned board: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM board_cards WHERE board_id = ?`, boardID); err != nil {
		return fmt.Errorf("delete board cards: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM boards WHERE id = ?`, boardID); err != nil {
		return fmt.Errorf("delete board: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge board transaction: %w", err)
	}
	return nil
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
	query, args = appendIfUpdatedAtClause(query, args, ifUpdatedAt)

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("update board: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(result, ifUpdatedAt, "board update"); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	boardTargets := appendRefEdgeTarget(nil, refEdgeTypeBoardPrimaryThread, "thread", currentRow.PrimaryThreadID)
	boardTargets = appendRefEdgeTarget(boardTargets, refEdgeTypeBoardPrimaryDocument, "document", derefBoardString(nextPrimaryDocumentID))
	boardTargets = append(boardTargets, typedRefEdgeTargets(refEdgeTypeBoardPinnedRef, nextPinnedRefs)...)
	if err := replaceRefEdges(ctx, tx, "board", boardID, boardTargets); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit board update transaction: %w", err)
	}

	out := map[string]any{
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
	}
	mergeBoardArchiveTombstoneFields(out, currentRow)
	return out, nil
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
		card, mapErr := row.toMap()
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, card)
	}
	return out, nil
}

func (s *Store) GetBoardCard(ctx context.Context, boardID, identifier string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	var (
		row boardCardRow
		err error
	)
	if strings.TrimSpace(boardID) != "" {
		row, err = s.loadBoardCardByIdentifier(ctx, s.db, boardID, identifier, true)
	} else {
		row, err = s.loadBoardCardByGlobalID(ctx, s.db, identifier, true)
	}
	if err != nil {
		return nil, err
	}
	card, err := row.toMap()
	if err != nil {
		return nil, err
	}
	history, err := s.ListBoardCardHistory(ctx, row.CardID)
	if err != nil {
		return nil, err
	}
	card["history"] = history
	return card, nil
}

func (s *Store) CreateBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	cardID := strings.TrimSpace(input.CardID)
	if cardID == "" {
		cardID = uuid.NewString()
	}
	if err := validateCardID(cardID); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}

	parentThreadID := strings.TrimSpace(firstNonEmpty(input.ParentThreadID, input.ThreadID))
	if parentThreadID != "" {
		if err := validateThreadID(parentThreadID); err != nil {
			return BoardCardMutationResult{}, invalidBoardRequestError(err)
		}
	}

	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		columnKey = boardDefaultColumn
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID, input.BeforeThreadID, input.AfterThreadID); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	if err := validateBoardCardStatus(input.Status, true); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}

	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	status := normalizeBoardCardStatus(input.Status)
	assignee := normalizeBoardOptionalPointer(input.Assignee)
	priority := normalizeBoardOptionalPointer(input.Priority)
	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card create transaction: %w", err)
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
	if parentThreadID != "" {
		if err := ensureThreadExists(ctx, tx, parentThreadID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if parentThreadID == boardRow.PrimaryThreadID {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("board.primary_thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, parentThreadID, ""); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if title == "" && parentThreadID != "" {
		title, err = loadThreadTitleForBoardCard(ctx, tx, parentThreadID)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if title == "" {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, invalidBoardRequest("card.title is required")
	}
	if pinnedDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *pinnedDocumentID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}

	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, input.BeforeCardID, input.AfterCardID, input.BeforeThreadID, input.AfterThreadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, "")
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	provenanceJSON := inferredProvenanceJSON()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO cards(
			id, title, body_markdown, version, parent_thread_id, pinned_document_id, assignee, priority, status,
			created_at, created_by, updated_at, updated_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		title,
		body,
		1,
		nullableString(parentThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		nullableString(derefBoardString(priority)),
		status,
		now,
		actorID,
		now,
		actorID,
		provenanceJSON,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return BoardCardMutationResult{}, ErrConflict
		}
		return BoardCardMutationResult{}, fmt.Errorf("insert card: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO card_versions(
			card_id, version, title, body_markdown, parent_thread_id, pinned_document_id, assignee, priority, status,
			created_at, created_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		1,
		title,
		body,
		nullableString(parentThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		nullableString(derefBoardString(priority)),
		status,
		now,
		actorID,
		provenanceJSON,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("insert card version: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO board_cards(
			board_id, card_id, column_key, rank, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		cardID,
		columnKey,
		rank,
		now,
		actorID,
		now,
		actorID,
	); err != nil {
		_ = tx.Rollback()
		if isUniqueViolation(err) {
			return BoardCardMutationResult{}, ErrConflict
		}
		return BoardCardMutationResult{}, fmt.Errorf("insert board card membership: %w", err)
	}
	cardTargets := appendRefEdgeTarget(nil, refEdgeTypeCardParentThread, "thread", parentThreadID)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", derefBoardString(pinnedDocumentID))
	if err := replaceRefEdges(ctx, tx, "card", cardID, cardTargets); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow, err := s.loadBoardCardByIdentifier(ctx, tx, boardID, cardID, true)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card create transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) AddBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("thread_id is required")
	}
	input.ParentThreadID = threadID
	if strings.TrimSpace(input.Status) == "" {
		input.Status = inferLegacyBoardCardStatus(input.ColumnKey)
	}
	return s.CreateBoardCard(ctx, actorID, boardID, input)
}

func (s *Store) UpdateBoardCard(ctx context.Context, actorID, boardID, identifier string, input UpdateBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card update transaction: %w", err)
	}

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	boardID = cardRow.BoardID
	if err := ensureBoardCardMutable(cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
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

	nextTitle := cardRow.Title
	if input.Title != nil {
		nextTitle = strings.TrimSpace(*input.Title)
		if nextTitle == "" {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("card.title must not be empty")
		}
	}
	nextBody := cardRow.Body
	if input.Body != nil {
		nextBody = strings.TrimSpace(*input.Body)
	}
	nextParentThread := strings.TrimSpace(cardRow.ParentThreadID.String)
	if input.ParentThreadID != nil {
		nextParentThread = strings.TrimSpace(*input.ParentThreadID)
	}
	nextAssignee := strings.TrimSpace(cardRow.Assignee.String)
	if input.Assignee != nil {
		nextAssignee = strings.TrimSpace(*input.Assignee)
	}
	nextPriority := strings.TrimSpace(cardRow.Priority.String)
	if input.Priority != nil {
		nextPriority = strings.TrimSpace(*input.Priority)
	}
	nextStatus := cardRow.Status
	if input.Status != nil {
		if err := validateBoardCardStatus(*input.Status, false); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequestError(err)
		}
		nextStatus = normalizeBoardCardStatus(*input.Status)
	}
	nextPinnedDocumentID := strings.TrimSpace(cardRow.PinnedDocumentID.String)
	if input.PinnedDocumentID != nil {
		nextPinnedDocumentID = strings.TrimSpace(*input.PinnedDocumentID)
	}

	if nextParentThread != "" {
		if err := ensureThreadExists(ctx, tx, nextParentThread); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if nextParentThread == boardRow.PrimaryThreadID {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("board.primary_thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, nextParentThread, cardRow.CardID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if nextPinnedDocumentID != "" {
		if err := ensureDocumentExists(ctx, tx, nextPinnedDocumentID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if nextTitle == cardRow.Title &&
		nextBody == cardRow.Body &&
		nextParentThread == strings.TrimSpace(cardRow.ParentThreadID.String) &&
		nextAssignee == strings.TrimSpace(cardRow.Assignee.String) &&
		nextPriority == strings.TrimSpace(cardRow.Priority.String) &&
		nextStatus == cardRow.Status &&
		nextPinnedDocumentID == strings.TrimSpace(cardRow.PinnedDocumentID.String) {
		boardMap, mapErr := boardRow.toMap()
		if mapErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, mapErr
		}
		_ = tx.Rollback()
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	nextVersion := cardRow.Version + 1
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO card_versions(
			card_id, version, title, body_markdown, parent_thread_id, pinned_document_id, assignee, priority, status,
			created_at, created_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardRow.CardID,
		nextVersion,
		nextTitle,
		nextBody,
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nullableString(nextPriority),
		nextStatus,
		now,
		actorID,
		cardRow.ProvenanceJSON,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("insert board card version: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cards
		    SET title = ?, body_markdown = ?, version = ?, parent_thread_id = ?, pinned_document_id = ?, assignee = ?, priority = ?, status = ?,
		        updated_at = ?, updated_by = ?
		  WHERE id = ?`,
		nextTitle,
		nextBody,
		nextVersion,
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nullableString(nextPriority),
		nextStatus,
		now,
		actorID,
		cardRow.CardID,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("update board card: %w", err)
	}
	cardTargets := appendRefEdgeTarget(nil, refEdgeTypeCardParentThread, "thread", nextParentThread)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", nextPinnedDocumentID)
	if err := replaceRefEdges(ctx, tx, "card", cardRow.CardID, cardTargets); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
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
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) MoveBoardCard(ctx context.Context, actorID, boardID, identifier string, input MoveBoardCardInput) (BoardCardMutationResult, error) {
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
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID, input.BeforeThreadID, input.AfterThreadID); err != nil {
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
	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardCardMutable(cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, input.BeforeCardID, input.AfterCardID, input.BeforeThreadID, input.AfterThreadID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := validateBoardAnchors(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE board_cards
		    SET column_key = ?, rank = ?, updated_at = ?, updated_by = ?
		  WHERE board_id = ? AND card_id = ?`,
		columnKey,
		rank,
		now,
		actorID,
		boardID,
		cardRow.CardID,
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
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) RemoveBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (BoardCardRemovalResult, error) {
	card, err := s.ArchiveBoardCard(ctx, actorID, boardID, identifier, input)
	if err != nil {
		return BoardCardRemovalResult{}, err
	}
	return BoardCardRemovalResult{
		Board:           card.Board,
		Card:            card.Card,
		RemovedCardID:   strings.TrimSpace(anyStringValue(card.Card["id"])),
		RemovedThreadID: strings.TrimSpace(anyStringValue(card.Card["parent_thread"])),
	}, nil
}

func (s *Store) ArchiveBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card archive transaction: %w", err)
	}

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	boardID = cardRow.BoardID
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != "" {
		boardMap, mapErr := boardRow.toMap()
		if mapErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, mapErr
		}
		_ = tx.Rollback()
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET archived_at = ?, archived_by = ? WHERE id = ?`, now, actorID, cardRow.CardID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("archive board card: %w", err)
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardRow.ArchivedAt = sql.NullString{String: now, Valid: true}
	cardRow.ArchivedBy = sql.NullString{String: actorID, Valid: true}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card archive transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT card_id, version, title, body_markdown, parent_thread_id, pinned_document_id, assignee, priority, status, created_at, created_by, provenance_json
		   FROM card_versions
		  WHERE card_id = ?
		  ORDER BY version ASC`,
		strings.TrimSpace(cardID),
	)
	if err != nil {
		return nil, fmt.Errorf("query board card history: %w", err)
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		versionRow, scanErr := scanBoardCardVersionRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, versionRow.toMap())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board card history: %w", err)
	}
	return out, nil
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
		`SELECT b.id, b.title, b.status, bc.board_id, bc.card_id, bc.column_key, c.title, c.status, c.parent_thread_id, c.pinned_document_id
		   FROM board_cards bc
		   JOIN boards b ON b.id = bc.board_id
		   JOIN cards c ON c.id = bc.card_id
		  WHERE c.parent_thread_id = ? AND c.archived_at IS NULL
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
			cardID           string
			columnKey        string
			cardTitle        string
			cardStatus       string
			parentThreadID   sql.NullString
			pinnedDocumentID sql.NullString
		)
		if err := rows.Scan(&boardID, &title, &status, &cardBoardID, &cardID, &columnKey, &cardTitle, &cardStatus, &parentThreadID, &pinnedDocumentID); err != nil {
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
				"id":                 cardID,
				"thread_id":          nullableBoardString(parentThreadID.String),
				"title":              cardTitle,
				"status":             cardStatus,
				"column_key":         columnKey,
				"parent_thread":      nullableBoardString(parentThreadID.String),
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
			if threadID := strings.TrimSpace(row.ParentThreadID.String); threadID != "" {
				allThreadIDs = append(allThreadIDs, threadID)
			}
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
			if threadID := strings.TrimSpace(card.ParentThreadID.String); threadID != "" {
				threadSet[threadID] = struct{}{}
			}
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
	query := `SELECT id, title, status, labels_json, owners_json, primary_thread_id, primary_document_id, column_schema_json, pinned_refs_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		FROM boards
		WHERE 1=1`
	args := make([]any, 0, 8)
	if filter.TombstonedOnly {
		query += ` AND tombstoned_at IS NOT NULL`
	} else if !filter.IncludeTombstoned {
		query += ` AND tombstoned_at IS NULL`
	}
	if filter.ArchivedOnly {
		query += ` AND archived_at IS NOT NULL AND tombstoned_at IS NULL`
	} else if !filter.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
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
		`SELECT bc.board_id, bc.card_id, bc.column_key, bc.rank,
		        c.title, c.body_markdown, c.version, c.parent_thread_id, c.pinned_document_id, c.assignee, c.priority, c.status,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		   FROM board_cards bc
		   JOIN cards c ON c.id = bc.card_id
		  WHERE bc.board_id IN (`+strings.Join(placeholders, ", ")+`) AND c.archived_at IS NULL
		  ORDER BY `+boardColumnOrderSQL(`bc.column_key`)+`, bc.rank ASC, bc.card_id ASC`,
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

	query := `SELECT bc.board_id, bc.card_id, bc.column_key, bc.rank,
			c.title, c.body_markdown, c.version, c.parent_thread_id, c.pinned_document_id, c.assignee, c.priority, c.status,
			c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		FROM board_cards bc
		JOIN cards c ON c.id = bc.card_id
		WHERE bc.board_id = ? AND c.archived_at IS NULL`
	args := []any{boardID}
	if strings.TrimSpace(columnKey) != "" {
		query += ` AND bc.column_key = ?`
		args = append(args, columnKey)
	}
	query += ` ORDER BY ` + boardColumnOrderSQL(`bc.column_key`) + `, bc.rank ASC, bc.card_id ASC`

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

func (s *Store) allocateBoardCardRank(ctx context.Context, tx *sql.Tx, boardID, columnKey, beforeCardID, afterCardID, excludeCardID string) (string, error) {
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

func rebalanceBoardColumnRanks(ctx context.Context, tx *sql.Tx, boardID, columnKey, excludeCardID string) error {
	rows, err := loadBoardCardsForColumn(ctx, tx, boardID, columnKey)
	if err != nil {
		return err
	}
	nextRankValue := boardRankStep
	for _, row := range rows {
		if row.CardID == excludeCardID {
			continue
		}
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE board_cards SET rank = ? WHERE board_id = ? AND card_id = ?`,
			formatBoardRank(nextRankValue),
			boardID,
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
		`SELECT bc.board_id, bc.card_id, bc.column_key, bc.rank,
		        c.title, c.body_markdown, c.version, c.parent_thread_id, c.pinned_document_id, c.assignee, c.priority, c.status,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		   FROM board_cards bc
		   JOIN cards c ON c.id = bc.card_id
		  WHERE bc.board_id = ? AND bc.column_key = ? AND c.archived_at IS NULL
		  ORDER BY bc.rank ASC, bc.card_id ASC`,
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

func validateBoardAnchors(ctx context.Context, tx *sql.Tx, boardID, targetColumn, beforeCardID, afterCardID, movingCardID string) error {
	anchorCardID := beforeCardID
	if anchorCardID == "" {
		anchorCardID = afterCardID
	}
	anchorCardID = strings.TrimSpace(anchorCardID)
	if anchorCardID == "" {
		return nil
	}
	if anchorCardID == strings.TrimSpace(movingCardID) {
		return invalidBoardRequest("placement anchor cannot reference the moving card")
	}
	anchor, err := loadBoardCardRow(ctx, tx, boardID, anchorCardID)
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
	return ensureUpdatedAtMatches(board.UpdatedAt, ifUpdatedAt)
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
	snapshot, err := getSnapshotRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
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
		`SELECT id, title, status, labels_json, owners_json, primary_thread_id, primary_document_id, column_schema_json, pinned_refs_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
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
		&row.ArchivedAt,
		&row.ArchivedBy,
		&row.TombstonedAt,
		&row.TombstonedBy,
		&row.TombstoneReason,
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
		&row.ArchivedAt,
		&row.ArchivedBy,
		&row.TombstonedAt,
		&row.TombstonedBy,
		&row.TombstoneReason,
	); err != nil {
		return boardRow{}, fmt.Errorf("scan board row: %w", err)
	}
	return row, nil
}

func loadBoardCardRow(ctx context.Context, rower queryRower, boardID, identifier string) (boardCardRow, error) {
	return (&Store{}).loadBoardCardByIdentifier(ctx, rower, boardID, identifier, true)
}

func (s *Store) loadBoardCardByIdentifier(ctx context.Context, rower queryRower, boardID, identifier string, includeArchived bool) (boardCardRow, error) {
	identifier = strings.TrimSpace(identifier)
	boardID = strings.TrimSpace(boardID)
	if identifier == "" || boardID == "" {
		return boardCardRow{}, ErrNotFound
	}

	baseQuery := `SELECT bc.board_id, bc.card_id, bc.column_key, bc.rank,
			c.title, c.body_markdown, c.version, c.parent_thread_id, c.pinned_document_id, c.assignee, c.priority, c.status,
			c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		FROM board_cards bc
		JOIN cards c ON c.id = bc.card_id
		WHERE `
	archivedClause := ``
	if !includeArchived {
		archivedClause = ` AND c.archived_at IS NULL`
	}

	row, err := scanBoardCardRow(rower.QueryRowContext(ctx, baseQuery+`bc.board_id = ? AND bc.card_id = ?`+archivedClause, boardID, identifier))
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return boardCardRow{}, err
	}

	row, err = scanBoardCardRow(rower.QueryRowContext(
		ctx,
		baseQuery+`bc.board_id = ? AND c.parent_thread_id = ?`+archivedClause+` ORDER BY c.updated_at DESC, c.id ASC LIMIT 1`,
		boardID,
		identifier,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return boardCardRow{}, ErrNotFound
	}
	if err != nil {
		return boardCardRow{}, err
	}
	return row, nil
}

func (s *Store) loadBoardCardByGlobalID(ctx context.Context, rower queryRower, cardID string, includeArchived bool) (boardCardRow, error) {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return boardCardRow{}, ErrNotFound
	}
	query := `SELECT bc.board_id, bc.card_id, bc.column_key, bc.rank,
			c.title, c.body_markdown, c.version, c.parent_thread_id, c.pinned_document_id, c.assignee, c.priority, c.status,
			c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		FROM board_cards bc
		JOIN cards c ON c.id = bc.card_id
		WHERE c.id = ?`
	if !includeArchived {
		query += ` AND c.archived_at IS NULL`
	}
	row, err := scanBoardCardRow(rower.QueryRowContext(ctx, query, cardID))
	if errors.Is(err, sql.ErrNoRows) {
		return boardCardRow{}, ErrNotFound
	}
	if err != nil {
		return boardCardRow{}, err
	}
	return row, nil
}

func ensureBoardCardParentThreadAvailable(ctx context.Context, rower queryRower, boardID, parentThreadID, excludeCardID string) error {
	if strings.TrimSpace(parentThreadID) == "" {
		return nil
	}
	var existingCardID string
	err := rower.QueryRowContext(
		ctx,
		`SELECT c.id
		   FROM board_cards bc
		   JOIN cards c ON c.id = bc.card_id
		  WHERE bc.board_id = ? AND c.parent_thread_id = ? AND c.archived_at IS NULL AND c.id != ?
		  LIMIT 1`,
		boardID,
		parentThreadID,
		strings.TrimSpace(excludeCardID),
	).Scan(&existingCardID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query board card parent thread membership: %w", err)
	}
	return ErrConflict
}

func resolveBoardPlacementAnchors(ctx context.Context, rower queryRower, boardID, beforeCardID, afterCardID, beforeThreadID, afterThreadID string) (string, string, error) {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
	if beforeCardID != "" || afterCardID != "" {
		return beforeCardID, afterCardID, nil
	}
	resolve := func(threadID string) (string, error) {
		threadID = strings.TrimSpace(threadID)
		if threadID == "" {
			return "", nil
		}
		row, err := (&Store{}).loadBoardCardByIdentifier(ctx, rower, boardID, threadID, false)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "", invalidBoardRequest("placement anchor must reference a card already on the board")
			}
			return "", err
		}
		return row.CardID, nil
	}
	var err error
	beforeCardID, err = resolve(beforeThreadID)
	if err != nil {
		return "", "", err
	}
	afterCardID, err = resolve(afterThreadID)
	if err != nil {
		return "", "", err
	}
	return beforeCardID, afterCardID, nil
}

func loadThreadTitleForBoardCard(ctx context.Context, rower queryRower, threadID string) (string, error) {
	snapshot, err := getSnapshotRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
	if err != nil {
		return "", err
	}
	body := map[string]any{}
	if strings.TrimSpace(snapshot.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(snapshot.BodyJSON), &body); err != nil {
			return "", fmt.Errorf("decode board card thread snapshot: %w", err)
		}
	}
	title := strings.TrimSpace(anyStringValue(body["title"]))
	if title == "" {
		return strings.TrimSpace(threadID), nil
	}
	return title, nil
}

func scanBoardCardRow(scanner interface{ Scan(dest ...any) error }) (boardCardRow, error) {
	row := boardCardRow{}
	if err := scanner.Scan(
		&row.BoardID,
		&row.CardID,
		&row.ColumnKey,
		&row.Rank,
		&row.Title,
		&row.Body,
		&row.Version,
		&row.ParentThreadID,
		&row.PinnedDocumentID,
		&row.Assignee,
		&row.Priority,
		&row.Status,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
		&row.ProvenanceJSON,
		&row.ArchivedAt,
		&row.ArchivedBy,
	); err != nil {
		return boardCardRow{}, fmt.Errorf("scan board card row: %w", err)
	}
	return row, nil
}

type boardCardVersionRow struct {
	CardID           string
	Version          int
	Title            string
	Body             string
	ParentThreadID   sql.NullString
	PinnedDocumentID sql.NullString
	Assignee         sql.NullString
	Priority         sql.NullString
	Status           string
	CreatedAt        string
	CreatedBy        string
	ProvenanceJSON   string
}

func scanBoardCardVersionRow(scanner interface{ Scan(dest ...any) error }) (boardCardVersionRow, error) {
	row := boardCardVersionRow{}
	if err := scanner.Scan(
		&row.CardID,
		&row.Version,
		&row.Title,
		&row.Body,
		&row.ParentThreadID,
		&row.PinnedDocumentID,
		&row.Assignee,
		&row.Priority,
		&row.Status,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.ProvenanceJSON,
	); err != nil {
		return boardCardVersionRow{}, fmt.Errorf("scan board card version row: %w", err)
	}
	return row, nil
}

func mergeBoardArchiveTombstoneFields(m map[string]any, r boardRow) {
	if m == nil {
		return
	}
	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		m["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		m["archived_by"] = r.ArchivedBy.String
	}
	if r.TombstonedAt.Valid && strings.TrimSpace(r.TombstonedAt.String) != "" {
		m["tombstoned_at"] = r.TombstonedAt.String
	}
	if r.TombstonedBy.Valid && strings.TrimSpace(r.TombstonedBy.String) != "" {
		m["tombstoned_by"] = r.TombstonedBy.String
	}
	if r.TombstoneReason.Valid && strings.TrimSpace(r.TombstoneReason.String) != "" {
		m["tombstone_reason"] = r.TombstoneReason.String
	}
}

func (r boardRow) toMap() (map[string]any, error) {
	columnSchema, err := decodeBoardColumnSchema(r.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	m := map[string]any{
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
	}
	mergeBoardArchiveTombstoneFields(m, r)
	return m, nil
}

func (r boardCardRow) toMap() (map[string]any, error) {
	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode board card provenance: %w", err)
		}
	}
	m := map[string]any{
		"id":                 r.CardID,
		"board_id":           r.BoardID,
		"column_key":         r.ColumnKey,
		"rank":               r.Rank,
		"title":              r.Title,
		"body":               r.Body,
		"version":            r.Version,
		"parent_thread":      nullableBoardString(r.ParentThreadID.String),
		"thread_id":          nullableBoardString(r.ParentThreadID.String),
		"pinned_document_id": nullableBoardString(r.PinnedDocumentID.String),
		"assignee":           nullableBoardString(r.Assignee.String),
		"priority":           nullableBoardString(r.Priority.String),
		"status":             r.Status,
		"created_at":         r.CreatedAt,
		"created_by":         r.CreatedBy,
		"updated_at":         r.UpdatedAt,
		"updated_by":         r.UpdatedBy,
		"provenance":         provenance,
	}
	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		m["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		m["archived_by"] = r.ArchivedBy.String
	}
	return m, nil
}

func (r boardCardVersionRow) toMap() map[string]any {
	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		_ = json.Unmarshal([]byte(r.ProvenanceJSON), &provenance)
	}
	return map[string]any{
		"id":                 r.CardID,
		"version":            r.Version,
		"title":              r.Title,
		"body":               r.Body,
		"parent_thread":      nullableBoardString(r.ParentThreadID.String),
		"thread_id":          nullableBoardString(r.ParentThreadID.String),
		"pinned_document_id": nullableBoardString(r.PinnedDocumentID.String),
		"assignee":           nullableBoardString(r.Assignee.String),
		"priority":           nullableBoardString(r.Priority.String),
		"status":             r.Status,
		"created_at":         r.CreatedAt,
		"created_by":         r.CreatedBy,
		"provenance":         provenance,
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

func validateCardID(cardID string) error {
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return fmt.Errorf("card.id is required")
	}
	if strings.Contains(cardID, "/") || strings.Contains(cardID, `\`) {
		return fmt.Errorf("card.id contains invalid path characters")
	}
	return nil
}

func validateThreadID(threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return fmt.Errorf("thread_id is required")
	}
	if strings.Contains(threadID, "/") || strings.Contains(threadID, `\`) {
		return fmt.Errorf("thread_id contains invalid path characters")
	}
	return nil
}

func validateBoardCardStatus(raw string, allowDefault bool) error {
	status := strings.TrimSpace(raw)
	if status == "" && allowDefault {
		return nil
	}
	switch status {
	case "todo", "in_progress", "done", "cancelled":
		return nil
	default:
		return fmt.Errorf("card.status must be one of: todo, in_progress, done, cancelled")
	}
}

func normalizeBoardCardStatus(raw string) string {
	status := strings.TrimSpace(raw)
	if status == "" {
		return "todo"
	}
	return status
}

func inferLegacyBoardCardStatus(columnKey string) string {
	if strings.TrimSpace(columnKey) == "done" {
		return "done"
	}
	return "todo"
}

// ValidateBoardPlacementAnchors enforces placement anchor rules shared by HTTP handlers and the store.
func ValidateBoardPlacementAnchors(beforeCardID, afterCardID, beforeThreadID, afterThreadID string) error {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
	beforeThreadID = strings.TrimSpace(beforeThreadID)
	afterThreadID = strings.TrimSpace(afterThreadID)

	if beforeCardID != "" && beforeThreadID != "" {
		return fmt.Errorf("before_card_id and before_thread_id are mutually exclusive")
	}
	if afterCardID != "" && afterThreadID != "" {
		return fmt.Errorf("after_card_id and after_thread_id are mutually exclusive")
	}

	hasCardAnchor := beforeCardID != "" || afterCardID != ""
	hasThreadAnchor := beforeThreadID != "" || afterThreadID != ""
	if hasCardAnchor && hasThreadAnchor {
		return fmt.Errorf("card-id and thread-id placement anchors cannot be combined")
	}

	if firstNonEmpty(beforeCardID, beforeThreadID) != "" && firstNonEmpty(afterCardID, afterThreadID) != "" {
		return fmt.Errorf("before and after anchors are mutually exclusive")
	}
	return nil
}

func ensureBoardCardMutable(card boardCardRow) error {
	if card.ArchivedAt.Valid && strings.TrimSpace(card.ArchivedAt.String) != "" {
		return invalidBoardRequest("card is archived")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
