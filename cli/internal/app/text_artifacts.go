// Default text projections for artifacts: records, inspect views, MIME badges
// and list rows.

package app

import (
	"fmt"
	"strings"
)

func shortMimeBadgeFromContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if ct == "" {
		return ""
	}
	slash := strings.Index(ct, "/")
	if slash <= 0 || slash >= len(ct)-1 {
		return "FILE"
	}
	major := ct[:slash]
	minor := ct[slash+1:]
	minorBase := minor
	if plus := strings.Index(minor, "+"); plus >= 0 {
		minorBase = minor[:plus]
	}
	switch minorBase {
	case "pdf":
		return "PDF"
	case "json":
		return "JSON"
	case "markdown", "x-markdown":
		return "MD"
	case "plain":
		return "TXT"
	case "csv":
		return "CSV"
	case "zip":
		return "ZIP"
	}
	if major == "text" {
		return strings.ToUpper(minorBase)
	}
	if major == "image" {
		return strings.ToUpper(minorBase)
	}
	if major == "application" {
		if minorBase == "octet-stream" {
			return "FILE"
		}
		return strings.ToUpper(minorBase)
	}
	return "FILE"
}

func formatArtifactRecord(artifact map[string]any) string {
	if artifact == nil {
		return formatPrettyBody(artifact)
	}

	kindStr := strings.ToLower(strings.TrimSpace(anyString(artifact["kind"])))
	lines := []string(nil)
	if kindStr == "attachment" {
		fn := strings.TrimSpace(anyString(artifact["original_filename"]))
		if fn == "" {
			fn = strings.TrimSpace(anyString(artifact["summary"]))
		}
		if fn != "" {
			lines = append(lines, fn)
		}
		if badge := shortMimeBadgeFromContentType(anyString(artifact["content_type"])); badge != "" {
			lines = append(lines, badge)
		}
		lines = append(lines, "Artifact "+displayPublicIdentity(artifact, "artifact"))
		lines = appendScalar(lines, "kind", artifact, "kind")
		lines = appendScalar(lines, "thread_id", artifact, "thread_id")
		lines = appendScalar(lines, "created_at", artifact, "created_at")
		lines = appendScalar(lines, "summary", artifact, "summary", "title")
		lines = appendScalar(lines, "content_hash", artifact, "content_hash")
		lines = appendStringList(lines, "refs", stringList(artifact["refs"]))
		if trashedAt := anyString(artifact["trashed_at"]); trashedAt != "" {
			lines = append(lines, "⚠ TRASHED")
			lines = appendScalar(lines, "trashed_at", artifact, "trashed_at")
			lines = appendScalar(lines, "trashed_by", artifact, "trashed_by")
			lines = appendScalar(lines, "trash_reason", artifact, "trash_reason")
		} else if archivedAt := anyString(artifact["archived_at"]); archivedAt != "" {
			lines = append(lines, "⚠ ARCHIVED")
			lines = appendScalar(lines, "archived_at", artifact, "archived_at")
			lines = appendScalar(lines, "archived_by", artifact, "archived_by")
		}
		return strings.Join(lines, "\n")
	}

	lines = []string{"Artifact " + displayPublicIdentity(artifact, "artifact")}
	lines = appendScalar(lines, "kind", artifact, "kind")
	lines = appendScalar(lines, "thread_id", artifact, "thread_id")
	lines = appendScalar(lines, "content_type", artifact, "content_type")
	lines = appendScalar(lines, "created_at", artifact, "created_at")
	lines = appendScalar(lines, "summary", artifact, "summary", "title")
	lines = appendScalar(lines, "content_hash", artifact, "content_hash")
	lines = appendStringList(lines, "refs", stringList(artifact["refs"]))
	if trashedAt := anyString(artifact["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", artifact, "trashed_at")
		lines = appendScalar(lines, "trashed_by", artifact, "trashed_by")
		lines = appendScalar(lines, "trash_reason", artifact, "trash_reason")
	} else if archivedAt := anyString(artifact["archived_at"]); archivedAt != "" {
		lines = append(lines, "⚠ ARCHIVED")
		lines = appendScalar(lines, "archived_at", artifact, "archived_at")
		lines = appendScalar(lines, "archived_by", artifact, "archived_by")
	}
	return strings.Join(lines, "\n")
}

func formatArtifactInspect(body any) string {
	root := asMap(body)
	artifact := extractNestedMap(root, "artifact")
	content := extractNestedMap(root, "content")
	lines := []string{formatArtifactRecord(artifact)}
	lines = appendScalar(lines, "content_bytes", content, "bytes")
	if bodyText := strings.TrimSpace(anyString(content["body_text"])); bodyText != "" {
		lines = append(lines, "content:")
		lines = append(lines, indentBlock(bodyText)...)
	}
	return strings.Join(lines, "\n")
}

func renderArtifactListItem(item map[string]any, listID string) string {
	return renderArtifactListItemRow(item, listID)
}

func renderArtifactListItemWithMode(item map[string]any, fullID bool) string {
	artifact := item
	if nested := asMap(item["artifact"]); nested != nil {
		artifact = nested
	}
	return renderArtifactListItemRow(item, displayCompactIDWithMode(artifact, fullID))
}

func renderArtifactListItemRow(item map[string]any, listID string) string {
	artifact := item
	if nested := asMap(item["artifact"]); nested != nil {
		artifact = nested
	}
	summary := firstNonEmpty(anyString(artifact["summary"]), anyString(artifact["title"]))
	if summary == "" {
		summary = anyString(item["content_preview"])
	}
	ref := strings.TrimSpace(anyString(item["ref"]))
	if ref != "" {
		summary = firstNonEmpty(summary, "ref="+ref)
	}
	return compactSummary(listID, anyString(artifact["kind"]), summary)
}

func appendArtifactListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderArtifactListItemWithMode(item, fullID))
	}
	return lines
}
