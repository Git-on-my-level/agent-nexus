// Generic list rendering machinery: scan-friendly id disambiguation and
// grouping, named list formatting, and shared list section appending.

package app

import (
	"fmt"
	"strings"
)

const textListScanTailRunes = 10

func listKindIDPrefixes(kind string) []string {
	switch strings.TrimSpace(kind) {
	case "board":
		return []string{"bd-", "board-"}
	case "thread":
		return []string{"thread-", "thread_"}
	case "document":
		return []string{"dc-", "document-", "document_", "doc-", "doc_"}
	case "artifact":
		return []string{"rc-", "rv-", "rce-", "rve-", "artifact-", "artifact_"}
	case "topic":
		return []string{"tp-", "topic-", "topic_"}
	case "card":
		return []string{"cd-", "card-", "card_"}
	case "event":
		return []string{"ev-", "event-", "event_"}
	default:
		return nil
	}
}

func stripListKindPrefix(kind, id string) (tail string, ok bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	for _, prefix := range listKindIDPrefixes(kind) {
		if prefix != "" && strings.HasPrefix(id, prefix) {
			t := strings.TrimPrefix(id, prefix)
			if t != "" {
				return t, true
			}
		}
	}
	return "", false
}

func truncateFirstRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	var b strings.Builder
	count := 0
	for _, r := range s {
		b.WriteRune(r)
		count++
		if count >= n {
			break
		}
	}
	return b.String()
}

func listItemIdentityForScan(item map[string]any, kind string) map[string]any {
	if item == nil {
		return nil
	}
	switch strings.TrimSpace(kind) {
	case "artifact":
		if nested := asMap(item["artifact"]); nested != nil {
			return nested
		}
	case "board":
		if nested := asMap(item["board"]); nested != nil {
			return nested
		}
	}
	return item
}

func displayListScanID(item map[string]any, kind string, fullID bool) string {
	target := listItemIdentityForScan(item, kind)
	if target == nil {
		return ""
	}
	if fullID {
		id := strings.TrimSpace(anyString(target["id"]))
		if id == "" {
			id = strings.TrimSpace(anyString(target["revision_id"]))
		}
		return id
	}
	return displayPublicIdentity(target, kind)
}

func disambiguateListScanIDs(items []map[string]any, kind string, fullID bool) []string {
	n := len(items)
	out := make([]string, n)
	if n == 0 {
		return out
	}
	if fullID {
		for i, item := range items {
			out[i] = displayListScanID(item, kind, true)
		}
		return out
	}
	scans := make([]string, n)
	for i, item := range items {
		scans[i] = displayListScanID(item, kind, false)
	}
	counts := make(map[string]int, n)
	for _, s := range scans {
		if s != "" {
			counts[s]++
		}
	}
	for i, item := range items {
		s := scans[i]
		if s == "" {
			out[i] = displayCompactIDWithMode(listItemIdentityForScan(item, kind), false)
			continue
		}
		if counts[s] <= 1 {
			out[i] = s
			continue
		}
		short := displayCompactIDWithMode(listItemIdentityForScan(item, kind), false)
		if short == "" {
			out[i] = s
		} else {
			out[i] = s + " [" + short + "]"
		}
	}
	return out
}

func appendGroupedScanList(lines []string, sectionTitle string, rawItems []any, kind string, fullID bool, renderRow func(item map[string]any, listID string) string) []string {
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if m := asMap(raw); m != nil {
			items = append(items, m)
		}
	}
	if len(items) == 0 {
		return lines
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("%s (%d):", sectionTitle, len(items)))
	labels := disambiguateListScanIDs(items, kind, fullID)
	for i, item := range items {
		lines = append(lines, "- "+renderRow(item, labels[i]))
	}
	return lines
}

func formatNamedList(body any, field string, label string, kind string, render func(map[string]any, string) string) string {
	root := asMap(body)
	rawItems := asSlice(root[field])
	items := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item := asMap(raw); item != nil {
			items = append(items, item)
		}
	}
	fullID := asBool(root["full_id"])
	scanIDs := disambiguateListScanIDs(items, kind, fullID)
	lines := []string{fmt.Sprintf("%s (%d):", label, len(items))}
	for i, item := range items {
		lines = append(lines, "- "+render(item, scanIDs[i]))
	}
	return strings.Join(lines, "\n")
}

func compactSummary(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "(empty)"
	}
	return strings.Join(clean, " :: ")
}

func appendListSection(lines []string, label string, items []any, render func(map[string]any) string) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+render(item))
	}
	return lines
}
