package server

import (
	"errors"
	"strings"

	"organization-autorunner-core/internal/schema"
)

// publicCardView maps a store-backed card record to the contract-first JSON shape
// returned by HTTP handlers. Internal-only keys (legacy aliases, duplicate bodies,
// scalar assignee, legacy status, board_id) are omitted.
func publicCardView(card map[string]any) map[string]any {
	if card == nil {
		return nil
	}
	out := make(map[string]any)

	out["id"] = card["id"]
	out["title"] = card["title"]

	summary := strings.TrimSpace(anyString(card["summary"]))
	if summary == "" {
		summary = strings.TrimSpace(anyString(card["body"]))
	}
	out["summary"] = summary

	boardRef := strings.TrimSpace(anyString(card["board_ref"]))
	if boardRef == "" {
		if bid := strings.TrimSpace(anyString(card["board_id"])); bid != "" {
			boardRef = "board:" + bid
		}
	}
	if boardRef != "" {
		out["board_ref"] = boardRef
	}

	if tr := topicRefFromCardRefs(card); tr != "" {
		out["topic_ref"] = tr
	}

	if v, ok := card["thread_id"]; ok {
		out["thread_id"] = v
	}

	if dr := documentRefForPublicCard(card); dr != "" {
		out["document_ref"] = dr
	}

	out["column_key"] = card["column_key"]
	out["rank"] = card["rank"]

	out["assignee_refs"] = publicAssigneeRefs(card)

	if v, ok := card["due_at"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["due_at"] = v
		} else {
			out["due_at"] = nil
		}
	} else {
		out["due_at"] = nil
	}

	out["definition_of_done"] = stringSliceAsAnyList(card["definition_of_done"])

	out["risk"] = publicCardRisk(card)

	res := strings.TrimSpace(anyString(card["resolution"]))
	if res == "" {
		out["resolution"] = nil
	} else {
		out["resolution"] = res
	}

	out["resolution_refs"] = stringSliceAsAnyList(card["resolution_refs"])
	out["related_refs"] = mergeRelatedRefsForPublicView(card)

	out["created_at"] = card["created_at"]
	out["created_by"] = card["created_by"]
	out["updated_at"] = card["updated_at"]
	out["updated_by"] = card["updated_by"]
	out["provenance"] = card["provenance"]

	if v, ok := card["archived_at"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["archived_at"] = v
		}
	}
	if v, ok := card["archived_by"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["archived_by"] = v
		}
	}
	if v, ok := card["tombstoned_at"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["tombstoned_at"] = v
		}
	}
	if v, ok := card["tombstoned_by"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["tombstoned_by"] = v
		}
	}
	if v, ok := card["tombstone_reason"]; ok {
		if s := strings.TrimSpace(anyString(v)); s != "" {
			out["tombstone_reason"] = v
		}
	}
	return out
}

func normalizeBoardCardMutationReplayPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		out[k] = v
	}
	if c, ok := payload["card"].(map[string]any); ok && c != nil {
		out["card"] = publicCardView(c)
	}
	return out
}

func publicCardsView(cards []map[string]any) []map[string]any {
	if cards == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(cards))
	for _, c := range cards {
		out = append(out, publicCardView(c))
	}
	return out
}

// publicCardPayload is like publicCardView but preserves optional card history with the same shaping.
func publicCardPayload(card map[string]any) map[string]any {
	pub := publicCardView(card)
	if pub == nil {
		return nil
	}
	if hist, ok := card["history"]; ok && hist != nil {
		pub["history"] = publicCardHistoryList(hist)
	}
	return pub
}

func publicCardHistoryList(raw any) any {
	switch h := raw.(type) {
	case []map[string]any:
		out := make([]map[string]any, 0, len(h))
		for _, item := range h {
			out = append(out, publicCardView(item))
		}
		return out
	case []any:
		out := make([]map[string]any, 0, len(h))
		for _, item := range h {
			if m, ok := item.(map[string]any); ok {
				out = append(out, publicCardView(m))
			}
		}
		return out
	default:
		return raw
	}
}

func documentRefForPublicCard(card map[string]any) string {
	if dr := strings.TrimSpace(anyString(card["document_ref"])); dr != "" {
		return dr
	}
	if pid := strings.TrimSpace(anyString(card["pinned_document_id"])); pid != "" {
		return "document:" + pid
	}
	return ""
}

func pinnedDocumentIDFromCard(card map[string]any) string {
	if pid := strings.TrimSpace(anyString(card["pinned_document_id"])); pid != "" {
		return pid
	}
	ref := strings.TrimSpace(anyString(card["document_ref"]))
	if ref == "" {
		return ""
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil || prefix != "document" {
		return ""
	}
	return id
}

func mergeRelatedRefsForPublicView(card map[string]any) []any {
	refs := stringSliceAsAnyList(card["refs"])
	parent := strings.TrimSpace(anyString(card["parent_thread"]))
	if parent == "" {
		return refs
	}
	want := "thread:" + parent
	for _, r := range refs {
		if strings.TrimSpace(anyString(r)) == want {
			return refs
		}
	}
	out := make([]any, 0, len(refs)+1)
	out = append(out, want)
	out = append(out, refs...)
	return out
}

func topicRefFromCardRefs(card map[string]any) string {
	refs, err := extractStringSlice(card["refs"])
	if err != nil {
		return ""
	}
	for _, r := range refs {
		r = strings.TrimSpace(r)
		prefix, _, err := schema.SplitTypedRef(r)
		if err == nil && prefix == "topic" {
			return r
		}
	}
	return ""
}

func publicAssigneeRefs(card map[string]any) []any {
	if raw := card["assignee_refs"]; raw != nil {
		if refs, err := extractStringSlice(raw); err == nil && len(refs) > 0 {
			out := make([]any, len(refs))
			for i, r := range refs {
				out[i] = normalizeAssigneeTypedRef(r)
			}
			return out
		}
	}
	a := strings.TrimSpace(anyString(card["assignee"]))
	if a == "" {
		return []any{}
	}
	return []any{normalizeAssigneeTypedRef(a)}
}

func normalizeAssigneeTypedRef(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.Contains(raw, ":") {
		return raw
	}
	return "actor:" + raw
}

func assigneeStorageStringFromRefs(refs []string) *string {
	if len(refs) == 0 {
		return nil
	}
	ref := strings.TrimSpace(refs[0])
	if ref == "" {
		return nil
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil {
		s := ref
		return &s
	}
	switch prefix {
	case "actor", "human", "agent":
		if strings.TrimSpace(id) == "" {
			return nil
		}
		return &id
	default:
		s := ref
		return &s
	}
}

func pinnedDocumentIDFromTypedRef(ref string) (*string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		empty := ""
		return &empty, nil
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil {
		return nil, err
	}
	if prefix != "document" {
		return nil, errors.New("document_ref must be a document: typed ref")
	}
	if err := validateDocumentID(id); err != nil {
		return nil, err
	}
	return &id, nil
}

func publicCardRisk(card map[string]any) string {
	if r := strings.TrimSpace(anyString(card["risk"])); r != "" {
		switch r {
		case "low", "medium", "high", "critical":
			return r
		}
	}
	return "low"
}

func stringSliceAsAnyList(raw any) []any {
	refs, err := extractStringSlice(raw)
	if err != nil || len(refs) == 0 {
		return []any{}
	}
	out := make([]any, len(refs))
	for i, r := range refs {
		out[i] = r
	}
	return out
}
