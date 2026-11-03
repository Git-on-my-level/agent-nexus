package app

import (
	"fmt"
	"strings"
)

// Card history pages are metadata only; content comes from `cards revision
// get` (one revision) or `cards revision diff` (a bounded change).

func renderCardRevisionListItem(item map[string]any, listID string) string {
	number := anyString(item["revision_number"])
	if number != "" {
		number = "r" + number
	}
	size := ""
	if n := intValue(item["summary_bytes"]); n > 0 {
		size = fmt.Sprintf("summary_bytes=%d", n)
	}
	return compactSummary(listID, number, anyString(item["created_at"]), anyString(item["created_by"]), size)
}

func formatCardRevisionsPage(body any) string {
	text := formatNamedList(body, "revisions", "Revisions", "revision", renderCardRevisionListItem)
	if cursor := strings.TrimSpace(anyString(asMap(body)["next_cursor"])); cursor != "" {
		text += "\nnext_cursor: " + cursor
	}
	return text
}

func formatCardRevisionRecord(revision map[string]any) string {
	if revision == nil {
		return formatPrettyBody(revision)
	}
	lines := []string{"Revision " + displayPublicIdentity(revision, "revision")}
	lines = appendScalar(lines, "revision_number", revision, "revision_number")
	lines = appendScalar(lines, "created_at", revision, "created_at")
	lines = appendScalar(lines, "created_by", revision, "created_by")
	lines = appendScalar(lines, "summary_bytes", revision, "summary_bytes")
	lines = appendScalar(lines, "content_hash", revision, "content_hash")
	lines = appendScalar(lines, "title", revision, "title")
	if dod := asSlice(revision["definition_of_done"]); len(dod) > 0 {
		lines = append(lines, "definition_of_done:")
		for _, item := range dod {
			lines = append(lines, "  - "+anyString(item))
		}
	}
	if summary := anyString(revision["summary"]); strings.TrimSpace(summary) != "" {
		lines = append(lines, "summary:")
		lines = append(lines, indentBlock(summary)...)
	}
	return strings.Join(lines, "\n")
}

func formatCardRevisionDiff(body any) string {
	root := asMap(body)
	revision := asMap(root["revision"])
	base := "(empty)"
	if b := asMap(root["base_revision"]); b != nil {
		base = displayPublicIdentity(b, "revision")
	}
	lines := []string{fmt.Sprintf("Diff %s -> %s", base, displayPublicIdentity(revision, "revision"))}
	if asBool(root["identical"]) {
		return strings.Join(append(lines, "identical: true"), "\n")
	}
	if title := asMap(root["title"]); title != nil {
		lines = append(lines, "title: "+anyString(title["before"])+" -> "+anyString(title["after"]))
	}
	if dod := asMap(root["definition_of_done"]); dod != nil {
		for _, item := range asSlice(dod["removed"]) {
			lines = append(lines, "definition_of_done removed: "+anyString(item))
		}
		for _, item := range asSlice(dod["added"]) {
			lines = append(lines, "definition_of_done added: "+anyString(item))
		}
	}
	summary := asMap(root["summary"])
	if asBool(summary["changed"]) {
		lines = append(lines, fmt.Sprintf("summary: +%d -%d lines", intValue(summary["added_lines"]), intValue(summary["removed_lines"])))
		if asBool(summary["truncated"]) {
			lines = append(lines, "warning code=diff_truncated the unified diff was cut at 256 KiB")
		}
		if unified := anyString(summary["unified"]); unified != "" {
			lines = append(lines, strings.TrimRight(unified, "\n"))
		}
	}
	return strings.Join(lines, "\n")
}
