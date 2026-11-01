// Document create and fetch: documentRow, CreateDocument with its revision
// and artifact writes, and the invalid-document-request errors.

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

type documentRow struct {
	ID                       string
	Handle                   sql.NullString
	ThreadID                 sql.NullString
	Title                    sql.NullString
	Summary                  string
	Source                   string
	TagsJSON                 string
	HostsJSON                string
	VerifiedAt               string
	Slug                     sql.NullString
	SupersedesJSON           string
	RefsJSON                 string
	ProvenanceJSON           string
	HeadRevisionID           string
	HeadRevisionNum          int
	CreatedAt                string
	CreatedBy                string
	UpdatedAt                string
	UpdatedBy                string
	TrashedAt                sql.NullString
	TrashedBy                sql.NullString
	TrashReason              sql.NullString
	ArchivedAt               sql.NullString
	ArchivedBy               sql.NullString
	HeadArtifactID           sql.NullString
	HeadContentType          sql.NullString
	HeadCreatedAt            sql.NullString
	HeadCreatedBy            sql.NullString
	ListRevisionCount        sql.NullInt64
	ListTimelineMessageCount sql.NullInt64
	ListLastCommentBody      sql.NullString
	ListLastCommentAt        sql.NullString
	ListLastCommentBy        sql.NullString
}

func (s *Store) CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error) {
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
	if document == nil {
		return nil, nil, invalidDocumentRequest("document is required")
	}
	contentType, err := normalizeDocumentContentType(contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	documentID := strings.TrimSpace(anyStringValue(document["document_id"]))
	if documentID == "" {
		documentID = strings.TrimSpace(anyStringValue(document["id"]))
	}
	if documentID == "" {
		documentID = uuid.NewString()
	}
	if err := validateDocumentID(documentID); err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}

	threadID, err := optionalStringField(document, "thread_id")
	if err != nil {
		return nil, nil, err
	}
	threadID = normalizeDocumentBackingThreadID(documentID, threadID)
	title, err := optionalStringField(document, "title")
	if err != nil {
		return nil, nil, err
	}
	slug, err := optionalStringField(document, "slug")
	if err != nil {
		return nil, nil, err
	}
	docSummary := strings.TrimSpace(anyStringValue(document["summary"]))
	source, err := optionalStringField(document, "source")
	if err != nil {
		return nil, nil, err
	}
	tags, err := normalizeDocumentTags(document["tags"], document, "tags")
	if err != nil {
		return nil, nil, err
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document tags: %w", err)
	}
	hosts, err := normalizeDocumentHosts(document["hosts"], document, "hosts")
	if err != nil {
		return nil, nil, err
	}
	hostsJSON, err := json.Marshal(hosts)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document hosts: %w", err)
	}
	verifiedAt, err := normalizeDocumentVerifiedAt(document["verified_at"], document, "verified_at")
	if err != nil {
		return nil, nil, err
	}
	requestedHandle := strings.TrimSpace(anyStringValue(document["handle"]))
	if _, exists := document["labels"]; exists {
		return nil, nil, invalidDocumentRequest("document.labels is not supported")
	}
	supersedes, err := optionalStringListField(document, "supersedes")
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if _, exists := document["status"]; exists {
		return nil, nil, invalidDocumentRequest("document.status is not writable; canonical lifecycle uses document.state and lifecycle endpoints")
	}
	if _, exists := document["state"]; exists {
		return nil, nil, invalidDocumentRequest("document.state is not writable on create; new documents start active")
	}

	encodedContent, err := encodeContent(content, contentType)
	if err != nil {
		return nil, nil, invalidDocumentRequestError(err)
	}
	if err := rejectInvalidVisualReport(encodedContent); err != nil {
		return nil, nil, err
	}
	searchText := documentSearchText(title, docSummary, source, tags, encodedContent, contentType)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionNumber := 1
	artifactID := uuid.NewString()
	revisionID := artifactID
	contentHash := sha256Hex(encodedContent)
	blobPlan, err := s.prepareBlobLedgerWritePlan(ctx, contentHash, int64(len(encodedContent)))
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, int64(len(encodedContent)), quotaWriteDelta{artifacts: 1, documents: 1, revisions: 1}, blobPlan); err != nil {
		return nil, nil, err
	}

	revisionRefs := append([]string(nil), refs...)
	if threadID != "" {
		revisionRefs = append(revisionRefs, "thread:"+threadID)
	}
	revisionRefs = uniqueStrings(revisionRefs)
	sortStringsStable(revisionRefs)

	var docResourceRefs []string
	if _, has := document["refs"]; has {
		dr, err := optionalStringListField(document, "refs")
		if err != nil {
			return nil, nil, invalidDocumentRequestError(err)
		}
		docResourceRefs = uniqueStrings(dr)
		sortStringsStable(docResourceRefs)
	} else {
		docResourceRefs = append([]string(nil), revisionRefs...)
	}
	docRefsJSON, err := json.Marshal(docResourceRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document resource refs: %w", err)
	}
	docProvJSON := inferredProvenanceJSON()
	if _, has := document["provenance"]; has {
		_, pj, perr := marshalProvenance(document["provenance"], "marshal document")
		if perr != nil {
			return nil, nil, invalidDocumentRequestError(perr)
		}
		docProvJSON = pj
	}

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
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
	}
	if move, ok := document["workspace_move"]; ok {
		artifactMetadata["workspace_move"] = move
	}
	if title != "" {
		artifactMetadata["summary"] = title
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

	revisionHash := computeRevisionHash(contentHash, "", documentID, revisionNumber, now, actorID)

	refsJSON, err := json.Marshal(revisionRefs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document refs: %w", err)
	}
	artifactMetadataJSON, err := json.Marshal(artifactMetadata)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document artifact metadata: %w", err)
	}
	supersedesJSON, err := json.Marshal(supersedes)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal document supersedes: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin document create transaction: %w", err)
	}
	if threadID, err = ensureDocumentBackingThreadTx(ctx, tx, actorID, documentID, threadID, title, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}
	documentHandle, err := allocateDocumentHandleTx(ctx, tx, requestedHandle, firstNonEmpty(slug, title), "document-"+documentID)
	if err != nil {
		_ = tx.Rollback()
		return nil, nil, fmt.Errorf("allocate document handle: %w", err)
	}
	artifactHandle, err := uniqueHandleTx(ctx, tx, "artifact", revisionArtifactDesiredHandle(documentHandle, revisionNumber), "artifact-"+artifactID)
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
		nullableString(threadID),
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
		`INSERT INTO documents(
			id, handle, thread_id, title, summary, source, tags_json, hosts_json, verified_at, search_text, slug, supersedes_json,
			refs_json, provenance_json,
			head_revision_id, head_revision_number,
			created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		documentID,
		documentHandle,
		nullableString(threadID),
		nullableString(title),
		docSummary,
		source,
		string(tagsJSON),
		string(hostsJSON),
		verifiedAt,
		searchText,
		nullableString(slug),
		string(supersedesJSON),
		string(docRefsJSON),
		docProvJSON,
		revisionID,
		revisionNumber,
		now,
		actorID,
		now,
		actorID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if isUniqueViolation(err) {
			return nil, nil, ErrConflict
		}
		return nil, nil, fmt.Errorf("insert document: %w", err)
	}
	if err := replaceRefEdges(ctx, tx, "document", documentID, documentResourceRefEdgeTargets(threadID, docResourceRefs)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?)`,
		revisionID,
		documentID,
		revisionNumber,
		artifactID,
		nullableString(threadID),
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
		"document_created",
		threadID,
		documentID,
		revisionID,
		artifactID,
		revisionNumber,
		title,
		func() map[string]any {
			if move, ok := document["workspace_move"]; ok {
				return map[string]any{"workspace_move": move}
			}
			return nil
		}(),
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

	if err := s.applyBlobLedgerWritePlanTx(ctx, tx, blobPlan); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, nil, err
	}

	if err := upsertDocumentFTSTx(ctx, tx, documentID, title, documentSearchBody(encodedContent, contentType), docSummary, source, tags, threadID, revisionID); err != nil {
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
		return nil, nil, fmt.Errorf("commit document create transaction: %w", err)
	}
	preload.markCommitted()

	docMap, err := documentRow{
		ID:              documentID,
		Handle:          nullableString(documentHandle),
		ThreadID:        nullableString(threadID),
		Title:           nullableString(title),
		Summary:         docSummary,
		Source:          source,
		TagsJSON:        string(tagsJSON),
		HostsJSON:       string(hostsJSON),
		VerifiedAt:      verifiedAt,
		Slug:            nullableString(slug),
		SupersedesJSON:  string(supersedesJSON),
		RefsJSON:        string(docRefsJSON),
		ProvenanceJSON:  docProvJSON,
		HeadRevisionID:  revisionID,
		HeadRevisionNum: revisionNumber,
		CreatedAt:       now,
		CreatedBy:       actorID,
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
		"revision_number":  revisionNumber,
		"prev_revision_id": nil,
		"thread_id":        nullableMapValue(threadID),
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
	return docMap, revisionMap, nil
}

func (s *Store) GetDocument(ctx context.Context, documentID string) (map[string]any, map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("primitives store database is not initialized")
	}
	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true)
	if err != nil {
		return nil, nil, err
	}
	docMap, err := doc.toMap()
	if err != nil {
		return nil, nil, err
	}
	return docMap, revision, nil
}

func (s *Store) loadDocumentRow(ctx context.Context, documentID string) (documentRow, error) {
	documentID = strings.TrimSpace(documentID)
	if err := validateDocumentID(documentID); err != nil {
		return documentRow{}, err
	}

	var row documentRow
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, handle, thread_id, title, summary, source, tags_json, hosts_json, verified_at, slug, supersedes_json,
			 refs_json, provenance_json,
			 head_revision_id, head_revision_number, created_at, created_by, updated_at, updated_by,
			 trashed_at, trashed_by, trash_reason,
			 archived_at, archived_by
		 FROM documents WHERE id = ?`,
		documentID,
	).Scan(
		&row.ID,
		&row.Handle,
		&row.ThreadID,
		&row.Title,
		&row.Summary,
		&row.Source,
		&row.TagsJSON,
		&row.HostsJSON,
		&row.VerifiedAt,
		&row.Slug,
		&row.SupersedesJSON,
		&row.RefsJSON,
		&row.ProvenanceJSON,
		&row.HeadRevisionID,
		&row.HeadRevisionNum,
		&row.CreatedAt,
		&row.CreatedBy,
		&row.UpdatedAt,
		&row.UpdatedBy,
		&row.TrashedAt,
		&row.TrashedBy,
		&row.TrashReason,
		&row.ArchivedAt,
		&row.ArchivedBy,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return documentRow{}, ErrNotFound
	}
	if err != nil {
		return documentRow{}, fmt.Errorf("query document: %w", err)
	}
	return row, nil
}

func (r documentRow) toMap() (map[string]any, error) {
	supersedes, err := decodeStoredJSONList(r.SupersedesJSON, "document.supersedes")
	if err != nil {
		return nil, err
	}
	refs, err := decodeStoredJSONList(r.RefsJSON, "document.refs")
	if err != nil {
		return nil, err
	}
	state := canonicalLifecycleState(r.ArchivedAt, r.TrashedAt)
	out := map[string]any{
		"id":                   r.ID,
		"ref":                  "document:" + firstNonEmpty(strings.TrimSpace(r.Handle.String), r.ID),
		"handle":               firstNonEmpty(strings.TrimSpace(r.Handle.String), r.ID),
		"state":                state,
		"supersedes":           supersedes,
		"head_revision_id":     r.HeadRevisionID,
		"head_revision_ref":    "document_revision:" + revisionHandle(firstNonEmpty(strings.TrimSpace(r.Handle.String), r.ID), r.HeadRevisionNum),
		"head_revision_number": r.HeadRevisionNum,
		"created_at":           r.CreatedAt,
		"created_by":           r.CreatedBy,
		"updated_at":           r.UpdatedAt,
		"updated_by":           r.UpdatedBy,
	}
	if subjectRef := canonicalDocumentSubjectRef(nullStringValue(r.ThreadID)); subjectRef != "" {
		out["subject_ref"] = subjectRef
	}
	headRevision := map[string]any{
		"revision_id":     r.HeadRevisionID,
		"revision_number": r.HeadRevisionNum,
	}
	if r.HeadArtifactID.Valid && strings.TrimSpace(r.HeadArtifactID.String) != "" {
		headRevision["artifact_id"] = r.HeadArtifactID.String
	}
	if r.HeadContentType.Valid && strings.TrimSpace(r.HeadContentType.String) != "" {
		headRevision["content_type"] = r.HeadContentType.String
	}
	if r.HeadCreatedAt.Valid && strings.TrimSpace(r.HeadCreatedAt.String) != "" {
		headRevision["created_at"] = r.HeadCreatedAt.String
	}
	if r.HeadCreatedBy.Valid && strings.TrimSpace(r.HeadCreatedBy.String) != "" {
		headRevision["created_by"] = r.HeadCreatedBy.String
	}
	out["head_revision"] = headRevision
	if r.ThreadID.Valid && strings.TrimSpace(r.ThreadID.String) != "" {
		out["thread_id"] = r.ThreadID.String
	}
	if r.Title.Valid && strings.TrimSpace(r.Title.String) != "" {
		out["title"] = r.Title.String
	}
	out["summary"] = strings.TrimSpace(r.Summary)
	out["source"] = strings.TrimSpace(r.Source)
	tags, err := decodeStoredJSONList(r.TagsJSON, "document.tags")
	if err != nil {
		return nil, err
	}
	out["tags"] = tags
	hosts, err := decodeStoredJSONList(r.HostsJSON, "document.hosts")
	if err != nil {
		return nil, err
	}
	out["hosts"] = hosts
	if verifiedAt := strings.TrimSpace(r.VerifiedAt); verifiedAt != "" {
		out["verified_at"] = verifiedAt
	}
	if r.Slug.Valid && strings.TrimSpace(r.Slug.String) != "" {
		out["slug"] = r.Slug.String
	}
	out["refs"] = refs
	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			provenance = map[string]any{}
		}
	}
	if len(provenance) == 0 {
		provenance = map[string]any{"sources": []string{"inferred"}}
	}
	out["provenance"] = provenance
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		out["trashed_at"] = r.TrashedAt.String
	}
	if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
		out["trashed_by"] = r.TrashedBy.String
	}
	if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
		out["trash_reason"] = r.TrashReason.String
	}
	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		out["archived_at"] = r.ArchivedAt.String
	}
	if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
		out["archived_by"] = r.ArchivedBy.String
	}

	if r.ListRevisionCount.Valid {
		out["revision_count"] = int(r.ListRevisionCount.Int64)
	}
	if r.ThreadID.Valid && strings.TrimSpace(r.ThreadID.String) != "" && r.ListTimelineMessageCount.Valid {
		out["timeline_message_count"] = int(r.ListTimelineMessageCount.Int64)
	}
	if body := strings.TrimSpace(r.ListLastCommentBody.String); body != "" && r.ListLastCommentAt.Valid {
		lastComment := map[string]any{
			"body":       body,
			"created_at": r.ListLastCommentAt.String,
		}
		if by := strings.TrimSpace(r.ListLastCommentBy.String); by != "" {
			lastComment["created_by"] = by
		}
		out["last_comment"] = lastComment
	}

	return out, nil
}

func invalidDocumentRequest(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDocumentRequest, strings.TrimSpace(message))
}

func invalidDocumentRequestError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidDocumentRequest, strings.TrimSpace(err.Error()))
}
