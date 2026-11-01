// Board lifecycle mutations: create, patch, archive/trash/restore/purge over
// boardRow, the row loading those mutations share, and board role handling.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/actors"
)

var ErrInvalidBoardRequest = errors.New("invalid board request")

func rejectReservedBoardOwners(ids []string) error {
	for _, id := range ids {
		if actors.IsReservedServiceActorID(id) {
			return invalidBoardRequest("board.owners must not include the reserved system actor")
		}
	}
	return nil
}

func rejectReservedCardAssignee(assignee *string) error {
	if assignee == nil {
		return nil
	}
	if actors.IsReservedServiceActorID(strings.TrimSpace(*assignee)) {
		return invalidBoardRequest("card assignee must not be the reserved system actor")
	}
	return nil
}

type boardRow struct {
	ID               string
	Handle           sql.NullString
	Title            string
	Summary          string
	Role             string
	InitiativeTarget sql.NullInt64
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
	TrashedAt        sql.NullString
	TrashedBy        sql.NullString
	TrashReason      sql.NullString
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
	if _, exists := board["labels"]; exists {
		return nil, invalidBoardRequest("board.labels is not supported")
	}
	owners, err := normalizeOptionalStringList(board, "owners")
	if err != nil {
		return nil, invalidBoardRequestError(err)
	}
	if err := rejectReservedBoardOwners(owners); err != nil {
		return nil, err
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
	target, err := normalizeInitiativeTarget(board, sql.NullInt64{})
	if err != nil {
		return nil, err
	}
	role, err := normalizeBoardRole(board, "")
	if err != nil {
		return nil, err
	}
	summary := strings.TrimSpace(anyStringValue(board["summary"]))

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin board create transaction: %w", err)
	}

	if err := ensureBoardBackingThreadTx(ctx, tx, actorID, boardID, threadID, title, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	boardHandle, err := uniqueHandleTx(ctx, tx, "board", title, "board-"+boardID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("allocate board handle: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO boards(
			id, handle, title, summary, role, initiative_target, owners_json, thread_id, refs_json,
			column_schema_json, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		boardHandle,
		title,
		summary,
		role,
		nullableInitiativeTarget(target),
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, fmt.Errorf("insert board: %w", err)
	}
	if err := replaceRefEdgesSelective(ctx, tx, "board", boardID, []string{refEdgeTypeRef}, typedRefEdgeTargets(refEdgeTypeRef, refs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}

	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{threadID}, time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("commit board create transaction: %w", err)
	}

	row, err := loadBoardRow(ctx, s.db, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
}

func (s *Store) ArchiveBoard(ctx context.Context, actorID, boardID string) (map[string]any, error) {
	return s.ArchiveBoardIfUpdatedAt(ctx, actorID, boardID, nil)
}

func (s *Store) ArchiveBoardIfUpdatedAt(ctx context.Context, actorID, boardID string, ifUpdatedAt *string) (map[string]any, error) {
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
	if err := ensureUpdatedAtMatches(row.UpdatedAt, ifUpdatedAt); err != nil {
		return nil, err
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return boardRowToAPI(ctx, s.db, row)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	query := `UPDATE boards SET archived_at = ?, archived_by = ? WHERE id = ?`
	args := []any{now, strings.TrimSpace(actorID), boardID}
	if ifUpdatedAt != nil {
		query += ` AND updated_at = ?`
		args = append(args, *ifUpdatedAt)
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("archive board: %w", err)
	}
	if ifUpdatedAt != nil {
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if rows != 1 {
			return nil, ErrConflict
		}
	}
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{row.ThreadID}, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE boards SET archived_at = NULL, archived_by = NULL WHERE id = ?`,
		boardID,
	); err != nil {
		return nil, fmt.Errorf("unarchive board: %w", err)
	}
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{row.ThreadID}, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
}

func (s *Store) TrashBoard(ctx context.Context, actorID, boardID, reason string) (map[string]any, error) {
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
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return boardRowToAPI(ctx, s.db, row)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`UPDATE boards SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, strings.TrimSpace(actorID), strings.TrimSpace(reason), boardID,
	); err != nil {
		return nil, fmt.Errorf("trash board: %w", err)
	}
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{row.ThreadID}, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
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
	if !row.TrashedAt.Valid || strings.TrimSpace(row.TrashedAt.String) == "" {
		return nil, ErrNotTrashed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE boards SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		boardID,
	); err != nil {
		return nil, fmt.Errorf("restore board: %w", err)
	}
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{row.ThreadID}, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	row, err = s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
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
		`SELECT id FROM boards WHERE id = ? AND trashed_at IS NOT NULL`,
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
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed board: %w", err)
	}

	if err := preservePurgedAccess(ctx, tx, "board", boardID); err != nil {
		return err
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

	nextTarget, err := normalizeInitiativeTarget(patch, currentRow.InitiativeTarget)
	if err != nil {
		return nil, err
	}
	nextRole, err := normalizeBoardRole(patch, currentRow.Role)
	if err != nil {
		return nil, err
	}
	nextTitle := strings.TrimSpace(currentRow.Title)
	if _, exists := patch["title"]; exists {
		nextTitle = strings.TrimSpace(anyStringValue(patch["title"]))
		if nextTitle == "" {
			return nil, invalidBoardRequest("board.title must not be empty")
		}
	}
	nextSummary := strings.TrimSpace(currentRow.Summary)
	if _, exists := patch["summary"]; exists {
		nextSummary = strings.TrimSpace(anyStringValue(patch["summary"]))
	}
	if _, exists := patch["status"]; exists {
		return nil, invalidBoardRequest("board.status is not writable; use archive/trash lifecycle routes")
	}
	if _, exists := patch["labels"]; exists {
		return nil, invalidBoardRequest("board.labels is not supported")
	}
	nextOwners, err := decodeStoredJSONList(currentRow.OwnersJSON, "board.owners")
	if err != nil {
		return nil, err
	}
	if rawOwners, exists := patch["owners"]; exists {
		nextOwners, err = normalizeStringSlice(rawOwners)
		if err != nil {
			return nil, invalidBoardRequest("board.owners must be a list of strings")
		}
		nextOwners = uniqueNormalizedStrings(nextOwners)
		if err := rejectReservedBoardOwners(nextOwners); err != nil {
			return nil, err
		}
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
	nextRefs, err := boardEffectiveTypedRefsForPatch(ctx, s.db, currentRow)
	if err != nil {
		return nil, err
	}
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
	nextRefs = uniqueSortedStrings(nextRefs)

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
		SET title = ?, summary = ?, role = ?, initiative_target = ?, owners_json = ?, refs_json = ?,
		    column_schema_json = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`
	args := []any{
		nextTitle,
		nextSummary,
		nextRole,
		nullableInitiativeTarget(nextTarget),
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("update board: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(result, ifUpdatedAt, "board update"); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}

	if err := replaceRefEdgesSelective(ctx, tx, "board", boardID, []string{refEdgeTypeRef}, typedRefEdgeTargets(refEdgeTypeRef, nextRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	if err := ensureBoardBackingThreadTx(ctx, tx, actorID, boardID, currentRow.ThreadID, nextTitle, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}

	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{currentRow.ThreadID}, time.Now().UTC()); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("commit board update transaction: %w", err)
	}

	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
}

func ensureBoardUpdatedAtMatches(board boardRow, ifUpdatedAt *string) error {
	return ensureUpdatedAtMatches(board.UpdatedAt, ifUpdatedAt)
}

func touchBoardRow(ctx context.Context, tx *accessTx, board boardRow, actorID string) (boardRow, error) {
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
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{board.ThreadID}, time.Now().UTC()); err != nil {
		return boardRow{}, err
	}
	board.UpdatedAt = now
	board.UpdatedBy = actorID
	return board, nil
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
		`SELECT id, handle, title, summary, role, initiative_target, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		   FROM boards
		  WHERE id = ?`,
		strings.TrimSpace(boardID),
	).Scan(
		&row.ID,
		&row.Handle,
		&row.Title,
		&row.Summary,
		&row.Role,
		&row.InitiativeTarget,
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
		&row.TrashedAt,
		&row.TrashedBy,
		&row.TrashReason,
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
		&row.Handle,
		&row.Title,
		&row.Summary,
		&row.Role,
		&row.InitiativeTarget,
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
		&row.TrashedAt,
		&row.TrashedBy,
		&row.TrashReason,
	); err != nil {
		return boardRow{}, fmt.Errorf("scan board row: %w", err)
	}
	return row, nil
}

func mergeBoardArchiveTrashFields(m map[string]any, r boardRow) {
	if m == nil {
		return
	}
	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		m["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		m["archived_by"] = r.ArchivedBy.String
	}
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		m["trashed_at"] = r.TrashedAt.String
	}
	if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
		m["trashed_by"] = r.TrashedBy.String
	}
	if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
		m["trash_reason"] = r.TrashReason.String
	}
}

// Roles are open strings so clients can round-trip future roles. Empty clears.
func normalizeBoardRole(input map[string]any, fallback string) (string, error) {
	raw, exists := input["role"]
	if !exists {
		return fallback, nil
	}
	role, ok := raw.(string)
	if !ok || len(role) > 64 {
		return "", invalidBoardRequest("board.role must be a string of at most 64 bytes")
	}
	return strings.TrimSpace(role), nil
}
