// Typed-ref (related_refs) contract shaping for inbox items: picking subject
// anchors from event refs, merging sorted ref lists, and reshaping legacy
// stored rows so HTTP surfaces expose only the contract fields.

package server

import (
	"sort"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// subjectRefPrefixesPreferred orders typed refs when choosing an inbox subject anchor
// from event refs. Earlier prefixes win.
var subjectRefPrefixesPreferred = []string{
	"topic", "card", "board", "document", "receipt", "artifact", "thread",
}

const (
	humanAttentionRequestedEventType = "human_attention_requested"
	humanAttentionRespondedEventType = "human_attention_responded"
	humanAttentionWithdrawnEventType = "human_attention_withdrawn"
)

func pickSubjectRefFromEventRefs(refs []string, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	for _, wantPrefix := range subjectRefPrefixesPreferred {
		for _, raw := range refs {
			raw = strings.TrimSpace(raw)
			prefix, id, err := schema.SplitTypedRef(raw)
			if err != nil || strings.TrimSpace(id) == "" {
				continue
			}
			if prefix == wantPrefix {
				return raw
			}
		}
	}
	for _, raw := range refs {
		raw = strings.TrimSpace(raw)
		prefix, id, err := schema.SplitTypedRef(raw)
		if err != nil || strings.TrimSpace(id) == "" {
			continue
		}
		switch prefix {
		case "inbox", "event":
			continue
		default:
			return raw
		}
	}
	if threadID != "" {
		return "thread:" + threadID
	}
	return ""
}

func mergeUniqueSortedRefs(refs ...string) []string {
	seen := make(map[string]struct{}, len(refs))
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func eventBackedInboxRelatedRefs(eventRefs []string, threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	var merged []string
	for _, r := range eventRefs {
		merged = append(merged, strings.TrimSpace(r))
	}
	if threadID != "" {
		merged = append(merged, "thread:"+threadID)
	}
	return mergeUniqueSortedRefs(merged...)
}

func workItemRiskInboxRelatedRefs(card map[string]any, threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	var merged []string
	if rr, err := extractStringSlice(card["related_refs"]); err == nil {
		for _, r := range rr {
			merged = append(merged, strings.TrimSpace(r))
		}
	}
	if rfs, err := extractStringSlice(card["refs"]); err == nil {
		for _, r := range rfs {
			merged = append(merged, strings.TrimSpace(r))
		}
	}
	if bid := strings.TrimSpace(anyString(card["board_id"])); bid != "" {
		merged = append(merged, "board:"+bid)
	}
	if threadID != "" {
		merged = append(merged, "thread:"+threadID)
	}
	if doc := pinnedDocumentIDFromCard(card); doc != "" {
		merged = append(merged, "document:"+strings.TrimSpace(doc))
	}
	return mergeUniqueSortedRefs(merged...)
}

func typedRefStringsToAnyList(refs []string) []any {
	out := make([]any, len(refs))
	for i, r := range refs {
		out[i] = r
	}
	return out
}

type inboxContractHint struct {
	ThreadID      string
	Category      string
	SourceEventID string
	SourceCardID  string
}

func inboxContractHintFromDerived(item primitives.DerivedInboxItem) inboxContractHint {
	// Use indexed columns (plus rehydrate into Data) as the only source for mirrored ids —
	// do not re-derive from data_json, which may contain stale pre-canonical values.
	return inboxContractHint{
		ThreadID:      strings.TrimSpace(item.ThreadID),
		Category:      strings.TrimSpace(item.Category),
		SourceEventID: strings.TrimSpace(item.SourceEventID),
		SourceCardID:  strings.TrimSpace(item.SourceCardID),
	}
}

func inboxRelatedRefsAbsentOrEmpty(m map[string]any) bool {
	raw, ok := m["related_refs"]
	if !ok || raw == nil {
		return true
	}
	list, err := extractStringSlice(raw)
	return err != nil || len(list) == 0
}

// backfillInboxRelatedRefsFromStoredData merges stored inbox row fields into a contract-shaped
// `related_refs` list. The `refs` key is legacy-only on persisted derived inbox rows (pre-
// related_refs); HTTP/OpenAPI surfaces still expose only `related_refs` after applyInboxContractShape.
func backfillInboxRelatedRefsFromStoredData(m map[string]any, h inboxContractHint) []any {
	tid := strings.TrimSpace(h.ThreadID)
	var merged []string
	if rr, err := extractStringSlice(m["related_refs"]); err == nil {
		for _, r := range rr {
			merged = append(merged, strings.TrimSpace(r))
		}
	}
	if rfs, err := extractStringSlice(m["refs"]); err == nil {
		for _, r := range rfs {
			merged = append(merged, strings.TrimSpace(r))
		}
	}
	if tid != "" {
		merged = append(merged, "thread:"+tid)
	}
	return typedRefStringsToAnyList(mergeUniqueSortedRefs(merged...))
}

// applyInboxContractShape ensures OpenAPI-required InboxItem fields (subject_ref, related_refs)
// and optional source_event_ref are present for list/get/stream payloads. Callers may rely on
// this for legacy derived rows that predate contract-first shaping. Drops deprecated
// recommended_action so older stored rows do not leak removed contract fields.
func applyInboxContractShape(m map[string]any, h inboxContractHint) {
	if m == nil {
		return
	}
	tid := strings.TrimSpace(h.ThreadID)

	if strings.TrimSpace(anyString(m["subject_ref"])) == "" {
		if strings.TrimSpace(anyString(m["subject_ref"])) == "" && tid != "" {
			m["subject_ref"] = "thread:" + tid
		}
	}

	if inboxRelatedRefsAbsentOrEmpty(m) {
		backfilled := backfillInboxRelatedRefsFromStoredData(m, h)
		if len(backfilled) == 0 && tid != "" {
			backfilled = []any{"thread:" + tid}
		}
		m["related_refs"] = backfilled
	}

	if eid := strings.TrimSpace(h.SourceEventID); eid != "" {
		if strings.TrimSpace(anyString(m["request_event_ref"])) == "" {
			m["request_event_ref"] = "event:" + eid
		}
		if strings.TrimSpace(anyString(m["source_event_ref"])) == "" {
			m["source_event_ref"] = "event:" + eid
		}
	}

	if strings.TrimSpace(anyString(m["kind"])) == "" {
		m["kind"] = strings.TrimSpace(h.Category)
	}

	delete(m, "category")
	delete(m, "recommended_action")
}
