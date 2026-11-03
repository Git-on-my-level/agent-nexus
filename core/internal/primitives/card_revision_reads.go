// Card revision reads: the summary byte limit, metadata-only history pages,
// single-revision content and revision diffs. None of them materializes more
// than one page of metadata or two revisions of content.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/textdiff"
)

// MaxCardSummaryBytes is the largest card summary core stores, in UTF-8 bytes
// after normalization. Every content write (card create, batch create, work
// create, ask compatibility cards and POST /cards/{id}/revisions) enforces it.
// Existing larger summaries stay readable; a new revision of such a card fails
// until its summary is brought under the limit.
const MaxCardSummaryBytes = 64 << 10

// ErrCardSummaryTooLarge marks a rejected oversized summary. It also matches
// ErrInvalidBoardRequest, so every write path answers 400 invalid_request.
var ErrCardSummaryTooLarge = errors.New("card summary too large")

type cardSummaryTooLargeError struct{ size int }

func (e cardSummaryTooLargeError) Error() string {
	return fmt.Sprintf("invalid board request: card summary is %d bytes, over the %d-byte (64 KiB) summary limit; keep long evidence in a document or source_refs and link it from the summary", e.size, MaxCardSummaryBytes)
}

func (e cardSummaryTooLargeError) Is(target error) bool {
	return target == ErrCardSummaryTooLarge || target == ErrInvalidBoardRequest
}

func (s *Store) checkCardSummarySize(summary string) error {
	limit := MaxCardSummaryBytes
	if s.cardSummaryLimit > 0 {
		limit = s.cardSummaryLimit
	}
	if n := len(strings.TrimSpace(summary)); n > limit {
		return cardSummaryTooLargeError{size: n}
	}
	return nil
}

const (
	DefaultCardRevisionPageSize = 50
	MaxCardRevisionPageSize     = 200
)

// CardRevisionPage is one metadata-only page of a card's history, newest
// first. NextCursor is empty on the last page.
type CardRevisionPage struct {
	Revisions  []map[string]any
	NextCursor string
}

type cardRevisionMetaRow struct {
	RevisionID     string
	CardID         string
	CardHandle     sql.NullString
	Number         int
	PrevRevisionID sql.NullString
	ArtifactID     string
	RevisionHash   string
	CreatedAt      string
	CreatedBy      string
	ContentHash    sql.NullString
	ContentBytes   sql.NullInt64
	SummaryBytes   sql.NullInt64
}

const cardRevisionMetaColumns = `cr.revision_id, cr.card_id, c.handle, cr.revision_number, cr.prev_revision_id, cr.artifact_id, cr.revision_hash,
	cr.created_at, cr.created_by, cr.content_hash, cr.content_bytes, cr.summary_bytes`

func scanCardRevisionMeta(scanner interface{ Scan(...any) error }) (cardRevisionMetaRow, error) {
	var r cardRevisionMetaRow
	err := scanner.Scan(&r.RevisionID, &r.CardID, &r.CardHandle, &r.Number, &r.PrevRevisionID, &r.ArtifactID, &r.RevisionHash,
		&r.CreatedAt, &r.CreatedBy, &r.ContentHash, &r.ContentBytes, &r.SummaryBytes)
	return r, err
}

func (r cardRevisionMetaRow) toMap() map[string]any {
	cardHandle := firstNonEmpty(strings.TrimSpace(r.CardHandle.String), r.CardID)
	handle := revisionHandle(cardHandle, r.Number)
	return map[string]any{
		"id":                r.RevisionID,
		"revision_id":       r.RevisionID,
		"ref":               "card_revision:" + handle,
		"handle":            handle,
		"card_id":           r.CardID,
		"card_ref":          "card:" + cardHandle,
		"card_handle":       cardHandle,
		"revision_number":   r.Number,
		"prev_revision_ref": boardTypedRefOrNil("card_revision", r.PrevRevisionID.String),
		"artifact_ref":      "artifact:" + strings.TrimSpace(r.ArtifactID),
		"revision_hash":     r.RevisionHash,
		"content_hash":      r.ContentHash.String,
		"content_bytes":     r.ContentBytes.Int64,
		"summary_bytes":     r.SummaryBytes.Int64,
		"created_at":        r.CreatedAt,
		"created_by":        r.CreatedBy,
	}
}

// ParseCardRevisionCursor validates an opaque history cursor.
func ParseCardRevisionCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(cursor, "r"))
	if err != nil || n < 1 || !strings.HasPrefix(cursor, "r") {
		return 0, invalidBoardRequest("cursor must be a next_cursor value from a previous card history page")
	}
	return n, nil
}

// ListCardRevisions returns one page of revision metadata, newest first. It
// never reads revision content. Cost: one indexed range scan of at most
// limit+1 rows on (card_id, revision_number).
func (s *Store) ListCardRevisions(ctx context.Context, cardID string, limit int, cursor string) (CardRevisionPage, error) {
	if s == nil || s.db == nil {
		return CardRevisionPage{}, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		limit = DefaultCardRevisionPageSize
	}
	if limit > MaxCardRevisionPageSize {
		return CardRevisionPage{}, invalidBoardRequest(fmt.Sprintf("limit must be between 1 and %d", MaxCardRevisionPageSize))
	}
	before, err := ParseCardRevisionCursor(cursor)
	if err != nil {
		return CardRevisionPage{}, err
	}
	if _, err := s.GetBoardCardCurrent(ctx, "", cardID); err != nil {
		return CardRevisionPage{}, err
	}
	query := `SELECT ` + cardRevisionMetaColumns + `
		   FROM card_revisions cr
		   JOIN cards c ON c.id = cr.card_id
		  WHERE cr.card_id = ?`
	args := []any{strings.TrimSpace(cardID)}
	if before > 0 {
		query += ` AND cr.revision_number < ?`
		args = append(args, before)
	}
	query += ` ORDER BY cr.revision_number DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return CardRevisionPage{}, fmt.Errorf("query card revisions: %w", err)
	}
	defer rows.Close()
	page := CardRevisionPage{Revisions: make([]map[string]any, 0, limit)}
	last := 0
	for rows.Next() {
		row, err := scanCardRevisionMeta(rows)
		if err != nil {
			return CardRevisionPage{}, fmt.Errorf("scan card revision: %w", err)
		}
		if len(page.Revisions) == limit {
			page.NextCursor = "r" + strconv.Itoa(last)
			break
		}
		page.Revisions = append(page.Revisions, row.toMap())
		last = row.Number
	}
	if err := rows.Err(); err != nil {
		return CardRevisionPage{}, fmt.Errorf("iterate card revisions: %w", err)
	}
	return page, nil
}

// CardRevisionContent is the decoded content of one revision.
type CardRevisionContent struct {
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	DefinitionOfDone []string `json:"definition_of_done"`
}

func decodeCardRevisionContent(raw []byte) (CardRevisionContent, error) {
	var content CardRevisionContent
	if err := json.Unmarshal(raw, &content); err != nil {
		return CardRevisionContent{}, fmt.Errorf("decode card revision content: %w", err)
	}
	if content.DefinitionOfDone == nil {
		content.DefinitionOfDone = []string{}
	}
	return content, nil
}

type rowsQuerier interface {
	cardcontent.Querier
}

func (s *Store) loadCardRevisionMeta(ctx context.Context, q rowsQuerier, cardID, revisionID string) (cardRevisionMetaRow, error) {
	row, err := scanCardRevisionMeta(q.QueryRowContext(ctx, `SELECT `+cardRevisionMetaColumns+`
		   FROM card_revisions cr
		   JOIN cards c ON c.id = cr.card_id
		  WHERE cr.card_id = ? AND cr.revision_id = ?`, strings.TrimSpace(cardID), strings.TrimSpace(revisionID)))
	if errors.Is(err, sql.ErrNoRows) {
		return cardRevisionMetaRow{}, ErrNotFound
	}
	if err != nil {
		return cardRevisionMetaRow{}, fmt.Errorf("load card revision: %w", err)
	}
	return row, nil
}

func (s *Store) loadCardRevisionContent(ctx context.Context, q rowsQuerier, revisionID string) (CardRevisionContent, error) {
	raw, err := cardcontent.LoadRevisionContent(ctx, q, revisionID)
	if err != nil {
		return CardRevisionContent{}, fmt.Errorf("load card revision content: %w", err)
	}
	return decodeCardRevisionContent(raw)
}

// loadCardRevisionTx loads one revision's metadata and, when asked, its
// content (at most a snapshot and one delta are decoded).
func (s *Store) loadCardRevisionTx(ctx context.Context, q rowsQuerier, cardID, revisionID string, withContent bool) (map[string]any, error) {
	meta, err := s.loadCardRevisionMeta(ctx, q, cardID, revisionID)
	if err != nil {
		return nil, err
	}
	out := meta.toMap()
	if withContent {
		content, err := s.loadCardRevisionContent(ctx, q, revisionID)
		if err != nil {
			return nil, err
		}
		out["title"] = content.Title
		out["summary"] = content.Summary
		out["definition_of_done"] = content.DefinitionOfDone
	}
	return out, nil
}

// GetCardRevision returns one revision of cardID with its full content.
func (s *Store) GetCardRevision(ctx context.Context, cardID, revisionID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if _, err := s.GetBoardCardCurrent(ctx, "", cardID); err != nil {
		return nil, err
	}
	return s.loadCardRevisionTx(ctx, s.db, cardID, revisionID, true)
}

// DiffCardRevisions compares revisionID with baseRevisionID (default: its
// previous revision; the first revision diffs against empty content). Both
// must belong to cardID. The summary diff is bounded by package textdiff.
func (s *Store) DiffCardRevisions(ctx context.Context, cardID, revisionID, baseRevisionID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if _, err := s.GetBoardCardCurrent(ctx, "", cardID); err != nil {
		return nil, err
	}
	target, err := s.loadCardRevisionMeta(ctx, s.db, cardID, revisionID)
	if err != nil {
		return nil, err
	}
	baseRevisionID = strings.TrimSpace(baseRevisionID)
	if baseRevisionID == "" {
		baseRevisionID = strings.TrimSpace(target.PrevRevisionID.String)
	}
	after, err := s.loadCardRevisionContent(ctx, s.db, target.RevisionID)
	if err != nil {
		return nil, err
	}
	before := CardRevisionContent{DefinitionOfDone: []string{}}
	var baseMeta any
	baseLabel := "(empty)"
	if baseRevisionID != "" {
		base, err := s.loadCardRevisionMeta(ctx, s.db, cardID, baseRevisionID)
		if err != nil {
			return nil, err
		}
		if before, err = s.loadCardRevisionContent(ctx, s.db, base.RevisionID); err != nil {
			return nil, err
		}
		m := base.toMap()
		baseMeta = m
		baseLabel = anyStringValue(m["ref"])
	}
	targetMeta := target.toMap()
	out := map[string]any{
		"base_revision": baseMeta,
		"revision":      targetMeta,
	}
	identical := true
	if before.Title != after.Title {
		identical = false
		out["title"] = map[string]any{"before": before.Title, "after": after.Title}
	}
	added, removed := stringSetDelta(before.DefinitionOfDone, after.DefinitionOfDone)
	if len(added) > 0 || len(removed) > 0 {
		identical = false
		out["definition_of_done"] = map[string]any{"added": added, "removed": removed}
	}
	summary := map[string]any{"changed": before.Summary != after.Summary}
	if before.Summary != after.Summary {
		identical = false
		res := textdiff.Lines(baseLabel, before.Summary, anyStringValue(targetMeta["ref"]), after.Summary)
		summary["unified"] = res.Unified
		summary["added_lines"] = res.AddedLines
		summary["removed_lines"] = res.RemovedLines
		summary["minimal"] = res.Minimal
		summary["truncated"] = res.Truncated
	}
	out["summary"] = summary
	out["identical"] = identical
	return out, nil
}

func stringSetDelta(before, after []string) (added, removed []string) {
	in := func(list []string, v string) bool {
		for _, item := range list {
			if item == v {
				return true
			}
		}
		return false
	}
	added, removed = []string{}, []string{}
	for _, v := range after {
		if !in(before, v) {
			added = append(added, v)
		}
	}
	for _, v := range before {
		if !in(after, v) {
			removed = append(removed, v)
		}
	}
	return added, removed
}

// cardRevisionArtifactContent serves GET /artifacts/{id}/content for a card
// revision artifact from the revision record when the record holds exactly
// the bytes the artifact's content_hash names. ok is false for any other
// artifact, which keeps reading its blob.
func (s *Store) cardRevisionArtifactContent(ctx context.Context, artifactID, contentHash string) ([]byte, bool, error) {
	var revisionID string
	err := s.db.QueryRowContext(ctx, `SELECT revision_id FROM card_revisions WHERE artifact_id = ? AND content_hash = ? LIMIT 1`, artifactID, contentHash).Scan(&revisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	raw, err := cardcontent.LoadRevisionContent(ctx, s.db, revisionID)
	if err != nil {
		return nil, false, err
	}
	return raw, true, nil
}
