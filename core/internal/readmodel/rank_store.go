package readmodel

import "context"

const RankSchemaProposal = `CREATE TABLE scope_ordering (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL, board_id TEXT NOT NULL, column_id TEXT NOT NULL,
 rank INTEGER NOT NULL, rid INTEGER NOT NULL,
 PRIMARY KEY(scope_id,generation,board_id,column_id,rank,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_ordering_resource ON scope_ordering(scope_id,generation,board_id,column_id,rid);`
const RankLeft = `SELECT rank,rid FROM scope_ordering WHERE scope_id=? AND generation=? AND board_id=? AND column_id=?
 AND (rank,rid)<(?,?) ORDER BY rank DESC,rid DESC LIMIT 1`
const RankRight = `SELECT rank,rid FROM scope_ordering WHERE scope_id=? AND generation=? AND board_id=? AND column_id=?
 AND (rank,rid)>(?,?) ORDER BY rank,rid LIMIT 1`

// RankNeighbor performs one exact-prefix indexed seek. Callers bind a trusted
// board, column and active generation in the source write transaction, then use
// RankBetween on the two neighbors. Equal ranks refuse a gap; no rebalance runs.
func RankNeighbor(ctx context.Context, tx QueryTx, scope string, generation int64, board, column string, anchor Key, left bool) (*Key, error) {
	if !boundedText(scope, 512) || generation < 1 || !boundedText(board, 512) || !boundedText(column, 512) || anchor.RID < 1 {
		return nil, ErrProjection
	}
	query := RankRight
	if left {
		query = RankLeft
	}
	rows, err := tx.Query(ctx, query, scope, generation, board, column, anchor.Sort, anchor.RID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var key Key
	if err = rows.Scan(&key.Sort, &key.RID); err != nil {
		return nil, err
	}
	if key.RID < 1 || rows.Next() {
		return nil, ErrProjection
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return &key, nil
}
