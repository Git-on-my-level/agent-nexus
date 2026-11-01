// Default text projection shared by typed commands: the summary dispatcher,
// scalar/body/section formatting primitives, public identity display, and map
// coercion helpers. Per-resource projections live in text_*.go siblings; the
// proposal preview/apply projections also live here.

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
		return formatNamedList(body, "threads", "Threads", "thread", renderThreadListItem)
	case "topics.list":
		return formatNamedList(body, "topics", "Topics", "topic", renderTopicListItem)
	case "cards.list":
		return formatNamedList(body, "cards", "Cards", "card", renderCardListItem)
	case "artifacts.list":
		return formatNamedList(body, "artifacts", "Artifacts", "artifact", renderArtifactListItem)
	case "docs.list", "docs.search":
		return formatNamedList(body, "documents", "Documents", "document", renderDocumentListItem)
	case "events.list":
		return formatEventsList(body)
	case "inbox.list":
		return formatInboxList(body)
	case "docs.history", "docs.revisions.list", "cards.revisions.list":
		return formatNamedList(body, "revisions", "Revisions", "revision", renderRevisionListItem)
	case "topics.get", "topics.create", "topics.patch":
		return formatTopicRecord(extractNestedMap(body, "topic"))
	case "topics.timeline":
		return formatTopicTimeline(body)
	case "cards.timeline":
		return formatCardTimeline(body)
	case "cards.messages":
		return formatCardsMessages(body)
	case "topics.workspace":
		return formatTopicWorkspace(body)
	case "cards.message", "cards.reply":
		return messageWriteText(body, "Card")
	case "threads.message", "threads.reply":
		return messageWriteText(body, "Thread")
	case "cards.create":
		return formatBoardCardCreateResult(body)
	case "cards.get", "cards.patch", "cards.move", "cards.revisions.create", "cards.archive", "cards.trash", "cards.restore":
		if board := extractNestedMap(body, "board"); board != nil && extractNestedMap(body, "card") != nil {
			return formatBoardCardMutationResult(body)
		}
		return formatCardRecord(extractNestedMap(body, "card"))
	case "cards.purge":
		root := asMap(body)
		label := firstNonEmpty(anyString(root["card_ref"]), anyString(root["card_handle"]), anyString(root["card_id"]))
		if purged, _ := root["purged"].(bool); purged && label != "" {
			return "Card " + label + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "threads.context":
		return formatThreadContext(body)
	case "threads.inspect":
		return formatThreadInspect(body)
	case "threads.review":
		return formatThreadWorkspace(body)
	case "threads.workspace":
		return formatThreadWorkspace(body)
	case "threads.timeline":
		return formatThreadTimeline(body)
	case "artifacts.get", "artifacts.create", "artifacts.trash", "artifacts.restore", "artifacts.archive", "artifacts.unarchive":
		return formatArtifactRecord(extractNestedMap(body, "artifact"))
	case "artifacts.purge":
		root := asMap(body)
		label := firstNonEmpty(anyString(root["artifact_ref"]), anyString(root["artifact_handle"]), anyString(root["artifact_id"]))
		if purged, _ := root["purged"].(bool); purged && label != "" {
			return "Artifact " + label + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "artifacts.inspect":
		return formatArtifactInspect(body)
	case "events.get", "events.create", "events.archive", "events.unarchive", "events.trash", "events.restore":
		return formatEventRecord(extractNestedMap(body, "event"))
	case "docs.get", "docs.create", "docs.put", "docs.update", "docs.revisions.create", "docs.trash", "docs.archive", "docs.unarchive", "docs.restore":
		return formatDocumentRecord(body)
	case "docs.update.propose", "docs.revisions.create.propose":
		return formatProposalPreview(body)
	case "docs.update.apply", "docs.revisions.create.apply":
		return formatProposalApply(body)
	case "docs.purge":
		root := asMap(body)
		label := firstNonEmpty(anyString(root["document_ref"]), anyString(root["document_handle"]), anyString(root["document_id"]))
		if purged, _ := root["purged"].(bool); purged && label != "" {
			return "Document " + label + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "docs.content":
		return formatDocumentContentRecord(body)
	case "docs.message", "docs.reply":
		return messageWriteText(body, "Document")
	case "docs.messages":
		return formatDocMessages(body)
	case "docs.comments.list":
		return formatDocsComments(body)
	case "docs.comments.create", "docs.comments.reply":
		return formatDocsComment(body)
	case "topics.message", "topics.reply":
		return messageWriteText(body, "Topic")
	case "topics.messages":
		return formatTopicMessages(body)
	case "docs.revision.get", "docs.revisions.get", "cards.revisions.get":
		return formatRevisionRecord(extractNestedMap(body, "revision"))
	case "provenance.walk":
		return formatProvenanceWalkSummary(asMap(body))
	case "boards.list":
		return formatBoardsList(body)
	case "boards.get", "boards.create", "boards.patch", "boards.archive", "boards.unarchive", "boards.trash", "boards.restore":
		return formatBoardRecord(extractNestedMap(body, "board"))
	case "boards.purge":
		root := asMap(body)
		label := firstNonEmpty(anyString(root["board_ref"]), anyString(root["board_handle"]), anyString(root["board_id"]))
		if purged, _ := root["purged"].(bool); purged && label != "" {
			return "Board " + label + " permanently deleted"
		}
		return formatPrettyBody(body)
	case "boards.workspace":
		return formatBoardWorkspace(body)
	case "boards.cards.list":
		return formatBoardCardsList(body)
	case "boards.cards.get":
		return formatBoardCardGetResult(body)
	case "boards.cards.batch_add":
		return formatBoardCardsBatchCreateResult(body)
	case "boards.cards.add":
		return formatBoardCardMutationResult(body)
	case "boards.cards.remove":
		return formatBoardCardRemoveResult(body)
	default:
		return formatPrettyBody(body)
	}
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

func appendFollowUpSection(lines []string, followUp map[string]any) []string {
	if followUp == nil {
		return lines
	}
	template := strings.TrimSpace(anyString(followUp["events_get_template"]))
	examples := stringList(followUp["events_get_examples"])
	contextRefresh := strings.TrimSpace(anyString(followUp["context_refresh_command"]))
	if template == "" && len(examples) == 0 && contextRefresh == "" {
		return lines
	}
	lines = append(lines, "follow_up:")
	if template != "" {
		lines = append(lines, "- events_get_template: "+template)
	}
	for _, example := range examples {
		lines = append(lines, "- events_get_example: "+example)
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
	return displayCompactIDWithMode(item, fullID)
}

func displayCompactIDWithMode(item map[string]any, fullID bool) string {
	if item == nil {
		return ""
	}
	if !fullID {
		if identity := displayPublicIdentity(item, ""); identity != "" {
			return identity
		}
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		id = strings.TrimSpace(anyString(item["revision_id"]))
	}
	if fullID && id != "" {
		return id
	}
	return firstNonEmpty(id, "(unknown)")
}

func displayPublicIdentity(item map[string]any, kind string) string {
	if item == nil {
		return ""
	}
	if ref := publicRef(item, kind); ref != "" {
		return ref
	}
	if handle := publicHandle(item, kind); handle != "" {
		if normalizedKind := canonicalResourceKind(kind); normalizedKind != "" {
			return normalizedKind + ":" + handle
		}
		return handle
	}
	return publicIDFallback(item)
}

func publicRef(item map[string]any, kind string) string {
	for _, key := range publicIdentityKeys(kind, "ref") {
		if value := strings.TrimSpace(anyString(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func publicHandle(item map[string]any, kind string) string {
	for _, key := range publicIdentityKeys(kind, "handle") {
		if value := strings.TrimSpace(anyString(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func publicIdentityKeys(kind string, suffix string) []string {
	normalizedKind := canonicalResourceKind(kind)
	keys := []string{suffix}
	switch normalizedKind {
	case "document":
		keys = append(keys, "document_"+suffix, "doc_"+suffix)
	case "revision":
		keys = append(keys, "revision_"+suffix)
	case "inbox":
		keys = append(keys, "inbox_"+suffix, "alias")
	case "event", "topic", "card", "board", "artifact", "thread":
		keys = append(keys, normalizedKind+"_"+suffix)
	}
	return keys
}

func publicIDFallback(item map[string]any) string {
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		id = strings.TrimSpace(anyString(item["revision_id"]))
	}
	return firstNonEmpty(id, "(unknown)")
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
