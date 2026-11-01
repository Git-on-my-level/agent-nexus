// Card revision inserts: the revision insert helpers shared by card create
// and update, and the explicit CreateCardRevision store method.

package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
)

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

// cardRevisionContent is the stored blob content of a card revision.
func cardRevisionContent(title, summary string, definitionOfDone []string) ([]byte, error) {
	return encodeContent(map[string]any{
		"title":              strings.TrimSpace(title),
		"summary":            strings.TrimSpace(summary),
		"definition_of_done": uniqueSortedStrings(definitionOfDone),
	}, "structured")
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
	encodedContent, err := cardRevisionContent(title, summary, definitionOfDone)
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

func (s *Store) insertCardRevisionTx(ctx context.Context, tx *accessTx, actorID, cardID, cardHandle, threadID string, revision cardRevisionInsert) error {
	parent := firstNonEmpty(strings.TrimSpace(cardHandle), cardID)
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", revisionArtifactDesiredHandle(parent, revision.RevisionNumber), "artifact-"+revision.ArtifactID)
	if err != nil {
		return fmt.Errorf("allocate card artifact handle: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		 VALUES (?, ?, 'card', ?, ?, ?, 'structured', ?, ?, ?, ?)`,
		revision.ArtifactID,
		artifactHandle,
		nullableString(threadID),
		revision.CreatedAt,
		actorID,
		revision.ContentHash,
		revision.RefsJSON,
		revision.MetadataJSON,
		resourceaccess.ContentReferenceAtomsJSON(string(revision.EncodedContent), "structured"),
	); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
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
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, s)

	}
	// Bind quota/ledger and projection reads to the writer, even without an
	// HTTP replay wrapper. Nested callers borrow it through scoped savepoints.
	var result BoardCardMutationResult
	var revision map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, _ *resourceaccess.Tx) error {
		var err error
		result, revision, err = s.createCardRevision(ctx, actorID, cardID, input)
		return err
	})
	return result, revision, err
}

func (s *Store) createCardRevision(ctx context.Context, actorID, cardID string, input CreateCardRevisionInput) (BoardCardMutationResult, map[string]any, error) {
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
	if err := ensureNativeWorkMutation(ctx, tx, cardRow.CardID); err != nil {
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
	if err := s.insertCardRevisionTx(ctx, tx, actorID, cardRow.CardID, cardRow.Handle.String, threadID, revision); err != nil {
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
	if err := markBoardCardProjectionDirtyTx(ctx, tx, cardRow); err != nil {
		_ = stagedContent.Cleanup()
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		_ = stagedContent.Cleanup()
		return BoardCardMutationResult{}, nil, fmt.Errorf("commit card revision transaction: %w", err)
	}
	// With replay, this savepoint has released but the outer transaction is
	// still open. Promotion must succeed before the HTTP receipt can commit.
	// A later outer failure may leave an unreferenced content-addressed blob.
	if err := stagedContent.Promote(); err != nil {
		_ = stagedContent.Cleanup()
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
