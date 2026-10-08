package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// The pointer is internal routing state, keyed only by the authenticated writer.
// It grants no access: the board is loaded through the caller's scoped transaction
// on every use. Allocation, pointer replacement, card and ask commit together.
func ensureAskSubjectBoardTx(ctx context.Context, tx *accessTx, actor string) (boardRow, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT board_id FROM ask_subject_boards WHERE actor_id=?`, actor).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return boardRow{}, err
	}
	if id != "" {
		board, err := loadBoardRow(ctx, tx, id)
		if err == nil && !board.ArchivedAt.Valid && !board.TrashedAt.Valid {
			return board, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			return boardRow{}, err
		}
	}
	// Never reuse a reserved identity: it may belong to an inaccessible board or
	// backing thread. Random handle seeds also avoid title-collision searches.
	id = uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	threadHandle, err := uniqueHandleTx(ctx, tx, "thread", "asks-"+id, "asks-"+id)
	if err != nil {
		return boardRow{}, err
	}
	boardHandle, err := uniqueHandleTx(ctx, tx, "board", "asks-"+id, "asks-"+id)
	if err != nil {
		return boardRow{}, err
	}
	body, err := json.Marshal(buildBoardBackingThreadBody(id, id, "Asks"))
	if err != nil {
		return boardRow{}, err
	}
	columns, err := json.Marshal(defaultBoardColumnSchema())
	if err != nil {
		return boardRow{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO threads(id,handle,kind,thread_id,updated_at,updated_by,body_json,provenance_json) VALUES(?,?,'thread',?,?,?,?,?)`, id, threadHandle, id, now, actor, string(body), inferredProvenanceJSON()); err != nil {
		return boardRow{}, err
	}
	if err = replaceRefEdges(ctx, tx, "thread", id, typedRefEdgeTargets(refEdgeTypeRef, []string{"board:" + id})); err != nil {
		return boardRow{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO boards(id,handle,title,owners_json,thread_id,refs_json,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Asks','[]',?,'[]',?,?,?,?,?)`, id, boardHandle, id, string(columns), now, actor, now, actor); err != nil {
		return boardRow{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO ask_subject_boards(actor_id,board_id) VALUES(?,?) ON CONFLICT(actor_id) DO UPDATE SET board_id=excluded.board_id`, actor, id); err != nil {
		return boardRow{}, err
	}
	return loadBoardRow(ctx, tx, id)
}
