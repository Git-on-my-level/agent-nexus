// Default text projections for boards and board cards: board lists, records,
// workspace columns, and card get/create/mutation results.

package app

import (
	"fmt"
	"strings"

	contractenums "agent-nexus-contracts-go-client/enums"
)

func renderBoardRowFlat(board map[string]any, listID string) string {
	return compactSummary(listID, anyString(board["status"]), anyString(board["title"]))
}

var canonicalColumnOrder = contractenums.BoardColumnKeyValues

func formatBoardsList(body any) string {
	root := asMap(body)
	rawItems := asSlice(root["boards"])
	wrappers := make([]map[string]any, 0, len(rawItems))
	for _, raw := range rawItems {
		if item := asMap(raw); item != nil {
			wrappers = append(wrappers, item)
		}
	}
	validWrappers := make([]map[string]any, 0, len(wrappers))
	for _, item := range wrappers {
		if item == nil || asMap(item["board"]) == nil {
			continue
		}
		validWrappers = append(validWrappers, item)
	}
	fullID := asBool(root["full_id"])
	scanIDs := disambiguateListScanIDs(validWrappers, "board", fullID)
	lines := []string{fmt.Sprintf("Boards (%d):", len(validWrappers))}
	for i, item := range validWrappers {
		board := asMap(item["board"])
		summary := asMap(item["summary"])
		lines = append(lines, "- "+renderBoardListItem(board, summary, scanIDs[i]))
	}
	return strings.Join(lines, "\n")
}

func renderBoardListItem(board map[string]any, summary map[string]any, listID string) string {
	title := anyString(board["title"])
	state := anyString(board["state"])
	cardCount := intValue(summary["card_count"])
	unresolved := intValue(summary["unresolved_card_count"])
	docCount := intValue(summary["document_count"])
	return compactSummary(
		listID,
		state,
		title,
		fmt.Sprintf("cards=%d", cardCount),
		fmt.Sprintf("unresolved_cards=%d", unresolved),
		fmt.Sprintf("docs=%d", docCount),
	)
}

func formatBoardRecord(board map[string]any) string {
	if board == nil {
		return formatPrettyBody(board)
	}
	lines := []string{"Board " + displayPublicIdentity(board, "board")}
	lines = appendScalar(lines, "title", board, "title")
	lines = appendScalar(lines, "state", board, "state")
	lines = appendScalar(lines, "thread_id", board, "thread_id")
	lines = appendStringList(lines, "document_refs", stringList(board["document_refs"]))
	lines = appendStringList(lines, "labels", stringList(board["labels"]))
	lines = appendStringList(lines, "owners", stringList(board["owners"]))
	lines = appendScalar(lines, "updated_at", board, "updated_at")
	lines = appendScalar(lines, "created_at", board, "created_at")
	if trashedAt := anyString(board["trashed_at"]); trashedAt != "" {
		lines = append(lines, "⚠ TRASHED")
		lines = appendScalar(lines, "trashed_at", board, "trashed_at")
		lines = appendScalar(lines, "trashed_by", board, "trashed_by")
		lines = appendScalar(lines, "trash_reason", board, "trash_reason")
	} else if archivedAt := anyString(board["archived_at"]); archivedAt != "" {
		lines = append(lines, "⚠ ARCHIVED")
		lines = appendScalar(lines, "archived_at", board, "archived_at")
		lines = appendScalar(lines, "archived_by", board, "archived_by")
	}
	return strings.Join(lines, "\n")
}

func formatBoardWorkspace(body any) string {
	root := asMap(body)
	board := extractNestedMap(root, "board")
	primaryTopic := extractNestedMap(root, "primary_topic")
	boardSummary := extractNestedMap(root, "board_summary")
	cards := extractNestedMap(root, "cards")

	lines := make([]string, 0, 64)

	lines = append(lines, "Board "+displayPublicIdentity(board, "board"))
	lines = appendScalar(lines, "title", board, "title")
	lines = appendScalar(lines, "state", board, "state")
	lines = appendStringList(lines, "document_refs", stringList(board["document_refs"]))

	if primaryTopic != nil {
		lines = append(lines, "")
		lines = append(lines, "Primary topic:")
		lines = append(lines, "- "+formatTopicRecord(primaryTopic))
	}

	if boardSummary != nil {
		lines = append(lines, "")
		lines = append(lines, "Summary:")
		cardCount := intValue(boardSummary["card_count"])
		unresolved := intValue(boardSummary["unresolved_card_count"])
		docCount := intValue(boardSummary["document_count"])
		latestActivity := anyString(boardSummary["latest_activity_at"])
		lines = append(lines, fmt.Sprintf("- cards=%d :: unresolved_cards=%d :: documents=%d", cardCount, unresolved, docCount))
		if latestActivity != "" {
			lines = append(lines, fmt.Sprintf("- latest_activity: %s", latestActivity))
		}
	}

	if cards != nil {
		cardItems := asSlice(cards["items"])
		if len(cardItems) > 0 {
			lines = append(lines, "")
			lines = appendBoardCardsByColumn(lines, cardItems)
		}
	}

	generatedAt := anyString(root["generated_at"])
	if generatedAt != "" {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("generated_at: %s", generatedAt))
	}

	return strings.Join(lines, "\n")
}

func appendBoardCardsByColumn(lines []string, cards []any) []string {
	cardsByColumn := make(map[string][]any)
	for _, raw := range cards {
		cardWrapper := asMap(raw)
		if cardWrapper == nil {
			continue
		}
		card := asMap(cardWrapper["card"])
		if card == nil {
			continue
		}
		columnKey := anyString(card["column_key"])
		if columnKey == "" {
			columnKey = "backlog"
		}
		cardsByColumn[columnKey] = append(cardsByColumn[columnKey], cardWrapper)
	}

	for _, col := range canonicalColumnOrder {
		colCards := cardsByColumn[col]
		if len(colCards) == 0 {
			continue
		}
		colTitle := strings.Title(strings.ReplaceAll(col, "_", " "))
		lines = append(lines, fmt.Sprintf("%s (%d):", colTitle, len(colCards)))
		for _, raw := range colCards {
			cardWrapper := asMap(raw)
			lines = append(lines, "- "+renderBoardCardItem(cardWrapper))
		}
	}
	return lines
}

func renderBoardCardItem(cardWrapper map[string]any) string {
	thread := asMap(cardWrapper["thread"])
	if thread == nil {
		backing := asMap(cardWrapper["backing"])
		thread = asMap(backing["thread"])
	}
	card := asMap(cardWrapper["card"])
	summary := asMap(cardWrapper["summary"])
	pinnedDoc := cardWrapper["pinned_document"]

	cardID := displayPublicIdentity(card, "card")
	threadID := displayPublicIdentity(thread, "thread")
	threadTitle := anyString(thread["title"])
	if cardID == "" || cardID == "(empty)" {
		cardID = threadID
	}
	if threadTitle == "" {
		threadTitle = firstNonEmpty(anyString(card["title"]), anyString(card["summary"]))
	}

	badges := make([]string, 0, 8)
	if summary != nil {
		relatedTopics := intValue(summary["related_topic_count"])
		decisionRequests := intValue(summary["decision_request_count"])
		decisions := intValue(summary["decision_count"])
		recommendations := intValue(summary["recommendation_count"])
		docs := intValue(summary["document_count"])
		inbox := intValue(summary["inbox_count"])
		stale := asBool(summary["stale"])

		if relatedTopics > 0 {
			badges = append(badges, fmt.Sprintf("topics=%d", relatedTopics))
		}
		if decisionRequests > 0 {
			badges = append(badges, fmt.Sprintf("dr=%d", decisionRequests))
		}
		if decisions > 0 {
			badges = append(badges, fmt.Sprintf("d=%d", decisions))
		}
		if recommendations > 0 {
			badges = append(badges, fmt.Sprintf("rec=%d", recommendations))
		}
		if docs > 0 {
			badges = append(badges, fmt.Sprintf("doc=%d", docs))
		}
		if inbox > 0 {
			badges = append(badges, fmt.Sprintf("inbox=%d", inbox))
		}
		if stale {
			badges = append(badges, "risk_exception")
		}
	}

	if pinnedDoc != nil {
		badges = append(badges, "pinned")
	}

	badgeStr := ""
	if len(badges) > 0 {
		badgeStr = " [" + strings.Join(badges, ", ") + "]"
	}

	row := compactSummary(cardID, threadTitle)
	if threadID != "" && threadID != "(empty)" && threadID != cardID {
		row += " :: thread=" + threadID
	}
	return row + badgeStr
}

func formatBoardCardsList(body any) string {
	root := asMap(body)
	boardID := anyString(root["board_id"])
	cards := asSlice(root["cards"])
	fullID := asBool(root["full_id"])
	lines := []string{fmt.Sprintf("Cards (%d):", len(cards))}
	for _, raw := range cards {
		card := asMap(raw)
		if card == nil {
			continue
		}
		lines = append(lines, "- "+renderBoardCardListItemWithMode(card, fullID))
	}
	if boardID != "" {
		lines = append([]string{fmt.Sprintf("Board: %s", boardID)}, lines...)
	}
	return strings.Join(lines, "\n")
}

func renderBoardCardListItem(card map[string]any) string {
	return renderBoardCardListItemWithMode(card, false)
}

func renderBoardCardListItemWithMode(card map[string]any, fullID bool) string {
	threadID := threadRefFromCard(card)
	columnKey := anyString(card["column_key"])
	rank := anyString(card["rank"])
	documentRef := firstNonEmpty(anyString(card["document_ref"]), anyString(card["pinned_document_id"]))
	cardID := strings.TrimSpace(anyString(card["id"]))
	cardIdentity := displayPublicIdentity(card, "card")
	title := strings.TrimSpace(anyString(card["title"]))

	lead := cardIdentity
	if fullID && cardID != "" {
		lead = cardID
	}
	if lead == "" {
		if title != "" {
			lead = title
		} else if threadID != "" {
			lead = "thread=" + threadID
		} else {
			lead = "standalone card"
		}
	}

	parts := []string{lead, columnKey}
	if title != "" && title != lead {
		parts = append(parts, title)
	}
	if threadID != "" {
		parts = append(parts, "thread="+threadID)
	}
	if rank != "" {
		parts = append(parts, "rank="+rank)
	}
	if documentRef != "" {
		parts = append(parts, "document="+documentRef)
	}
	return strings.Join(parts, " :: ")
}

func threadRefFromCard(card map[string]any) string {
	if card == nil {
		return ""
	}
	if ref := strings.TrimSpace(anyString(card["thread_ref"])); ref != "" {
		return ref
	}
	threadID := strings.TrimSpace(anyString(card["thread_id"]))
	if threadID == "" {
		return ""
	}
	if strings.Contains(threadID, ":") {
		return threadID
	}
	return "thread:" + threadID
}

func formatBoardCardGetResult(body any) string {
	card := extractNestedMap(body, "card")
	if card == nil {
		return formatPrettyBody(body)
	}
	lines := []string{"Board card:"}
	lines = append(lines, renderBoardCardListItem(card))
	return strings.Join(lines, "\n")
}

func formatBoardCardBoardAndCardSummary(body any, headline string) string {
	board := extractNestedMap(body, "board")
	card := extractNestedMap(body, "card")
	lines := []string{headline}
	if board != nil {
		lines = appendScalar(lines, "board_updated_at", board, "updated_at")
	}
	if card != nil {
		cardID := strings.TrimSpace(anyString(card["id"]))
		title := strings.TrimSpace(anyString(card["title"]))
		subject := displayPublicIdentity(card, "card")
		if subject == "(unknown)" {
			subject = cardID
		}
		if title != "" && title != subject {
			if subject != "" {
				subject = subject + " — " + title
			} else {
				subject = title
			}
		}
		if subject == "" {
			subject = "standalone card"
		}
		if strings.HasPrefix(subject, "card:") {
			lines = append(lines, "- "+subject)
		} else {
			lines = append(lines, "- card: "+subject)
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID != "" {
			if strings.HasPrefix(threadID, "thread:") {
				lines = append(lines, "  "+threadID)
			} else {
				lines = append(lines, "  thread: "+threadID)
			}
		}
		lines = append(lines, "  column: "+anyString(card["column_key"]))
		lines = append(lines, "  rank: "+anyString(card["rank"]))
		if documentRef := firstNonEmpty(anyString(card["document_ref"]), anyString(card["pinned_document_id"])); documentRef != "" {
			lines = append(lines, "  document_ref: "+documentRef)
		}
	}
	return strings.Join(lines, "\n")
}

func formatBoardCardMutationResult(body any) string {
	return formatBoardCardBoardAndCardSummary(body, "Card updated:")
}

func formatBoardCardCreateResult(body any) string {
	board := extractNestedMap(body, "board")
	card := extractNestedMap(body, "card")
	if card == nil {
		return formatPrettyBody(body)
	}
	lines := []string{"Card created:"}
	if board != nil {
		lines = appendScalar(lines, "board_updated_at", board, "updated_at")
	}
	cardID := displayPublicIdentity(card, "card")
	title := strings.TrimSpace(anyString(card["title"]))
	subject := cardID
	if title != "" && title != cardID {
		if subject != "" {
			subject += " — " + title
		} else {
			subject = title
		}
	}
	if subject == "" {
		subject = "card"
	}
	if strings.HasPrefix(subject, "card:") {
		lines = append(lines, "- "+subject)
	} else {
		lines = append(lines, "- card: "+subject)
	}
	if threadID := threadRefFromCard(card); threadID != "" && threadID != cardID {
		lines = append(lines, "  "+threadID)
	}
	lines = append(lines, "  column: "+anyString(card["column_key"]))
	lines = append(lines, "  rank: "+anyString(card["rank"]))
	return strings.Join(lines, "\n")
}

func formatBoardCardsBatchCreateResult(body any) string {
	root := asMap(body)
	board := extractNestedMap(root, "board")
	cards := asSlice(root["cards"])
	lines := []string{fmt.Sprintf("Created %d cards:", len(cards))}
	if board != nil {
		lines = appendScalar(lines, "board_updated_at", board, "updated_at")
	}
	for i, raw := range cards {
		card, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		cardID := displayPublicIdentity(card, "card")
		if cardID == "(unknown)" {
			cardID = anyString(card["id"])
		}
		lines = append(lines, fmt.Sprintf("- [%d] %s — %s", i, cardID, strings.TrimSpace(anyString(card["title"]))))
	}
	return strings.Join(lines, "\n")
}

func formatBoardCardRemoveResult(body any) string {
	if extractNestedMap(body, "card") != nil {
		return formatBoardCardBoardAndCardSummary(body, "Card removed:")
	}
	board := extractNestedMap(body, "board")
	root := asMap(body)
	removedThreadID := formatScalar(root["removed_thread_id"])
	lines := []string{"Card removed:"}
	if board != nil {
		lines = appendScalar(lines, "board_updated_at", board, "updated_at")
	}
	if removedThreadID != "" {
		lines = append(lines, "- thread: "+removedThreadID)
	}
	return strings.Join(lines, "\n")
}
