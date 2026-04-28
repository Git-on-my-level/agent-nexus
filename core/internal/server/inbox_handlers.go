package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
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

const defaultInboxRiskHorizon = 7 * 24 * time.Hour

type derivedInboxItem struct {
	Data      map[string]any
	Category  string
	ID        string
	TriggerAt time.Time
	DueAt     time.Time
	HasDueAt  bool
}

func payloadFromDerivedInboxItem(item primitives.DerivedInboxItem) map[string]any {
	m := cloneWorkspaceMap(item.Data)
	if m == nil {
		m = map[string]any{}
	}
	trigger := strings.TrimSpace(item.TriggerAt)
	if trigger != "" {
		if _, ok := m["source_event_time"]; !ok {
			m["source_event_time"] = trigger
		}
		if _, ok := m["trigger_at"]; !ok {
			m["trigger_at"] = trigger
		}
	}
	applyInboxContractShape(m, inboxContractHintFromDerived(item))
	return m
}

func payloadFromLocalDerivedInboxItem(item derivedInboxItem) map[string]any {
	m := cloneWorkspaceMap(item.Data)
	if m == nil {
		m = map[string]any{}
	}
	if !item.TriggerAt.IsZero() {
		trigger := item.TriggerAt.Format(time.RFC3339Nano)
		if _, ok := m["source_event_time"]; !ok {
			m["source_event_time"] = trigger
		}
		if _, ok := m["trigger_at"]; !ok {
			m["trigger_at"] = trigger
		}
	}
	applyInboxContractShape(m, inboxContractHint{
		ThreadID:      strings.TrimSpace(anyString(m["thread_id"])),
		Category:      strings.TrimSpace(item.Category),
		SourceEventID: strings.TrimSpace(anyString(m["source_event_id"])),
		SourceCardID:  strings.TrimSpace(anyString(m["card_id"])),
	})
	return m
}

func handleGetInbox(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	now := time.Now().UTC()
	horizon, ok := resolveInboxRiskHorizon(w, r, opts)
	if !ok {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")) != "" {
		items, err := deriveInboxItems(r.Context(), opts, now, horizon)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive inbox items")
			return
		}

		payloadItems := make([]map[string]any, 0, len(items))
		for _, item := range items {
			payload := payloadFromLocalDerivedInboxItem(item)
			enrichHumanAttentionNotificationStatus(r.Context(), opts, payload)
			payloadItems = append(payloadItems, payload)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"items":        payloadItems,
			"generated_at": now.Format(time.RFC3339Nano),
		})
		return
	}

	threads, _, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load threads")
		return
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadIDs = append(threadIDs, anyString(thread["id"]))
	}
	states, err := loadTopicProjectionStates(r.Context(), opts, threadIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projection status")
		return
	}

	projected, err := opts.primitiveStore.ListDerivedInboxItems(r.Context(), primitives.DerivedInboxListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projections")
		return
	}

	payloadItems := make([]map[string]any, 0, len(projected))
	for _, item := range projected {
		payload := payloadFromDerivedInboxItem(item)
		enrichHumanAttentionNotificationStatus(r.Context(), opts, payload)
		payloadItems = append(payloadItems, payload)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":                payloadItems,
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": aggregateTopicProjectionFreshness(states, threadIDs),
	})
}

func handleGetInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, inboxItemID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	inboxItemID = strings.TrimSpace(inboxItemID)
	if inboxItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required")
		return
	}

	now := time.Now().UTC()
	horizon, ok := resolveInboxRiskHorizon(w, r, opts)
	if !ok {
		return
	}
	if strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")) != "" {
		items, err := deriveInboxItems(r.Context(), opts, now, horizon)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive inbox items")
			return
		}

		wantedIDs := make(map[string]struct{})
		for _, id := range inboxItemIDVariants(inboxItemID) {
			wantedIDs[id] = struct{}{}
		}
		for _, item := range items {
			if _, ok := wantedIDs[item.ID]; !ok {
				continue
			}
			payload := payloadFromLocalDerivedInboxItem(item)
			enrichHumanAttentionNotificationStatus(r.Context(), opts, payload)
			writeJSON(w, http.StatusOK, map[string]any{
				"item":         payload,
				"generated_at": now.Format(time.RFC3339Nano),
			})
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
		return
	}

	threads, _, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load threads")
		return
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadIDs = append(threadIDs, anyString(thread["id"]))
	}
	states, err := loadTopicProjectionStates(r.Context(), opts, threadIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projection status")
		return
	}

	var item primitives.DerivedInboxItem
	for _, candidate := range inboxItemIDVariants(inboxItemID) {
		item, err = opts.primitiveStore.GetDerivedInboxItem(r.Context(), candidate)
		if err == nil {
			break
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load inbox projections")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "inbox item not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"item":                 enrichHumanAttentionNotificationStatus(r.Context(), opts, payloadFromDerivedInboxItem(item)),
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": cloneWorkspaceMap(states[item.ThreadID].Freshness),
	})
}

func enrichHumanAttentionNotificationStatus(ctx context.Context, opts handlerOptions, item map[string]any) map[string]any {
	if item == nil {
		return item
	}
	if canonicalHumanAttentionKind(anyString(item["kind"])) == "" {
		return item
	}
	requesterActorID := strings.TrimSpace(anyString(item["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(item["requester_agent_id"]))
	target, found, err := resolveAgentNotificationTarget(ctx, opts, requesterActorID, requesterAgentID)
	status := map[string]any{
		"mode":               "original",
		"requester_actor_id": requesterActorID,
		"requester_agent_id": requesterAgentID,
		"resolvable":         found && err == nil,
	}
	if err != nil {
		status["state"] = "error"
		status["message"] = "Failed to resolve requester notification target."
	} else if found {
		status["state"] = "resolvable"
		status["target_actor_id"] = target.ActorID
		status["target_agent_id"] = target.AgentID
		status["target_handle"] = target.Handle
		status["message"] = "Original requester can be notified."
	} else {
		status["state"] = "unresolved"
		status["message"] = "Original requester is not resolvable; choose a replacement target or record without notification."
	}
	item["notification_target_status"] = status
	return item
}

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

func deriveInboxItems(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	if _, err := emitStaleThreadExceptions(ctx, opts, now, ""); err != nil {
		return nil, err
	}

	return deriveInboxItemsNoStaleEmission(ctx, opts, now, riskHorizon)
}

func resolveInboxRiskHorizon(w http.ResponseWriter, r *http.Request, opts handlerOptions) (time.Duration, bool) {
	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}

	if rawDays := strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")); rawDays != "" {
		days, err := strconv.Atoi(rawDays)
		if err != nil || days < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "risk_horizon_days must be a non-negative integer")
			return 0, false
		}
		horizon = time.Duration(days) * 24 * time.Hour
	}
	return horizon, true
}

func deriveInboxItemsNoStaleEmission(ctx context.Context, opts handlerOptions, now time.Time, riskHorizon time.Duration) ([]derivedInboxItem, error) {
	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{
		Types: []string{
			humanAttentionRequestedEventType,
			humanAttentionRespondedEventType,
		},
	})
	if err != nil {
		return nil, err
	}

	decidedIDs := decidedInboxItemIDs(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case humanAttentionRequestedEventType:
			item, ok := deriveHumanAttentionInboxItem(event)
			if !ok {
				continue
			}
			if _, decided := decidedIDs[item.ID]; decided {
				continue
			}
			items = append(items, item)
		}
	}

	sortInboxItems(items)
	return items, nil
}

func isStaleTopicException(event map[string]any) bool {
	payload, _ := event["payload"].(map[string]any)
	subtype, _ := payload["subtype"].(string)
	return subtype == "stale_topic"
}

func decidedInboxItemIDs(events []map[string]any) map[string]struct{} {
	out := make(map[string]struct{})
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != humanAttentionRespondedEventType {
			continue
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
	return []string{inboxItemID}
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
	if severity := strings.TrimSpace(anyString(payload["severity"])); severity != "" {
		data["severity"] = severity
	}
	if coverageHint := strings.TrimSpace(anyString(payload["coverage_hint"])); coverageHint != "" {
		data["coverage_hint"] = coverageHint
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

func boardCardCountsAsOpenWorkItem(card map[string]any) bool {
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
	if strings.TrimSpace(subjectID) == "" {
		subjectID = "none"
	}
	if strings.TrimSpace(sourceEventID) == "" {
		sourceEventID = "none"
	}
	return "inbox:" + category + ":" + threadID + ":" + subjectID + ":" + sourceEventID
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
