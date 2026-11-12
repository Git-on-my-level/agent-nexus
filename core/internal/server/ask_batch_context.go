// Shared batch context and per-question fields for filed ask batches: the
// optional context block stated once for every question, the server-assigned
// display order, and the per-question multi_select flag and note.

package server

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/askpresentation"
	"agent-nexus-core/internal/primitives"
)

const (
	askBatchContextTitleMax = 160
	askBatchNoteMax         = 4000
	askBatchEvidenceMax     = 20
	askBatchEvidenceURLMax  = 2000
)

// normalizeAskBatchContext validates the optional context block of
// POST /asks/batch. It returns nil when the block is absent. A present block
// must state the situation; impact, details, evidence and a batch title are
// optional and bounded like the matching per-ask fields.
func normalizeAskBatchContext(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("context must be an object with situation, impact, details and evidence")
	}
	out := map[string]any{}
	for key, value := range block {
		switch key {
		case "title", "situation", "impact", "details":
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("context.%s must be a string", key)
			}
			if text = strings.TrimSpace(text); text != "" {
				out[key] = text
			}
		case "evidence":
			links, err := normalizeAskBatchEvidence(value)
			if err != nil {
				return nil, err
			}
			if len(links) > 0 {
				out[key] = links
			}
		default:
			return nil, fmt.Errorf("context.%s is not a context field; use title, situation, impact, details or evidence", key)
		}
	}
	if anyString(out["situation"]) == "" {
		return nil, fmt.Errorf("context.situation is required when a batch states a shared context")
	}
	title := anyString(out["title"])
	if strings.ContainsAny(title, "\r\n") || utf8.RuneCountInString(title) > askBatchContextTitleMax {
		return nil, fmt.Errorf("context.title must be one line of at most %d characters", askBatchContextTitleMax)
	}
	// The same bounds as the per-ask situation, impact and details.
	bounded := map[string]any{}
	for _, key := range []string{"situation", "impact", "details"} {
		if value, ok := out[key]; ok {
			bounded[key] = value
		}
	}
	if err := askpresentation.Validate(bounded); err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return out, nil
}

func normalizeAskBatchEvidence(raw any) ([]any, error) {
	list, ok := raw.([]any)
	if !ok || len(list) > askBatchEvidenceMax {
		return nil, fmt.Errorf("context.evidence must be an array of at most %d {label, url} links", askBatchEvidenceMax)
	}
	out := make([]any, 0, len(list))
	for index, entry := range list {
		link, ok := entry.(map[string]any)
		label, labelOK := link["label"].(string)
		url, urlOK := link["url"].(string)
		url = strings.TrimSpace(url)
		if !ok || !labelOK || !urlOK || url == "" || utf8.RuneCountInString(url) > askBatchEvidenceURLMax || utf8.RuneCountInString(label) > 240 {
			return nil, fmt.Errorf("context.evidence[%d] must be {label, url} with a non-empty url", index)
		}
		out = append(out, map[string]any{"label": strings.TrimSpace(label), "url": url})
	}
	return out, nil
}

// validateAskBatchQuestionFields checks the per-question fields a batch member
// may carry and rejects the ones the server assigns.
func validateAskBatchQuestionFields(payload map[string]any) error {
	for _, key := range []string{"batch_context", "batch_position"} {
		if _, present := payload[key]; present {
			return fmt.Errorf("payload.%s is assigned by the server", key)
		}
	}
	if value, present := payload["multi_select"]; present {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("payload.multi_select must be a boolean")
		}
	}
	if value, present := payload["note"]; present {
		note, ok := value.(string)
		if !ok || utf8.RuneCountInString(note) > askBatchNoteMax {
			return fmt.Errorf("payload.note must be a string of at most %d characters", askBatchNoteMax)
		}
	}
	return nil
}

// stampAskBatchMembers returns copies of the validated member events with
// their 1-based display position and, when the batch states one, the shared
// context. The caller's maps are not mutated, so a retry hashes identically.
func stampAskBatchMembers(members []map[string]any, context map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(members))
	for index, member := range members {
		event := cloneWorkspaceMap(member)
		payload := cloneWorkspaceMap(member["payload"].(map[string]any))
		payload["batch_position"] = index + 1
		if context != nil {
			payload["batch_context"] = cloneWorkspaceMap(context)
		}
		event["payload"] = payload
		out = append(out, event)
	}
	return out
}

// askBatchSharedContext is the context stated once for the batch. Every
// member carries the same copy; the first one found wins, withdrawn or not,
// so withdrawing a question never drops the context of the rest.
func askBatchSharedContext(members []map[string]any) map[string]any {
	for _, member := range members {
		payload, _ := member["payload"].(map[string]any)
		if context, ok := payload["batch_context"].(map[string]any); ok && len(context) > 0 {
			return cloneWorkspaceMap(context)
		}
	}
	return nil
}

func askBatchQuestionFields(dst, payload map[string]any) {
	primitives.AskBatchQuestionFields(dst, payload)
}

func askBatchPosition(payload map[string]any) int { return primitives.AskBatchPosition(payload) }

// askBatchRecipientKey compares recipients the way principals are matched:
// with or without the actor: prefix.
func askBatchRecipientKey(payload map[string]any) string {
	return strings.TrimPrefix(strings.TrimSpace(anyString(payload["recipient_actor_id"])), "actor:")
}

// askBatchRecipientsDiffer reports members addressed to different people (or
// some addressed and some not). Such a batch cannot be one inbox row.
func askBatchRecipientsDiffer(members []map[string]any) bool {
	first := ""
	for index, member := range members {
		payload, _ := member["payload"].(map[string]any)
		key := askBatchRecipientKey(payload)
		if index == 0 {
			first = key
		} else if key != first {
			return true
		}
	}
	return false
}

// askBatchRecipient is the recipient every member names, as stored.
func askBatchRecipient(members []map[string]any) string {
	for _, member := range members {
		payload, _ := member["payload"].(map[string]any)
		if recipient := strings.TrimSpace(anyString(payload["recipient_actor_id"])); recipient != "" {
			return recipient
		}
	}
	return ""
}
