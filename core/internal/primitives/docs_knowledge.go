package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/handles"
)

const documentSearchTextMaxBytes = 64 * 1024
const documentSearchDefaultLimit = 50
const documentTagMaxCount = 32
const documentTagMaxLen = 64
const knowledgeTag = "knowledge"

type DocumentSearchFilter struct {
	Query     string
	Tag       string
	Host      string
	Knowledge bool
	Limit     *int
	Cursor    string
	States    []string
}

func documentTagFilterSQL(column, tag string, knowledge bool) (conditions []string, args []any) {
	tags := make([]string, 0, 2)
	if knowledge {
		tags = append(tags, knowledgeTag)
	}
	if t := strings.TrimSpace(tag); t != "" && !strings.EqualFold(t, knowledgeTag) {
		tags = append(tags, t)
	} else if t != "" && !knowledge {
		tags = append(tags, t)
	}
	for _, item := range uniqueStrings(tags) {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM json_each(`+column+`) WHERE json_each.value = ?)`)
		args = append(args, item)
	}
	return conditions, args
}

func normalizeDocumentTags(raw any, parent map[string]any, key string) ([]string, error) {
	if raw == nil {
		if parent != nil {
			if _, exists := parent[key]; !exists {
				return []string{}, nil
			}
		} else {
			return []string{}, nil
		}
	}
	parsed, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, invalidDocumentRequest("document.tags must be a list of strings")
	}
	out := make([]string, 0, len(parsed))
	seen := map[string]struct{}{}
	for _, item := range parsed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if utf8.RuneCountInString(item) > documentTagMaxLen {
			return nil, invalidDocumentRequest(fmt.Sprintf("document.tags entries must be at most %d characters", documentTagMaxLen))
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
		if len(out) > documentTagMaxCount {
			return nil, invalidDocumentRequest(fmt.Sprintf("document.tags supports at most %d entries", documentTagMaxCount))
		}
	}
	return out, nil
}

func documentSearchText(title, summary, source string, tags []string, content []byte, contentType string) string {
	parts := []string{
		strings.TrimSpace(title),
		strings.TrimSpace(summary),
		strings.TrimSpace(source),
		strings.Join(tags, " "),
	}
	if strings.TrimSpace(contentType) != "binary" {
		parts = append(parts, string(content))
	}
	text := strings.Join(parts, "\n")
	if len(text) <= documentSearchTextMaxBytes {
		return text
	}
	trunc := text[:documentSearchTextMaxBytes]
	for !utf8.ValidString(trunc) && len(trunc) > 0 {
		trunc = trunc[:len(trunc)-1]
	}
	return trunc
}

func fts5MatchQuery(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", invalidDocumentRequest("q is required")
	}
	terms := make([]string, 0, 8)
	for _, tok := range strings.Fields(q) {
		tok = strings.Trim(tok, `"'`)
		if tok == "" {
			continue
		}
		tok = strings.ReplaceAll(tok, `"`, `""`)
		terms = append(terms, `"`+tok+`"`)
	}
	if len(terms) == 0 {
		return "", invalidDocumentRequest("q is required")
	}
	return strings.Join(terms, " "), nil
}

func documentHostFilterSQL(column, host string) (string, []any) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", nil
	}
	return `EXISTS (SELECT 1 FROM json_each(` + column + `) WHERE json_each.value = ?)`, []any{host}
}

func normalizeDocumentHosts(raw any, parent map[string]any, key string) ([]string, error) {
	return normalizeDocumentTags(raw, parent, key)
}

func normalizeDocumentVerifiedAt(raw any, parent map[string]any, key string) (string, error) {
	if raw == nil {
		if parent != nil {
			if _, exists := parent[key]; !exists {
				return "", nil
			}
		}
		return "", nil
	}
	text := strings.TrimSpace(anyStringValue(raw))
	if text == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return text, nil
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano), nil
	}
	return "", invalidDocumentRequest("document.verified_at must be an RFC3339 timestamp")
}

func documentFTSTagsText(tags []string) string {
	return strings.Join(tags, " ")
}

func documentCommentSearchText(ctx context.Context, q documentFTSExecer, threadID string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", nil
	}
	rows, err := q.QueryContext(ctx, `SELECT COALESCE(json_extract(payload_json, '$.payload.text'), '')
		FROM events
		WHERE thread_id = ?
		  AND type = 'message_posted'
		  AND COALESCE(trim(trashed_at), '') = ''
		ORDER BY ts ASC, id ASC`, threadID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	parts := make([]string, 0)
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return "", err
		}
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}

type documentFTSExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func upsertDocumentFTSTx(ctx context.Context, tx documentFTSExecer, documentID, title, body, summary, source string, tags []string, threadID string) error {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return nil
	}
	comments, err := documentCommentSearchText(ctx, tx, threadID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_fts WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("clear document fts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_fts(document_id, title, body, summary, source, tags, comments)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		documentID,
		strings.TrimSpace(title),
		body,
		strings.TrimSpace(summary),
		strings.TrimSpace(source),
		documentFTSTagsText(tags),
		comments,
	); err != nil {
		return fmt.Errorf("insert document fts: %w", err)
	}
	return nil
}

func (s *Store) rebuildDocumentFTS(ctx context.Context, documentID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	doc, err := s.loadDocumentRow(ctx, documentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM document_fts WHERE document_id = ?`, documentID)
			return nil
		}
		return err
	}
	body := ""
	if revision, err := s.loadDocumentRevision(ctx, documentID, doc.HeadRevisionID, true); err == nil {
		body = strings.TrimSpace(anyStringValue(revision["content"]))
		if body == "" {
			body = strings.TrimSpace(anyStringValue(revision["body_text"]))
		}
	}
	tags, err := decodeStoredJSONList(doc.TagsJSON, "document.tags")
	if err != nil {
		return err
	}
	return upsertDocumentFTSTx(ctx, s.db, documentID, nullStringValue(doc.Title), body, doc.Summary, doc.Source, tags, nullStringValue(doc.ThreadID))
}

func allocateDocumentHandleTx(ctx context.Context, tx queryRower, requested, desired, fallbackSeed string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return uniqueHandleTx(ctx, tx, "document", desired, fallbackSeed)
	}
	candidate := handles.Candidate(requested, fallbackSeed)
	if candidate == "" {
		return "", invalidDocumentRequest("document.handle is invalid")
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM documents WHERE handle = ? OR id = ?`, candidate, candidate).Scan(&n); err != nil {
		return "", err
	}
	var aliasN int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM resource_handle_aliases WHERE resource_type = ? AND alias_handle = ?`, "document", candidate).Scan(&aliasN); err != nil {
		return "", err
	}
	if n > 0 || aliasN > 0 {
		return "", ErrConflict
	}
	return candidate, nil
}

func (s *Store) SearchDocuments(ctx context.Context, filter DocumentSearchFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	q := strings.TrimSpace(filter.Query)
	if q == "" {
		return nil, "", invalidDocumentRequest("q is required")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}
	limit := documentSearchDefaultLimit
	if filter.Limit != nil && *filter.Limit > 0 {
		limit = *filter.Limit
	}

	matchQuery, err := fts5MatchQuery(q)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{
		`document_fts MATCH ?`,
		LifecycleStatesOrGroup("d.archived_at", "d.trashed_at", filter.States),
	}
	args := []any{matchQuery}
	if tagConditions, nextTagArgs := documentTagFilterSQL("d.tags_json", filter.Tag, filter.Knowledge); len(tagConditions) > 0 {
		conditions = append(conditions, tagConditions...)
		args = append(args, nextTagArgs...)
	}
	if hostSQL, hostArgs := documentHostFilterSQL("d.hosts_json", filter.Host); hostSQL != "" {
		conditions = append(conditions, hostSQL)
		args = append(args, hostArgs...)
	}
	rankExpr := `CAST(ROUND((0.0 - bm25(document_fts, 0.0, 10.0, 4.0, 3.0, 3.0, 3.0, 2.0)) * 100) AS INTEGER)`
	query := `SELECT d.id, d.handle, d.thread_id, d.title, d.summary, d.source, d.tags_json, d.hosts_json, d.verified_at, d.slug, d.supersedes_json,
		d.refs_json, d.provenance_json,
		d.head_revision_id, d.head_revision_number, d.created_at, d.created_by, d.updated_at, d.updated_by,
		d.trashed_at, d.trashed_by, d.trash_reason,
		d.archived_at, d.archived_by,
		` + rankExpr + ` AS search_rank
		FROM document_fts
		JOIN documents d ON d.id = document_fts.document_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY bm25(document_fts, 0.0, 10.0, 4.0, 3.0, 3.0, 3.0, 2.0) ASC, d.updated_at DESC, d.id ASC
		LIMIT ?`
	args = append(args, limit+1)
	if filter.Cursor != "" {
		if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "fts5") || strings.Contains(errText, "syntax error") {
			return nil, "", invalidDocumentRequest("q is not a valid full-text query")
		}
		return nil, "", fmt.Errorf("search documents: %w", err)
	}
	defer rows.Close()

	documents := make([]map[string]any, 0)
	for rows.Next() {
		var row documentRow
		var rank int
		if err := rows.Scan(
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
			&rank,
		); err != nil {
			return nil, "", fmt.Errorf("scan document search row: %w", err)
		}
		document, mapErr := row.toMap()
		if mapErr != nil {
			return nil, "", mapErr
		}
		document["search_rank"] = rank
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate document search rows: %w", err)
	}

	var nextCursor string
	if len(documents) > limit {
		documents = documents[:limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + limit)
	}
	return documents, nextCursor, nil
}

func documentCommentFromEvent(documentRef string, event map[string]any, parentRef string) map[string]any {
	payload, _ := event["payload"].(map[string]any)
	body := strings.TrimSpace(anyStringValue(payload["text"]))
	if body == "" {
		body = strings.TrimSpace(anyStringValue(event["summary"]))
	}
	comment := map[string]any{
		"id":           strings.TrimSpace(anyStringValue(event["id"])),
		"ref":          strings.TrimSpace(anyStringValue(event["ref"])),
		"handle":       strings.TrimSpace(anyStringValue(event["handle"])),
		"document_ref": documentRef,
		"body":         body,
		"created_at":   firstNonEmpty(anyStringValue(event["ts"]), anyStringValue(event["created_at"])),
		"created_by":   strings.TrimSpace(anyStringValue(event["actor_id"])),
	}
	parent := strings.TrimSpace(anyStringValue(payload["reply_to_event_id"]))
	if parent != "" {
		comment["parent_id"] = parent
		replyTo := firstNonEmpty(strings.TrimSpace(parentRef), strings.TrimSpace(anyStringValue(payload["reply_to_ref"])), "event:"+parent)
		comment["reply_to"] = replyTo
	}
	if editedAt := strings.TrimSpace(anyStringValue(payload["edited_at"])); editedAt != "" {
		comment["edited_at"] = editedAt
	}
	if editedBy := strings.TrimSpace(anyStringValue(payload["edited_by"])); editedBy != "" {
		comment["edited_by"] = editedBy
	}
	return comment
}

func projectDocumentComments(documentRef string, events []map[string]any) []map[string]any {
	refsByID := make(map[string]string, len(events))
	for _, event := range events {
		id := strings.TrimSpace(anyStringValue(event["id"]))
		ref := strings.TrimSpace(anyStringValue(event["ref"]))
		if id != "" && ref != "" {
			refsByID[id] = ref
		}
	}
	comments := make([]map[string]any, 0, len(events))
	for _, event := range events {
		payload, _ := event["payload"].(map[string]any)
		parentID := strings.TrimSpace(anyStringValue(payload["reply_to_event_id"]))
		comments = append(comments, documentCommentFromEvent(documentRef, event, refsByID[parentID]))
	}
	return comments
}

func (s *Store) ListDocumentComments(ctx context.Context, documentID string, limit *int, cursor string) ([]map[string]any, string, error) {
	document, _, err := s.GetDocument(ctx, documentID)
	if err != nil {
		return nil, "", err
	}
	threadID := strings.TrimSpace(anyStringValue(document["thread_id"]))
	if threadID == "" {
		return []map[string]any{}, "", nil
	}
	documentRef := strings.TrimSpace(anyStringValue(document["ref"]))
	if limit != nil && *limit > 0 {
		page, err := s.ListEventsPage(ctx, EventListFilter{
			Types:    []string{"message_posted"},
			ThreadID: threadID,
			Limit:    *limit,
			Cursor:   cursor,
		})
		if err != nil {
			return nil, "", err
		}
		return projectDocumentComments(documentRef, page.Events), page.NextCursor, nil
	}
	events, err := s.ListEvents(ctx, EventListFilter{
		Types:    []string{"message_posted"},
		ThreadID: threadID,
	})
	if err != nil {
		return nil, "", err
	}
	return projectDocumentComments(documentRef, events), "", nil
}

func (s *Store) loadDocumentCommentEvent(ctx context.Context, documentID, commentID string) (document map[string]any, event map[string]any, err error) {
	document, _, err = s.GetDocument(ctx, documentID)
	if err != nil {
		return nil, nil, err
	}
	threadID := strings.TrimSpace(anyStringValue(document["thread_id"]))
	if threadID == "" {
		return nil, nil, invalidDocumentRequest("document has no backing thread for comments")
	}
	if resolved, resolveErr := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "event", Ref: commentID}); resolveErr == nil {
		commentID = resolved.ID
	}
	event, err = s.GetEvent(ctx, commentID)
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(anyStringValue(event["type"])) != "message_posted" {
		return nil, nil, invalidDocumentRequest("comment_id must be a document comment")
	}
	if strings.TrimSpace(anyStringValue(event["thread_id"])) != threadID {
		return nil, nil, invalidDocumentRequest("comment is not on this document")
	}
	if strings.TrimSpace(anyStringValue(event["trashed_at"])) != "" {
		return nil, nil, ErrNotFound
	}
	return document, event, nil
}

func (s *Store) CreateDocumentComment(ctx context.Context, actorID, documentID, text, parentID string) (map[string]any, error) {
	actorID = strings.TrimSpace(actorID)
	text = strings.TrimSpace(text)
	parentID = strings.TrimSpace(parentID)
	if actorID == "" {
		return nil, invalidDocumentRequest("actorID is required")
	}
	if text == "" {
		return nil, invalidDocumentRequest("text is required")
	}
	document, _, err := s.GetDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	threadID := strings.TrimSpace(anyStringValue(document["thread_id"]))
	if threadID == "" {
		return nil, invalidDocumentRequest("document has no backing thread for comments")
	}
	documentRef := strings.TrimSpace(anyStringValue(document["ref"]))
	parentRef := ""
	if parentID != "" {
		if resolved, resolveErr := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "event", Ref: parentID}); resolveErr == nil {
			parentID = resolved.ID
		}
		parent, err := s.GetEvent(ctx, parentID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, invalidDocumentRequest("parent comment not found")
			}
			return nil, err
		}
		if strings.TrimSpace(anyStringValue(parent["type"])) != "message_posted" {
			return nil, invalidDocumentRequest("parent_id must be a document comment")
		}
		if strings.TrimSpace(anyStringValue(parent["thread_id"])) != threadID {
			return nil, invalidDocumentRequest("parent comment is not on this document")
		}
		parentID = strings.TrimSpace(anyStringValue(parent["id"]))
		parentRef = strings.TrimSpace(anyStringValue(parent["ref"]))
	}

	payload := map[string]any{
		"kind":         "document_message",
		"text":         text,
		"subject_ref":  documentRef,
		"subject_kind": "document",
		"subject_id":   documentID,
	}
	refs := []string{documentRef, "thread:" + threadID}
	if parentID != "" {
		payload["reply_to_event_id"] = parentID
		if parentRef != "" {
			payload["reply_to_ref"] = parentRef
		}
		refs = append(refs, firstNonEmpty(parentRef, "event:"+parentID))
	}
	event := map[string]any{
		"type":      "message_posted",
		"actor_id":  actorID,
		"thread_id": threadID,
		"refs":      uniqueStrings(refs),
		"summary":   truncatePreview(text),
		"payload":   payload,
		"provenance": map[string]any{
			"sources": []string{"event:" + provenanceEventIDPlaceholder},
		},
	}
	created, err := s.AppendEvent(ctx, actorID, event)
	if err != nil {
		return nil, err
	}
	if err := s.rebuildDocumentFTS(ctx, documentID); err != nil {
		return nil, err
	}
	return documentCommentFromEvent(documentRef, created, parentRef), nil
}

func (s *Store) UpdateDocumentComment(ctx context.Context, actorID, documentID, commentID, text string) (map[string]any, error) {
	actorID = strings.TrimSpace(actorID)
	text = strings.TrimSpace(text)
	if actorID == "" {
		return nil, invalidDocumentRequest("actorID is required")
	}
	if text == "" {
		return nil, invalidDocumentRequest("text is required")
	}
	document, event, err := s.loadDocumentCommentEvent(ctx, documentID, commentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(anyStringValue(event["actor_id"])) != actorID {
		return nil, ErrForbidden
	}
	payload, _ := event["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload["text"] = text
	payload["edited_at"] = now
	payload["edited_by"] = actorID
	// Bounded exception to append-only event payloads: keep the same event
	// identity so UI deep-links (`ref`) stay valid across author edits.
	wrapper := map[string]any{
		"payload": payload,
		"summary": truncatePreview(text),
	}
	if provenance, ok := event["provenance"]; ok && provenance != nil {
		wrapper["provenance"] = provenance
	}
	payloadJSON, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("marshal edited comment: %w", err)
	}
	eventID := strings.TrimSpace(anyStringValue(event["id"]))
	if _, err := s.db.ExecContext(ctx, `UPDATE events SET payload_json = ? WHERE id = ?`, string(payloadJSON), eventID); err != nil {
		return nil, fmt.Errorf("edit document comment: %w", err)
	}
	if err := s.rebuildDocumentFTS(ctx, strings.TrimSpace(anyStringValue(document["id"]))); err != nil {
		return nil, err
	}
	updated, err := s.GetEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	parentRef := strings.TrimSpace(anyStringValue(payload["reply_to_ref"]))
	return documentCommentFromEvent(strings.TrimSpace(anyStringValue(document["ref"])), updated, parentRef), nil
}

func (s *Store) DeleteDocumentComment(ctx context.Context, actorID, documentID, commentID string) (map[string]any, error) {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, invalidDocumentRequest("actorID is required")
	}
	document, event, err := s.loadDocumentCommentEvent(ctx, documentID, commentID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(anyStringValue(event["actor_id"])) != actorID {
		return nil, ErrForbidden
	}
	eventID := strings.TrimSpace(anyStringValue(event["id"]))
	trashed, err := s.TrashEvent(ctx, actorID, eventID, "document comment deleted")
	if err != nil {
		return nil, err
	}
	if err := s.rebuildDocumentFTS(ctx, strings.TrimSpace(anyStringValue(document["id"]))); err != nil {
		return nil, err
	}
	payload, _ := event["payload"].(map[string]any)
	parentRef := ""
	if payload != nil {
		parentRef = strings.TrimSpace(anyStringValue(payload["reply_to_ref"]))
	}
	return documentCommentFromEvent(strings.TrimSpace(anyStringValue(document["ref"])), trashed, parentRef), nil
}

func truncatePreview(text string) string {
	text = strings.TrimSpace(text)
	const maxRunes = 120
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "…"
}
