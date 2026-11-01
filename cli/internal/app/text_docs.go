// Default text projections for documents: records, content, comments,
// revisions and doc list sections.

package app

import (
	"fmt"
	"strings"
)

func formatDocumentRecord(body any) string {
	root := asMap(body)
	document := extractNestedMap(root, "document")
	revision := extractNestedMap(root, "revision")
	lines := []string{"Document " + displayPublicIdentity(document, "document")}
	lines = appendScalar(lines, "title", document, "title")
	lines = appendScalar(lines, "source", document, "source")
	if tags := stringList(document["tags"]); len(tags) > 0 {
		lines = append(lines, "tags: "+strings.Join(tags, ", "))
	}
	lines = appendScalar(lines, "kind", document, "kind")
	lines = appendScalar(lines, "head_revision_id", document, "head_revision_id")
	lines = appendScalar(lines, "revision_id", revision, "revision_id")
	lines = appendScalar(lines, "revision_number", revision, "revision_number")
	lines = appendScalar(lines, "content_type", revision, "content_type")
	if trashedAt := anyString(document["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", document, "trashed_at")
		lines = appendScalar(lines, "trashed_by", document, "trashed_by")
		lines = appendScalar(lines, "trash_reason", document, "trash_reason")
	} else if archivedAt := anyString(document["archived_at"]); archivedAt != "" {
		lines = append(lines, "⚠ ARCHIVED")
		lines = appendScalar(lines, "archived_at", document, "archived_at")
		lines = appendScalar(lines, "archived_by", document, "archived_by")
	}
	if content := firstNonEmpty(anyString(revision["content"]), anyString(root["content"]), anyString(root["body_text"])); content != "" {
		lines = append(lines, "content:")
		lines = append(lines, indentBlock(strings.TrimSpace(content))...)
	}
	return strings.Join(lines, "\n")
}

func formatDocumentContentRecord(body any) string {
	root := asMap(body)
	document := extractNestedMap(root, "document")
	revision := extractNestedMap(root, "revision")
	lines := []string{"Document " + displayPublicIdentity(document, "document")}
	lines = appendScalar(lines, "revision_id", revision, "revision_id")
	lines = appendScalar(lines, "revision_number", revision, "revision_number")
	lines = appendScalar(lines, "content_type", revision, "content_type")
	content := firstNonEmpty(anyString(root["content"]), anyString(revision["content"]), anyString(root["body_text"]))
	if content == "" {
		lines = append(lines, "content: (empty)")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "content:")
	lines = append(lines, indentBlock(strings.TrimSpace(content))...)
	return strings.Join(lines, "\n")
}

func formatDocsComments(body any) string {
	root := asMap(body)
	if root == nil {
		return formatPrettyBody(body)
	}
	comments := asSlice(root["comments"])
	lines := []string{fmt.Sprintf("Document comments (%d)", len(comments))}
	if len(comments) == 0 {
		lines = append(lines, "(none)")
		return strings.Join(lines, "\n")
	}
	for i, row := range comments {
		m := asMap(row)
		if m == nil {
			continue
		}
		lines = append(lines, formatDocsCommentLine(i+1, m))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func formatDocsComment(body any) string {
	root := asMap(body)
	comment := extractNestedMap(root, "comment")
	if comment == nil {
		return formatPrettyBody(body)
	}
	return strings.TrimRight(formatDocsCommentLine(1, comment), "\n")
}

func formatDocsCommentLine(index int, comment map[string]any) string {
	id := firstNonEmpty(anyString(comment["ref"]), anyString(comment["handle"]), anyString(comment["id"]))
	lines := []string{fmt.Sprintf("%d) %s", index, id)}
	if parent := strings.TrimSpace(anyString(comment["parent_id"])); parent != "" {
		lines = append(lines, "   parent: "+parent)
	}
	if body := strings.TrimSpace(anyString(comment["body"])); body != "" {
		lines = append(lines, "   body: "+body)
	}
	if actor := strings.TrimSpace(anyString(comment["created_by"])); actor != "" {
		lines = append(lines, "   actor: "+actor)
	}
	if ts := strings.TrimSpace(anyString(comment["created_at"])); ts != "" {
		lines = append(lines, "   ts: "+ts)
	}
	return strings.Join(lines, "\n")
}

func formatRevisionRecord(revision map[string]any) string {
	if revision == nil {
		return formatPrettyBody(revision)
	}
	lines := []string{"Revision " + displayPublicIdentity(revision, "revision")}
	lines = appendScalar(lines, "revision_number", revision, "revision_number")
	lines = appendScalar(lines, "content_type", revision, "content_type")
	lines = appendScalar(lines, "content_hash", revision, "content_hash")
	lines = appendScalar(lines, "revision_hash", revision, "revision_hash")
	if content := firstNonEmpty(anyString(revision["content"]), anyString(revision["body_text"])); content != "" {
		lines = append(lines, "content:")
		lines = append(lines, indentBlock(strings.TrimSpace(content))...)
	}
	return strings.Join(lines, "\n")
}

func renderRevisionListItem(item map[string]any, listID string) string {
	return compactSummary(listID, anyString(item["revision_number"]), anyString(item["created_at"]))
}

func renderDocumentListItem(item map[string]any, listID string) string {
	headRevision := asMap(item["head_revision"])
	stateParts := make([]string, 0, 3)
	if status := strings.TrimSpace(anyString(item["status"])); status != "" {
		stateParts = append(stateParts, status)
	}
	if revisionNumber := intValue(headRevision["revision_number"]); revisionNumber > 0 {
		stateParts = append(stateParts, fmt.Sprintf("v%d", revisionNumber))
	}
	if contentType := strings.TrimSpace(anyString(headRevision["content_type"])); contentType != "" {
		stateParts = append(stateParts, contentType)
	}
	return compactSummary(
		listID,
		strings.Join(stateParts, " "),
		firstNonEmpty(anyString(item["title"]), anyString(item["slug"])),
		firstNonEmpty(anyString(item["updated_at"]), anyString(headRevision["created_at"])),
	)
}

func appendDocumentListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		headRevision := asMap(item["head_revision"])
		identifier := displayPublicIdentity(item, "document")
		if fullID {
			if id := strings.TrimSpace(anyString(item["id"])); id != "" {
				identifier = id
			}
		}
		stateParts := make([]string, 0, 3)
		if status := strings.TrimSpace(anyString(item["status"])); status != "" {
			stateParts = append(stateParts, status)
		}
		if revisionNumber := intValue(headRevision["revision_number"]); revisionNumber > 0 {
			stateParts = append(stateParts, fmt.Sprintf("v%d", revisionNumber))
		}
		if contentType := strings.TrimSpace(anyString(headRevision["content_type"])); contentType != "" {
			stateParts = append(stateParts, contentType)
		}
		lines = append(lines, "- "+compactSummary(
			identifier,
			strings.Join(stateParts, " "),
			firstNonEmpty(anyString(item["title"]), anyString(item["slug"])),
			firstNonEmpty(anyString(item["updated_at"]), anyString(headRevision["created_at"])),
		))
	}
	return lines
}
