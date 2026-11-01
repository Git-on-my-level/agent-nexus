// Idempotent-create replay matching for board cards: loading a previously
// created card, comparing it against the replayed request, and checking the
// board preconditions under which a replay is accepted.

package server

import (
	"context"
	"errors"
	"strings"

	"agent-nexus-core/internal/primitives"
)

func loadExistingBoardCardForCreateReplay(ctx context.Context, opts handlerOptions, boardID, cardID, parentThreadID, legacyThreadID string) (map[string]any, error) {
	candidates := []string{
		strings.TrimSpace(cardID),
		strings.TrimSpace(parentThreadID),
		strings.TrimSpace(legacyThreadID),
	}
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		card, err := opts.primitiveStore.GetBoardCard(ctx, boardID, candidate)
		if err == nil {
			return card, nil
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			return nil, err
		}
	}
	return nil, primitives.ErrNotFound
}

func boardCardMatchesCreateReplay(existingCard map[string]any, explicitCardID, title, body, parentThreadID, legacyThreadID, columnKey string, assignee, pinnedDocumentID, dueAt, resolution *string, definitionOfDone, resolutionRefs, refs []string, risk *string) bool {
	explicitCardID = strings.TrimSpace(explicitCardID)
	if explicitCardID != "" && strings.TrimSpace(anyString(existingCard["id"])) != explicitCardID {
		return false
	}
	expectedParentThread := strings.TrimSpace(firstNonEmptyString(parentThreadID, legacyThreadID))
	if expectedParentThread == "" {
		if derived, err := primitives.SoleThreadRefIDFromRefs(refs); err == nil {
			expectedParentThread = derived
		}
	}
	if expectedParentThread != "" && primaryRelatedThreadID(existingCard) != expectedParentThread {
		return false
	}
	expectedColumn := strings.TrimSpace(columnKey)
	if expectedColumn == "" {
		expectedColumn = "backlog"
	}
	if strings.TrimSpace(anyString(existingCard["column_key"])) != expectedColumn {
		return false
	}
	if title = strings.TrimSpace(title); title != "" && strings.TrimSpace(anyString(existingCard["title"])) != title {
		return false
	}
	if body = strings.TrimSpace(body); body != "" && strings.TrimSpace(anyString(existingCard["summary"])) != body {
		return false
	}
	if assignee != nil && canonicalAssigneeStorageString(existingCard) != strings.TrimSpace(*assignee) {
		return false
	}
	if pinnedDocumentID != nil && strings.TrimSpace(pinnedDocumentIDFromCard(existingCard)) != strings.TrimSpace(*pinnedDocumentID) {
		return false
	}
	expectedDueAt := ""
	if dueAt != nil {
		expectedDueAt = strings.TrimSpace(*dueAt)
	}
	if strings.TrimSpace(anyString(existingCard["due_at"])) != expectedDueAt {
		return false
	}
	expectedDefinitionOfDone := uniqueSortedStrings(definitionOfDone)
	existingDefinitionOfDone, err := extractStringSlice(existingCard["definition_of_done"])
	if err != nil {
		existingDefinitionOfDone = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingDefinitionOfDone), expectedDefinitionOfDone) {
		return false
	}
	expectedResolution := replayExpectedCardResolution(normalizeOptionalRequestStringPointer(resolution))
	if strings.TrimSpace(anyString(existingCard["resolution"])) != expectedResolution {
		return false
	}
	existingResolutionRefs, err := extractStringSlice(existingCard["resolution_refs"])
	if err != nil {
		existingResolutionRefs = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingResolutionRefs), uniqueSortedStrings(resolutionRefs)) {
		return false
	}
	existingRefs, err := extractStringSlice(existingCard["refs"])
	if err != nil {
		existingRefs = nil
	}
	if !stringSlicesEqual(uniqueSortedStrings(existingRefs), uniqueSortedStrings(refs)) {
		return false
	}
	expectedRisk := "low"
	if risk != nil && strings.TrimSpace(*risk) != "" {
		expectedRisk = strings.TrimSpace(*risk)
	}
	if publicCardRisk(existingCard) != expectedRisk {
		return false
	}
	return true
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func replayExpectedCardResolution(raw *string) string {
	if raw != nil {
		return strings.TrimSpace(*raw)
	}
	return ""
}

func boardCardReplayPreconditionMatches(board map[string]any, ifBoardUpdatedAt *string) bool {
	if ifBoardUpdatedAt == nil {
		return true
	}
	return strings.TrimSpace(anyString(board["updated_at"])) == strings.TrimSpace(*ifBoardUpdatedAt)
}
