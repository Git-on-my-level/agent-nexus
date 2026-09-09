package primitives

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

func likeLiteral(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return "%" + strings.ToLower(q) + "%"
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

	pattern := likeLiteral(q)
	conditions := []string{LifecycleStatesOrGroup("d.archived_at", "d.trashed_at", filter.States)}
	var tagArgs []any
	if tagConditions, nextTagArgs := documentTagFilterSQL("d.tags_json", filter.Tag, filter.Knowledge); len(tagConditions) > 0 {
		conditions = append(conditions, tagConditions...)
		tagArgs = nextTagArgs
	}
	rankExpr := `(CASE WHEN LOWER(COALESCE(d.title, '')) LIKE ? ESCAPE '\' THEN 40 ELSE 0 END) +
		(CASE WHEN LOWER(COALESCE(d.search_text, '')) LIKE ? ESCAPE '\' THEN 30 ELSE 0 END) +
		(CASE WHEN LOWER(COALESCE(d.summary, '')) LIKE ? ESCAPE '\' THEN 20 ELSE 0 END) +
		(CASE WHEN LOWER(COALESCE(d.source, '')) LIKE ? ESCAPE '\' THEN 20 ELSE 0 END) +
		(CASE WHEN LOWER(COALESCE(d.tags_json, '')) LIKE ? ESCAPE '\' THEN 20 ELSE 0 END) +
		(CASE WHEN EXISTS (
			SELECT 1 FROM events e
			WHERE e.thread_id = d.thread_id
			  AND e.type = 'message_posted'
			  AND COALESCE(trim(e.trashed_at), '') = ''
			  AND LOWER(COALESCE(e.payload_json, '')) LIKE ? ESCAPE '\'
		) THEN 10 ELSE 0 END)`
	rankArgs := []any{pattern, pattern, pattern, pattern, pattern, pattern}
	inner := `SELECT d.id, d.handle, d.thread_id, d.title, d.summary, d.source, d.tags_json, d.slug, d.supersedes_json,
		d.refs_json, d.provenance_json,
		d.head_revision_id, d.head_revision_number, d.created_at, d.created_by, d.updated_at, d.updated_by,
		d.trashed_at, d.trashed_by, d.trash_reason,
		d.archived_at, d.archived_by,
		` + rankExpr + ` AS search_rank
		FROM documents d
		WHERE ` + strings.Join(conditions, " AND ") + `
		  AND (` + rankExpr + `) > 0
		ORDER BY search_rank DESC, d.updated_at DESC, d.id ASC
		LIMIT ?`
	// Same page-scoped enrichment joins as ListDocuments so search rows carry
	// revision_count, timeline_message_count, and last_comment.
	query := `WITH doc_page AS (` + inner + `)
SELECT dp.*,
	COALESCE(rc.revision_cnt, 0),
	COALESCE(tmc.timeline_msg_cnt, 0),
	tml.last_body, tml.last_at, tml.last_by
FROM doc_page dp
LEFT JOIN (
	SELECT dr2.document_id, COUNT(*) AS revision_cnt FROM document_revisions dr2
	WHERE dr2.document_id IN (SELECT id FROM doc_page)
	GROUP BY dr2.document_id
) rc ON dp.id = rc.document_id
LEFT JOIN (
	SELECT trim(COALESCE(e.thread_id,'')) AS tid, COUNT(*) AS timeline_msg_cnt FROM events e
	WHERE e.type = 'message_posted'
	  AND COALESCE(trim(e.thread_id),'') <> ''
	  AND COALESCE(trim(e.trashed_at),'') = ''
	  AND trim(COALESCE(e.thread_id,'')) IN (
		SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
	  )
	GROUP BY tid
) tmc ON trim(COALESCE(dp.thread_id,'')) = tmc.tid
LEFT JOIN (
	SELECT tid, last_body, last_at, last_by FROM (
		SELECT trim(COALESCE(e.thread_id,'')) AS tid,
			TRIM(COALESCE(NULLIF(json_extract(e.payload_json, '$.text'), ''), json_extract(e.payload_json, '$.summary'))) AS last_body,
			e.ts AS last_at, e.actor_id AS last_by,
			ROW_NUMBER() OVER (PARTITION BY trim(COALESCE(e.thread_id,'')) ORDER BY e.ts DESC, e.id DESC) AS rn
		FROM events e
		WHERE e.type = 'message_posted'
		  AND COALESCE(trim(e.thread_id),'') <> ''
		  AND COALESCE(trim(e.trashed_at),'') = ''
		  AND trim(COALESCE(e.thread_id,'')) IN (
			SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
		  )
	) ranked WHERE rn = 1
) tml ON trim(COALESCE(dp.thread_id,'')) = tml.tid`
	args := make([]any, 0, len(rankArgs)*2+len(tagArgs)+2)
	args = append(args, rankArgs...)
	args = append(args, tagArgs...)
	args = append(args, rankArgs...)
	args = append(args, limit+1)
	if filter.Cursor != "" {
		if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
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
			&row.ListRevisionCount,
			&row.ListTimelineMessageCount,
			&row.ListLastCommentBody,
			&row.ListLastCommentAt,
			&row.ListLastCommentBy,
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

func documentCommentFromEvent(documentRef string, event map[string]any) map[string]any {
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
	}
	return comment
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
		documentRef := strings.TrimSpace(anyStringValue(document["ref"]))
		comments := make([]map[string]any, 0, len(page.Events))
		for _, event := range page.Events {
			comments = append(comments, documentCommentFromEvent(documentRef, event))
		}
		return comments, page.NextCursor, nil
	}
	events, err := s.ListEvents(ctx, EventListFilter{
		Types:    []string{"message_posted"},
		ThreadID: threadID,
	})
	if err != nil {
		return nil, "", err
	}
	documentRef := strings.TrimSpace(anyStringValue(document["ref"]))
	comments := make([]map[string]any, 0, len(events))
	for _, event := range events {
		comments = append(comments, documentCommentFromEvent(documentRef, event))
	}
	return comments, "", nil
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
		refs = append(refs, "event:"+parentID)
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
	return documentCommentFromEvent(documentRef, created), nil
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
