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

	"agent-nexus-core/internal/cardcontent"
)

type cardRevisionInsert struct {
	RevisionID     string
	RevisionNumber int
	PrevRevisionID sql.NullString
	ArtifactID     string
	RevisionHash   string
	CreatedAt      string
	ContentHash    string
	SummaryBytes   int
	RefsJSON       string
	MetadataJSON   string
	EncodedContent []byte
}

// cardRevisionContent is the canonical content of a card revision. Its
// SHA-256 is the revision artifact's content_hash.
func cardRevisionContent(title, summary string, definitionOfDone []string) ([]byte, error) {
	return encodeContent(map[string]any{
		"title":              strings.TrimSpace(title),
		"summary":            strings.TrimSpace(summary),
		"definition_of_done": uniqueSortedStrings(definitionOfDone),
	}, "structured")
}

func (s *Store) prepareCardRevisionInsert(actorID, cardID string, revisionNumber int, prevRevisionID sql.NullString, threadID, title, summary string, definitionOfDone []string, refs []string) (cardRevisionInsert, error) {
	cardID = strings.TrimSpace(cardID)
	actorID = strings.TrimSpace(actorID)
	if err := s.checkCardSummarySize(summary); err != nil {
		return cardRevisionInsert{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionID := uuid.NewString()
	artifactID := revisionID
	encodedContent, err := cardRevisionContent(title, summary, definitionOfDone)
	if err != nil {
		return cardRevisionInsert{}, err
	}
	contentHash := sha256Hex(encodedContent)
	revisionRefs := append([]string(nil), refs...)
	if strings.TrimSpace(threadID) != "" {
		revisionRefs = append(revisionRefs, "thread:"+strings.TrimSpace(threadID))
	}
	revisionRefs = append(revisionRefs, "card:"+cardID)
	revisionRefs = uniqueSortedStrings(revisionRefs)
	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return cardRevisionInsert{}, fmt.Errorf("marshal card revision refs: %w", err)
	}
	prevID := ""
	if prevRevisionID.Valid {
		prevID = strings.TrimSpace(prevRevisionID.String)
	}
	revisionHash := computeRevisionHash(contentHash, prevID, cardID, revisionNumber, now, actorID)
	// The summary and definition of done live only in the revision content
	// (card_revisions.content_*); metadata keeps small descriptive fields.
	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "card",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     "structured",
		"content_hash":     contentHash,
		"refs":             revisionRefs,
		"card_id":          cardID,
		"revision_id":      revisionID,
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
		"title":            strings.TrimSpace(title),
	}
	if prevID != "" {
		artifactMetadata["prev_revision_id"] = prevID
	}
	metadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return cardRevisionInsert{}, fmt.Errorf("marshal card artifact metadata: %w", err)
	}
	return cardRevisionInsert{
		RevisionID:     revisionID,
		RevisionNumber: revisionNumber,
		PrevRevisionID: prevRevisionID,
		ArtifactID:     artifactID,
		RevisionHash:   revisionHash,
		CreatedAt:      now,
		ContentHash:    contentHash,
		SummaryBytes:   len(strings.TrimSpace(summary)),
		RefsJSON:       string(refsJSON),
		MetadataJSON:   string(metadataJSON),
		EncodedContent: encodedContent,
	}, nil
}

func (s *Store) insertCardRevisionTx(ctx context.Context, tx *accessTx, actorID, cardID, cardHandle, threadID string, revision cardRevisionInsert) error {
	// Content is stored once, as a bounded keyframe/delta record on the
	// revision row; no blob is written. Planning reads only this card's latest
	// snapshot and a content-hash match.
	record, err := cardcontent.PlanForCard(ctx, tx, cardID, revision.RevisionNumber, revision.EncodedContent)
	if err != nil {
		return fmt.Errorf("encode card revision content: %w", err)
	}
	if record.ContentHash != revision.ContentHash {
		return fmt.Errorf("card revision content hash mismatch")
	}
	// Stored bytes count toward the storage quota through database accounting.
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{artifacts: 1, revisions: 1, dbBytes: int64(len(record.Payload) + len(revision.MetadataJSON) + len(revision.RefsJSON))}, blobLedgerWritePlan{}); err != nil {
		return err
	}
	parent := firstNonEmpty(strings.TrimSpace(cardHandle), cardID)
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", revisionArtifactDesiredHandle(parent, revision.RevisionNumber), "artifact-"+revision.ArtifactID)
	if err != nil {
		return fmt.Errorf("allocate card artifact handle: %w", err)
	}
	manifest := resourceaccess.ContentReferenceAtomsJSON(string(revision.EncodedContent), "structured")
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
		manifest,
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
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO card_revisions(
			revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by,
			content_encoding, content_codec, content_base_revision_id, content_hash, content_bytes, summary_bytes, content_payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		record.Encoding,
		nullableString(record.Codec),
		nullableString(record.BaseRevisionID),
		record.ContentHash,
		record.ContentBytes,
		revision.SummaryBytes,
		record.Payload,
	); err != nil {
		return fmt.Errorf("insert card revision: %w", err)
	}
	// The complete manifest above created every raw access edge. Now that the
	// revision row exists, store only its reference atoms; the installed
	// trigger recognizes this exact prune and leaves those edges untouched.
	if pruned := resourceaccess.WithoutProse(string(manifest)); pruned != string(manifest) {
		if _, err := tx.ExecContext(ctx,
			`UPDATE artifacts SET content_refs_json=? WHERE id=? AND content_hash=? AND content_refs_json=?`,
			pruned, revision.ArtifactID, revision.ContentHash, string(manifest)); err != nil {
			return fmt.Errorf("prune card artifact manifest: %w", err)
		}
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
	revision, err := s.prepareCardRevisionInsert(actorID, cardRow.CardID, nextRevisionNumber, prevRevision, threadID, nextTitle, nextSummary, nextDefinition, nextRefs)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := s.insertCardRevisionTx(ctx, tx, actorID, cardRow.CardID, cardRow.Handle.String, threadID, revision); err != nil {
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
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, fmt.Errorf("update card revision head: %w", err)
	}
	boardRow, err := loadBoardRow(ctx, tx, cardRow.BoardID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, cardRow.CardID, true)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	// Echo only the new revision; never materialize the card's history.
	revisionMap, err := s.loadCardRevisionTx(ctx, tx, cardRow.CardID, revision.RevisionID, true)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := markBoardCardProjectionDirtyTx(ctx, tx, cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return BoardCardMutationResult{}, nil, fmt.Errorf("commit card revision transaction: %w", err)
	}
	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, nil, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, nil, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, revisionMap, nil
}
