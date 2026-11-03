package cardcontent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Querier is the read handle of the caller's transaction or scoped snapshot.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Columns are the card_revisions columns that hold revision content, in the
// order Load scans them.
const loadColumns = `revision_id, content_encoding, COALESCE(content_codec,''), COALESCE(content_base_revision_id,''), COALESCE(content_hash,''), COALESCE(content_bytes,0), content_payload`

type storedRow struct {
	RevisionID string
	Encoding   sql.NullString
	Record
}

func loadStoredRow(ctx context.Context, q Querier, revisionID string) (storedRow, error) {
	var row storedRow
	err := q.QueryRowContext(ctx, `SELECT `+loadColumns+` FROM card_revisions WHERE revision_id = ?`, revisionID).Scan(
		&row.RevisionID, &row.Encoding, &row.Codec, &row.BaseRevisionID, &row.ContentHash, &row.ContentBytes, &row.Payload,
	)
	if err != nil {
		return storedRow{}, err
	}
	row.Record.Encoding = row.Encoding.String
	return row, nil
}

// ErrNotStored reports a revision whose content predates this storage and has
// not been migrated (it never happens after the migration completes).
var ErrNotStored = errors.New("card revision content is not stored")

// LoadRevisionContent returns the canonical content bytes of one revision, decoding at most a
// snapshot and one delta. It verifies the bytes against the recorded hash.
func LoadRevisionContent(ctx context.Context, q Querier, revisionID string) ([]byte, error) {
	row, err := loadStoredRow(ctx, q, strings.TrimSpace(revisionID))
	if err != nil {
		return nil, err
	}
	if !row.Encoding.Valid {
		return nil, ErrNotStored
	}
	if row.Record.Encoding == EncodingRef {
		target, err := loadStoredRow(ctx, q, row.BaseRevisionID)
		if err != nil {
			return nil, fmt.Errorf("%w: ref target %s: %v", ErrCorrupt, row.BaseRevisionID, err)
		}
		if target.Record.Encoding == EncodingRef || target.ContentHash != row.ContentHash {
			return nil, fmt.Errorf("%w: invalid ref target %s", ErrCorrupt, row.BaseRevisionID)
		}
		row = target
	}
	var base []byte
	if row.Record.Encoding == EncodingDelta {
		snapshot, err := loadStoredRow(ctx, q, row.BaseRevisionID)
		if err != nil {
			return nil, fmt.Errorf("%w: delta base %s: %v", ErrCorrupt, row.BaseRevisionID, err)
		}
		if snapshot.Record.Encoding != EncodingSnapshot {
			return nil, fmt.Errorf("%w: delta base %s is not a snapshot", ErrCorrupt, row.BaseRevisionID)
		}
		if base, err = DecodeRecord(snapshot.Record, nil); err != nil {
			return nil, err
		}
	}
	return DecodeRecord(row.Record, base)
}

// RowQuerier is the batched read handle of the caller's transaction.
type RowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// LoadRevisionContents returns the canonical content of many revisions. Each
// revision is verified exactly as LoadRevisionContent verifies it, but the
// stored rows are read in one statement per link of the chains (a revision,
// then a ref's target, then a delta's snapshot), not one statement per row.
// The first revision that does not load or verify fails the whole call.
func LoadRevisionContents(ctx context.Context, q RowQuerier, revisionIDs []string) (map[string][]byte, error) {
	ids := make([]string, 0, len(revisionIDs))
	requested := make(map[string]bool, len(revisionIDs))
	for _, id := range revisionIDs {
		id = strings.TrimSpace(id)
		if !requested[id] {
			requested[id] = true
			ids = append(ids, id)
		}
	}
	rows := make(map[string]storedRow, len(ids))
	for frontier := ids; len(frontier) > 0; {
		if err := loadStoredRows(ctx, q, frontier, rows); err != nil {
			return nil, err
		}
		frontier = nil
		for _, id := range ids {
			// An id that is still missing after its own round is never asked
			// for again; resolveStoredRow reports it.
			if next := nextChainRow(id, rows); next != "" && !requested[next] {
				requested[next] = true
				frontier = append(frontier, next)
			}
		}
	}
	out := make(map[string][]byte, len(ids))
	bases := map[string][]byte{}
	for _, id := range ids {
		content, err := resolveStoredRow(id, rows, bases)
		if err != nil {
			return nil, fmt.Errorf("revision %s: %w", id, err)
		}
		out[id] = content
	}
	return out, nil
}

// loadStoredRows adds the content rows named by ids to rows. Ids without a row
// stay absent.
func loadStoredRows(ctx context.Context, q RowQuerier, ids []string, rows map[string]storedRow) error {
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	found, err := q.QueryContext(ctx, `SELECT `+loadColumns+` FROM card_revisions WHERE revision_id IN (SELECT value FROM json_each(?))`, string(raw))
	if err != nil {
		return err
	}
	defer found.Close()
	for found.Next() {
		var row storedRow
		if err := found.Scan(&row.RevisionID, &row.Encoding, &row.Codec, &row.BaseRevisionID, &row.ContentHash, &row.ContentBytes, &row.Payload); err != nil {
			return err
		}
		row.Record.Encoding = row.Encoding.String
		rows[row.RevisionID] = row
	}
	return found.Err()
}

// nextChainRow names the first row that resolving id still needs and that is
// not loaded yet, or "" when every row on its chain is loaded.
func nextChainRow(id string, rows map[string]storedRow) string {
	row, ok := rows[id]
	if !ok {
		return id
	}
	if !row.Encoding.Valid {
		return ""
	}
	if row.Record.Encoding == EncodingRef {
		target, ok := rows[row.BaseRevisionID]
		if !ok {
			return row.BaseRevisionID
		}
		row = target
	}
	if row.Record.Encoding == EncodingDelta {
		if _, ok := rows[row.BaseRevisionID]; !ok {
			return row.BaseRevisionID
		}
	}
	return ""
}

// resolveStoredRow applies LoadRevisionContent's rules to rows that
// loadStoredRows has already read. bases caches decoded delta snapshots.
func resolveStoredRow(id string, rows map[string]storedRow, bases map[string][]byte) ([]byte, error) {
	row, ok := rows[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	if !row.Encoding.Valid {
		return nil, ErrNotStored
	}
	if row.Record.Encoding == EncodingRef {
		target, ok := rows[row.BaseRevisionID]
		if !ok {
			return nil, fmt.Errorf("%w: ref target %s: %v", ErrCorrupt, row.BaseRevisionID, sql.ErrNoRows)
		}
		if target.Record.Encoding == EncodingRef || target.ContentHash != row.ContentHash {
			return nil, fmt.Errorf("%w: invalid ref target %s", ErrCorrupt, row.BaseRevisionID)
		}
		row = target
	}
	var base []byte
	if row.Record.Encoding == EncodingDelta {
		if cached, ok := bases[row.BaseRevisionID]; ok {
			base = cached
		} else {
			snapshot, ok := rows[row.BaseRevisionID]
			if !ok {
				return nil, fmt.Errorf("%w: delta base %s: %v", ErrCorrupt, row.BaseRevisionID, sql.ErrNoRows)
			}
			if snapshot.Record.Encoding != EncodingSnapshot {
				return nil, fmt.Errorf("%w: delta base %s is not a snapshot", ErrCorrupt, row.BaseRevisionID)
			}
			decoded, err := DecodeRecord(snapshot.Record, nil)
			if err != nil {
				return nil, err
			}
			bases[row.BaseRevisionID] = decoded
			base = decoded
		}
	}
	return DecodeRecord(row.Record, base)
}

// PlanForCard plans the record for a new revision of cardID, reading the
// card's existing records through q (the writer's transaction). Revisions with
// a higher number than revisionNumber are ignored, so a migration can replay a
// card's history in order.
func PlanForCard(ctx context.Context, q Querier, cardID string, revisionNumber int, content []byte) (Record, error) {
	hash := Hash(content)
	var duplicate string
	err := q.QueryRowContext(ctx,
		`SELECT revision_id FROM card_revisions
		  WHERE card_id = ? AND content_hash = ? AND content_encoding IN ('snapshot','delta') AND revision_number < ?
		  ORDER BY revision_number DESC LIMIT 1`,
		cardID, hash, revisionNumber,
	).Scan(&duplicate)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("find duplicate card revision content: %w", err)
	}
	in := PlanInput{Content: content, RevisionNumber: revisionNumber, DuplicateOf: duplicate}
	if duplicate == "" {
		var (
			kfID     string
			kfNumber int
		)
		err = q.QueryRowContext(ctx,
			`SELECT revision_id, revision_number FROM card_revisions
			  WHERE card_id = ? AND content_encoding = 'snapshot' AND revision_number < ?
			  ORDER BY revision_number DESC LIMIT 1`,
			cardID, revisionNumber,
		).Scan(&kfID, &kfNumber)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return Record{}, fmt.Errorf("find card content keyframe: %w", err)
		case revisionNumber-kfNumber < KeyframeInterval:
			// Decode the keyframe only when a delta against it is allowed.
			row, err := loadStoredRow(ctx, q, kfID)
			if err != nil {
				return Record{}, fmt.Errorf("load card content keyframe: %w", err)
			}
			kfContent, err := DecodeRecord(row.Record, nil)
			if err != nil {
				return Record{}, err
			}
			in.Keyframe = &Keyframe{RevisionID: kfID, RevisionNumber: kfNumber, Content: kfContent, PayloadBytes: len(row.Payload)}
		}
	}
	return Plan(in)
}
