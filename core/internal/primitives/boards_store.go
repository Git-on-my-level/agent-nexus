package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/blob"
)

var ErrInvalidBoardRequest = errors.New("invalid board request")

const boardSummaryRiskHorizon = 7 * 24 * time.Hour

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

// sqlRowsQuerier is implemented by *sql.DB and *sql.Tx for loading ref_edges sets.
type sqlRowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadBoardTypedRefStringsByBoardIDs(ctx context.Context, q sqlRowsQuerier, boardIDs []string) (map[string][]string, error) {
	out := make(map[string][]string)
	if len(boardIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(boardIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, 1+len(boardIDs))
	args = append(args, refEdgeTypeRef)
	for _, id := range boardIDs {
		args = append(args, strings.TrimSpace(id))
	}
	query := `SELECT source_id, target_type, target_id FROM ref_edges
		WHERE source_type = 'board' AND edge_type = ? AND source_id IN (` + placeholders + `)
		ORDER BY source_id, target_type, target_id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query board typed ref edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, targetType, targetID string
		if err := rows.Scan(&sourceID, &targetType, &targetID); err != nil {
			return nil, fmt.Errorf("scan board ref edge: %w", err)
		}
		targetType = strings.TrimSpace(targetType)
		targetID = strings.TrimSpace(targetID)
		if targetType == "" || targetID == "" {
			continue
		}
		out[sourceID] = append(out[sourceID], makeTypedRef(targetType, targetID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, refs := range out {
		out[id] = uniqueSortedStrings(refs)
	}
	return out, nil
}

func loadBoardCardRefStringsByBoardIDs(ctx context.Context, q sqlRowsQuerier, boardIDs []string) (map[string][]string, error) {
	out := make(map[string][]string)
	if len(boardIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(boardIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, 1+len(boardIDs))
	args = append(args, refEdgeTypeBoardCard)
	for _, id := range boardIDs {
		args = append(args, strings.TrimSpace(id))
	}
	query := `SELECT source_id, target_id FROM ref_edges
		WHERE source_type = 'board' AND edge_type = ? AND source_id IN (` + placeholders + `)
		ORDER BY source_id, target_id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query board card ref edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, cardID string
		if err := rows.Scan(&sourceID, &cardID); err != nil {
			return nil, fmt.Errorf("scan board card edge: %w", err)
		}
		cardID = strings.TrimSpace(cardID)
		if cardID == "" {
			continue
		}
		out[sourceID] = append(out[sourceID], makeTypedRef("card", cardID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, refs := range out {
		out[id] = uniqueSortedStrings(refs)
	}
	return out, nil
}

// boardRowToAPI maps a boards row to the HTTP shape. Typed associations are canonical in
// ref_edges (edge_type=ref for document/topic/thread/…); boards.refs_json is a denormalized copy
// kept in sync on write. Responses prefer ref_edges and fall back to refs_json for pre-migration rows.
func boardRowToAPI(ctx context.Context, q sqlRowsQuerier, row boardRow) (map[string]any, error) {
	typedMap, err := loadBoardTypedRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	cardMap, err := loadBoardCardRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	typedRefs := typedMap[row.ID]
	if len(typedRefs) == 0 {
		typedRefs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
		if err != nil {
			return nil, err
		}
	}
	cardRefs := cardMap[row.ID]
	if cardRefs == nil {
		cardRefs = []string{}
	}
	return row.boardToMapWithRefData(typedRefs, cardRefs)
}

func boardEffectiveTypedRefsForPatch(ctx context.Context, q sqlRowsQuerier, row boardRow) ([]string, error) {
	typedMap, err := loadBoardTypedRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	refs := typedMap[row.ID]
	if len(refs) == 0 {
		refs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}

type BoardListFilter struct {
	States []string

	Owner  string
	Owners []string
	Query  string
	Limit  *int
	Cursor string
}

// CardListFilter scopes global card listing (GET /cards).
type CardListFilter struct {
	States []string
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
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
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

// maxBoardCardsBatchSize caps POST /boards/{id}/cards/batch item count.
const maxBoardCardsBatchSize = 100

type boardCardInsertPrep struct {
	CardID               string
	ColumnKey            string
	Title                string
	Body                 string
	SourceThreadID       string
	BackingThreadID      string
	Refs                 []string
	RefsJSON             []byte
	DueAt                *string
	DefinitionOfDone     []string
	DefinitionOfDoneJSON []byte
	Assignee             *string
	PinnedDocumentID     *string
	RiskValue            string
	Resolution           string
	ResolutionRefs       []string
	ResolutionRefsJSON   []byte
	BeforeCardID         string
	AfterCardID          string
}

type cardRevisionInsert struct {
	RevisionID     string
	RevisionNumber int
	PrevRevisionID sql.NullString
	ArtifactID     string
	RevisionHash   string
	CreatedAt      string
	ContentHash    string
	BlobPlan       blobLedgerWritePlan
	RefsJSON       string
	MetadataJSON   string
	EncodedContent []byte
}

func (s *Store) prepareCardRevisionInsert(ctx context.Context, actorID, cardID string, revisionNumber int, prevRevisionID sql.NullString, threadID, title, summary string, definitionOfDone []string, refs []string) (cardRevisionInsert, blob.StagedWrite, error) {
	if s == nil || s.blob == nil {
		return cardRevisionInsert{}, nil, fmt.Errorf("blob backend is not configured")
	}
	cardID = strings.TrimSpace(cardID)
	actorID = strings.TrimSpace(actorID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionID := uuid.NewString()
	artifactID := revisionID
	content := map[string]any{
		"title":              strings.TrimSpace(title),
		"summary":            strings.TrimSpace(summary),
		"definition_of_done": uniqueSortedStrings(definitionOfDone),
	}
	encodedContent, err := encodeContent(content)
	if err != nil {
		return cardRevisionInsert{}, nil, err
	}
	contentHash := sha256Hex(encodedContent)
	blobPlan := blobLedgerWritePlan{contentHash: contentHash, sizeBytes: int64(len(encodedContent))}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1, revisions: 1}, blobPlan); err != nil {
		return cardRevisionInsert{}, nil, err
	}
	revisionRefs := append([]string(nil), refs...)
	if strings.TrimSpace(threadID) != "" {
		revisionRefs = append(revisionRefs, "thread:"+strings.TrimSpace(threadID))
	}
	revisionRefs = append(revisionRefs, "card:"+cardID)
	revisionRefs = uniqueSortedStrings(revisionRefs)
	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return cardRevisionInsert{}, nil, fmt.Errorf("marshal card revision refs: %w", err)
	}
	prevID := ""
	if prevRevisionID.Valid {
		prevID = strings.TrimSpace(prevRevisionID.String)
	}
	revisionHash := computeRevisionHash(contentHash, prevID, cardID, revisionNumber, now, actorID)
	artifactMetadata := map[string]any{
		"id":                 artifactID,
		"kind":               "card",
		"created_at":         now,
		"created_by":         actorID,
		"content_type":       "structured",
		"content_hash":       contentHash,
		"refs":               revisionRefs,
		"card_id":            cardID,
		"revision_id":        revisionID,
		"revision_number":    revisionNumber,
		"prev_revision_id":   nil,
		"title":              strings.TrimSpace(title),
		"summary":            strings.TrimSpace(summary),
		"definition_of_done": uniqueSortedStrings(definitionOfDone),
	}
	if prevID != "" {
		artifactMetadata["prev_revision_id"] = prevID
	}
	metadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return cardRevisionInsert{}, nil, fmt.Errorf("marshal card artifact metadata: %w", err)
	}
	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return cardRevisionInsert{}, nil, fmt.Errorf("stage card content: %w", err)
	}
	return cardRevisionInsert{
		RevisionID:     revisionID,
		RevisionNumber: revisionNumber,
		PrevRevisionID: prevRevisionID,
		ArtifactID:     artifactID,
		RevisionHash:   revisionHash,
		CreatedAt:      now,
		ContentHash:    contentHash,
		BlobPlan:       blobPlan,
		RefsJSON:       string(refsJSON),
		MetadataJSON:   string(metadataJSON),
		EncodedContent: encodedContent,
	}, stagedContent, nil
}

func (s *Store) insertCardRevisionTx(ctx context.Context, tx *sql.Tx, actorID, cardID, threadID string, revision cardRevisionInsert) error {
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", "card-revision", "artifact-"+revision.ArtifactID)
	if err != nil {
		return fmt.Errorf("allocate card artifact handle: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, 'card', ?, ?, ?, 'structured', ?, ?, ?)`,
		revision.ArtifactID,
		artifactHandle,
		nullableString(threadID),
		revision.CreatedAt,
		actorID,
		revision.ContentHash,
		revision.RefsJSON,
		revision.MetadataJSON,
	); err != nil {
		return fmt.Errorf("insert card artifact: %w", err)
	}
	revisionRefs, err := decodeStoredJSONList(revision.RefsJSON, "card_revision.refs")
	if err != nil {
		return err
	}
	if err := replaceRefEdges(ctx, tx, "artifact", revision.ArtifactID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		return err
	}
	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, revision.BlobPlan); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO card_revisions(
			revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revision.RevisionID,
		cardID,
		revision.RevisionNumber,
		nullableString(revision.PrevRevisionID.String),
		revision.ArtifactID,
		nullableString(threadID),
		revision.RefsJSON,
		revision.RevisionHash,
		revision.CreatedAt,
		actorID,
	); err != nil {
		return fmt.Errorf("insert card revision: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "card_revision", revision.RevisionID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		return err
	}
	return nil
}

func prepareBoardCardInsert(input AddBoardCardInput) (boardCardInsertPrep, error) {
	cardID := strings.TrimSpace(input.CardID)
	if cardID == "" {
		cardID = uuid.NewString()
	}
	if err := validateCardID(cardID); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}

	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		columnKey = boardDefaultColumn
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}

	refs := uniqueSortedStrings(input.Refs)
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card refs: %w", err)
	}

	sourceThreadID := strings.TrimSpace(input.ParentThreadID)
	if sourceThreadID == "" {
		derived, derr := SoleThreadRefIDFromRefs(refs)
		if derr != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(derr)
		}
		sourceThreadID = derived
	}
	if sourceThreadID != "" {
		if err := validateThreadID(sourceThreadID); err != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(err)
		}
	}
	backingThreadID := uuid.NewString()

	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	dueAt := normalizeBoardOptionalPointer(input.DueAt)
	definitionOfDone := uniqueSortedStrings(input.DefinitionOfDone)
	definitionOfDoneJSON, err := json.Marshal(definitionOfDone)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card definition_of_done: %w", err)
	}
	resolution := ""
	if input.Resolution != nil {
		resolution = normalizeIncomingCardResolution(strings.TrimSpace(*input.Resolution))
	}
	resolutionRefs := uniqueSortedStrings(input.ResolutionRefs)
	if columnKey == "done" && resolution == "" && len(resolutionRefs) > 0 {
		resolution = "done"
	}
	if err := validateCardResolution(resolution, true); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}
	if columnKey == "done" {
		if err := validateCardResolution(resolution, false); err != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(err)
		}
		if len(resolutionRefs) == 0 {
			return boardCardInsertPrep{}, invalidBoardRequest("done column requires resolution_refs")
		}
		if !containsTypedRefPrefix(resolutionRefs, "artifact") && !containsTypedRefPrefix(resolutionRefs, "event") {
			return boardCardInsertPrep{}, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	}
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	assignee := normalizeBoardOptionalPointer(input.Assignee)
	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)
	riskValue := canonicalBoardCardRisk("")
	if input.Risk != nil {
		riskValue = canonicalBoardCardRisk(*input.Risk)
	}
	if err := rejectReservedCardAssignee(assignee); err != nil {
		return boardCardInsertPrep{}, err
	}

	return boardCardInsertPrep{
		CardID:               cardID,
		ColumnKey:            columnKey,
		Title:                title,
		Body:                 body,
		SourceThreadID:       sourceThreadID,
		BackingThreadID:      backingThreadID,
		Refs:                 refs,
		RefsJSON:             refsJSON,
		DueAt:                dueAt,
		DefinitionOfDone:     definitionOfDone,
		DefinitionOfDoneJSON: definitionOfDoneJSON,
		Assignee:             assignee,
		PinnedDocumentID:     pinnedDocumentID,
		RiskValue:            riskValue,
		Resolution:           resolution,
		ResolutionRefs:       resolutionRefs,
		ResolutionRefsJSON:   resolutionRefsJSON,
		BeforeCardID:         input.BeforeCardID,
		AfterCardID:          input.AfterCardID,
	}, nil
}

func (s *Store) execBoardCardInsert(ctx context.Context, tx *sql.Tx, boardRow boardRow, actorID, boardID string, prep boardCardInsertPrep) (boardRow, boardCardRow, blob.StagedWrite, error) {
	cardID := prep.CardID
	columnKey := prep.ColumnKey
	sourceThreadID := prep.SourceThreadID
	title := prep.Title
	body := prep.Body
	dueAt := prep.DueAt
	assignee := prep.Assignee
	pinnedDocumentID := prep.PinnedDocumentID
	riskValue := prep.RiskValue
	resolutionStr := prep.Resolution
	refs := prep.Refs
	refsJSON := prep.RefsJSON
	definitionOfDone := prep.DefinitionOfDone
	definitionOfDoneJSON := prep.DefinitionOfDoneJSON
	resolutionRefsJSON := prep.ResolutionRefsJSON
	backingThreadID := prep.BackingThreadID

	if sourceThreadID != "" {
		if err := ensureThreadExists(ctx, tx, sourceThreadID); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
		if sourceThreadID == boardRow.ThreadID {
			return boardRow, boardCardRow{}, nil, invalidBoardRequest("board.thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, sourceThreadID, ""); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
	}
	if title == "" {
		if derivedTitle, err := loadThreadTitleForBoardCard(ctx, tx, sourceThreadID); err == nil {
			title = derivedTitle
		} else if !errors.Is(err, ErrNotFound) {
			return boardRow, boardCardRow{}, nil, err
		}
	}
	if title == "" {
		title = cardID
	}
	if title == "" {
		return boardRow, boardCardRow{}, nil, invalidBoardRequest("card.title is required")
	}
	if pinnedDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *pinnedDocumentID); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
	}

	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, prep.BeforeCardID, prep.AfterCardID)
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, "")
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	provenanceJSON := inferredProvenanceJSON()
	if err := ensureCardBackingThreadTx(ctx, tx, actorID, cardID, backingThreadID, title, now); err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	revision, stagedContent, err := s.prepareCardRevisionInsert(ctx, actorID, cardID, 1, sql.NullString{}, backingThreadID, title, body, definitionOfDone, refs)
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	cardHandle, err := uniqueHandleTx(ctx, tx, "card", title, "card-"+cardID)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, fmt.Errorf("allocate card handle: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO cards(
			id, handle, board_id, thread_id, title, summary, due_at, definition_of_done_json, column_key, rank, version, head_revision_id, head_revision_number,
			parent_thread_id, pinned_document_id, assignee, risk, resolution, resolution_refs_json, refs_json,
			created_at, created_by, updated_at, updated_by, provenance_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		cardHandle,
		boardID,
		backingThreadID,
		title,
		body,
		nullableString(derefBoardString(dueAt)),
		string(definitionOfDoneJSON),
		columnKey,
		rank,
		1,
		revision.RevisionID,
		revision.RevisionNumber,
		nullableString(sourceThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		riskValue,
		nullableString(resolutionStr),
		string(resolutionRefsJSON),
		string(refsJSON),
		now,
		actorID,
		now,
		actorID,
		provenanceJSON,
	); err != nil {
		if isUniqueViolation(err) {
			return boardRow, boardCardRow{}, stagedContent, ErrConflict
		}
		return boardRow, boardCardRow{}, stagedContent, fmt.Errorf("insert card: %w", err)
	}

	if err := s.insertCardRevisionTx(ctx, tx, actorID, cardID, backingThreadID, revision); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}

	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardID, columnKey, rank); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	cardTargets := typedRefEdgeTargets(refEdgeTypeRef, refs)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardParentThread, "thread", sourceThreadID)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", derefBoardString(pinnedDocumentID))
	if err := replaceRefEdges(ctx, tx, "card", cardID, cardTargets); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	cardRow, err := s.loadBoardCardByIdentifier(ctx, tx, boardID, cardID, true)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	return boardRow, cardRow, stagedContent, nil
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
	Handle           sql.NullString
	Title            string
	Summary          string
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

type boardCardRow struct {
	BoardID              string
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
			id, handle, title, summary, owners_json, thread_id, refs_json,
			column_schema_json, created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		boardID,
		boardHandle,
		title,
		summary,
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

func (s *Store) GetBoard(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	return boardRowToAPI(ctx, s.db, row)
}

func (s *Store) GetBoardSummary(ctx context.Context, boardID string) (map[string]any, error) {
	row, err := s.getBoardRow(ctx, boardID)
	if err != nil {
		return nil, err
	}
	summaries, err := s.computeBoardSummaries(ctx, []boardRow{row}, nil)
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
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		return boardRowToAPI(ctx, s.db, row)
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, strings.TrimSpace(actorID), strings.TrimSpace(reason), boardID,
	); err != nil {
		return nil, fmt.Errorf("trash board: %w", err)
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
	if _, err := s.db.ExecContext(ctx,
		`UPDATE boards SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`,
		boardID,
	); err != nil {
		return nil, fmt.Errorf("restore board: %w", err)
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
		SET title = ?, summary = ?, owners_json = ?, refs_json = ?,
		    column_schema_json = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`
	args := []any{
		nextTitle,
		nextSummary,
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

func (s *Store) ListBoards(ctx context.Context, filter BoardListFilter) ([]BoardListItem, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
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

	boardIDs := make([]string, 0, len(boardRows))
	for _, row := range boardRows {
		boardIDs = append(boardIDs, row.ID)
	}
	typedByBoard, err := loadBoardTypedRefStringsByBoardIDs(ctx, s.db, boardIDs)
	if err != nil {
		return nil, "", err
	}
	cardByBoard, err := loadBoardCardRefStringsByBoardIDs(ctx, s.db, boardIDs)
	if err != nil {
		return nil, "", err
	}

	summaries, err := s.computeBoardSummaries(ctx, boardRows, typedByBoard)
	if err != nil {
		return nil, "", err
	}

	out := make([]BoardListItem, 0, len(boardRows))
	for _, row := range boardRows {
		typedRefs := typedByBoard[row.ID]
		if len(typedRefs) == 0 {
			typedRefs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
			if err != nil {
				return nil, "", err
			}
		}
		cardRefs := cardByBoard[row.ID]
		if cardRefs == nil {
			cardRefs = []string{}
		}
		board, err := row.boardToMapWithRefData(typedRefs, cardRefs)
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
	filter.States = NormalizeListLifecycleStates(filter.States)
	whereSQL := LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT board_id, id, column_key, rank, title, summary, version, head_revision_id, head_revision_number, thread_id, parent_thread_id, due_at,
		        definition_of_done_json, pinned_document_id, assignee, risk, resolution, resolution_refs_json, refs_json,
		        created_at, created_by, updated_at, updated_by, provenance_json, archived_at, archived_by,
		        trashed_at, trashed_by, trash_reason
		   FROM cards
		  WHERE `+whereSQL+`
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

// SoleThreadRefIDFromRefs returns the thread id when refs contains exactly one distinct thread: ref.
func SoleThreadRefIDFromRefs(refs []string) (string, error) {
	var out string
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if !strings.HasPrefix(r, "thread:") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(r, "thread:"))
		if id == "" {
			continue
		}
		if out != "" && out != id {
			return "", fmt.Errorf("ambiguous thread refs in refs")
		}
		out = id
	}
	return out, nil
}

func (s *Store) CreateBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return BoardCardMutationResult{}, err
			}
		}
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	prep, err := prepareBoardCardInsert(input)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card create transaction: %w", err)
	}
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	boardRow, cardRow, stagedContent, err := s.execBoardCardInsert(ctx, tx, boardRow, actorID, boardID, prep)
	if err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card create transaction: %w", err)
	}
	if stagedContent != nil {
		if err := stagedContent.Promote(); err != nil {
			return BoardCardMutationResult{}, fmt.Errorf("finalize card content: %w", err)
		}
	}
	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) CreateBoardCardsBatch(ctx context.Context, actorID, boardID string, ifBoard *string, inputs []AddBoardCardInput) ([]BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return nil, err
			}
		}
	}
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	if len(inputs) == 0 {
		return nil, invalidBoardRequest("items must include at least one card")
	}
	if len(inputs) > maxBoardCardsBatchSize {
		return nil, invalidBoardRequest(fmt.Sprintf("items exceeds maximum batch size of %d", maxBoardCardsBatchSize))
	}
	preps := make([]boardCardInsertPrep, len(inputs))
	for i := range inputs {
		in := inputs[i]
		in.IfBoardUpdatedAt = nil
		prep, err := prepareBoardCardInsert(in)
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		preps[i] = prep
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin board cards batch transaction: %w", err)
	}
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, ifBoard); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	results := make([]BoardCardMutationResult, 0, len(preps))
	stagedWrites := make([]blob.StagedWrite, 0, len(preps))
	for i, prep := range preps {
		var cardRow boardCardRow
		var stagedContent blob.StagedWrite
		boardRow, cardRow, stagedContent, err = s.execBoardCardInsert(ctx, tx, boardRow, actorID, boardID, prep)
		if err != nil {
			if stagedContent != nil {
				_ = stagedContent.Cleanup()
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		if stagedContent != nil {
			stagedWrites = append(stagedWrites, stagedContent)
		}
		boardMap, terr := boardRowToAPI(ctx, tx, boardRow)
		if terr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			return nil, terr
		}
		cardMap, terr := cardRow.toMap()
		if terr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			return nil, terr
		}
		results = append(results, BoardCardMutationResult{Board: boardMap, Card: cardMap})
	}
	if err := tx.Commit(); err != nil {
		for _, staged := range stagedWrites {
			_ = staged.Cleanup()
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("commit board cards batch transaction: %w", err)
	}
	for _, staged := range stagedWrites {
		if err := staged.Promote(); err != nil {
			return nil, fmt.Errorf("finalize card content: %w", err)
		}
	}
	return results, nil
}

func (s *Store) AddBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	var boardRow boardRow
	if err := ensureBoardCardMutable(cardRow); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if input.Title != nil || input.Body != nil || input.DefinitionOfDone != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequest("card content must be changed with POST /cards/{card_id}/revisions")
	}
	if strings.TrimSpace(boardID) == "" {
		if err := ensureUpdatedAtMatches(cardRow.UpdatedAt, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
		if strings.TrimSpace(boardID) != "" {
			boardRow, err = loadBoardRow(ctx, tx, boardID)
			if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("tx rollback failed: %v", rbErr)
				}
				return BoardCardMutationResult{}, err
			}
		}
	} else {
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}

	nextTitle := cardRow.Title
	nextBody := cardRow.Body
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
	nextAssignee := strings.TrimSpace(cardRow.Assignee.String)
	if input.Assignee != nil {
		nextAssignee = strings.TrimSpace(*input.Assignee)
		if err := rejectReservedCardAssignee(&nextAssignee); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	nextResolutionRefsJSON := cardRow.ResolutionRefsJSON
	if input.ResolutionRefs != nil {
		resolutionRefs := uniqueSortedStrings(*input.ResolutionRefs)
		resolutionRefsBytes, marshalErr := json.Marshal(resolutionRefs)
		if marshalErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("marshal card resolution refs: %w", marshalErr)
		}
		nextResolutionRefsJSON = string(resolutionRefsBytes)
	}
	nextRefsJSON := cardRow.RefsJSON
	if input.Refs != nil {
		refs := uniqueSortedStrings(*input.Refs)
		refsBytes, marshalErr := json.Marshal(refs)
		if marshalErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("marshal card refs: %w", marshalErr)
		}
		nextRefsJSON = string(refsBytes)
	}
	nextRisk := canonicalBoardCardRisk(cardRow.Risk)
	if input.Risk != nil {
		nextRisk = canonicalBoardCardRisk(*input.Risk)
	}

	if strings.TrimSpace(cardRow.ColumnKey) == "done" && nextResolution == "" {
		var refs []string
		if err := json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err == nil && len(uniqueSortedStrings(refs)) > 0 {
			nextResolution = "done"
		}
	}
	columnKey := strings.TrimSpace(cardRow.ColumnKey)
	if columnKey == "done" {
		if err := validateCardResolution(nextResolution, false); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequestError(err)
		}
		var refs []string
		if err := json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("unmarshal resolution refs: %w", err)
		}
		refs = uniqueSortedStrings(refs)
		if len(refs) == 0 {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("done column requires resolution_refs")
		}
		if !containsTypedRefPrefix(refs, "artifact") && !containsTypedRefPrefix(refs, "event") {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	} else if nextResolution != "" {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequest("resolution must be null when column_key is not done")
	}

	if nextParentThread != "" {
		if err := ensureThreadExists(ctx, tx, nextParentThread); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if nextParentThread == boardRow.ThreadID {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("board.thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, nextParentThread, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}
	if nextPinnedDocumentID != "" {
		if err := ensureDocumentExists(ctx, tx, nextPinnedDocumentID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}
	if nextTitle == cardRow.Title &&
		nextBody == cardRow.Body &&
		nextParentThread == strings.TrimSpace(cardRow.ParentThreadID.String) &&
		nextAssignee == strings.TrimSpace(cardRow.Assignee.String) &&
		nextPinnedDocumentID == strings.TrimSpace(cardRow.PinnedDocumentID.String) &&
		nextThreadID == strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String)) &&
		nextDueAt == strings.TrimSpace(cardRow.DueAt.String) &&
		nextDefinitionOfDoneJSON == cardRow.DefinitionOfDoneJSON &&
		nextResolution == strings.TrimSpace(cardRow.Resolution.String) &&
		nextResolutionRefsJSON == cardRow.ResolutionRefsJSON &&
		nextRefsJSON == cardRow.RefsJSON &&
		nextRisk == canonicalBoardCardRisk(cardRow.Risk) {
		boardMap, mapErr := boardRowToAPI(ctx, tx, boardRow)
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cards
		    SET board_id = ?, thread_id = ?, title = ?, summary = ?, due_at = ?, definition_of_done_json = ?, column_key = ?, rank = ?,
		        parent_thread_id = ?, pinned_document_id = ?, assignee = ?, risk = ?, resolution = ?, resolution_refs_json = ?, refs_json = ?,
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
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nextRisk,
		nullableString(nextResolution),
		nextResolutionRefsJSON,
		nextRefsJSON,
		now,
		actorID,
		cardRow.CardID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("update board card: %w", err)
	}
	var refsForEdges []string
	if err := json.Unmarshal([]byte(nextRefsJSON), &refsForEdges); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("unmarshal card refs for ref_edges: %w", err)
	}
	refsForEdges = uniqueSortedStrings(refsForEdges)
	cardTargets := typedRefEdgeTargets(refEdgeTypeRef, refsForEdges)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardParentThread, "thread", nextParentThread)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", nextPinnedDocumentID)
	if err := replaceRefEdges(ctx, tx, "card", cardRow.CardID, cardTargets); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, cardRow.ColumnKey, cardRow.Rank); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card update transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
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
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID); err != nil {
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
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	} else {
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	}
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardCardMutable(cardRow); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, input.BeforeCardID, input.AfterCardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := validateBoardAnchors(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	nextResolution, nextResolutionRefsJSON, updateCard, err := resolveBoardCardMoveResolution(cardRow, columnKey, input)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, columnKey, rank); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if updateCard {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE cards
			    SET board_id = ?, thread_id = ?, column_key = ?, rank = ?, resolution = ?, resolution_refs_json = ?,
			        updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			boardID,
			firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String),
			columnKey,
			rank,
			nullableString(nextResolution),
			nextResolutionRefsJSON,
			now,
			actorID,
			cardRow.CardID,
		); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("update board card resolution: %w", err)
		}
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	} else {
		cardRow.ColumnKey = columnKey
		cardRow.Rank = rank
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card move transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
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
		RemovedThreadID: parentThreadIDFromCardRefs(card.Card),
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	boardID = cardRow.BoardID
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != "" {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, ErrAlreadyTrashed
	}
	if cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != "" {
		boardMap, mapErr := boardRowToAPI(ctx, tx, boardRow)
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET archived_at = ?, archived_by = ? WHERE id = ?`, now, actorID, cardRow.CardID); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("archive board card: %w", err)
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	cardRow.ArchivedAt = sql.NullString{String: now, Valid: true}
	cardRow.ArchivedBy = sql.NullString{String: actorID, Valid: true}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card archive transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	boardID = cardRow.BoardID
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	trashed := cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != ""
	archived := cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != ""
	if !trashed && !archived {
		boardMap, mapErr := boardRowToAPI(ctx, tx, boardRow)
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	if trashed {
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("restore trashed board card: %w", err)
		}
		cardRow.TrashedAt = sql.NullString{}
		cardRow.TrashedBy = sql.NullString{}
		cardRow.TrashReason = sql.NullString{}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET archived_at = NULL, archived_by = NULL WHERE id = ?`, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("restore board card: %w", err)
		}
		cardRow.ArchivedAt = sql.NullString{}
		cardRow.ArchivedBy = sql.NullString{}
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card restore transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
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
	archived := cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != ""
	trashed := cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != ""
	if !archived && !trashed {
		return ErrNotArchived
	}
	cardID := strings.TrimSpace(cardRow.CardID)

	boardRow, err := loadBoardRow(ctx, tx, cardRow.BoardID)
	if err != nil {
		return err
	}

	revisionRows, err := tx.QueryContext(ctx,
		`SELECT cr.revision_id, cr.artifact_id, a.content_hash
		   FROM card_revisions cr
		   JOIN artifacts a ON a.id = cr.artifact_id
		  WHERE cr.card_id = ?`,
		cardID,
	)
	if err != nil {
		return fmt.Errorf("query card revision artifacts for purge: %w", err)
	}
	var revisionIDs, artifactIDs, contentHashes []string
	for revisionRows.Next() {
		var revisionID, artifactID, contentHash string
		if err := revisionRows.Scan(&revisionID, &artifactID, &contentHash); err != nil {
			_ = revisionRows.Close()
			return fmt.Errorf("scan card revision artifact for purge: %w", err)
		}
		revisionIDs = append(revisionIDs, strings.TrimSpace(revisionID))
		artifactIDs = append(artifactIDs, strings.TrimSpace(artifactID))
		contentHashes = append(contentHashes, strings.TrimSpace(contentHash))
	}
	if err := revisionRows.Close(); err != nil {
		return fmt.Errorf("close card revision artifact rows: %w", err)
	}
	if err := revisionRows.Err(); err != nil {
		return fmt.Errorf("iterate card revision artifacts for purge: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card source ref edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card target ref edges: %w", err)
	}
	for _, revisionID := range revisionIDs {
		if revisionID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE (source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)`, "card_revision", revisionID, "card_revision", revisionID); err != nil {
			return fmt.Errorf("delete card revision ref edges: %w", err)
		}
	}
	for _, artifactID := range artifactIDs {
		if artifactID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE (source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)`, "artifact", artifactID, "artifact", artifactID); err != nil {
			return fmt.Errorf("delete card artifact ref edges: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM card_revisions WHERE card_id = ?`, cardID); err != nil {
		return fmt.Errorf("delete card revisions: %w", err)
	}
	for _, artifactID := range artifactIDs {
		if artifactID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
			return fmt.Errorf("delete card revision artifact: %w", err)
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE id = ? AND (archived_at IS NOT NULL OR trashed_at IS NOT NULL)`, cardID)
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

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actors.SystemActorID)
	if err != nil {
		return err
	}
	blobHashesToDelete := make([]string, 0)
	for _, contentHash := range uniqueSortedStrings(contentHashes) {
		if contentHash == "" {
			continue
		}
		var cnt int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifacts WHERE content_hash = ?`, contentHash).Scan(&cnt); err != nil {
			return fmt.Errorf("count card blob references: %w", err)
		}
		if cnt == 0 {
			if err := s.removeBlobLedgerEntryTx(ctx, tx, contentHash); err != nil {
				return err
			}
			blobHashesToDelete = append(blobHashesToDelete, contentHash)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge card transaction: %w", err)
	}
	for _, contentHash := range blobHashesToDelete {
		if contentHash == "" || s.blob == nil {
			continue
		}
		if err := s.blob.Delete(ctx, contentHash); err != nil && !errors.Is(err, blob.ErrBlobNotFound) {
			return fmt.Errorf("delete card blob object: %w", err)
		}
	}
	return nil
}

// TrashBoardCard records an operational soft-delete and clears archive columns (distinct soft-delete lanes).
func (s *Store) TrashBoardCard(ctx context.Context, actorID, boardID, identifier, reason string, input RemoveBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card trash transaction: %w", err)
	}

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	boardID = cardRow.BoardID
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != "" {
		boardMap, mapErr := boardRowToAPI(ctx, tx, boardRow)
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap["_mutation_applied"] = false
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	reason = strings.TrimSpace(reason)
	if _, err := tx.ExecContext(ctx,
		`UPDATE cards SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, reason, cardRow.CardID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("trash board card: %w", err)
	}
	cardRow.TrashedAt = sql.NullString{String: now, Valid: true}
	cardRow.TrashedBy = sql.NullString{String: actorID, Valid: true}
	if reason != "" {
		cardRow.TrashReason = sql.NullString{String: reason, Valid: true}
	} else {
		cardRow.TrashReason = sql.NullString{}
	}
	cardRow.ArchivedAt = sql.NullString{}
	cardRow.ArchivedBy = sql.NullString{}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card trash transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap["_mutation_applied"] = true
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func (s *Store) ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	return s.listBoardCardHistoryTx(ctx, s.db, cardID)
}

func (s *Store) listBoardCardHistoryTx(ctx context.Context, q sqlRowsQuerier, cardID string) ([]map[string]any, error) {
	rows, err := q.QueryContext(
		ctx,
		`SELECT cr.revision_id, cr.card_id, cr.revision_number, cr.prev_revision_id, cr.artifact_id, cr.revision_hash,
		        c.board_id, cr.thread_id,
		        COALESCE(json_extract(a.metadata_json, '$.title'), c.title),
		        COALESCE(json_extract(a.metadata_json, '$.summary'), c.summary),
		        c.due_at,
		        COALESCE(json_extract(a.metadata_json, '$.definition_of_done'), c.definition_of_done_json),
		        c.column_key, c.rank, c.parent_thread_id, c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        cr.created_at, cr.created_by, c.provenance_json
		   FROM card_revisions cr
		   JOIN cards c ON c.id = cr.card_id
		   JOIN artifacts a ON a.id = cr.artifact_id
		  WHERE cr.card_id = ?
		  ORDER BY cr.revision_number ASC`,
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
		version, mapErr := versionRow.toMap()
		if mapErr != nil {
			return nil, mapErr
		}
		out = append(out, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board card history: %w", err)
	}
	return out, nil
}

type CreateCardRevisionInput struct {
	Title            *string
	Summary          *string
	DefinitionOfDone *[]string
	IfBaseRevision   *string
}

func (s *Store) CreateCardRevision(ctx context.Context, actorID, cardID string, input CreateCardRevisionInput) (BoardCardMutationResult, map[string]any, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return BoardCardMutationResult{}, nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return BoardCardMutationResult{}, nil, err
			}
		}
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return BoardCardMutationResult{}, nil, invalidBoardRequest("actorID is required")
	}
	cardID = strings.TrimSpace(cardID)
	if cardID == "" {
		return BoardCardMutationResult{}, nil, invalidBoardRequest("card_id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, nil, fmt.Errorf("begin card revision transaction: %w", err)
	}
	cardRow, err := s.loadBoardCardByGlobalID(ctx, tx, cardID, true)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := ensureBoardCardMutable(cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	baseRevisionID := strings.TrimSpace(cardRow.HeadRevisionID.String)
	if input.IfBaseRevision != nil {
		expected := strings.TrimSpace(*input.IfBaseRevision)
		if resolved, err := resolveRevisionResourceRef(ctx, tx, "card_revision", strings.TrimPrefix(expected, "card_revision:")); err == nil {
			expected = resolved.ID
		} else {
			expected = strings.TrimPrefix(expected, "card_revision:")
		}
		if expected != "" && baseRevisionID != "" && expected != baseRevisionID {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, nil, ErrConflict
		}
	}
	nextTitle := cardRow.Title
	if input.Title != nil {
		nextTitle = strings.TrimSpace(*input.Title)
	}
	if nextTitle == "" {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, invalidBoardRequest("card.title must not be empty")
	}
	nextSummary := cardRow.Body
	if input.Summary != nil {
		nextSummary = strings.TrimSpace(*input.Summary)
	}
	nextDefinition, err := decodeStoredJSONList(cardRow.DefinitionOfDoneJSON, "card.definition_of_done")
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if input.DefinitionOfDone != nil {
		nextDefinition = uniqueSortedStrings(*input.DefinitionOfDone)
	}
	nextDefinitionJSON, err := json.Marshal(nextDefinition)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, fmt.Errorf("marshal card definition_of_done: %w", err)
	}
	nextRevisionNumber := cardRow.Version + 1
	if cardRow.HeadRevisionNumber > 0 {
		nextRevisionNumber = cardRow.HeadRevisionNumber + 1
	}
	prevRevision := sql.NullString{String: baseRevisionID, Valid: baseRevisionID != ""}
	threadID := strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String))
	nextRefs, err := decodeStoredJSONList(cardRow.RefsJSON, "card.refs")
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	revision, stagedContent, err := s.prepareCardRevisionInsert(ctx, actorID, cardRow.CardID, nextRevisionNumber, prevRevision, threadID, nextTitle, nextSummary, nextDefinition, nextRefs)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := s.insertCardRevisionTx(ctx, tx, actorID, cardRow.CardID, threadID, revision); err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx,
		`UPDATE cards
		    SET title = ?, summary = ?, definition_of_done_json = ?, version = ?, head_revision_id = ?, head_revision_number = ?, updated_at = ?, updated_by = ?
		  WHERE id = ?`,
		nextTitle, nextSummary, string(nextDefinitionJSON), nextRevisionNumber, revision.RevisionID, nextRevisionNumber, now, actorID, cardRow.CardID,
	); err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, fmt.Errorf("update card revision head: %w", err)
	}
	boardRow, err := loadBoardRow(ctx, tx, cardRow.BoardID)
	if err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, cardRow.CardID, true)
	if err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	revisions, err := s.listBoardCardHistoryTx(ctx, tx, cardRow.CardID)
	if err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = stagedContent.Cleanup()
		return BoardCardMutationResult{}, nil, fmt.Errorf("commit card revision transaction: %w", err)
	}
	if err := stagedContent.Promote(); err != nil {
		return BoardCardMutationResult{}, nil, fmt.Errorf("finalize card content: %w", err)
	}
	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, nil, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, nil, err
	}
	revisionMap := map[string]any{}
	if len(revisions) > 0 {
		revisionMap = revisions[len(revisions)-1]
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, revisionMap, nil
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
		`SELECT b.id, b.title, b.archived_at, b.trashed_at, re.source_id, re.target_id, json_extract(re.metadata_json, '$.column_key'), c.title, c.resolution, c.parent_thread_id, c.pinned_document_id, c.due_at, c.updated_at
		   FROM ref_edges re
		   JOIN boards b ON b.id = re.source_id
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND c.parent_thread_id = ?
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
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
			boardArchivedAt  sql.NullString
			boardTrashedAt   sql.NullString
			cardBoardID      string
			cardID           string
			columnKey        sql.NullString
			cardTitle        string
			cardResolution   sql.NullString
			parentThreadID   sql.NullString
			pinnedDocumentID sql.NullString
			dueAt            sql.NullString
			updatedAt        string
		)
		if err := rows.Scan(&boardID, &title, &boardArchivedAt, &boardTrashedAt, &cardBoardID, &cardID, &columnKey, &cardTitle, &cardResolution, &parentThreadID, &pinnedDocumentID, &dueAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan board membership: %w", err)
		}
		out = append(out, BoardMembership{
			Board: map[string]any{
				"id":    boardID,
				"title": title,
				"state": canonicalLifecycleState(boardArchivedAt, boardTrashedAt),
			},
			Card: map[string]any{
				"board_id":     cardBoardID,
				"board_ref":    "board:" + strings.TrimSpace(cardBoardID),
				"id":           cardID,
				"thread_id":    nullableBoardString(parentThreadID.String),
				"title":        cardTitle,
				"resolution":   canonicalizeCardResolutionForAPI(cardResolution.String),
				"column_key":   nullableBoardString(columnKey.String),
				"related_refs": boardCardRelatedRefs(nil, parentThreadID.String),
				"document_ref": boardTypedRefOrNil("document", pinnedDocumentID.String),
				"due_at":       nullableBoardString(dueAt.String),
				"updated_at":   updatedAt,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate board memberships: %w", err)
	}
	return out, nil
}

func (s *Store) computeBoardSummaries(ctx context.Context, boards []boardRow, typedRefsByBoard map[string][]string) (map[string]map[string]any, error) {
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
	var err error
	if typedRefsByBoard == nil {
		typedRefsByBoard, err = loadBoardTypedRefStringsByBoardIDs(ctx, s.db, boardIDs)
		if err != nil {
			return nil, err
		}
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

		refs := typedRefsByBoard[board.ID]
		if len(refs) == 0 {
			refs, err = decodeStoredJSONList(board.RefsJSON, "board.refs")
			if err != nil {
				return nil, err
			}
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
			"has_document_refs":     len(boardDocumentRefsFromRefs(refs)) > 0,
		}
	}

	return summaries, nil
}

// BoardCardIsOpenWorkItem is the single definition of “open work” for board cards:
// any non-done column is open; in the done column, open until resolution is done.
func BoardCardIsOpenWorkItem(columnKey, resolution string) bool {
	if strings.TrimSpace(columnKey) != "done" {
		return true
	}
	res := normalizeIncomingCardResolution(strings.TrimSpace(resolution))
	return res != "done"
}

func boardCardRowCountsAsOpenWorkItem(card boardCardRow) bool {
	if card.TrashedAt.Valid && strings.TrimSpace(card.TrashedAt.String) != "" {
		return false
	}
	if card.ArchivedAt.Valid && strings.TrimSpace(card.ArchivedAt.String) != "" {
		return false
	}
	res := ""
	if card.Resolution.Valid {
		res = card.Resolution.String
	}
	return BoardCardIsOpenWorkItem(card.ColumnKey, res)
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
	query := `SELECT id, handle, title, summary, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM boards
		WHERE 1=1`
	args := make([]any, 0, 8)
	query += ` AND ` + LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)

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
		query += ` AND (LOWER(id) LIKE ? OR LOWER(title) LIKE ? OR LOWER(summary) LIKE ?)`
		args = append(args, searchPattern, searchPattern, searchPattern)
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
			SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
			  FROM ref_edges re
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

func (s *Store) loadOrderedBoardCards(ctx context.Context, q queryRower, boardID string, columnKey string) ([]boardCardRow, error) {
	db, ok := q.(interface {
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	})
	if !ok {
		return nil, fmt.Errorf("board card query does not support row iteration")
	}

	query := `SELECT *
		FROM (
			SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
			  FROM ref_edges re
			  JOIN cards c ON c.id = re.target_id
			 WHERE re.source_type = 'board'
			   AND re.edge_type = ?
			   AND re.source_id = ?
			   AND c.archived_at IS NULL AND c.trashed_at IS NULL`
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
			actors.SystemActorID,
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
		`SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
		        COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
		        COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
		   FROM ref_edges re
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND re.source_id = ?
		    AND COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) = ?
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
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
		`SELECT id FROM documents WHERE id = ? AND trashed_at IS NULL`,
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
		`SELECT id, handle, title, summary, owners_json, thread_id, refs_json, column_schema_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		   FROM boards
		  WHERE id = ?`,
		strings.TrimSpace(boardID),
	).Scan(
		&row.ID,
		&row.Handle,
		&row.Title,
		&row.Summary,
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
			SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
			  FROM ref_edges re
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
			SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
		        c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
			  FROM ref_edges re
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
			SELECT re.source_id AS board_id, re.target_id AS card_id, c.handle AS card_handle,
			       COALESCE(json_extract(re.metadata_json, '$.column_key'), ?) AS column_key,
			       COALESCE(json_extract(re.metadata_json, '$.rank'), '') AS rank,
			       c.title, c.summary, c.version, c.head_revision_id, c.head_revision_number, c.thread_id, c.parent_thread_id, c.due_at, c.definition_of_done_json,
	c.pinned_document_id, c.assignee, c.risk, c.resolution, c.resolution_refs_json, c.refs_json,
			       c.created_at, c.created_by, c.updated_at, c.updated_by, c.provenance_json, c.archived_at, c.archived_by, c.trashed_at, c.trashed_by, c.trash_reason
			  FROM ref_edges re
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
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
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

func resolveBoardPlacementAnchors(ctx context.Context, rower queryRower, boardID, beforeCardID, afterCardID string) (string, string, error) {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
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
	); err != nil {
		return boardCardRow{}, fmt.Errorf("scan board card row: %w", err)
	}
	return row, nil
}

type boardCardVersionRow struct {
	RevisionID           string
	CardID               string
	Version              int
	PrevRevisionID       sql.NullString
	ArtifactID           string
	RevisionHash         string
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
		&row.Version,
		&row.PrevRevisionID,
		&row.ArtifactID,
		&row.RevisionHash,
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
		threadHandle, handleErr := uniqueHandleTx(ctx, tx, "thread", title, "thread-"+threadID)
		if handleErr != nil {
			return fmt.Errorf("allocate board backing thread handle: %w", handleErr)
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, handle, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
			 VALUES (?, ?, 'thread', ?, ?, ?, ?, ?)`,
			threadID,
			threadHandle,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
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

	provenance := cloneProvenance(threadBody["provenance"])
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	threadBody = buildBoardBackingThreadBody(boardID, threadID, title)
	threadBody["provenance"] = provenance

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal board backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal board backing thread provenance: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
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
		threadHandle, handleErr := uniqueHandleTx(ctx, tx, "thread", title, "thread-"+threadID)
		if handleErr != nil {
			return fmt.Errorf("allocate card backing thread handle: %w", handleErr)
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO threads(id, handle, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
			 VALUES (?, ?, 'thread', ?, ?, ?, ?, ?)`,
			threadID,
			threadHandle,
			threadID,
			updatedAt,
			actorID,
			string(bodyJSON),
			inferredProvenanceJSON(),
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

	provenance := cloneProvenance(threadBody["provenance"])
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	threadBody = buildCardBackingThreadBody(cardID, threadID, title)
	threadBody["provenance"] = provenance

	bodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return fmt.Errorf("marshal card backing thread update: %w", err)
	}
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return fmt.Errorf("marshal card backing thread provenance: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
		    SET thread_id = ?, updated_at = ?, updated_by = ?, body_json = ?, provenance_json = ?
		  WHERE id = ?`,
		threadID,
		updatedAt,
		actorID,
		string(bodyJSON),
		string(provenanceJSON),
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

// boardToMapWithRefData builds the public board map using canonical typed refs and board→card edges.
func (r boardRow) boardToMapWithRefData(typedRefs, cardRefs []string) (map[string]any, error) {
	columnSchema, err := decodeBoardColumnSchema(r.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	typedRefs = uniqueSortedStrings(typedRefs)
	if typedRefs == nil {
		typedRefs = []string{}
	}
	cardRefs = uniqueSortedStrings(cardRefs)
	if cardRefs == nil {
		cardRefs = []string{}
	}
	handle := firstNonEmpty(strings.TrimSpace(r.Handle.String), r.ID)
	m := map[string]any{
		"id":            r.ID,
		"ref":           "board:" + handle,
		"handle":        handle,
		"title":         r.Title,
		"summary":       strings.TrimSpace(r.Summary),
		"state":         canonicalLifecycleState(r.ArchivedAt, r.TrashedAt),
		"thread_id":     r.ThreadID,
		"refs":          typedRefs,
		"card_refs":     cardRefs,
		"column_schema": columnSchema,
		"created_at":    r.CreatedAt,
		"created_by":    r.CreatedBy,
		"updated_at":    r.UpdatedAt,
		"updated_by":    r.UpdatedBy,
	}
	owners, err := decodeStoredJSONList(r.OwnersJSON, "board.owners")
	if err != nil {
		return nil, err
	}
	m["owners"] = owners
	if documentRefs := boardDocumentRefsFromRefs(typedRefs); len(documentRefs) > 0 {
		m["document_refs"] = documentRefs
	}
	if pinnedRefs := boardPinnedRefsFromRefs(typedRefs); len(pinnedRefs) > 0 {
		m["pinned_refs"] = pinnedRefs
	}
	if topicRefs := boardTopicRefsFromRefs(typedRefs); len(topicRefs) > 0 {
		m["primary_topic_ref"] = topicRefs[0]
	}
	mergeBoardArchiveTrashFields(m, r)
	return m, nil
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
	headRevisionRef := boardTypedRefOrNil("card_revision", headRevisionID)
	if headRevisionID != "" && headRevisionNumber > 0 {
		headRevisionRef = "card_revision:" + revisionHandle(cardHandle, headRevisionNumber)
	}
	m := map[string]any{
		"id":                   r.CardID,
		"ref":                  "card:" + cardHandle,
		"handle":               cardHandle,
		"board_id":             r.BoardID,
		"board_ref":            "board:" + strings.TrimSpace(r.BoardID),
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
		"card_ref":           "card:" + strings.TrimSpace(r.CardID),
		"revision_number":    r.Version,
		"prev_revision_ref":  boardTypedRefOrNil("card_revision", r.PrevRevisionID.String),
		"artifact_ref":       "artifact:" + strings.TrimSpace(r.ArtifactID),
		"revision_hash":      r.RevisionHash,
		"title":              r.Title,
		"summary":            r.Body,
		"board_id":           r.BoardID,
		"board_ref":          "board:" + strings.TrimSpace(r.BoardID),
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

func canonicalizeCardResolutionForAPI(raw string) any {
	s := normalizeIncomingCardResolution(raw)
	if s == "" {
		return nil
	}
	switch s {
	case "done":
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
	case "done":
		return nil
	default:
		return fmt.Errorf("card.resolution must be null or done")
	}
}

func resolveBoardCardMoveResolution(cardRow boardCardRow, columnKey string, input MoveBoardCardInput) (string, string, bool, error) {
	columnKey = strings.TrimSpace(columnKey)
	currentResolution := normalizeIncomingCardResolution(strings.TrimSpace(cardRow.Resolution.String))
	effectiveResolution := input.Resolution
	if effectiveResolution == nil && columnKey == "done" && input.ResolutionRefs != nil {
		rr := uniqueSortedStrings(*input.ResolutionRefs)
		if len(rr) > 0 {
			done := "done"
			effectiveResolution = &done
		}
	}
	if effectiveResolution == nil {
		if columnKey != "done" {
			resolutionRefs, err := decodeStoredJSONList(cardRow.ResolutionRefsJSON, "card.resolution_refs")
			if err != nil {
				return "", "", false, err
			}
			hasRefs := len(resolutionRefs) > 0
			if currentResolution != "" || hasRefs {
				emptyRefs, err := json.Marshal([]string{})
				if err != nil {
					return "", "", false, fmt.Errorf("marshal cleared resolution refs: %w", err)
				}
				return "", string(emptyRefs), true, nil
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

	nextResolution := normalizeIncomingCardResolution(strings.TrimSpace(*effectiveResolution))
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
	default:
		return "", "", false, invalidBoardRequest("resolution must be done")
	}
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return "", "", false, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	return nextResolution, string(resolutionRefsJSON), true, nil
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

// ValidateBoardPlacementAnchors enforces placement anchor rules shared by HTTP handlers and the store.
func ValidateBoardPlacementAnchors(beforeCardID, afterCardID string) error {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
	if beforeCardID != "" && afterCardID != "" {
		return fmt.Errorf("before and after anchors are mutually exclusive")
	}
	return nil
}

func ensureBoardCardMutable(card boardCardRow) error {
	if card.TrashedAt.Valid && strings.TrimSpace(card.TrashedAt.String) != "" {
		return invalidBoardRequest("card is trashed")
	}
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
