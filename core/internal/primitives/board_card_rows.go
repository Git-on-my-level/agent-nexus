// boardCardRow and boardCardVersionRow: row structs, scanning and loaders,
// their API map projections, and NULL/optional value handling.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type boardCardRow struct {
	BoardID              string
	BoardHandle          sql.NullString
	CardID               string
	Handle               sql.NullString
	ColumnKey            string
	Rank                 string
	Title                string
	Body                 string
	Version              int
	HeadRevisionID       sql.NullString
	HeadRevisionNumber   int
	ThreadID             sql.NullString
	ParentThreadID       sql.NullString
	DueAt                sql.NullString
	DefinitionOfDoneJSON string
	PinnedDocumentID     sql.NullString
	Assignee             sql.NullString
	Risk                 string
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
	TrashedAt            sql.NullString
	TrashedBy            sql.NullString
	TrashReason          sql.NullString
	ParentCardID         sql.NullString
	NeedsHome            bool
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
			SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
			  FROM ref_edges re
			  LEFT JOIN boards b ON b.id = re.source_id
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id IN (` + strings.Join(placeholders, ", ") + `)
			   AND c.archived_at IS NULL AND c.trashed_at IS NULL
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
			SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
			  FROM ref_edges re
			  LEFT JOIN boards b ON b.id = re.source_id
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND re.target_id = ?`
	args := []any{boardDefaultColumn, refEdgeTypeBoardCard, boardID, identifier}
	if !includeArchived {
		cardQuery += ` AND c.archived_at IS NULL AND c.trashed_at IS NULL`
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
			SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
			  FROM ref_edges re
			  LEFT JOIN boards b ON b.id = re.source_id
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND c.parent_thread_id = ?`
	threadArgs := []any{boardDefaultColumn, refEdgeTypeBoardCard, boardID, identifier}
	if !includeArchived {
		threadQuery += ` AND c.archived_at IS NULL AND c.trashed_at IS NULL`
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
			SELECT re.source_id AS board_id, b.handle AS board_handle, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason, c.parent_card_id, c.needs_home
			  FROM ref_edges re
			  LEFT JOIN boards b ON b.id = re.source_id
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.target_id = ?`
	args := []any{boardDefaultColumn, refEdgeTypeBoardCard, cardID}
	if !includeArchived {
		query += ` AND c.archived_at IS NULL AND c.trashed_at IS NULL`
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

func scanBoardCardRow(scanner interface{ Scan(dest ...any) error }, extra ...any) (boardCardRow, error) {
	row := boardCardRow{}
	if err := scanner.Scan(append([]any{
		&row.BoardID,
		&row.BoardHandle,
		&row.CardID,
		&row.Handle,
		&row.ColumnKey,
		&row.Rank,
		&row.Title,
		&row.Body,
		&row.Version,
		&row.HeadRevisionID,
		&row.HeadRevisionNumber,
		&row.ThreadID,
		&row.ParentThreadID,
		&row.DueAt,
		&row.DefinitionOfDoneJSON,
		&row.PinnedDocumentID,
		&row.Assignee,
		&row.Risk,
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
		&row.TrashedAt,
		&row.TrashedBy,
		&row.TrashReason,
		&row.ParentCardID,
		&row.NeedsHome,
	}, extra...)...); err != nil {
		return boardCardRow{}, fmt.Errorf("scan board card row: %w", err)
	}
	return row, nil
}

type boardCardVersionRow struct {
	RevisionID           string
	CardID               string
	CardHandle           string
	Version              int
	PrevRevisionID       sql.NullString
	ArtifactID           string
	RevisionHash         string
	BoardID              string
	BoardHandle          string
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
	Risk                 string
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
		&row.RevisionID,
		&row.CardID,
		&row.CardHandle,
		&row.Version,
		&row.PrevRevisionID,
		&row.ArtifactID,
		&row.RevisionHash,
		&row.BoardID,
		&row.BoardHandle,
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
		&row.Risk,
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

func (r boardCardRow) toMap() (map[string]any, error) {
	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode board card provenance: %w", err)
		}
	}
	definitionOfDone, err := decodeStoredJSONList(r.DefinitionOfDoneJSON, "card.definition_of_done")
	if err != nil {
		return nil, err
	}
	resolutionRefs, err := decodeStoredJSONList(r.ResolutionRefsJSON, "card.resolution_refs")
	if err != nil {
		return nil, err
	}
	refs, err := decodeStoredJSONList(r.RefsJSON, "card.refs")
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(firstNonEmpty(r.ThreadID.String, r.ParentThreadID.String))
	assigneeRefs := boardCardAssigneeRefs(r.Assignee.String)
	relatedRefs := boardCardRelatedRefs(refs, r.ParentThreadID.String)
	headRevisionNumber := r.HeadRevisionNumber
	if headRevisionNumber == 0 {
		headRevisionNumber = r.Version
	}
	headRevisionID := strings.TrimSpace(r.HeadRevisionID.String)
	cardHandle := firstNonEmpty(strings.TrimSpace(r.Handle.String), r.CardID)
	boardHandle := firstNonEmpty(strings.TrimSpace(r.BoardHandle.String), r.BoardID)
	headRevisionRef := boardTypedRefOrNil("card_revision", headRevisionID)
	if headRevisionID != "" && headRevisionNumber > 0 {
		headRevisionRef = "card_revision:" + revisionHandle(cardHandle, headRevisionNumber)
	}
	m := map[string]any{
		"id":                   r.CardID,
		"ref":                  "card:" + cardHandle,
		"handle":               cardHandle,
		"board_id":             r.BoardID,
		"board_ref":            "board:" + boardHandle,
		"board_handle":         boardHandle,
		"thread_id":            nullableBoardString(threadID),
		"column_key":           r.ColumnKey,
		"rank":                 r.Rank,
		"title":                r.Title,
		"summary":              r.Body,
		"head_revision_ref":    headRevisionRef,
		"head_revision_number": headRevisionNumber,
		"document_ref":         boardTypedRefOrNil("document", r.PinnedDocumentID.String),
		"risk":                 canonicalBoardCardRisk(r.Risk),
		"due_at":               nullableBoardString(r.DueAt.String),
		"definition_of_done":   definitionOfDone,
		"assignee_refs":        assigneeRefs,
		"related_refs":         relatedRefs,
		"resolution":           canonicalizeCardResolutionForAPI(r.Resolution.String),
		"resolution_refs":      resolutionRefs,
		"refs":                 refs,
		"created_at":           r.CreatedAt,
		"created_by":           r.CreatedBy,
		"updated_at":           r.UpdatedAt,
		"updated_by":           r.UpdatedBy,
		"provenance":           provenance,
		"needs_home":           r.NeedsHome,
		"is_initiative":        !r.ParentCardID.Valid && !r.NeedsHome,
	}
	lifecycleFieldsFromSQLColumns(r.ArchivedAt, r.ArchivedBy, r.TrashedAt, r.TrashedBy, r.TrashReason).apply(m)
	return m, nil
}

func (r boardCardVersionRow) toMap() (map[string]any, error) {
	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode board card revision provenance: %w", err)
		}
	}
	threadID := strings.TrimSpace(firstNonEmpty(r.ThreadID.String, r.ParentThreadID.String))
	refs, err := decodeStoredJSONList(r.RefsJSON, "card_revision.refs")
	if err != nil {
		return nil, err
	}
	definitionOfDone, err := decodeStoredJSONList(r.DefinitionOfDoneJSON, "card_revision.definition_of_done")
	if err != nil {
		return nil, err
	}
	resolutionRefs, err := decodeStoredJSONList(r.ResolutionRefsJSON, "card_revision.resolution_refs")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id":                 r.RevisionID,
		"revision_id":        r.RevisionID,
		"card_id":            r.CardID,
		"card_ref":           "card:" + strings.TrimSpace(firstNonEmpty(r.CardHandle, r.CardID)),
		"card_handle":        strings.TrimSpace(firstNonEmpty(r.CardHandle, r.CardID)),
		"revision_number":    r.Version,
		"prev_revision_ref":  boardTypedRefOrNil("card_revision", r.PrevRevisionID.String),
		"artifact_ref":       "artifact:" + strings.TrimSpace(r.ArtifactID),
		"revision_hash":      r.RevisionHash,
		"title":              r.Title,
		"summary":            r.Body,
		"board_id":           r.BoardID,
		"board_ref":          "board:" + strings.TrimSpace(firstNonEmpty(r.BoardHandle, r.BoardID)),
		"board_handle":       strings.TrimSpace(firstNonEmpty(r.BoardHandle, r.BoardID)),
		"thread_id":          nullableBoardString(threadID),
		"document_ref":       boardTypedRefOrNil("document", r.PinnedDocumentID.String),
		"risk":               canonicalBoardCardRisk(r.Risk),
		"due_at":             nullableBoardString(r.DueAt.String),
		"definition_of_done": definitionOfDone,
		"assignee_refs":      boardCardAssigneeRefs(r.Assignee.String),
		"related_refs":       boardCardRelatedRefs(refs, r.ParentThreadID.String),
		"column_key":         r.ColumnKey,
		"rank":               r.Rank,
		"resolution":         canonicalizeCardResolutionForAPI(r.Resolution.String),
		"resolution_refs":    resolutionRefs,
		"refs":               refs,
		"created_at":         r.CreatedAt,
		"created_by":         r.CreatedBy,
		"provenance":         provenance,
	}, nil
}

func boardCardAssigneeRefs(rawAssignee string) []string {
	assignee := strings.TrimSpace(rawAssignee)
	if assignee == "" {
		return []string{}
	}
	if strings.Contains(assignee, ":") {
		return []string{assignee}
	}
	return []string{"actor:" + assignee}
}

func parentThreadIDFromCardRefs(card map[string]any) string {
	var refs []string
	switch v := card["related_refs"].(type) {
	case []string:
		refs = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				refs = append(refs, s)
			}
		}
	}
	backingThread := ""
	if s, ok := card["thread_id"].(string); ok {
		backingThread = strings.TrimSpace(s)
	}
	var firstThreadID string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if !strings.HasPrefix(ref, "thread:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
		if id == "" {
			continue
		}
		if firstThreadID == "" {
			firstThreadID = id
		}
		if backingThread != "" && id != backingThread {
			return id
		}
	}
	return firstThreadID
}

func boardCardRelatedRefs(refs []string, parentThreadID string) []string {
	out := uniqueSortedStrings(refs)
	parentThreadID = strings.TrimSpace(parentThreadID)
	if parentThreadID == "" {
		return out
	}
	threadRef := "thread:" + parentThreadID
	for _, ref := range out {
		if ref == threadRef {
			return out
		}
	}
	out = append(out, threadRef)
	sort.Strings(out)
	return out
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

func nullableStringPtr(value sql.NullString) *string {
	if !value.Valid || value.String == "" {
		return nil
	}
	return &value.String
}

func equalLifecycleString(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
