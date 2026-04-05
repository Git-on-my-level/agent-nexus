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

const boardSummaryRiskHorizon = 7 * 24 * time.Hour

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

// CardListFilter scopes global card listing (GET /cards).
type CardListFilter struct {
	// ArchivedOnly returns cards with archived_at set (soft-deleted / trash lane).
	ArchivedOnly bool
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
	DueAt            *string
	DefinitionOfDone []string
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
	Resolution       *string
	ResolutionRefs   []string
	Refs             []string
	Risk             *string
	IfBoardUpdatedAt *string
}

type UpdateBoardCardInput struct {
	Title            *string
	Body             *string
	ParentThreadID   *string
	DueAt            *string
	DefinitionOfDone *[]string
	Assignee         *string
	Priority         *string
	Status           *string
	PinnedDocumentID *string
	Resolution       *string
	ResolutionRefs   *[]string
	Refs             *[]string
	Risk             *string
	IfBoardUpdatedAt *string
}

type MoveBoardCardInput struct {
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
	BeforeThreadID   string
	AfterThreadID    string
	Resolution       *string
	ResolutionRefs   *[]string
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
	ThreadID         string
	RefsJSON         string
	ColumnSchemaJSON string
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
	BoardID              string
	CardID               string
	ColumnKey            string
	Rank                 string
	Title                string
	Body                 string
	Version              int
	ThreadID             sql.NullString
	ParentThreadID       sql.NullString
	DueAt                sql.NullString
	DefinitionOfDoneJSON string
	PinnedDocumentID     sql.NullString
	Assignee             sql.NullString
	Priority             sql.NullString
	Risk                 string
	Status               string
	Resolution           sql.NullString
	ResolutionRefsJSON   string
	RefsJSON             string
	CreatedAt            string
	CreatedBy            string
	UpdatedAt            string
	UpdatedBy            string
	ProvenanceJSON       string
	ArchivedAt           sql.NullString
	ArchivedBy           sql.NullString
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

func canonicalBoardCardRisk(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "medium", "high", "critical":
		return strings.TrimSpace(strings.ToLower(raw))
	default:
		return "low"
	}
}

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
	threadID := strings.TrimSpace(anyStringValue(board["thread_id"]))
	if threadID == "" {
		threadID = boardID
	}
	if err := validateThreadID(threadID); err != nil {
		return nil, invalidBoardRequestError(err)
	}
	refs, err := normalizeBoardRefs(board)
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
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return nil, fmt.Errorf("marshal board refs: %w", err)
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

	if err := ensureBoardBackingThreadTx(ctx, tx, actorID, boardID, threadID, title, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO boards(
			id, title, status, labels_json, owners_json, thread_id, refs_json,
			column_schema_json, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		title,
		status,
		string(labelsJSON),
		string(ownersJSON),
		threadID,
		string(refsJSON),
		string(columnSchemaJSON),
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
	if err := replaceRefEdges(ctx, tx, "board", boardID, typedRefEdgeTargets(refEdgeTypeRef, refs)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit board create transaction: %w", err)
	}

	return map[string]any{
		"id":            boardID,
		"title":         title,
		"status":        status,
		"labels":        labels,
		"owners":        owners,
		"thread_id":     threadID,
		"refs":          refs,
		"column_schema": columnSchema,
		"created_at":    now,
		"created_by":    actorID,
		"updated_at":    now,
		"updated_by":    actorID,
	}, nil
}

func (s *Store) GetBoard(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

func (s *Store) GetBoardSummary(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	summaries, err := s.computeBoardSummaries(ctx, []boardRow{row})
	if err != nil {
		return nil, err
	}
	summary, ok := summaries[row.ID]
	if !ok {
		return map[string]any{}, nil
	}
	return summary, nil
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

	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "board", boardID); err != nil {
		return fmt.Errorf("delete board ref edges: %w", err)
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
	if _, exists := patch["thread_id"]; exists {
		return nil, invalidBoardRequest("board.thread_id cannot be patched")
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
	nextRefs := decodeJSONListOrEmpty(currentRow.RefsJSON)
	if rawRefs, exists := patch["refs"]; exists {
		nextRefs, err = normalizeBoardRefsFromValue(rawRefs)
		if err != nil {
			return nil, invalidBoardRequestError(err)
		}
	}
	if rawDocumentRefs, exists := patch["document_refs"]; exists {
		documentRefs, err := normalizeBoardTypedRefs(rawDocumentRefs)
		if err != nil {
			return nil, invalidBoardRequest("board.document_refs must be a list of strings")
		}
		nextRefs = replaceTypedRefs(nextRefs, "document", documentRefs)
	}
	if rawPinnedRefs, exists := patch["pinned_refs"]; exists {
		pinnedRefs, err := normalizeBoardTypedRefs(rawPinnedRefs)
		if err != nil {
			return nil, invalidBoardRequest("board.pinned_refs must be a list of strings")
		}
		nextRefs = replaceBoardPinnedRefs(nextRefs, pinnedRefs)
	}
	if rawPrimaryDocumentID, exists := patch["primary"+"_document_id"]; exists {
		value := normalizeNullableString(rawPrimaryDocumentID)
		nextRefs = removeTypedRefPrefix(nextRefs, "document")
		if value != nil {
			nextRefs = append(nextRefs, "document:"+strings.TrimSpace(*value))
		}
	}
	nextRefs = uniqueSortedStrings(nextRefs)

	labelsJSON, err := json.Marshal(nextLabels)
	if err != nil {
		return nil, fmt.Errorf("marshal board labels: %w", err)
	}
	ownersJSON, err := json.Marshal(nextOwners)
	if err != nil {
		return nil, fmt.Errorf("marshal board owners: %w", err)
	}
	refsJSON, err := json.Marshal(nextRefs)
	if err != nil {
		return nil, fmt.Errorf("marshal board refs: %w", err)
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

	query := `UPDATE boards
		SET title = ?, status = ?, labels_json = ?, owners_json = ?, refs_json = ?,
		    column_schema_json = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`
	args := []any{
		nextTitle,
		nextStatus,
		string(labelsJSON),
		string(ownersJSON),
		string(refsJSON),
		string(columnSchemaJSON),
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

	if err := replaceRefEdges(ctx, tx, "board", boardID, typedRefEdgeTargets(refEdgeTypeRef, nextRefs)); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := ensureBoardBackingThreadTx(ctx, tx, actorID, boardID, currentRow.ThreadID, nextTitle, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("commit board update transaction: %w", err)
	}

	out := map[string]any{
		"id":            boardID,
		"title":         nextTitle,
		"status":        nextStatus,
		"labels":        nextLabels,
		"owners":        nextOwners,
		"thread_id":     currentRow.ThreadID,
		"refs":          nextRefs,
		"column_schema": nextColumnSchema,
		"created_at":    currentRow.CreatedAt,
		"created_by":    currentRow.CreatedBy,
		"updated_at":    now,
		"updated_by":    actorID,
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

func (s *Store) ListCards(ctx context.Context, filter CardListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	archivedClause := `archived_at IS NULL`
	if filter.ArchivedOnly {
		archivedClause = `archived_at IS NOT NULL`
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT board_id, id, column_key, rank, title, body_markdown, version, thread_id, parent_thread_id, due_at,
		        definition_of_done_json, pinned_document_id, assignee, priority, status, resolution, resolution_refs_json, refs_json,
		        created_at, created_by, updated_at, updated_by, provenance_json, archived_at, archived_by
		   FROM cards
		  WHERE `+archivedClause+`
		  ORDER BY board_id ASC, `+boardColumnOrderSQL("column_key")+`, rank ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query cards: %w", err)
	}
	defer rows.Close()

	out := make([]map[string]any, 0)
	for rows.Next() {
		row := boardCardRow{}
		if err := rows.Scan(
			&row.BoardID,
			&row.CardID,
			&row.ColumnKey,
			&row.Rank,
			&row.Title,
			&row.Body,
			&row.Version,
			&row.ThreadID,
			&row.ParentThreadID,
			&row.DueAt,
			&row.DefinitionOfDoneJSON,
			&row.PinnedDocumentID,
			&row.Assignee,
			&row.Priority,
			&row.Status,
			&row.Resolution,
			&row.ResolutionRefsJSON,
			&row.RefsJSON,
			&row.CreatedAt,
			&row.CreatedBy,
			&row.UpdatedAt,
			&row.UpdatedBy,
			&row.ProvenanceJSON,
			&row.ArchivedAt,
			&row.ArchivedBy,
		); err != nil {
			return nil, fmt.Errorf("scan card row: %w", err)
		}
		card, err := row.toMap()
		if err != nil {
			return nil, err
		}
		out = append(out, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cards: %w", err)
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

	sourceThreadID := strings.TrimSpace(firstNonEmpty(input.ParentThreadID, input.ThreadID))
	if sourceThreadID != "" {
		if err := validateThreadID(sourceThreadID); err != nil {
			return BoardCardMutationResult{}, invalidBoardRequestError(err)
		}
	}
	backingThreadID := uuid.NewString()

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
	dueAt := normalizeBoardOptionalPointer(input.DueAt)
	definitionOfDone := uniqueSortedStrings(input.DefinitionOfDone)
	definitionOfDoneJSON, err := json.Marshal(definitionOfDone)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("marshal card definition_of_done: %w", err)
	}
	resolution := normalizeCardResolution(input.Resolution, status)
	if err := validateCardResolution(resolution, true); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	resolutionRefs := uniqueSortedStrings(input.ResolutionRefs)
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	refs := uniqueSortedStrings(input.Refs)
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("marshal card refs: %w", err)
	}
	assignee := normalizeBoardOptionalPointer(input.Assignee)
	priority := normalizeBoardOptionalPointer(input.Priority)
	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)
	riskValue := canonicalBoardCardRisk("")
	if input.Risk != nil {
		riskValue = canonicalBoardCardRisk(*input.Risk)
	}

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
	if sourceThreadID != "" {
		if err := ensureThreadExists(ctx, tx, sourceThreadID); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if sourceThreadID == boardRow.ThreadID {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("board.thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, sourceThreadID, ""); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if title == "" {
		if derivedTitle, err := loadThreadTitleForBoardCard(ctx, tx, sourceThreadID); err == nil {
			title = derivedTitle
		} else if !errors.Is(err, ErrNotFound) {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}
	if title == "" {
		title = cardID
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
	if err := ensureCardBackingThreadTx(ctx, tx, actorID, cardID, backingThreadID, title, now); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO cards(
			id, board_id, thread_id, title, body_markdown, due_at, definition_of_done_json, column_key, rank, version,
			parent_thread_id, pinned_document_id, assignee, priority, risk, status, resolution, resolution_refs_json, refs_json,
			created_at, created_by, updated_at, updated_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		boardID,
		backingThreadID,
		title,
		body,
		nullableString(derefBoardString(dueAt)),
		string(definitionOfDoneJSON),
		columnKey,
		rank,
		1,
		nullableString(sourceThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		nullableString(derefBoardString(priority)),
		riskValue,
		status,
		nullableString(resolution),
		string(resolutionRefsJSON),
		string(refsJSON),
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
			card_id, version, board_id, thread_id, title, body_markdown, due_at, definition_of_done_json, column_key, rank,
			parent_thread_id, pinned_document_id, assignee, priority, risk, status, resolution, resolution_refs_json, refs_json,
			created_at, created_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		1,
		boardID,
		backingThreadID,
		title,
		body,
		nullableString(derefBoardString(dueAt)),
		string(definitionOfDoneJSON),
		columnKey,
		rank,
		nullableString(sourceThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		nullableString(derefBoardString(priority)),
		riskValue,
		status,
		nullableString(resolution),
		string(resolutionRefsJSON),
		string(refsJSON),
		now,
		actorID,
		provenanceJSON,
	); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("insert card version: %w", err)
	}

	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardID, columnKey, rank); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	cardTargets := appendRefEdgeTarget(nil, refEdgeTypeCardParentThread, "thread", sourceThreadID)
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
	if threadID != "" {
		input.ParentThreadID = threadID
	}
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
	var boardRow boardRow
	if err := ensureBoardCardMutable(cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if strings.TrimSpace(boardID) == "" {
		if err := ensureUpdatedAtMatches(cardRow.UpdatedAt, input.IfBoardUpdatedAt); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
		if strings.TrimSpace(boardID) != "" {
			boardRow, err = loadBoardRow(ctx, tx, boardID)
			if err != nil {
				_ = tx.Rollback()
				return BoardCardMutationResult{}, err
			}
		}
	} else {
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
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
	nextThreadID := strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, nextParentThread))
	nextDueAt := strings.TrimSpace(cardRow.DueAt.String)
	if input.DueAt != nil {
		nextDueAt = strings.TrimSpace(*input.DueAt)
	}
	nextDefinitionOfDoneJSON := cardRow.DefinitionOfDoneJSON
	if input.DefinitionOfDone != nil {
		definitionOfDone := uniqueSortedStrings(*input.DefinitionOfDone)
		definitionOfDoneBytes, marshalErr := json.Marshal(definitionOfDone)
		if marshalErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, fmt.Errorf("marshal card definition_of_done: %w", marshalErr)
		}
		nextDefinitionOfDoneJSON = string(definitionOfDoneBytes)
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
	nextResolution := normalizeIncomingCardResolution(strings.TrimSpace(cardRow.Resolution.String))
	if input.Resolution != nil {
		nextResolution = normalizeIncomingCardResolution(strings.TrimSpace(*input.Resolution))
	}
	if err := validateCardResolution(nextResolution, true); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	nextResolutionRefsJSON := cardRow.ResolutionRefsJSON
	if input.ResolutionRefs != nil {
		resolutionRefs := uniqueSortedStrings(*input.ResolutionRefs)
		resolutionRefsBytes, marshalErr := json.Marshal(resolutionRefs)
		if marshalErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, fmt.Errorf("marshal card resolution refs: %w", marshalErr)
		}
		nextResolutionRefsJSON = string(resolutionRefsBytes)
	}
	nextRefsJSON := cardRow.RefsJSON
	if input.Refs != nil {
		refs := uniqueSortedStrings(*input.Refs)
		refsBytes, marshalErr := json.Marshal(refs)
		if marshalErr != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, fmt.Errorf("marshal card refs: %w", marshalErr)
		}
		nextRefsJSON = string(refsBytes)
	}
	nextRisk := canonicalBoardCardRisk(cardRow.Risk)
	if input.Risk != nil {
		nextRisk = canonicalBoardCardRisk(*input.Risk)
	}

	if nextParentThread != "" {
		if err := ensureThreadExists(ctx, tx, nextParentThread); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if nextParentThread == boardRow.ThreadID {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("board.thread_id cannot be added as a board card")
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
		nextPinnedDocumentID == strings.TrimSpace(cardRow.PinnedDocumentID.String) &&
		nextThreadID == strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String)) &&
		nextDueAt == strings.TrimSpace(cardRow.DueAt.String) &&
		nextDefinitionOfDoneJSON == cardRow.DefinitionOfDoneJSON &&
		nextResolution == strings.TrimSpace(cardRow.Resolution.String) &&
		nextResolutionRefsJSON == cardRow.ResolutionRefsJSON &&
		nextRefsJSON == cardRow.RefsJSON &&
		nextRisk == canonicalBoardCardRisk(cardRow.Risk) {
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
			card_id, version, board_id, thread_id, title, body_markdown, due_at, definition_of_done_json, column_key, rank,
			parent_thread_id, pinned_document_id, assignee, priority, risk, status, resolution, resolution_refs_json, refs_json,
			created_at, created_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardRow.CardID,
		nextVersion,
		boardID,
		nextThreadID,
		nextTitle,
		nextBody,
		nullableString(nextDueAt),
		nextDefinitionOfDoneJSON,
		cardRow.ColumnKey,
		cardRow.Rank,
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nullableString(nextPriority),
		nextRisk,
		nextStatus,
		nullableString(nextResolution),
		nextResolutionRefsJSON,
		nextRefsJSON,
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
		    SET board_id = ?, thread_id = ?, title = ?, body_markdown = ?, due_at = ?, definition_of_done_json = ?, column_key = ?, rank = ?, version = ?,
		        parent_thread_id = ?, pinned_document_id = ?, assignee = ?, priority = ?, risk = ?, status = ?, resolution = ?, resolution_refs_json = ?, refs_json = ?,
		        updated_at = ?, updated_by = ?
		  WHERE id = ?`,
		boardID,
		nextThreadID,
		nextTitle,
		nextBody,
		nullableString(nextDueAt),
		nextDefinitionOfDoneJSON,
		cardRow.ColumnKey,
		cardRow.Rank,
		nextVersion,
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nullableString(nextPriority),
		nextRisk,
		nextStatus,
		nullableString(nextResolution),
		nextResolutionRefsJSON,
		nextRefsJSON,
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
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, cardRow.ColumnKey, cardRow.Rank); err != nil {
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

	var cardRow boardCardRow
	var boardRow boardRow
	if strings.TrimSpace(boardID) == "" {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	} else {
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
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

	nextResolution, nextResolutionRefsJSON, updateCard, err := resolveBoardCardMoveResolution(cardRow, columnKey, input)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, columnKey, rank); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	if updateCard {
		nextVersion := cardRow.Version + 1
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO card_versions(
				card_id, version, board_id, thread_id, title, body_markdown, due_at, definition_of_done_json, column_key, rank,
				parent_thread_id, pinned_document_id, assignee, priority, risk, status, resolution, resolution_refs_json, refs_json,
				created_at, created_by, provenance_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			cardRow.CardID,
			nextVersion,
			boardID,
			firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String),
			cardRow.Title,
			cardRow.Body,
			nullableString(cardRow.DueAt.String),
			cardRow.DefinitionOfDoneJSON,
			columnKey,
			rank,
			nullableString(cardRow.ParentThreadID.String),
			nullableString(cardRow.PinnedDocumentID.String),
			nullableString(cardRow.Assignee.String),
			nullableString(cardRow.Priority.String),
			canonicalBoardCardRisk(cardRow.Risk),
			cardRow.Status,
			nullableString(nextResolution),
			nextResolutionRefsJSON,
			cardRow.RefsJSON,
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
			    SET board_id = ?, thread_id = ?, column_key = ?, rank = ?, version = ?, resolution = ?, resolution_refs_json = ?,
			        updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			boardID,
			firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String),
			columnKey,
			rank,
			nextVersion,
			nullableString(nextResolution),
			nextResolutionRefsJSON,
			now,
			actorID,
			cardRow.CardID,
		); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, fmt.Errorf("update board card resolution: %w", err)
		}
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
		if err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	} else {
		cardRow.ColumnKey = columnKey
		cardRow.Rank = rank
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

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

func (s *Store) RestoreArchivedBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card restore transaction: %w", err)
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
	if !cardRow.ArchivedAt.Valid || strings.TrimSpace(cardRow.ArchivedAt.String) == "" {
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

	if _, err := tx.ExecContext(ctx, `UPDATE cards SET archived_at = NULL, archived_by = NULL WHERE id = ?`, cardRow.CardID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("restore board card: %w", err)
	}
	cardRow.ArchivedAt = sql.NullString{}
	cardRow.ArchivedBy = sql.NullString{}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, fmt.Errorf("commit board card restore transaction: %w", err)
	}

	boardMap, err := boardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardRow, err = s.loadBoardCardByIdentifier(ctx, s.db, boardID, cardRow.CardID, false)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

// PurgeArchivedBoardCard permanently removes a card that was soft-deleted (archived).
func (s *Store) PurgeArchivedBoardCard(ctx context.Context, boardID, identifier string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge card transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		return err
	}
	if !cardRow.ArchivedAt.Valid || strings.TrimSpace(cardRow.ArchivedAt.String) == "" {
		return ErrNotArchived
	}
	cardID := strings.TrimSpace(cardRow.CardID)

	boardRow, err := loadBoardRow(ctx, tx, cardRow.BoardID)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card source ref edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card target ref edges: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE id = ? AND archived_at IS NOT NULL`, cardID)
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected card purge: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, "oar-core")
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge card transaction: %w", err)
	}
	return nil
}

func (s *Store) ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT card_id, version, board_id, thread_id, title, body_markdown, due_at, definition_of_done_json, column_key, rank, parent_thread_id, pinned_document_id, assignee, priority, risk, status, resolution, resolution_refs_json, refs_json, created_at, created_by, provenance_json
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
		`SELECT b.id, b.title, b.status, re.source_id, re.target_id, json_extract(re.metadata_json, '$.column_key'), c.title, c.status, c.parent_thread_id, c.pinned_document_id, c.due_at, c.updated_at
		   FROM ref_edges re
		   JOIN boards b ON b.id = re.source_id
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND c.parent_thread_id = ?
		    AND c.archived_at IS NULL
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
			status           string
			cardBoardID      string
			cardID           string
			columnKey        sql.NullString
			cardTitle        string
			cardStatus       string
			parentThreadID   sql.NullString
			pinnedDocumentID sql.NullString
			dueAt            sql.NullString
			updatedAt        string
		)
		if err := rows.Scan(&boardID, &title, &status, &cardBoardID, &cardID, &columnKey, &cardTitle, &cardStatus, &parentThreadID, &pinnedDocumentID, &dueAt, &updatedAt); err != nil {
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
				"column_key":         nullableBoardString(columnKey.String),
				"parent_thread":      nullableBoardString(parentThreadID.String),
				"pinned_document_id": nullableBoardString(pinnedDocumentID.String),
				"due_at":             nullableBoardString(dueAt.String),
				"updated_at":         updatedAt,
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
	threadIDs := make([]string, 0, len(boards))
	for _, board := range boards {
		boardIDs = append(boardIDs, board.ID)
		threadIDs = append(threadIDs, board.ThreadID)
	}

	cardsByBoard, err := s.loadBoardCardRowsByBoardIDs(ctx, boardIDs)
	if err != nil {
		return nil, err
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
			"has_document_refs":     len(boardDocumentRefsFromRefs(decodeJSONListOrEmpty(board.RefsJSON))) > 0,
		}
	}

	return summaries, nil
}

func boardCardRowCountsAsOpenWorkItem(card boardCardRow) bool {
	switch strings.TrimSpace(card.Status) {
	case "done", "cancelled":
		return false
	default:
		return true
	}
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

func buildListBoardsQuery(filter BoardListFilter) (string, []any) {
	query := `SELECT id, title, status, labels_json, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
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

	query := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
		       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id IN (` + strings.Join(placeholders, ", ") + `)
			   AND c.archived_at IS NULL
		) AS ordered_cards
		ORDER BY ` + boardColumnOrderSQL(`column_key`) + `, rank ASC, card_id ASC`
	queryArgs := append([]any{boardDefaultColumn, refEdgeTypeBoardCard}, args...)
	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
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

	query := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND c.archived_at IS NULL`
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
			"oar-core",
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
		`SELECT re.source_id AS board_id, re.target_id AS card_id,
		        COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
		        COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
		        c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
		   FROM ref_edges re
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND re.source_id = ?
		    AND COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) = ?
		    AND c.archived_at IS NULL
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
	threadRow, err := getThreadRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if threadRow.Kind != "thread" {
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
		`SELECT id, title, status, labels_json, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, tombstoned_at, tombstoned_by, tombstone_reason
		   FROM boards
		  WHERE id = ?`,
		strings.TrimSpace(boardID),
	).Scan(
		&row.ID,
		&row.Title,
		&row.Status,
		&row.LabelsJSON,
		&row.OwnersJSON,
		&row.ThreadID,
		&row.RefsJSON,
		&row.ColumnSchemaJSON,
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
		&row.ThreadID,
		&row.RefsJSON,
		&row.ColumnSchemaJSON,
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

	cardQuery := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND re.target_id = ?`
	args := []any{boardDefaultColumn, refEdgeTypeBoardCard, boardID, identifier}
	if !includeArchived {
		cardQuery += ` AND c.archived_at IS NULL`
	}
	cardQuery += `
		) AS ordered_cards`

	row, err := scanBoardCardRow(rower.QueryRowContext(ctx, cardQuery, args...))
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return boardCardRow{}, err
	}

	threadQuery := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
		        c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND c.parent_thread_id = ?`
	threadArgs := []any{boardDefaultColumn, refEdgeTypeBoardCard, boardID, identifier}
	if !includeArchived {
		threadQuery += ` AND c.archived_at IS NULL`
	}
	threadQuery += `
		) AS ordered_cards
		ORDER BY updated_at DESC, card_id ASC
		LIMIT 1`
	row, err = scanBoardCardRow(rower.QueryRowContext(ctx, threadQuery, threadArgs...))
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
	query := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.body_markdown, c.version, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.priority, c.risk, c.status, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.target_id = ?`
	args := []any{boardDefaultColumn, refEdgeTypeBoardCard, cardID}
	if !includeArchived {
		query += ` AND c.archived_at IS NULL`
	}
	query += `
		) AS ordered_cards`
	row, err := scanBoardCardRow(rower.QueryRowContext(ctx, query, args...))
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
		   FROM ref_edges re
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND re.source_id = ?
		    AND c.parent_thread_id = ?
		    AND c.archived_at IS NULL
		    AND c.id != ?
		  LIMIT 1`,
		refEdgeTypeBoardCard,
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
	threadRow, err := getThreadRowFromQueryRower(ctx, rower, strings.TrimSpace(threadID), "threads")
	if err != nil {
		return "", err
	}
	body := map[string]any{}
	if strings.TrimSpace(threadRow.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(threadRow.BodyJSON), &body); err != nil {
			return "", fmt.Errorf("decode board card thread body: %w", err)
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
		&row.ThreadID,
		&row.ParentThreadID,
		&row.DueAt,
		&row.DefinitionOfDoneJSON,
		&row.PinnedDocumentID,
		&row.Assignee,
		&row.Priority,
		&row.Risk,
		&row.Status,
		&row.Resolution,
		&row.ResolutionRefsJSON,
		&row.RefsJSON,
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
	CardID               string
	Version              int
	BoardID              string
	ColumnKey            string
	Rank                 string
	Title                string
	Body                 string
	ThreadID             sql.NullString
	ParentThreadID       sql.NullString
	DueAt                sql.NullString
	DefinitionOfDoneJSON string
	PinnedDocumentID     sql.NullString
	Assignee             sql.NullString
	Priority             sql.NullString
	Risk                 string
	Status               string
	Resolution           sql.NullString
	ResolutionRefsJSON   string
	RefsJSON             string
	CreatedAt            string
	CreatedBy            string
	ProvenanceJSON       string
}

func scanBoardCardVersionRow(scanner interface{ Scan(dest ...any) error }) (boardCardVersionRow, error) {
	row := boardCardVersionRow{}
	if err := scanner.Scan(
		&row.CardID,
		&row.Version,
		&row.BoardID,
		&row.ThreadID,
		&row.Title,
		&row.Body,
		&row.DueAt,
		&row.DefinitionOfDoneJSON,
		&row.ColumnKey,
		&row.Rank,
		&row.ParentThreadID,
		&row.PinnedDocumentID,
		&row.Assignee,
		&row.Priority,
		&row.Risk,
		&row.Status,
		&row.Resolution,
		&row.ResolutionRefsJSON,
		&row.RefsJSON,
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

func ensureBoardBackingThreadTx(ctx context.Context, tx *sql.Tx, actorID, boardID, threadID, title, updatedAt string) error {
	boardID = strings.TrimSpace(boardID)
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	updatedAt = strings.TrimSpace(updatedAt)
	if boardID == "" || threadID == "" {
		return invalidBoardRequest("board thread is required")
	}
	subjectRef := "board:" + boardID

	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		body := buildBoardBackingThreadBody(boardID, threadID, title)
		bodyJSON, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("marshal board backing thread: %w", marshalErr)
		}
		filterColumns := threadFilterColumnsForKind("thread", body)
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, filter_status, filter_priority, filter_owner, filter_due_at, filter_cadence, filter_cadence_preset, filter_tags_json)
			 VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			threadID,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Priority),
			nil,
			nil,
			nullableString(filterColumns.Cadence),
			nullableString(filterColumns.CadencePreset),
			filterColumns.TagsJSON,
		); execErr != nil {
			if isUniqueViolation(execErr) {
				return ErrConflict
			}
			return fmt.Errorf("insert board backing thread: %w", execErr)
		}
		return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
	}
	if err != nil {
		return err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return err
	}
	existingSubjectRef := threadSubjectRef(threadBody)
	if existingSubjectRef != "" && existingSubjectRef != subjectRef {
		return invalidBoardRequest(fmt.Sprintf("board.thread_id %q is already bound to %q", threadID, existingSubjectRef))
	}

	delete(threadBody, "id")
	delete(threadBody, "updated_at")
	delete(threadBody, "updated_by")
	provenance := cloneProvenance(threadBody["provenance"])
	delete(threadBody, "provenance")
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}

	threadBody["subject_ref"] = subjectRef
	if title != "" {
		threadBody["title"] = title
	}

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal board backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal board backing thread provenance: %w", err)
	}
	filterColumns := threadFilterColumnsForKind("thread", threadBody)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?,
		        filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		nullableString(filterColumns.Status),
		nullableString(filterColumns.Priority),
		nil,
		nil,
		nullableString(filterColumns.Cadence),
		nullableString(filterColumns.CadencePreset),
		filterColumns.TagsJSON,
		threadID,
	); err != nil {
		return fmt.Errorf("update board backing thread: %w", err)
	}
	return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
}

func ensureCardBackingThreadTx(ctx context.Context, tx *sql.Tx, actorID, cardID, threadID, title, updatedAt string) error {
	cardID = strings.TrimSpace(cardID)
	threadID = strings.TrimSpace(threadID)
	title = strings.TrimSpace(title)
	updatedAt = strings.TrimSpace(updatedAt)
	if cardID == "" || threadID == "" {
		return invalidBoardRequest("card thread is required")
	}
	subjectRef := "card:" + cardID

	row, err := getThreadRowFromQueryRower(ctx, tx, threadID, "threads")
	if errors.Is(err, ErrNotFound) {
		body := buildCardBackingThreadBody(cardID, threadID, title)
		bodyJSON, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			return fmt.Errorf("marshal card backing thread: %w", marshalErr)
		}
		filterColumns := threadFilterColumnsForKind("thread", body)
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, filter_status, filter_priority, filter_owner, filter_due_at, filter_cadence, filter_cadence_preset, filter_tags_json)
			 VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			threadID,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
			nullableString(filterColumns.Status),
			nullableString(filterColumns.Priority),
			nil,
			nil,
			nullableString(filterColumns.Cadence),
			nullableString(filterColumns.CadencePreset),
			filterColumns.TagsJSON,
		); execErr != nil {
			if isUniqueViolation(execErr) {
				return ErrConflict
			}
			return fmt.Errorf("insert card backing thread: %w", execErr)
		}
		return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
	}
	if err != nil {
		return err
	}

	threadBody, err := row.ToThreadMap()
	if err != nil {
		return err
	}
	existingSubjectRef := threadSubjectRef(threadBody)
	if existingSubjectRef != "" && existingSubjectRef != subjectRef {
		return invalidBoardRequest(fmt.Sprintf("card.thread_id %q is already bound to %q", threadID, existingSubjectRef))
	}

	delete(threadBody, "id")
	delete(threadBody, "updated_at")
	delete(threadBody, "updated_by")
	provenance := cloneProvenance(threadBody["provenance"])
	delete(threadBody, "provenance")
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}

	threadBody["subject_ref"] = subjectRef
	if title != "" {
		threadBody["title"] = title
	}

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal card backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal card backing thread provenance: %w", err)
	}
	filterColumns := threadFilterColumnsForKind("thread", threadBody)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?,
		        filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
		nullableString(filterColumns.Status),
		nullableString(filterColumns.Priority),
		nil,
		nil,
		nullableString(filterColumns.Cadence),
		nullableString(filterColumns.CadencePreset),
		filterColumns.TagsJSON,
		threadID,
	); err != nil {
		return fmt.Errorf("update card backing thread: %w", err)
	}
	return replaceRefEdges(ctx, tx, "thread", threadID, typedRefEdgeTargets(refEdgeTypeRef, []string{subjectRef}))
}

func buildCardBackingThreadBody(cardID, threadID, title string) map[string]any {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Card " + strings.TrimSpace(cardID)
	}
	return map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "card:" + strings.TrimSpace(cardID),
		"title":       title,
		"status":      "active",
		"priority":    "p2",
		"tags":        []string{},
		"open_cards":  []string{},
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
}

func buildBoardBackingThreadBody(boardID, threadID, title string) map[string]any {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Board " + strings.TrimSpace(boardID)
	}
	return map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "board:" + strings.TrimSpace(boardID),
		"title":       title,
		"status":      "active",
		"priority":    "p2",
		"tags":        []string{},
		"open_cards":  []string{},
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
}

func upsertBoardCardRefEdge(ctx context.Context, tx *sql.Tx, boardID, cardID, columnKey, rank string) error {
	boardID = strings.TrimSpace(boardID)
	cardID = strings.TrimSpace(cardID)
	if boardID == "" || cardID == "" {
		return invalidBoardRequest("board card membership requires board and card ids")
	}
	metadataJSON, err := json.Marshal(map[string]any{
		"column_key": strings.TrimSpace(columnKey),
		"rank":       strings.TrimSpace(rank),
	})
	if err != nil {
		return fmt.Errorf("marshal board card edge metadata: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source_type, source_id, target_type, target_id, edge_type)
		 DO UPDATE SET metadata_json = excluded.metadata_json`,
		uuid.NewString(),
		"board",
		boardID,
		"card",
		cardID,
		refEdgeTypeBoardCard,
		time.Now().UTC().Format(time.RFC3339Nano),
		string(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert board card ref edge: %w", err)
	}
	return nil
}

func normalizeBoardRefs(board map[string]any) ([]string, error) {
	refs := make([]string, 0)
	if raw, exists := board["refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, values...)
	}
	if raw, exists := board["document_refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, replaceTypedRefs(nil, "document", values)...)
	}
	if raw, exists := board["pinned_refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, values...)
	}
	if raw, exists := board["primary"+"_document_id"]; exists {
		documentID := strings.TrimSpace(anyStringValue(raw))
		if documentID != "" {
			if err := validateDocumentID(documentID); err != nil {
				return nil, err
			}
			refs = append(refs, "document:"+documentID)
		}
	}
	refs = uniqueSortedStrings(refs)
	return refs, nil
}

func normalizeBoardRefsFromValue(raw any) ([]string, error) {
	values, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, err
	}
	return uniqueSortedStrings(values), nil
}

func normalizeBoardTypedRefs(raw any) ([]string, error) {
	values, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, err
	}
	for _, ref := range values {
		if _, _, ok := normalizeTypedRef(ref); !ok {
			return nil, fmt.Errorf("invalid typed ref %q", strings.TrimSpace(ref))
		}
	}
	return uniqueSortedStrings(values), nil
}

func replaceTypedRefs(refs []string, targetType string, values []string) []string {
	targetType = strings.TrimSpace(targetType)
	out := make([]string, 0, len(refs)+len(values))
	for _, ref := range refs {
		if _, refTargetType, ok := normalizeTypedRef(ref); ok && refTargetType == targetType {
			continue
		}
		out = append(out, strings.TrimSpace(ref))
	}
	out = append(out, values...)
	return uniqueSortedStrings(out)
}

func replaceBoardPinnedRefs(refs []string, values []string) []string {
	return append(refs, values...)
}

func removeTypedRefPrefix(refs []string, targetType string) []string {
	targetType = strings.TrimSpace(targetType)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, refTargetType, ok := normalizeTypedRef(ref); ok && refTargetType == targetType {
			continue
		}
		out = append(out, strings.TrimSpace(ref))
	}
	return uniqueSortedStrings(out)
}

func boardDocumentRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if prefix, value, ok := normalizeTypedRef(ref); ok && prefix == "document" {
			out = append(out, "document:"+value)
		}
	}
	return uniqueSortedStrings(out)
}

func boardPinnedRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if _, targetType, ok := normalizeTypedRef(ref); ok && targetType != "document" && targetType != "topic" && targetType != "thread" {
			out = append(out, ref)
		}
	}
	return uniqueSortedStrings(out)
}

func boardTopicRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if prefix, value, ok := normalizeTypedRef(ref); ok && prefix == "topic" {
			out = append(out, "topic:"+value)
		}
	}
	return uniqueSortedStrings(out)
}

func boardTypedRefOrNil(prefix, raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.TrimSpace(prefix) + ":" + raw
}

func (r boardRow) toMap() (map[string]any, error) {
	columnSchema, err := decodeBoardColumnSchema(r.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	m := map[string]any{
		"id":            r.ID,
		"title":         r.Title,
		"status":        r.Status,
		"labels":        decodeJSONListOrEmpty(r.LabelsJSON),
		"owners":        decodeJSONListOrEmpty(r.OwnersJSON),
		"thread_id":     r.ThreadID,
		"refs":          decodeJSONListOrEmpty(r.RefsJSON),
		"column_schema": columnSchema,
		"created_at":    r.CreatedAt,
		"created_by":    r.CreatedBy,
		"updated_at":    r.UpdatedAt,
		"updated_by":    r.UpdatedBy,
	}
	if documentRefs := boardDocumentRefsFromRefs(decodeJSONListOrEmpty(r.RefsJSON)); len(documentRefs) > 0 {
		m["document_refs"] = documentRefs
	}
	if pinnedRefs := boardPinnedRefsFromRefs(decodeJSONListOrEmpty(r.RefsJSON)); len(pinnedRefs) > 0 {
		m["pinned_refs"] = pinnedRefs
	}
	if topicRefs := boardTopicRefsFromRefs(decodeJSONListOrEmpty(r.RefsJSON)); len(topicRefs) > 0 {
		m["primary_topic_ref"] = topicRefs[0]
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
	definitionOfDone := decodeJSONListOrEmpty(r.DefinitionOfDoneJSON)
	resolutionRefs := decodeJSONListOrEmpty(r.ResolutionRefsJSON)
	refs := decodeJSONListOrEmpty(r.RefsJSON)
	threadID := strings.TrimSpace(firstNonEmpty(r.ThreadID.String, r.ParentThreadID.String))
	m := map[string]any{
		"id":                 r.CardID,
		"board_id":           r.BoardID,
		"board_ref":          "board:" + strings.TrimSpace(r.BoardID),
		"thread_id":          nullableBoardString(threadID),
		"column_key":         r.ColumnKey,
		"rank":               r.Rank,
		"title":              r.Title,
		"summary":            r.Body,
		"body":               r.Body,
		"body_markdown":      r.Body,
		"version":            r.Version,
		"parent_thread":      nullableBoardString(r.ParentThreadID.String),
		"pinned_document_id": nullableBoardString(r.PinnedDocumentID.String),
		"document_ref":       boardTypedRefOrNil("document", r.PinnedDocumentID.String),
		"assignee":           nullableBoardString(r.Assignee.String),
		"priority":           nullableBoardString(r.Priority.String),
		"risk":               canonicalBoardCardRisk(r.Risk),
		"due_at":             nullableBoardString(r.DueAt.String),
		"definition_of_done": definitionOfDone,
		"status":             r.Status,
		"resolution":         canonicalizeCardResolutionForAPI(r.Resolution.String),
		"resolution_refs":    resolutionRefs,
		"refs":               refs,
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
	threadID := strings.TrimSpace(firstNonEmpty(r.ThreadID.String, r.ParentThreadID.String))
	return map[string]any{
		"id":                 r.CardID,
		"version":            r.Version,
		"title":              r.Title,
		"body":               r.Body,
		"body_markdown":      r.Body,
		"board_id":           r.BoardID,
		"thread_id":          nullableBoardString(threadID),
		"parent_thread":      nullableBoardString(r.ParentThreadID.String),
		"pinned_document_id": nullableBoardString(r.PinnedDocumentID.String),
		"assignee":           nullableBoardString(r.Assignee.String),
		"priority":           nullableBoardString(r.Priority.String),
		"risk":               canonicalBoardCardRisk(r.Risk),
		"due_at":             nullableBoardString(r.DueAt.String),
		"definition_of_done": decodeJSONListOrEmpty(r.DefinitionOfDoneJSON),
		"column_key":         r.ColumnKey,
		"rank":               r.Rank,
		"status":             r.Status,
		"resolution":         canonicalizeCardResolutionForAPI(r.Resolution.String),
		"resolution_refs":    decodeJSONListOrEmpty(r.ResolutionRefsJSON),
		"refs":               decodeJSONListOrEmpty(r.RefsJSON),
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

func canonicalizeCardResolutionForAPI(raw string) any {
	s := normalizeIncomingCardResolution(raw)
	if s == "" {
		return nil
	}
	switch s {
	case "done", "canceled":
		return s
	default:
		return nil
	}
}

func normalizeIncomingCardResolution(raw string) string {
	s := strings.TrimSpace(raw)
	switch s {
	case "completed", "superseded":
		return "done"
	case "unresolved":
		return ""
	default:
		return s
	}
}

func validateCardResolution(raw string, allowEmpty bool) error {
	value := normalizeIncomingCardResolution(raw)
	if value == "" && allowEmpty {
		return nil
	}
	switch value {
	case "done", "canceled":
		return nil
	default:
		return fmt.Errorf("card.resolution must be null, done, or canceled")
	}
}

func resolveBoardCardMoveResolution(cardRow boardCardRow, columnKey string, input MoveBoardCardInput) (string, string, bool, error) {
	columnKey = strings.TrimSpace(columnKey)
	currentResolution := normalizeIncomingCardResolution(strings.TrimSpace(cardRow.Resolution.String))
	if input.Resolution == nil {
		if columnKey != "done" {
			if currentResolution != "" {
				return "", "", false, invalidBoardRequest("resolution must be null when column_key is not done")
			}
			return "", "", false, nil
		}
		if strings.TrimSpace(cardRow.ColumnKey) == "done" && currentResolution != "" {
			return currentResolution, cardRow.ResolutionRefsJSON, false, nil
		}
		if currentResolution == "" {
			return "", "", false, invalidBoardRequest("resolution is required when column_key is done")
		}
		return "", "", false, invalidBoardRequest("resolution is required when column_key is done")
	}

	nextResolution := normalizeIncomingCardResolution(strings.TrimSpace(*input.Resolution))
	if err := validateCardResolution(nextResolution, false); err != nil {
		return "", "", false, invalidBoardRequestError(err)
	}
	if columnKey != "done" {
		return "", "", false, invalidBoardRequest("resolution requires column_key done")
	}
	if input.ResolutionRefs == nil {
		return "", "", false, invalidBoardRequest("resolution_refs are required when resolution is set")
	}

	resolutionRefs := uniqueSortedStrings(*input.ResolutionRefs)
	if len(resolutionRefs) == 0 {
		return "", "", false, invalidBoardRequest("resolution_refs are required when resolution is set")
	}
	for _, ref := range resolutionRefs {
		if _, _, ok := normalizeTypedRef(ref); !ok {
			return "", "", false, invalidBoardRequest(fmt.Sprintf("invalid typed ref %q", strings.TrimSpace(ref)))
		}
	}
	switch nextResolution {
	case "done":
		if !containsTypedRefPrefix(resolutionRefs, "artifact") && !containsTypedRefPrefix(resolutionRefs, "event") {
			return "", "", false, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	case "canceled":
		if !containsTypedRefPrefix(resolutionRefs, "event") {
			return "", "", false, invalidBoardRequest("resolution_refs must include at least one event: ref for resolution canceled")
		}
	}
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return "", "", false, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	return nextResolution, string(resolutionRefsJSON), true, nil
}

func normalizeCardResolution(raw *string, status string) string {
	if raw != nil {
		return normalizeIncomingCardResolution(strings.TrimSpace(*raw))
	}
	switch strings.TrimSpace(status) {
	case "done":
		return "done"
	case "cancelled":
		return "canceled"
	default:
		return ""
	}
}

func resolutionFromStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "done":
		return "done"
	case "cancelled":
		return "canceled"
	default:
		return ""
	}
}

func containsTypedRefPrefix(refs []string, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, ref := range refs {
		refPrefix, _, ok := normalizeTypedRef(ref)
		if ok && refPrefix == prefix {
			return true
		}
	}
	return false
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
