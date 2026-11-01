// Document patch and replace updates: PatchDocument and UpdateDocument with
// the ref-edge targets they write.

package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Store) PatchDocument(ctx context.Context, actorID, documentID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if patch == nil || len(patch) == 0 {
		return nil, nil, invalidDocumentRequest("patch is required")
	}
	allowedPatch := map[string]struct{}{"summary": {}, "source": {}, "tags": {}, "title": {}, "hosts": {}, "verified_at": {}}
	for k := range patch {
		if _, ok := allowedPatch[k]; !ok {
			return nil, nil, invalidDocumentRequest("unsupported document patch field: " + k)
		}
	}
	documentID = strings.TrimSpace(documentID)
	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if err := ensureUpdatedAtMatches(doc.UpdatedAt, ifUpdatedAt); err != nil {
		return nil, nil, err
	}
	nextTitle := nullStringValue(doc.Title)
	nextSummary := strings.TrimSpace(doc.Summary)
	nextSource := strings.TrimSpace(doc.Source)
	nextTags, err := decodeStoredJSONList(doc.TagsJSON, "document.tags")
	if err != nil {
		return nil, nil, err
	}
	nextHosts, err := decodeStoredJSONList(doc.HostsJSON, "document.hosts")
	if err != nil {
		return nil, nil, err
	}
	nextVerifiedAt := strings.TrimSpace(doc.VerifiedAt)
	if value, exists := patch["title"]; exists {
		nextTitle = strings.TrimSpace(anyStringValue(value))
	}
	if value, exists := patch["summary"]; exists {
		nextSummary = strings.TrimSpace(anyStringValue(value))
	}
	if value, exists := patch["source"]; exists {
		nextSource = strings.TrimSpace(anyStringValue(value))
	}
	if _, exists := patch["tags"]; exists {
		nextTags, err = normalizeDocumentTags(patch["tags"], patch, "tags")
		if err != nil {
			return nil, nil, err
		}
	}
	if _, exists := patch["hosts"]; exists {
		nextHosts, err = normalizeDocumentHosts(patch["hosts"], patch, "hosts")
		if err != nil {
			return nil, nil, err
		}
	}
	if _, exists := patch["verified_at"]; exists {
		nextVerifiedAt, err = normalizeDocumentVerifiedAt(patch["verified_at"], patch, "verified_at")
		if err != nil {
			return nil, nil, err
		}
	}
	tagsJSON, err := json.Marshal(nextTags)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document tags: %w", err)
	}
	hostsJSON, err := json.Marshal(nextHosts)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document hosts: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE documents SET title = ?, summary = ?, source = ?, tags_json = ?, hosts_json = ?, verified_at = ?, updated_at = ?, updated_by = ? WHERE id = ? AND updated_at = ?`,
		nullableString(nextTitle),
		nextSummary,
		nextSource,
		string(tagsJSON),
		string(hostsJSON),
		nextVerifiedAt,
		now,
		actorID,
		documentID,
		doc.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("patch document: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, nil, fmt.Errorf("read patch document rows affected: %w", err)
	}
	if affected == 0 {
		return nil, nil, ErrConflict
	}

	// Keep the released metadata-only FTS projection current atomically. Its
	// body/comments remain untouched; dirty markers still gate lazy fallback.
	if _, err = tx.ExecContext(ctx, `UPDATE document_fts SET title=?,summary=?,source=?,tags=? WHERE rowid IN (SELECT fts_rowid FROM document_search_fts_positions WHERE document_id=?)`, nextTitle, nextSummary, nextSource, documentFTSTagsText(nextTags), documentID); err != nil {
		return nil, nil, fmt.Errorf("patch document search metadata: %w", err)
	}
	if err := markTopicProjectionsDirtyTx(ctx, tx, []string{doc.ThreadID.String}, time.Now().UTC()); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return s.GetDocument(ctx, documentID)
}

func (s *Store) UpdateDocument(ctx context.Context, actorID string, documentID string, documentPatch map[string]any, ifBaseRevision string, content any, contentType string, refs []string, revisionProvenance map[string]any) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return nil, nil, fmt.Errorf("blob backend is not configured")
	}
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, nil, invalidDocumentRequest("actorID is required")
	}
	ifBaseRevision = strings.TrimSpace(ifBaseRevision)
	if ifBaseRevision == "" {
		return nil, nil, invalidDocumentRequest("if_base_revision is required")
	}
	contentType, err := normalizeDocumentContentType(contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	if resolved, err := resolveRevisionResourceRef(ctx, s.db, "document_revision", strings.TrimPrefix(ifBaseRevision, "document_revision:")); err == nil {
		ifBaseRevision = resolved.ID
	}
	if doc.HeadRevisionID != ifBaseRevision {
		return nil, nil, ErrConflict
	}

	nextThreadID := nullStringValue(doc.ThreadID)
	nextTitle := nullStringValue(doc.Title)
	nextSlug := nullStringValue(doc.Slug)
	nextSummary := strings.TrimSpace(doc.Summary)
	nextSource := strings.TrimSpace(doc.Source)
	nextTags, err := decodeStoredJSONList(doc.TagsJSON, "document.tags")
	if err != nil {
		return nil, nil, err
	}
	nextHosts, err := decodeStoredJSONList(doc.HostsJSON, "document.hosts")
	if err != nil {
		return nil, nil, err
	}
	nextVerifiedAt := strings.TrimSpace(doc.VerifiedAt)
	nextSupersedes, err := decodeStoredJSONList(doc.SupersedesJSON, "document.supersedes")
	if err != nil {
		return nil, nil, err
	}
	nextDocRefs, err := decodeStoredJSONList(doc.RefsJSON, "document.refs")
	if err != nil {
		return nil, nil, err
	}
	nextDocProvJSON := strings.TrimSpace(doc.ProvenanceJSON)
	if nextDocProvJSON == "" {
		nextDocProvJSON = inferredProvenanceJSON()
	}

	origTitle := nextTitle
	origSummary := nextSummary
	origSource := nextSource
	origVerifiedAt := nextVerifiedAt
	origTags := append([]string(nil), nextTags...)
	origHosts := append([]string(nil), nextHosts...)

	if documentPatch != nil {
		if _, exists := documentPatch["id"]; exists {
			return nil, nil, invalidDocumentRequest("document.id cannot be patched")
		}
		if _, exists := documentPatch["document_id"]; exists {
			return nil, nil, invalidDocumentRequest("document.document_id cannot be patched")
		}
		if value, exists := documentPatch["thread_id"]; exists {
			parsed := strings.TrimSpace(anyStringValue(value))
			if parsed == "" {
				return nil, nil, invalidDocumentRequest("document.thread_id must be non-empty when provided")
			}
			nextThreadID = parsed
		}
		if value, exists := documentPatch["title"]; exists {
			nextTitle = strings.TrimSpace(anyStringValue(value))
		}
		if value, exists := documentPatch["summary"]; exists {
			nextSummary = strings.TrimSpace(anyStringValue(value))
		}
		if value, exists := documentPatch["source"]; exists {
			nextSource = strings.TrimSpace(anyStringValue(value))
		}
		if _, exists := documentPatch["tags"]; exists {
			parsed, tagErr := normalizeDocumentTags(documentPatch["tags"], documentPatch, "tags")
			if tagErr != nil {
				return nil, nil, tagErr
			}
			nextTags = parsed
		}
		if _, exists := documentPatch["hosts"]; exists {
			parsed, hostErr := normalizeDocumentHosts(documentPatch["hosts"], documentPatch, "hosts")
			if hostErr != nil {
				return nil, nil, hostErr
			}
			nextHosts = parsed
		}
		if _, exists := documentPatch["verified_at"]; exists {
			parsed, verifiedErr := normalizeDocumentVerifiedAt(documentPatch["verified_at"], documentPatch, "verified_at")
			if verifiedErr != nil {
				return nil, nil, verifiedErr
			}
			nextVerifiedAt = parsed
		}
		if value, exists := documentPatch["slug"]; exists {
			nextSlug = strings.TrimSpace(anyStringValue(value))
		}
		if _, exists := documentPatch["status"]; exists {
			return nil, nil, invalidDocumentRequest("document.status is not writable; canonical lifecycle uses document.state and lifecycle endpoints")
		}
		if _, exists := documentPatch["state"]; exists {
			return nil, nil, invalidDocumentRequest("document.state is not writable in revision updates; use archive/trash/restore lifecycle endpoints")
		}
		if _, exists := documentPatch["labels"]; exists {
			return nil, nil, invalidDocumentRequest("document.labels is not supported")
		}
		if value, exists := documentPatch["supersedes"]; exists {
			parsed, parseErr := normalizeStringSlice(value)
			if parseErr != nil {
				return nil, nil, invalidDocumentRequest("document.supersedes must be a list of strings")
			}
			nextSupersedes = parsed
		}
		if value, exists := documentPatch["refs"]; exists {
			parsed, parseErr := normalizeStringSlice(value)
			if parseErr != nil {
				return nil, nil, invalidDocumentRequest("document.refs must be a list of strings")
			}
			nextDocRefs = uniqueStrings(parsed)
			sortStringsStable(nextDocRefs)
		}
		if _, exists := documentPatch["provenance"]; exists {
			_, pj, perr := marshalProvenance(documentPatch["provenance"], "marshal document")
			if perr != nil {
				return nil, nil, invalidDocumentRequestError(perr)
			}
			nextDocProvJSON = pj
		}
	}

	docResourceRefsJSON, err := json.Marshal(nextDocRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document resource refs: %w", err)
	}

	encodedContent, err := encodeContent(content, contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if err := s.rejectInvalidVisualReportRevision(ctx, documentID, doc.HeadRevisionID, encodedContent); err != nil {
		return nil, nil, err
	}
	nextTagsJSON, err := json.Marshal(nextTags)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document tags: %w", err)
	}
	nextHostsJSON, err := json.Marshal(nextHosts)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document hosts: %w", err)
	}
	searchText := documentSearchText(nextTitle, nextSummary, nextSource, nextTags, encodedContent, contentType)

	contentHash := sha256Hex(encodedContent)
	var currentHash, currentType string
	hashErr := s.db.QueryRowContext(ctx,
		`SELECT TRIM(a.content_hash), TRIM(a.content_type)
		 FROM artifacts a
		 JOIN document_revisions dr ON dr.artifact_id = a.id
		 WHERE dr.document_id = ? AND dr.revision_id = ?`,
		documentID, doc.HeadRevisionID,
	).Scan(&currentHash, &currentType)
	if hashErr == nil &&
		currentHash == contentHash &&
		currentType == contentType &&
		origTitle == nextTitle &&
		origSummary == nextSummary &&
		origSource == nextSource &&
		origVerifiedAt == nextVerifiedAt &&
		stringSlicesEqual(origTags, nextTags) &&
		stringSlicesEqual(origHosts, nextHosts) {
		docMap, mapErr := doc.toMap()
		if mapErr != nil {
			return nil, nil, mapErr
		}
		revision, loadErr := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
		if loadErr != nil {
			return nil, nil, loadErr
		}
		return docMap, revision, nil
	}

	nextRevisionNumber := doc.HeadRevisionNum + 1
	artifactID := uuid.NewString()
	revisionID := artifactID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1, revisions: 1}, blobPlan); err != nil {
		return nil, nil, err
	}

	revisionRefs := append([]string(nil), refs...)
	if nextThreadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+nextThreadID)
	}
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)
	nextThreadID = normalizeDocumentBackingThreadID(documentID, nextThreadID)

	artifactMetadata := map[string]any{
		"id":               artifactID,
		"kind":             "doc",
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"refs":             revisionRefs,
		"document_id":      documentID,
		"revision_id":      revisionID,
		"revision_number":  nextRevisionNumber,
		"prev_revision_id": doc.HeadRevisionID,
	}
	if nextTitle != "" {
		artifactMetadata["summary"] = nextTitle
	}
	if revisionProvenance != nil {
		artifactMetadata["provenance"] = cloneProvenance(revisionProvenance)
	}

	stagedContent, err := s.blob.Write(ctx, contentHash, encodedContent)
	if err != nil {
		return nil, nil, fmt.Errorf("stage document content: %w", err)
	}
	defer func() { _ = stagedContent.Cleanup() }()
	preload, err := s.preloadStagedBlobs(ctx, stagedBlob{hash: contentHash, write: stagedContent})
	if err != nil {
		return nil, nil, fmt.Errorf("preload document content: %w", err)
	}
	defer preload.discardUnlessCommitted()

	var prevRevisionHash string
	err = s.db.QueryRowContext(ctx,
		`SELECT revision_hash FROM document_revisions WHERE document_id = ? AND revision_id = ?`,
		documentID, doc.HeadRevisionID,
	).Scan(&prevRevisionHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("load previous revision hash: %w", err)
	}

	revisionHash := computeRevisionHash(contentHash, prevRevisionHash, documentID, nextRevisionNumber, now, actorID)

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	supersedesJSON, err := json.Marshal(nextSupersedes)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document update transaction: %w", err)
	}
	if nextThreadID, err = ensureDocumentBackingThreadTx(ctx, tx, actorID, documentID, nextThreadID, nextTitle, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	documentHandle := firstNonEmpty(strings.TrimSpace(doc.Handle.String), documentID)
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", revisionArtifactDesiredHandle(documentHandle, nextRevisionNumber), "artifact-"+artifactID)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("allocate document artifact handle: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		artifactID,
		artifactHandle,
		"doc",
		nullableString(nextThreadID),
		now,
		actorID,
		contentType,
		contentHash,
		string(refsJSON),
		string(artifactMetadataJSON),
		resourceaccess.ContentReferenceAtomsJSON(string(encodedContent), contentType),
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document artifact: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "artifact", artifactID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		nextRevisionNumber,
		doc.HeadRevisionID,
		artifactID,
		nullableString(nextThreadID),
		string(refsJSON),
		revisionHash,
		now,
		actorID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document revision: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "document_revision", revisionID, typedRefEdgeTargets(refEdgeTypeRef, revisionRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	lifecycleEvent, err := prepareEventForInsert(actorID, buildDocumentLifecycleEvent(
		"document_revised",
		nextThreadID,
		documentID,
		revisionID,
		artifactID,
		nextRevisionNumber,
		nextTitle,
		map[string]any{
			"prev_revision_id": doc.HeadRevisionID,
		},
	))
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if err := insertPreparedEvent(ctx, tx, lifecycleEvent); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE documents SET
			thread_id = ?,
			title = ?,
			summary = ?,
			source = ?,
			tags_json = ?,
			hosts_json = ?,
			verified_at = ?,
			search_text = ?,
			slug = ?,
			supersedes_json = ?,
			refs_json = ?,
			provenance_json = ?,
			head_revision_id = ?,
			head_revision_number = ?,
			updated_at = ?,
			updated_by = ?
		 WHERE id = ? AND head_revision_id = ?`,
		nullableString(nextThreadID),
		nullableString(nextTitle),
		nextSummary,
		nextSource,
		string(nextTagsJSON),
		string(nextHostsJSON),
		nextVerifiedAt,
		searchText,
		nullableString(nextSlug),
		string(supersedesJSON),
		string(docResourceRefsJSON),
		nextDocProvJSON,
		revisionID,
		nextRevisionNumber,
		now,
		actorID,
		documentID,
		ifBaseRevision,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("update document head: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("read document head update result: %w", err)
	}
	if affected == 0 {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, ErrConflict
	}
	if err := replaceRefEdges(ctx, tx, "document", documentID, documentResourceRefEdgeTargets(nextThreadID, nextDocRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	if nullStringValue(doc.ThreadID) != nextThreadID {
		if err := markTopicProjectionsDirtyTx(ctx, tx, []string{doc.ThreadID.String}, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return nil, nil, err
		}
		if err := clearDocumentBackingThreadSubjectTx(ctx, tx, actorID, nullStringValue(doc.ThreadID), documentID, now); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return nil, nil, err
		}
	}

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := upsertDocumentFTSTx(ctx, tx, documentID, nextTitle, documentSearchBody(encodedContent, contentType), nextSummary, nextSource, nextTags, nextThreadID, revisionID); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := stagedContent.Promote(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("finalize document content: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, fmt.Errorf("commit document update transaction: %w", err)
	}
	preload.markCommitted()

	docMap, err := documentRow{
		ID:              documentID,
		Handle:          doc.Handle,
		ThreadID:        nullableString(nextThreadID),
		Title:           nullableString(nextTitle),
		Summary:         nextSummary,
		Source:          nextSource,
		TagsJSON:        string(nextTagsJSON),
		HostsJSON:       string(nextHostsJSON),
		VerifiedAt:      nextVerifiedAt,
		Slug:            nullableString(nextSlug),
		SupersedesJSON:  string(supersedesJSON),
		RefsJSON:        string(docResourceRefsJSON),
		ProvenanceJSON:  nextDocProvJSON,
		HeadRevisionID:  revisionID,
		HeadRevisionNum: nextRevisionNumber,
		CreatedAt:       doc.CreatedAt,
		CreatedBy:       doc.CreatedBy,
		UpdatedAt:       now,
		UpdatedBy:       actorID,
	}.toMap()
	if err != nil {
		return nil, nil, err
	}

	revisionMap := applyDocumentRevisionAliases(map[string]any{
		"document_id":      documentID,
		"revision_id":      revisionID,
		"artifact_id":      artifactID,
		"revision_number":  nextRevisionNumber,
		"prev_revision_id": doc.HeadRevisionID,
		"thread_id":        nullableMapValue(nextThreadID),
		"refs":             revisionRefs,
		"created_at":       now,
		"created_by":       actorID,
		"content_type":     contentType,
		"content_hash":     contentHash,
		"revision_hash":    revisionHash,
		"artifact":         artifactMetadata,
	})
	publicizeDocumentRevisionMap(ctx, tx, revisionMap)
	revisionMap["document_ref"] = docMap["ref"]
	setDocumentContentValue(revisionMap, encodedContent, contentType)
	if revisionProvenance != nil {
		revisionMap["provenance"] = cloneProvenance(revisionProvenance)
	}
	return docMap, revisionMap, nil
}

func documentResourceRefEdgeTargets(threadID string, refs []string) []refEdgeTarget {
	targets := appendRefEdgeTarget(nil, refEdgeTypeDocumentThread, "thread", strings.TrimSpace(threadID))
	return append(targets, typedRefEdgeTargets(refEdgeTypeRef, refs)...)
}
