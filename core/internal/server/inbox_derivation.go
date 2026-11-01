// Deriving local inbox items from canonical events: the rebuild endpoint,
// human attention and batch item derivation, work-item risk state, inbox item
// IDs and timestamps, and open-item ordering. Complements the stored topic
// projection path in derived_projections.go.

package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/askpresentation"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func handleRebuildDerived(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	maintainer := opts.projectionMaintainer
	if maintainer == nil {
		maintainer = NewProjectionMaintainer(ProjectionMaintainerConfig{
			PrimitiveStore:   opts.primitiveStore,
			Contract:         opts.contract,
			InboxRiskHorizon: opts.inboxRiskHorizon,
			SystemActorID:    actors.SystemActorID,
		})
	}
	if err := maintainer.RunFullRebuild(r.Context(), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild derived views")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func isStaleTopicException(event map[string]any) bool {
	payload, _ := event["payload"].(map[string]any)
	subtype, _ := payload["subtype"].(string)
	return subtype == "stale_topic"
}

func decidedInboxItemIDs(events []map[string]any) map[string]struct{} {
	out := make(map[string]struct{})
	// Undo names one exact retirement, so replay order and tied timestamps do
	// not accidentally reopen a later retirement or an ordinary withdrawal.
	undone := map[string]bool{}
	for _, event := range events {
		if event["type"] == primitives.AskReopenedEventType {
			p, _ := event["payload"].(map[string]any)
			undone[strings.TrimPrefix(anyString(p["retirement_event_ref"]), "event:")] = true
		}
	}
	for _, event := range events {
		if event["type"] == primitives.AskRetiredEventType {
			if !undone[anyString(event["id"])] && !undone[strings.TrimPrefix(anyString(event["ref"]), "event:")] {
				p, _ := event["payload"].(map[string]any)
				out["event:"+anyString(p["request_event_id"])] = struct{}{}
			}
			continue
		}

		eventType, _ := event["type"].(string)
		if eventType == humanAttentionWithdrawnEventType {
			payload, _ := event["payload"].(map[string]any)
			requestID := strings.TrimSpace(anyString(payload["request_event_id"]))
			if requestID == "" {
				requestRef := strings.TrimSpace(anyString(payload["request_event_ref"]))
				prefix, value, err := schema.SplitTypedRef(requestRef)
				if err == nil && prefix == "event" {
					requestID = value
				}
			}
			if requestID != "" {
				out["event:"+requestID] = struct{}{}
			}
			continue
		}
		if eventType != humanAttentionRespondedEventType {
			continue
		}
		// An answered ask must never keep a derived decision surface open,
		// even if the response's refs omit the item id it closed.
		payload, _ := event["payload"].(map[string]any)
		if requestRef := strings.TrimSpace(anyString(payload["request_event_ref"])); strings.HasPrefix(requestRef, "event:") {
			out[requestRef] = struct{}{}
		}
		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}
		for _, ref := range refs {
			prefix, value, err := schema.SplitTypedRef(ref)
			if err != nil || prefix != "inbox" {
				continue
			}
			suppressID := strings.TrimSpace(value)
			if suppressID != "" {
				out[suppressID] = struct{}{}
			}
		}
	}
	return out
}

func inboxItemIDVariants(inboxItemID string) []string {
	inboxItemID = strings.TrimSpace(inboxItemID)
	if inboxItemID == "" {
		return nil
	}
	variants := []string{inboxItemID}
	// Older links/clients can omit or wrap the typed inbox prefix. Preserve
	// the submitted path for replay hashing; normalize only identity lookup.
	if strings.HasPrefix(inboxItemID, "inbox:inbox:") {
		variants = append(variants, strings.TrimPrefix(inboxItemID, "inbox:"))
	} else if !strings.HasPrefix(inboxItemID, "inbox:") {
		variants = append(variants, "inbox:"+inboxItemID)
	}
	return variants
}

func canonicalHumanAttentionKind(kind string) string {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "ask", "review", "escalate":
		return strings.TrimSpace(strings.ToLower(kind))
	default:
		return ""
	}
}

func deriveHumanAttentionInboxItem(event map[string]any) (derivedInboxItem, bool) {
	threadID := strings.TrimSpace(anyString(event["thread_id"]))
	sourceEventID := strings.TrimSpace(anyString(event["id"]))
	triggerAt, ok := parseTimestamp(event["ts"])
	if threadID == "" || sourceEventID == "" || !ok {
		return derivedInboxItem{}, false
	}

	payload, _ := event["payload"].(map[string]any)
	kind := canonicalHumanAttentionKind(anyString(payload["kind"]))
	if kind == "" {
		return derivedInboxItem{}, false
	}

	rawRefs, _ := extractStringSlice(event["refs"])
	subjectRef := strings.TrimSpace(anyString(payload["subject_ref"]))
	if subjectRef == "" {
		subjectRef = pickSubjectRefFromEventRefs(rawRefs, threadID)
	}

	related := make([]string, 0, len(rawRefs)+4)
	related = append(related, eventBackedInboxRelatedRefs(rawRefs, threadID)...)
	if additional, err := extractStringSlice(payload["related_refs"]); err == nil {
		related = append(related, additional...)
	}

	title := strings.TrimSpace(anyString(payload["title"]))
	body := strings.TrimSpace(anyString(payload["body"]))
	if title == "" {
		title = strings.TrimSpace(anyString(event["summary"]))
	}
	if title == "" && body != "" {
		title = body
	}
	if title == "" {
		title = "Human attention requested"
	}

	requestID := strings.TrimSpace(anyString(payload["request_id"]))
	if requestID == "" {
		requestID = sourceEventID
	}

	id := makeInboxItemID(kind, threadID, requestID, sourceEventID)
	data := map[string]any{
		"id":                 id,
		"kind":               kind,
		"thread_id":          threadID,
		"source_event_id":    sourceEventID,
		"request_event_ref":  "event:" + sourceEventID,
		"subject_ref":        subjectRef,
		"related_refs":       typedRefStringsToAnyList(mergeUniqueSortedRefs(related...)),
		"title":              title,
		"body":               body,
		"requester_actor_id": strings.TrimSpace(anyString(payload["requester_actor_id"])),
		"requester_agent_id": strings.TrimSpace(anyString(payload["requester_agent_id"])),
		"requester_label":    strings.TrimSpace(anyString(payload["requester_label"])),
		"created_at":         strings.TrimSpace(anyString(event["ts"])),
	}
	askpresentation.Copy(data, payload)
	if severity := strings.TrimSpace(anyString(payload["severity"])); severity != "" {
		data["severity"] = severity
	}
	if coverageHint := strings.TrimSpace(anyString(payload["coverage_hint"])); coverageHint != "" {
		data["coverage_hint"] = coverageHint
	}
	if delivery := strings.TrimSpace(anyString(payload["delivery"])); delivery != "" {
		data["delivery"] = delivery
	}

	proposals, err := NormalizeHumanAttentionResponseProposals(payload["response_proposals"])
	if err != nil {
		return derivedInboxItem{}, false
	}
	data["response_proposals"] = HumanAttentionResponseProposalsToAnySlice(proposals)

	return derivedInboxItem{
		Data:      data,
		Category:  kind,
		ID:        id,
		TriggerAt: triggerAt,
	}, true
}

// deriveHumanAttentionBatchInboxItem collapses the members of one filed ask
// batch into a single inbox item. The id is pinned to the oldest member so it
// stays stable while members are withdrawn; sections list only the members
// that are neither withdrawn nor answered, and every access-bearing field
// (subject, refs, title, requester) is rebuilt from those open members so a
// withdrawn private member cannot keep gating or leaking onto the item. The
// caller suppresses the whole item once a response refs it (batch answers are
// atomic, so there are no partial answers) or when no member stays open.
func deriveHumanAttentionBatchInboxItem(members []map[string]any, decidedIDs map[string]struct{}) (derivedInboxItem, bool) {
	if len(members) < 2 {
		return derivedInboxItem{}, false
	}
	ordered := append([]map[string]any{}, members...)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftTS, leftOK := parseTimestamp(ordered[i]["ts"])
		rightTS, rightOK := parseTimestamp(ordered[j]["ts"])
		return primitives.FiledBatchAnchorBefore(leftTS, leftOK, strings.TrimSpace(anyString(ordered[i]["id"])), rightTS, rightOK, strings.TrimSpace(anyString(ordered[j]["id"])))
	})
	// The anchor is the oldest member of the full set, withdrawn or not: it
	// pins the stable item id across membership changes.
	anchor := ordered[0]
	threadID := strings.TrimSpace(anyString(anchor["thread_id"]))
	anchorEventID := strings.TrimSpace(anyString(anchor["id"]))
	triggerAt, ok := parseTimestamp(anchor["ts"])
	if threadID == "" || anchorEventID == "" || !ok {
		return derivedInboxItem{}, false
	}
	anchorPayload, _ := anchor["payload"].(map[string]any)
	batchID := strings.TrimSpace(anyString(anchorPayload["batch_id"]))
	kind := canonicalHumanAttentionKind(anyString(anchorPayload["kind"]))
	if batchID == "" || kind == "" {
		return derivedInboxItem{}, false
	}

	sections := make([]map[string]any, 0, len(ordered))
	for _, member := range ordered {
		memberEventID := strings.TrimSpace(anyString(member["id"]))
		if memberEventID == "" {
			return derivedInboxItem{}, false
		}
		if _, decided := decidedIDs["event:"+memberEventID]; decided {
			continue
		}
		payload, _ := member["payload"].(map[string]any)
		if payload == nil {
			return derivedInboxItem{}, false
		}
		proposals, err := NormalizeHumanAttentionResponseProposals(payload["response_proposals"])
		if err != nil {
			return derivedInboxItem{}, false
		}
		section := map[string]any{
			"request_event_ref":  "event:" + memberEventID,
			"title":              strings.TrimSpace(anyString(payload["title"])),
			"subject_ref":        strings.TrimSpace(anyString(payload["subject_ref"])),
			"response_proposals": HumanAttentionResponseProposalsToAnySlice(proposals),
		}
		askpresentation.Copy(section, payload)
		if body := strings.TrimSpace(anyString(payload["body"])); body != "" {
			section["body"] = body
		}
		if delivery := strings.TrimSpace(anyString(payload["delivery"])); delivery != "" {
			section["delivery"] = delivery
		}
		sections = append(sections, section)
	}
	if len(sections) == 0 {
		return derivedInboxItem{}, false
	}
	// The lead is the oldest still-open member: it carries the display and
	// access fields, so a withdrawn private member neither names nor gates
	// the item for readers who could never see it.
	var leadPayload map[string]any
	leadEventID := ""
	leadTS := anchor["ts"]
	for _, member := range ordered {
		memberEventID := strings.TrimSpace(anyString(member["id"]))
		if _, decided := decidedIDs["event:"+memberEventID]; decided {
			continue
		}
		payload, _ := member["payload"].(map[string]any)
		if payload == nil {
			return derivedInboxItem{}, false
		}
		// `ordered` is sorted, so the first open member is the oldest.
		leadPayload = payload
		leadEventID = memberEventID
		leadTS = member["ts"]
		break
	}
	if leadPayload == nil {
		return derivedInboxItem{}, false
	}

	subjectRef := strings.TrimSpace(anyString(leadPayload["subject_ref"]))
	if subjectRef == "" {
		for _, member := range ordered {
			memberEventID := strings.TrimSpace(anyString(member["id"]))
			if _, decided := decidedIDs["event:"+memberEventID]; decided {
				continue
			}
			if refs := pickSubjectRefFromEventRefs(mustEventRefs(member), threadID); refs != "" {
				subjectRef = refs
				break
			}
		}
	}
	if subjectRef == "" {
		subjectRef = "thread:" + threadID
	}

	title := strings.TrimSpace(anyString(leadPayload["title"]))
	if title == "" {
		title = "Human attention requested"
	}
	id := makeInboxItemID(kind, threadID, batchID, anchorEventID)
	// Access to the batch item must imply access to every open member: a
	// section whose subject card is restricted makes the whole item invisible
	// to a principal who cannot see it, matching what N single items would do.
	batchRelated := []string{"thread:" + threadID, subjectRef}
	for _, section := range sections {
		if sectionSubject := strings.TrimSpace(anyString(section["subject_ref"])); sectionSubject != "" {
			batchRelated = append(batchRelated, sectionSubject)
		}
	}
	data := map[string]any{
		"id":                 id,
		"kind":               kind,
		"thread_id":          threadID,
		"source_event_id":    leadEventID,
		"request_event_ref":  "event:" + leadEventID,
		"subject_ref":        subjectRef,
		"related_refs":       typedRefStringsToAnyList(mergeUniqueSortedRefs(batchRelated...)),
		"title":              title,
		"body":               strings.TrimSpace(anyString(leadPayload["body"])),
		"requester_actor_id": strings.TrimSpace(anyString(leadPayload["requester_actor_id"])),
		"requester_agent_id": strings.TrimSpace(anyString(leadPayload["requester_agent_id"])),
		"requester_label":    strings.TrimSpace(anyString(leadPayload["requester_label"])),
		"created_at":         strings.TrimSpace(anyString(leadTS)),
		"batch": map[string]any{
			"id":   batchID,
			"asks": sections,
		},
	}
	askpresentation.Copy(data, leadPayload)
	if delivery := strings.TrimSpace(anyString(leadPayload["delivery"])); delivery != "" {
		data["delivery"] = delivery
	}
	data["response_proposals"] = sections[0]["response_proposals"]

	return derivedInboxItem{
		Data:      data,
		Category:  kind,
		ID:        id,
		TriggerAt: triggerAt,
	}, true
}

func mustEventRefs(event map[string]any) []string {
	refs, err := extractStringSlice(event["refs"])
	if err != nil {
		return nil
	}
	return refs
}

func boardCardCountsAsOpenWorkItem(card map[string]any) bool {
	if strings.TrimSpace(anyString(card["trashed_at"])) != "" || strings.TrimSpace(anyString(card["archived_at"])) != "" {
		return false
	}
	return primitives.BoardCardIsOpenWorkItem(anyString(card["column_key"]), anyString(card["resolution"]))
}

func boardCardRiskState(card map[string]any, now time.Time, riskHorizon time.Duration) (string, time.Time, bool) {
	if !boardCardCountsAsOpenWorkItem(card) {
		return "", time.Time{}, false
	}

	if strings.TrimSpace(anyString(card["column_key"])) == "blocked" {
		if dueAt, ok := parseOptionalRFC3339(anyString(card["due_at"])); ok && !dueAt.After(now.Add(riskHorizon)) {
			if dueAt.Before(now) {
				return "overdue", dueAt, true
			}
			return "blocked", dueAt, true
		}
		return "blocked", time.Time{}, false
	}

	dueAt, ok := parseOptionalRFC3339(anyString(card["due_at"]))
	if !ok {
		return "", time.Time{}, false
	}
	if dueAt.After(now.Add(riskHorizon)) {
		return "", time.Time{}, true
	}
	if dueAt.Before(now) {
		return "overdue", dueAt, true
	}
	return "due_soon", dueAt, true
}

func makeInboxItemID(category string, threadID string, subjectID string, sourceEventID string) string {
	return primitives.HumanAttentionInboxItemID(category, threadID, subjectID, sourceEventID)
}

func parseTimestamp(raw any) (time.Time, bool) {
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, text); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func parseOptionalRFC3339(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return parsed, true
	}
	parsed, err = time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func sortInboxItems(items []derivedInboxItem) {
	categoryOrder := map[string]int{
		"escalate": 0,
		"ask":      1,
		"review":   2,
	}

	sort.Slice(items, func(i int, j int) bool {
		left := items[i]
		right := items[j]

		leftOrder, ok := categoryOrder[left.Category]
		if !ok {
			leftOrder = 99
		}
		rightOrder, ok := categoryOrder[right.Category]
		if !ok {
			rightOrder = 99
		}
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}

		if !left.TriggerAt.Equal(right.TriggerAt) {
			return left.TriggerAt.After(right.TriggerAt)
		}

		return left.ID < right.ID
	})
}
