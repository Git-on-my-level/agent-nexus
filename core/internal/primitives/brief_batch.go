// Filed ask batches on the brief: one decision whose members are the open
// questions in filing order, each with its own proposals, and the shared
// context stated once on the batch.

package primitives

import (
	"sort"
	"strings"

	"agent-nexus-core/internal/askpresentation"
)

// withBatchQuestion records a filed batch member's question fields (position,
// multi_select, note) and the batch context on its collapse meta.
func withBatchQuestion(meta briefDecisionMeta, payload map[string]any) briefDecisionMeta {
	if meta.batchID == "" {
		return meta
	}
	meta.question = map[string]any{}
	AskBatchQuestionFields(meta.question, payload)
	meta.context, _ = payload["batch_context"].(map[string]any)
	return meta
}

// briefBatchView is the batch object of a collapsed decision. A filed batch
// lists its questions in filing order (curation batches carry no positions
// and keep urgency order) and states its shared context once; a context title
// names the whole decision.
func briefBatchView(items []map[string]any, metas []briefDecisionMeta, key string, group []int) map[string]any {
	list := make([]map[string]any, 0, len(group))
	var context map[string]any
	for _, mi := range group {
		list = append(list, briefBatchMember(items[mi], metas[mi]))
		if context == nil && len(metas[mi].context) > 0 {
			context = metas[mi].context
		}
	}
	sort.SliceStable(list, func(a, b int) bool {
		left, _ := list[a]["position"].(int)
		right, _ := list[b]["position"].(int)
		return left < right
	})
	batch := map[string]any{"key": key, "count": len(group), "members": list}
	if context != nil {
		batch["context"] = context
		if title := strings.TrimSpace(anyStringValue(context["title"])); title != "" {
			items[group[0]]["title"] = title
		}
	}
	return batch
}

// briefBatchMember is one question of a collapsed batch decision.
func briefBatchMember(item map[string]any, meta briefDecisionMeta) map[string]any {
	member := map[string]any{"ref": item["ref"], "inbox_item_id": item["inbox_item_id"], "title": item["title"]}
	askpresentation.Copy(member, item)
	if proposals, ok := item["proposals"]; ok {
		member["proposals"] = proposals
	}
	for key, value := range meta.question {
		member[key] = value
	}
	if eventRef := strings.TrimSpace(anyStringValue(item["event_ref"])); eventRef != "" {
		member["event_ref"] = eventRef
	} else if meta.askID != "" {
		member["event_ref"] = "event:" + meta.askID
	}
	if meta.subject != "" {
		member["subject_ref"] = meta.subject
	}
	if blocks, ok := item["blocks"].([]map[string]any); ok && len(blocks) > 0 {
		copied := make([]map[string]any, 0, len(blocks))
		for _, block := range blocks {
			ref := strings.TrimSpace(anyStringValue(block["ref"]))
			if ref == "" {
				continue
			}
			entry := map[string]any{"ref": ref}
			if title := strings.TrimSpace(anyStringValue(block["title"])); title != "" {
				entry["title"] = title
			}
			copied = append(copied, entry)
		}
		if len(copied) > 0 {
			member["blocks"] = copied
		}
	}
	return member
}
