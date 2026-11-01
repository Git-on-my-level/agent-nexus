// Payload shaping shared by board and board-card handlers: list and
// membership response sections, typed-ref extraction, write-map preparation,
// derived card summary/freshness, and the small sorted-string helpers those
// views use.

package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func boardListItemsResponse(items []primitives.BoardListItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"board":   item.Board,
			"summary": item.Summary,
		})
	}
	return out
}

func boardMembershipSectionResponse(items []primitives.BoardMembership) map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"board": item.Board,
			"card":  publicCardView(item.Card),
		})
	}
	return map[string]any{
		"items": out,
		"count": len(out),
	}
}

func nullableTypedRef(prefix string, id string) any {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return prefix + ":" + id
}

func boardRefs(board map[string]any) []string {
	refs, err := extractStringSlice(board["refs"])
	if err == nil {
		return uniqueSortedStrings(refs)
	}
	return []string{}
}

func prepareBoardWriteMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	mergeBoardHTTPConvenienceFields(out)
	return out
}

// mergeBoardHTTPConvenienceFields folds the legacy primary_document_id field (not in OpenAPI) into
// `refs` so validation and the store see a single typed-ref list. Canonical create/patch inputs use
// document_refs / pinned_refs per OpenAPI; normalizeBoardRefs in the store merges those into `refs`.
func mergeBoardHTTPConvenienceFields(board map[string]any) {
	if board == nil {
		return
	}
	legacyDocumentField := "primary" + "_document_id"
	refs := boardRefs(board)
	changed := false
	if did := strings.TrimSpace(anyString(board[legacyDocumentField])); did != "" {
		refs = append(refs, "document:"+did)
		changed = true
	}
	if changed {
		board["refs"] = uniqueSortedStrings(refs)
	}
	delete(board, legacyDocumentField)
}

func boardDocumentRefs(board map[string]any) []string {
	if refs, err := extractStringSlice(board["document_refs"]); err == nil {
		return uniqueSortedStrings(refs)
	}
	return []string{}
}

func boardThreadRefs(board map[string]any) []string {
	refs := make([]string, 0)
	for _, ref := range boardRefs(board) {
		prefix, value, err := schema.SplitTypedRef(ref)
		if err != nil || prefix != "thread" {
			continue
		}
		refs = append(refs, "thread:"+value)
	}
	return uniqueSortedStrings(refs)
}

func validateBoardTypedRefs(contract *schema.Contract, board map[string]any, field string) error {
	raw, exists := board[field]
	if !exists || raw == nil {
		return nil
	}
	refs, err := extractStringSlice(raw)
	if err != nil {
		return fmt.Errorf("board.%s must be a list of strings", field)
	}
	return schema.ValidateTypedRefs(contract, refs)
}

func loadBoardCardPinnedDocument(ctx context.Context, opts handlerOptions, card map[string]any) (any, error) {
	documentID := strings.TrimSpace(pinnedDocumentIDFromCard(card))
	if documentID == "" {
		return nil, nil
	}
	document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	return document, nil
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	last := ""
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == last {
			continue
		}
		out = append(out, value)
		last = value
	}
	return out
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsTypedRefPrefix(refs []string, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, ref := range refs {
		refPrefix, _, err := schema.SplitTypedRef(strings.TrimSpace(ref))
		if err == nil && refPrefix == prefix {
			return true
		}
	}
	return false
}

func boardCardDerivedSummary(card map[string]any, threadID string, states map[string]topicProjectionState, now time.Time) any {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	projection := states[threadID].Projection
	_, dueAt, hasDueAt := boardCardRiskState(card, now, defaultInboxRiskHorizon)
	summary := map[string]any{
		"decision_request_count": projection.DecisionRequestCount,
		"decision_count":         projection.DecisionCount,
		"recommendation_count":   projection.RecommendationCount,
		"document_count":         projection.DocumentCount,
		"inbox_count":            projection.InboxCount,
		"latest_activity_at":     nullableStringValue(projection.LastActivityAt),
	}
	if hasDueAt {
		summary["due_at"] = dueAt.Format(time.RFC3339)
	}
	return summary
}

func boardCardDerivedFreshness(threadID string, states map[string]topicProjectionState) any {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	return cloneWorkspaceMap(states[threadID].Freshness)
}

func laterTimestamp(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	}
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		if right > left {
			return right
		}
		return left
	}
	if rightTime.After(leftTime) {
		return right
	}
	return left
}

func nullableStringValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableResolutionValue(raw any) any {
	s := strings.TrimSpace(anyString(raw))
	if s == "" {
		return nil
	}
	return s
}

func boardDisplayName(board map[string]any) string {
	if title := strings.TrimSpace(anyString(board["title"])); title != "" {
		return title
	}
	if boardID := strings.TrimSpace(anyString(board["id"])); boardID != "" {
		return boardID
	}
	return "board"
}

func cardDisplayName(card map[string]any) string {
	if card == nil {
		return "card"
	}
	if title := strings.TrimSpace(anyString(card["title"])); title != "" {
		return title
	}
	if summary := strings.TrimSpace(anyString(card["summary"])); summary != "" {
		return summary
	}
	if id := strings.TrimSpace(anyString(card["id"])); id != "" {
		return id
	}
	return "card"
}

func mapValues(values map[string]map[string]any) []map[string]any {
	if len(values) == 0 {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
