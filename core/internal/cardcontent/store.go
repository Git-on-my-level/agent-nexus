package cardcontent

import (
	"context"
	"database/sql"
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
