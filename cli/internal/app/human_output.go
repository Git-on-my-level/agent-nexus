package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func formatTypedCommandText(commandID string, statusCode int, headers map[string][]string, body any, verbose bool, includeHeaders bool) string {
	bodyText := ""
	if verbose {
		bodyText = formatPrettyBody(body)
	} else {
		bodyText = formatCommandSummary(commandID, body)
	}
	if !includeHeaders {
		return bodyText
	}
	return formatBodyWithHeaders(statusCode, headers, bodyText)
}

func formatArtifactContentText(statusCode int, headers map[string][]string, body []byte, verbose bool, includeHeaders bool) string {
	lines := make([]string, 0, 8)
	if includeHeaders {
		lines = append(lines, headerLines(statusCode, headers)...)
	}
	lines = append(lines, fmt.Sprintf("bytes: %d", len(body)))
	if verbose && len(body) > 0 {
		lines = append(lines, "")
		if text, ok := textualBody(body); ok {
			lines = append(lines, text)
		} else {
			lines = append(lines, "body_base64:")
			lines = append(lines, base64.StdEncoding.EncodeToString(body))
		}
	}
	return strings.Join(lines, "\n")
}

func formatCommandSummary(commandID string, body any) string {
	switch strings.TrimSpace(commandID) {
	case "threads.list":
		return formatNamedList(body, "threads", "Threads", renderThreadListItem)
	case "commitments.list":
		return formatNamedList(body, "commitments", "Commitments", renderCommitmentListItem)
	case "artifacts.list":
		return formatNamedList(body, "artifacts", "Artifacts", renderArtifactListItem)
	case "docs.list":
		return formatNamedList(body, "documents", "Documents", renderDocumentListItem)
	case "events.list":
		return formatEventsList(body)
	case "inbox.list":
		return formatInboxList(body)
	case "docs.history":
		return formatNamedList(body, "revisions", "Revisions", renderRevisionListItem)
	case "threads.get", "threads.create", "threads.patch", "threads.archive", "threads.unarchive", "threads.tombstone", "threads.restore":
		return formatThreadRecord(extractNestedMap(body, "thread"))
	case "threads.context":
		return formatThreadContext(body)
	case "threads.inspect":
		return formatThreadInspect(body)
	case "threads.review":
		return formatThreadWorkspace(body)
	case "threads.workspace":
		return formatThreadWorkspace(body)
	case "threads.recommendations":
		return formatThreadRecommendations(body)
	case "threads.timeline":
		return formatThreadTimeline(body)
	case "threads.purge":
		root := asMap(body)
		id, _ := root["thread_id"].(string)
		if id != "" {
			return "Thread " + id + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "commitments.get", "commitments.create", "commitments.patch":
		return formatCommitmentRecord(extractNestedMap(body, "commitment"))
	case "artifacts.get", "artifacts.create", "artifacts.tombstone", "artifacts.restore", "artifacts.archive", "artifacts.unarchive":
		return formatArtifactRecord(extractNestedMap(body, "artifact"))
	case "artifacts.purge":
		root := asMap(body)
		id, _ := root["artifact_id"].(string)
		if id != "" {
			return "Artifact " + id + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "artifacts.inspect":
		return formatArtifactInspect(body)
	case "events.get", "events.create":
		return formatEventRecord(extractNestedMap(body, "event"))
	case "docs.get", "docs.create", "docs.update", "docs.tombstone", "docs.archive", "docs.unarchive", "docs.restore":
		return formatDocumentRecord(body)
	case "threads.patch.propose", "commitments.patch.propose", "docs.update.propose":
		return formatProposalPreview(body)
	case "threads.patch.apply", "commitments.patch.apply", "docs.update.apply":
		return formatProposalApply(body)
	case "docs.purge":
		root := asMap(body)
		id, _ := root["document_id"].(string)
		if id != "" {
			return "Document " + id + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "docs.content":
		return formatDocumentContentRecord(body)
	case "docs.revision.get":
		return formatRevisionRecord(extractNestedMap(body, "revision"))
	case "provenance.walk":
		return formatProvenanceWalkSummary(asMap(body))
	case "boards.list":
		return formatBoardsList(body)
	case "boards.get", "boards.create", "boards.update", "boards.archive", "boards.unarchive", "boards.tombstone", "boards.restore":
		return formatBoardRecord(extractNestedMap(body, "board"))
	case "boards.purge":
		root := asMap(body)
		id, _ := root["board_id"].(string)
		if id != "" {
			return "Board " + id + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "boards.workspace":
		return formatBoardWorkspace(body)
	case "boards.cards.list":
		return formatBoardCardsList(body)
	case "boards.cards.add", "boards.cards.update", "boards.cards.move":
		return formatBoardCardMutationResult(body)
	case "boards.cards.remove":
		return formatBoardCardRemoveResult(body)
	default:
		return formatPrettyBody(body)
	}
}

func formatThreadContext(body any) string {
	root := asMap(body)
	fullID := asBool(root["full_id"])
	if len(asSlice(root["contexts"])) > 1 {
		return formatThreadContextAggregate(root, fullID)
	}
	thread := extractNestedMap(root, "thread")
	lines := make([]string, 0, 24)
	lines = append(lines, formatThreadRecord(thread))
	collaboration := extractNestedMap(root, "collaboration_summary")
	if collaboration != nil {
		lines = appendEventListSection(lines, "recommendations", asSlice(collaboration["recommendations"]), fullID)
		lines = appendEventListSection(lines, "decision_requests", asSlice(collaboration["decision_requests"]), fullID)
		lines = appendEventListSection(lines, "decisions", asSlice(collaboration["decisions"]), fullID)
	}
	lines = appendEventListSection(lines, "recent_events", asSlice(root["recent_events"]), fullID)
	lines = appendArtifactListSection(lines, "key_artifacts", asSlice(root["key_artifacts"]), fullID)
	lines = appendCommitmentListSection(lines, "open_commitments", asSlice(root["open_commitments"]), fullID)
	lines = appendDocumentListSection(lines, "documents", asSlice(root["documents"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadContextAggregate(root map[string]any, fullID bool) string {
	contexts := asSlice(root["contexts"])
	lines := []string{
		fmt.Sprintf("Thread contexts (%d):", len(contexts)),
	}
	for _, raw := range contexts {
		context := asMap(raw)
		if context == nil {
			continue
		}
		thread := asMap(context["thread"])
		collaboration := asMap(context["collaboration_summary"])
		recommendations := len(asSlice(collaboration["recommendations"]))
		decisionRequests := len(asSlice(collaboration["decision_requests"]))
		decisions := len(asSlice(collaboration["decisions"]))
		openCommitments := len(asSlice(context["open_commitments"]))
		documents := len(asSlice(context["documents"]))
		lines = append(lines, fmt.Sprintf(
			"- %s :: recommendations=%d :: decision_requests=%d :: decisions=%d :: open_commitments=%d :: documents=%d",
			displayID(thread),
			recommendations,
			decisionRequests,
			decisions,
			openCommitments,
			documents,
		))
	}

	if collaboration := extractNestedMap(root, "collaboration_summary"); collaboration != nil {
		lines = appendEventListSection(lines, "recommendations", asSlice(collaboration["recommendations"]), fullID)
		lines = appendEventListSection(lines, "decision_requests", asSlice(collaboration["decision_requests"]), fullID)
		lines = appendEventListSection(lines, "decisions", asSlice(collaboration["decisions"]), fullID)
	}
	lines = appendEventListSection(lines, "recent_events", asSlice(root["recent_events"]), fullID)
	lines = appendArtifactListSection(lines, "key_artifacts", asSlice(root["key_artifacts"]), fullID)
	lines = appendCommitmentListSection(lines, "open_commitments", asSlice(root["open_commitments"]), fullID)
	lines = appendDocumentListSection(lines, "documents", asSlice(root["documents"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadTimeline(body any) string {
	root := asMap(body)
	lines := []string{
		fmt.Sprintf("Timeline events: %d", len(asSlice(root["events"]))),
		fmt.Sprintf("Referenced snapshots: %d", len(asMap(root["snapshots"]))),
		fmt.Sprintf("Referenced artifacts: %d", len(asMap(root["artifacts"]))),
	}
	lines = appendListSection(lines, "events", asSlice(root["events"]), renderEventListItem)
	return strings.Join(lines, "\n")
}

func formatEventsList(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 16)
	if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
		lines = append(lines, "Thread "+threadID)
	}
	threadIDs := stringList(root["thread_ids"])
	if len(threadIDs) > 1 {
		lines = appendStringList(lines, "thread_ids", threadIDs)
	}
	lines = appendScalar(lines, "total_events", root, "total_events")
	lines = appendScalar(lines, "returned_events", root, "returned_events")
	lines = append(lines, fmt.Sprintf("referenced_snapshots: %d", len(asMap(root["snapshots"]))))
	lines = append(lines, fmt.Sprintf("referenced_artifacts: %d", len(asMap(root["artifacts"]))))
	lines = appendStringList(lines, "types", stringList(root["types"]))
	if actorID := strings.TrimSpace(anyString(root["actor_id"])); actorID != "" {
		lines = append(lines, "actor_id: "+actorID)
	}
	lines = appendEventListSection(lines, "events", asSlice(root["events"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func formatInboxList(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 16)
	if threadID := strings.TrimSpace(anyString(root["thread_id"])); threadID != "" {
		lines = append(lines, "Thread "+threadID)
	}
	threadIDs := stringList(root["thread_ids"])
	if len(threadIDs) > 1 {
		lines = appendStringList(lines, "thread_ids", threadIDs)
	}
	lines = appendScalar(lines, "total_items", root, "total_items")
	lines = appendScalar(lines, "returned_items", root, "returned_items")
	lines = appendStringList(lines, "types", stringList(root["types"]))
	lines = appendInboxListSection(lines, "items", asSlice(root["items"]), asBool(root["full_id"]))
	return strings.Join(lines, "\n")
}

func formatThreadInspect(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 28)
	lines = append(lines, formatThreadRecord(extractNestedMap(root, "thread")))
	collaboration := extractNestedMap(root, "collaboration")
	context := extractNestedMap(root, "context")
	fullID := asBool(root["full_id"])
	if collaboration != nil {
		lines = appendEventListSection(lines, "recommendations", asSlice(collaboration["recommendations"]), fullID)
		lines = appendEventListSection(lines, "decision_requests", asSlice(collaboration["decision_requests"]), fullID)
		lines = appendEventListSection(lines, "decisions", asSlice(collaboration["decisions"]), fullID)
	}
	if context != nil {
		lines = appendEventListSection(lines, "recent_events", asSlice(context["recent_events"]), fullID)
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(context["key_artifacts"]), fullID)
		lines = appendCommitmentListSection(lines, "open_commitments", asSlice(context["open_commitments"]), fullID)
		lines = appendDocumentListSection(lines, "documents", asSlice(context["documents"]), fullID)
	}
	inbox := extractNestedMap(root, "inbox")
	lines = appendInboxListSection(lines, "inbox_items", asSlice(inbox["items"]), fullID)
	return strings.Join(lines, "\n")
}

func formatThreadRecommendations(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 40)
	lines = append(lines, formatThreadRecord(extractNestedMap(root, "thread")))
	fullID := asBool(root["full_id"])
	fullSummary := asBool(root["full_summary"])
	lines = appendRecommendationEventSection(lines, "recommendations", extractNestedSlice(extractNestedMap(root, "recommendations"), "items"), fullID, fullSummary)
	lines = appendRecommendationEventSection(lines, "decision_requests", extractNestedSlice(extractNestedMap(root, "decision_requests"), "items"), fullID, fullSummary)
	lines = appendRecommendationEventSection(lines, "decisions", extractNestedSlice(extractNestedMap(root, "decisions"), "items"), fullID, fullSummary)
	lines = appendInboxListSection(lines, "pending_decisions", extractNestedSlice(extractNestedMap(root, "pending_decisions"), "items"), fullID)
	lines = appendWarningListSection(lines, "warnings", extractNestedSlice(extractNestedMap(root, "warnings"), "items"))
	lines = appendScalar(lines, "total_review_items", root, "total_review_items")
	lines = appendFollowUpSection(lines, extractNestedMap(root, "follow_up"))
	return strings.Join(lines, "\n")
}

func formatThreadWorkspace(body any) string {
	root := asMap(body)
	lines := make([]string, 0, 64)
	lines = append(lines, formatThreadRecord(extractNestedMap(root, "thread")))
	fullID := asBool(root["full_id"])
	fullSummary := asBool(root["full_summary"])

	collaboration := extractNestedMap(root, "collaboration")
	context := extractNestedMap(root, "context")
	if collaboration != nil {
		lines = appendRecommendationEventSection(lines, "recommendations", extractNestedSlice(collaboration, "recommendations"), fullID, fullSummary)
		lines = appendRecommendationEventSection(lines, "decision_requests", extractNestedSlice(collaboration, "decision_requests"), fullID, fullSummary)
		lines = appendRecommendationEventSection(lines, "decisions", extractNestedSlice(collaboration, "decisions"), fullID, fullSummary)
	}
	if context != nil {
		lines = appendEventListSection(lines, "recent_events", asSlice(context["recent_events"]), fullID)
		lines = appendArtifactListSection(lines, "key_artifacts", asSlice(context["key_artifacts"]), fullID)
		lines = appendCommitmentListSection(lines, "open_commitments", asSlice(context["open_commitments"]), fullID)
		lines = appendDocumentListSection(lines, "documents", asSlice(context["documents"]), fullID)
	}
	inbox := extractNestedMap(root, "inbox")
	lines = appendInboxListSection(lines, "inbox_items", extractNestedSlice(inbox, "items"), fullID)
	lines = appendInboxListSection(lines, "pending_decisions", extractNestedSlice(extractNestedMap(root, "pending_decisions"), "items"), fullID)
	lines = appendRecommendationEventSection(lines, "related_recommendations", extractNestedSlice(extractNestedMap(root, "related_recommendations"), "items"), fullID, fullSummary)
	lines = appendRecommendationEventSection(lines, "related_decision_requests", extractNestedSlice(extractNestedMap(root, "related_decision_requests"), "items"), fullID, fullSummary)
	lines = appendRecommendationEventSection(lines, "related_decisions", extractNestedSlice(extractNestedMap(root, "related_decisions"), "items"), fullID, fullSummary)
	lines = appendWarningListSection(lines, "warnings", extractNestedSlice(extractNestedMap(root, "warnings"), "items"))
	lines = appendScalar(lines, "total_review_items", root, "total_review_items")
	lines = appendFollowUpSection(lines, extractNestedMap(root, "follow_up"))
	return strings.Join(lines, "\n")
}

func formatProposalPreview(body any) string {
	root := asMap(body)
	lines := []string{
		"Proposal " + firstNonEmpty(anyString(root["proposal_id"]), "unknown"),
	}
	lines = appendScalar(lines, "target_command_id", root, "target_command_id")
	lines = appendScalar(lines, "method", root, "method")
	lines = appendScalar(lines, "path", root, "path")
	lines = appendScalar(lines, "apply_command", root, "apply_command")
	diff := extractNestedMap(root, "diff")
	if diffText := strings.TrimSpace(anyString(diff["text"])); diffText != "" {
		lines = append(lines, "diff:")
		lines = append(lines, indentBlock(diffText)...)
	}
	return strings.Join(lines, "\n")
}

func formatProposalApply(body any) string {
	root := asMap(body)
	lines := []string{
		"Proposal " + firstNonEmpty(anyString(root["proposal_id"]), "unknown"),
	}
	lines = appendScalar(lines, "target_command_id", root, "target_command_id")
	lines = appendScalar(lines, "applied", root, "applied")
	lines = appendScalar(lines, "kept", root, "kept")
	lines = appendScalar(lines, "warning", root, "warning")
	result := root["result"]
	if result != nil {
		lines = append(lines, "")
		lines = append(lines, formatPrettyBody(result))
	}
	return strings.Join(lines, "\n")
}

func appendWarningListSection(lines []string, label string, items []any) []string {
	if len(items) == 0 {
		return lines
	}
	lines = append(lines, label+":")
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		threadID := strings.TrimSpace(anyString(item["thread_id"]))
		message := strings.TrimSpace(anyString(item["message"]))
		switch {
		case threadID != "" && message != "":
			lines = append(lines, fmt.Sprintf("- %s :: %s", threadID, message))
		case message != "":
			lines = append(lines, "- "+message)
		case threadID != "":
			lines = append(lines, "- "+threadID)
		}
	}
	return lines
}

func formatThreadRecord(thread map[string]any) string {
	if thread == nil {
		return formatPrettyBody(thread)
	}
	lines := []string{"Thread " + displayID(thread)}
	lines = appendScalar(lines, "title", thread, "title")
	lines = appendScalar(lines, "status", thread, "status")
	lines = appendScalar(lines, "type", thread, "type")
	lines = appendScalar(lines, "priority", thread, "priority")
	lines = appendScalar(lines, "owner", thread, "owner")
	lines = appendScalar(lines, "cadence", thread, "cadence")
	lines = appendScalar(lines, "stale", thread, "stale")
	lines = appendScalar(lines, "updated_at", thread, "updated_at")
	lines = appendScalar(lines, "summary", thread, "current_summary")
	lines = appendStringList(lines, "tags", stringList(thread["tags"]))
	lines = appendStringList(lines, "next_actions", stringList(thread["next_actions"]))
	lines = appendStringList(lines, "key_artifacts", stringList(thread["key_artifacts"]))
	lines = appendStringList(lines, "open_commitments", stringList(thread["open_commitments"]))
	return strings.Join(lines, "\n")
}

func formatCommitmentRecord(commitment map[string]any) string {
	if commitment == nil {
		return formatPrettyBody(commitment)
	}
	lines := []string{"Commitment " + displayID(commitment)}
	lines = appendScalar(lines, "title", commitment, "title")
	lines = appendScalar(lines, "status", commitment, "status")
	lines = appendScalar(lines, "thread_id", commitment, "thread_id")
	lines = appendScalar(lines, "owner", commitment, "owner")
	lines = appendScalar(lines, "due_at", commitment, "due_at", "due_on")
	lines = appendScalar(lines, "summary", commitment, "summary")
	links := stringList(commitment["links"])
	if len(links) == 0 {
		links = stringList(commitment["refs"])
	}
	lines = appendStringList(lines, "links", links)
	return strings.Join(lines, "\n")
}

func formatArtifactRecord(artifact map[string]any) string {
	if artifact == nil {
		return formatPrettyBody(artifact)
	}
	lines := []string{"Artifact " + displayID(artifact)}
	lines = appendScalar(lines, "kind", artifact, "kind")
	lines = appendScalar(lines, "thread_id", artifact, "thread_id")
	lines = appendScalar(lines, "content_type", artifact, "content_type")
	lines = appendScalar(lines, "created_at", artifact, "created_at")
	lines = appendScalar(lines, "summary", artifact, "summary", "title")
	lines = appendScalar(lines, "content_hash", artifact, "content_hash")
	lines = appendStringList(lines, "refs", stringList(artifact["refs"]))
	if tombstonedAt := anyString(artifact["tombstoned_at"]); tombstonedAt != "" {
		lines = append(lines, "⚠ TOMBSTONED")
		lines = appendScalar(lines, "tombstoned_at", artifact, "tombstoned_at")
		lines = appendScalar(lines, "tombstoned_by", artifact, "tombstoned_by")
		lines = appendScalar(lines, "tombstone_reason", artifact, "tombstone_reason")
	}
	return strings.Join(lines, "\n")
}

func formatEventRecord(event map[string]any) string {
	if event == nil {
		return formatPrettyBody(event)
	}
	lines := []string{"Event " + displayID(event)}
	lines = appendScalar(lines, "type", event, "type")
	lines = appendScalar(lines, "thread_id", event, "thread_id")
	lines = appendScalar(lines, "actor_id", event, "actor_id")
	lines = appendScalar(lines, "ts", event, "ts", "created_at")
	lines = appendScalar(lines, "summary", event, "summary")
	lines = appendStringList(lines, "refs", stringList(event["refs"]))
	if payload := asMap(event["payload"]); len(payload) > 0 {
		lines = append(lines, "payload:")
		lines = append(lines, indentBlock(formatPrettyBody(payload))...)
	}
	return strings.Join(lines, "\n")
}

func formatDocumentRecord(body any) string {
	root := asMap(body)
	document := extractNestedMap(root, "document")
	revision := extractNestedMap(root, "revision")
	lines := []string{"Document " + displayID(document)}
	lines = appendScalar(lines, "title", document, "title")
	lines = appendScalar(lines, "kind", document, "kind")
	lines = appendScalar(lines, "head_revision_id", document, "head_revision_id")
	lines = appendScalar(lines, "revision_id", revision, "revision_id")
	lines = appendScalar(lines, "revision_number", revision, "revision_number")
	lines = appendScalar(lines, "content_type", revision, "content_type")
	if tombstonedAt := anyString(document["tombstoned_at"]); tombstonedAt != "" {
		lines = append(lines, "⚠ TOMBSTONED")
		lines = appendScalar(lines, "tombstoned_at", document, "tombstoned_at")
		lines = appendScalar(lines, "tombstoned_by", document, "tombstoned_by")
		lines = appendScalar(lines, "tombstone_reason", document, "tombstone_reason")
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
	lines := []string{"Document " + displayID(document)}
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

func formatRevisionRecord(revision map[string]any) string {
	if revision == nil {
		return formatPrettyBody(revision)
	}
	lines := []string{"Revision " + displayID(revision)}
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

func formatNamedList(body any, field string, label string, render func(map[string]any) string) string {
	root := asMap(body)
	items := asSlice(root[field])
	lines := []string{fmt.Sprintf("%s (%d):", label, len(items))}
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+render(item))
	}
	return strings.Join(lines, "\n")
}

func renderThreadListItem(item map[string]any) string {
	return compactSummary(displayID(item), firstNonEmpty(anyString(item["status"]), anyString(item["priority"])), firstNonEmpty(anyString(item["title"]), anyString(item["current_summary"])))
}

func renderCommitmentListItem(item map[string]any) string {
	return renderCommitmentListItemWithMode(item, false)
}

func renderCommitmentListItemWithMode(item map[string]any, fullID bool) string {
	return compactSummary(displayCompactIDWithMode(item, fullID), firstNonEmpty(anyString(item["status"]), anyString(item["owner"])), firstNonEmpty(anyString(item["title"]), anyString(item["summary"])))
}

func renderArtifactListItem(item map[string]any) string {
	return renderArtifactListItemWithMode(item, false)
}

func renderArtifactListItemWithMode(item map[string]any, fullID bool) string {
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
	return compactSummary(displayCompactIDWithMode(artifact, fullID), anyString(artifact["kind"]), summary)
}

func renderEventListItem(item map[string]any) string {
	return renderEventListItemWithMode(item, false)
}

func renderInboxItem(item map[string]any) string {
	return renderInboxItemWithMode(item, false)
}

func renderInboxItemWithMode(item map[string]any, fullID bool) string {
	identifier := displayID(item)
	if fullID {
		if id := strings.TrimSpace(anyString(item["id"])); id != "" {
			identifier = id
		}
	}
	return compactSummary(
		identifier,
		firstNonEmpty(anyString(item["type"]), anyString(item["category"]), anyString(item["kind"])),
		firstNonEmpty(anyString(item["title"]), anyString(item["summary"]), anyString(item["thread_id"])),
	)
}

func renderRevisionListItem(item map[string]any) string {
	return compactSummary(displayID(item), anyString(item["revision_number"]), anyString(item["created_at"]))
}

func renderDocumentListItem(item map[string]any) string {
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
		displayID(item),
		strings.Join(stateParts, " "),
		firstNonEmpty(anyString(item["title"]), anyString(item["slug"])),
		firstNonEmpty(anyString(item["updated_at"]), anyString(headRevision["created_at"])),
	)
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

func appendEventListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderEventListItemWithMode(item, fullID))
	}
	return lines
}

func appendRecommendationEventSection(lines []string, label string, items []any, fullID bool, fullSummary bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderRecommendationEventItemWithMode(item, fullID, fullSummary))
	}
	return lines
}

func appendInboxListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderInboxItemWithMode(item, fullID))
	}
	return lines
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

func appendCommitmentListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		lines = append(lines, "- "+renderCommitmentListItemWithMode(item, fullID))
	}
	return lines
}

func appendDocumentListSection(lines []string, label string, items []any, fullID bool) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", label, len(items)))
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		headRevision := asMap(item["head_revision"])
		identifier := displayID(item)
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

func appendFollowUpSection(lines []string, followUp map[string]any) []string {
	if followUp == nil {
		return lines
	}
	template := strings.TrimSpace(anyString(followUp["events_get_template"]))
	examples := stringList(followUp["events_get_examples"])
	recommendationsList := strings.TrimSpace(anyString(followUp["recommendations_list_command"]))
	decisionsList := strings.TrimSpace(anyString(followUp["decisions_list_command"]))
	contextRefresh := strings.TrimSpace(anyString(followUp["context_refresh_command"]))
	if template == "" && len(examples) == 0 && recommendationsList == "" && decisionsList == "" && contextRefresh == "" {
		return lines
	}
	lines = append(lines, "follow_up:")
	if template != "" {
		lines = append(lines, "- events_get_template: "+template)
	}
	for _, example := range examples {
		lines = append(lines, "- events_get_example: "+example)
	}
	if recommendationsList != "" {
		lines = append(lines, "- recommendations_list: "+recommendationsList)
	}
	if decisionsList != "" {
		lines = append(lines, "- decisions_list: "+decisionsList)
	}
	if contextRefresh != "" {
		lines = append(lines, "- context_refresh: "+contextRefresh)
	}
	return lines
}

func appendScalar(lines []string, label string, values map[string]any, keys ...string) []string {
	if values == nil {
		return lines
	}
	for _, key := range keys {
		value := formatScalar(values[key])
		if value == "" {
			continue
		}
		return append(lines, label+": "+value)
	}
	return lines
}

func appendStringList(lines []string, label string, values []string) []string {
	if len(values) == 0 {
		return lines
	}
	lines = append(lines, label+":")
	for _, value := range values {
		lines = append(lines, "- "+value)
	}
	return lines
}

func formatBodyWithHeaders(statusCode int, headers map[string][]string, bodyText string) string {
	lines := headerLines(statusCode, headers)
	if strings.TrimSpace(bodyText) != "" {
		lines = append(lines, "")
		lines = append(lines, bodyText)
	}
	return strings.Join(lines, "\n")
}

func headerLines(statusCode int, headers map[string][]string) []string {
	lines := []string{fmt.Sprintf("status: %d", statusCode)}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("header %s: %s", key, strings.Join(headers[key], ", ")))
	}
	return lines
}

func formatPrettyBody(body any) string {
	switch typed := body.(type) {
	case nil:
		return "null"
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return ""
		}
		return typed
	default:
		encoded, err := json.MarshalIndent(body, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", body)
		}
		return string(encoded)
	}
}

func textualBody(body []byte) (string, bool) {
	if len(body) == 0 || !utf8.Valid(body) {
		return "", false
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "", false
	}
	if strings.ContainsRune(text, rune(0)) {
		return "", false
	}
	return text, true
}

func displayID(item map[string]any) string {
	return displayIDWithMode(item, false)
}

func displayIDWithMode(item map[string]any, fullID bool) string {
	if item == nil {
		return ""
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		id = strings.TrimSpace(anyString(item["revision_id"]))
	}
	if fullID && id != "" {
		return id
	}
	short := strings.TrimSpace(anyString(item["short_id"]))
	if short == "" && id != "" {
		short = shortID(id)
	}
	if short != "" && id != "" && short != id {
		return short + " (id=" + id + ")"
	}
	return firstNonEmpty(id, short)
}

func renderEventListItemWithMode(item map[string]any, fullID bool) string {
	summary := firstNonEmpty(anyString(item["summary_preview"]), anyString(item["summary"]), anyString(item["created_at"]))
	return compactSummary(displayEventID(item, fullID), anyString(item["type"]), summary)
}

func renderRecommendationEventItemWithMode(item map[string]any, fullID bool, fullSummary bool) string {
	summary := firstNonEmpty(anyString(item["summary_preview"]), anyString(item["summary"]), anyString(item["created_at"]))
	if fullSummary {
		summary = firstNonEmpty(anyString(item["summary"]), anyString(item["summary_preview"]), anyString(item["created_at"]))
	}
	actor := strings.TrimSpace(anyString(item["actor_id"]))
	if actor == "" {
		actor = "unknown_actor"
	}
	createdAt := strings.TrimSpace(anyString(item["created_at"]))
	if createdAt == "" {
		createdAt = "unknown_time"
	}
	sources := stringList(item["provenance_sources"])
	sourceLabel := ""
	if len(sources) > 0 {
		sourceLabel = "sources=" + strings.Join(sources, ",")
	}
	return compactSummary(
		displayEventID(item, fullID),
		anyString(item["type"]),
		"actor="+actor,
		"at="+createdAt,
		summary,
		sourceLabel,
	)
}

func displayEventID(item map[string]any, fullID bool) string {
	if item == nil {
		return ""
	}
	id := strings.TrimSpace(anyString(item["id"]))
	short := strings.TrimSpace(anyString(item["short_id"]))
	if short == "" && id != "" {
		short = shortID(id)
	}
	if fullID && id != "" {
		return id
	}
	if short != "" {
		return short
	}
	return displayID(item)
}

func displayCompactIDWithMode(item map[string]any, fullID bool) string {
	if item == nil {
		return ""
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		id = strings.TrimSpace(anyString(item["revision_id"]))
	}
	if fullID && id != "" {
		return id
	}
	short := strings.TrimSpace(anyString(item["short_id"]))
	if short == "" && id != "" {
		short = shortID(id)
	}
	return firstNonEmpty(short, id)
}

func asBool(raw any) bool {
	switch typed := raw.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconvParseBool(typed)
		return err == nil && parsed
	default:
		return false
	}
}

func extractNestedMap(body any, key string) map[string]any {
	root := asMap(body)
	if root == nil {
		return nil
	}
	return asMap(root[key])
}

func extractNestedSlice(body map[string]any, key string) []any {
	if body == nil {
		return nil
	}
	return asSlice(body[key])
}

func asMap(raw any) map[string]any {
	typed, _ := raw.(map[string]any)
	return typed
}

func asSlice(raw any) []any {
	typed, _ := raw.([]any)
	return typed
}

func stringList(raw any) []string {
	if typed, ok := raw.([]string); ok {
		if len(typed) == 0 {
			return nil
		}
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			value := strings.TrimSpace(item)
			if value == "" {
				continue
			}
			out = append(out, value)
		}
		return out
	}
	items, _ := raw.([]any)
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(anyString(item))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func formatScalar(raw any) string {
	switch typed := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", typed)
	case int8:
		return fmt.Sprintf("%d", typed)
	case int16:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case uint8:
		return fmt.Sprintf("%d", typed)
	case uint16:
		return fmt.Sprintf("%d", typed)
	case uint32:
		return fmt.Sprintf("%d", typed)
	case uint64:
		return fmt.Sprintf("%d", typed)
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	default:
		return strings.TrimSpace(anyString(raw))
	}
}

func indentBlock(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for idx := range lines {
		lines[idx] = "  " + lines[idx]
	}
	return lines
}

var canonicalColumnOrder = []string{"backlog", "ready", "in_progress", "blocked", "review", "done"}

func formatBoardsList(body any) string {
	root := asMap(body)
	items := asSlice(root["boards"])
	lines := []string{fmt.Sprintf("Boards (%d):", len(items))}
	for _, raw := range items {
		item := asMap(raw)
		if item == nil {
			continue
		}
		board := asMap(item["board"])
		summary := asMap(item["summary"])
		if board == nil {
			continue
		}
		lines = append(lines, "- "+renderBoardListItem(board, summary))
	}
	return strings.Join(lines, "\n")
}

func renderBoardListItem(board map[string]any, summary map[string]any) string {
	id := displayID(board)
	title := anyString(board["title"])
	status := anyString(board["status"])
	cardCount := intValue(summary["card_count"])
	openCommitments := intValue(summary["open_commitment_count"])
	docCount := intValue(summary["document_count"])
	return compactSummary(
		id,
		status,
		title,
		fmt.Sprintf("cards=%d", cardCount),
		fmt.Sprintf("commitments=%d", openCommitments),
		fmt.Sprintf("docs=%d", docCount),
	)
}

func formatBoardRecord(board map[string]any) string {
	if board == nil {
		return formatPrettyBody(board)
	}
	lines := []string{"Board " + displayID(board)}
	lines = appendScalar(lines, "title", board, "title")
	lines = appendScalar(lines, "status", board, "status")
	lines = appendScalar(lines, "primary_thread_id", board, "primary_thread_id")
	lines = appendScalar(lines, "primary_document_id", board, "primary_document_id")
	lines = appendStringList(lines, "labels", stringList(board["labels"]))
	lines = appendStringList(lines, "owners", stringList(board["owners"]))
	lines = appendScalar(lines, "updated_at", board, "updated_at")
	lines = appendScalar(lines, "created_at", board, "created_at")
	return strings.Join(lines, "\n")
}

func formatBoardWorkspace(body any) string {
	root := asMap(body)
	board := extractNestedMap(root, "board")
	primaryThread := extractNestedMap(root, "primary_thread")
	primaryDocument := extractNestedMap(root, "primary_document")
	boardSummary := extractNestedMap(root, "board_summary")
	cards := extractNestedMap(root, "cards")

	lines := make([]string, 0, 64)

	lines = append(lines, "Board "+displayID(board))
	lines = appendScalar(lines, "title", board, "title")
	lines = appendScalar(lines, "status", board, "status")

	if primaryThread != nil {
		lines = append(lines, "")
		lines = append(lines, "Primary Thread:")
		lines = append(lines, "- "+formatThreadRecord(primaryThread))
	}

	if primaryDocument != nil {
		lines = append(lines, "")
		lines = append(lines, "Primary Document:")
		lines = append(lines, "- "+renderDocumentListItem(primaryDocument))
	}

	if boardSummary != nil {
		lines = append(lines, "")
		lines = append(lines, "Summary:")
		cardCount := intValue(boardSummary["card_count"])
		openCommitments := intValue(boardSummary["open_commitment_count"])
		docCount := intValue(boardSummary["document_count"])
		latestActivity := anyString(boardSummary["latest_activity_at"])
		hasPrimaryDoc := asBool(boardSummary["has_primary_document"])
		lines = append(lines, fmt.Sprintf("- cards=%d :: commitments=%d :: documents=%d", cardCount, openCommitments, docCount))
		if latestActivity != "" {
			lines = append(lines, fmt.Sprintf("- latest_activity: %s", latestActivity))
		}
		if hasPrimaryDoc {
			lines = append(lines, "- primary_document: yes")
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
	summary := asMap(cardWrapper["summary"])
	pinnedDoc := cardWrapper["pinned_document"]

	threadID := displayID(thread)
	threadTitle := anyString(thread["title"])

	badges := make([]string, 0, 8)
	if summary != nil {
		openCommitments := intValue(summary["open_commitment_count"])
		decisionRequests := intValue(summary["decision_request_count"])
		decisions := intValue(summary["decision_count"])
		recommendations := intValue(summary["recommendation_count"])
		docs := intValue(summary["document_count"])
		inbox := intValue(summary["inbox_count"])
		stale := asBool(summary["stale"])

		if openCommitments > 0 {
			badges = append(badges, fmt.Sprintf("c=%d", openCommitments))
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
			badges = append(badges, "STALE")
		}
	}

	if pinnedDoc != nil {
		badges = append(badges, "pinned")
	}

	badgeStr := ""
	if len(badges) > 0 {
		badgeStr = " [" + strings.Join(badges, ", ") + "]"
	}

	return compactSummary(threadID, threadTitle) + badgeStr
}

func formatBoardCardsList(body any) string {
	root := asMap(body)
	boardID := anyString(root["board_id"])
	cards := asSlice(root["cards"])
	lines := []string{fmt.Sprintf("Cards (%d):", len(cards))}
	for _, raw := range cards {
		card := asMap(raw)
		if card == nil {
			continue
		}
		lines = append(lines, "- "+renderBoardCardListItem(card))
	}
	if boardID != "" {
		lines = append([]string{fmt.Sprintf("Board: %s", boardID)}, lines...)
	}
	return strings.Join(lines, "\n")
}

func renderBoardCardListItem(card map[string]any) string {
	threadID := anyString(card["thread_id"])
	columnKey := anyString(card["column_key"])
	rank := anyString(card["rank"])
	pinnedDocID := anyString(card["pinned_document_id"])

	parts := []string{threadID, columnKey}
	if rank != "" {
		parts = append(parts, "rank="+rank)
	}
	if pinnedDocID != "" {
		parts = append(parts, "pinned="+pinnedDocID)
	}
	return strings.Join(parts, " :: ")
}

func formatBoardCardMutationResult(body any) string {
	board := extractNestedMap(body, "board")
	card := extractNestedMap(body, "card")
	lines := []string{"Card updated:"}
	if board != nil {
		lines = appendScalar(lines, "board_updated_at", board, "updated_at")
	}
	if card != nil {
		lines = append(lines, "- thread: "+anyString(card["thread_id"]))
		lines = append(lines, "  column: "+anyString(card["column_key"]))
		lines = append(lines, "  rank: "+anyString(card["rank"]))
		if pinnedDocID := anyString(card["pinned_document_id"]); pinnedDocID != "" {
			lines = append(lines, "  pinned_document: "+pinnedDocID)
		}
	}
	return strings.Join(lines, "\n")
}

func formatBoardCardRemoveResult(body any) string {
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
