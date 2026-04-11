package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

// subjectRefPrefixesPreferred orders typed refs when choosing an inbox subject anchor
// from event refs. Earlier prefixes win.
var subjectRefPrefixesPreferred = []string{
	"topic", "card", "board", "document", "receipt", "artifact", "thread",
}

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

func isWorkItemRiskStyleCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "risk_exception", "work_item_risk":
		return true
	default:
		return false
	}
}

func isEventSourcedInboxCategory(category string) bool {
	switch strings.TrimSpace(category) {
	case "action_needed", "risk_exception", "attention":
		return true
	// Legacy stored rows may still use pre-simplification category strings.
	case "decision_needed", "intervention_needed", "stale_topic", "document_attention":
		return true
	default:
		return false
	}
}

type inboxContractHint struct {
	ThreadID      string
	Category      string
	SourceEventID string
	SourceCardID  string
}

func inboxContractHintFromDerived(item primitives.DerivedInboxItem) inboxContractHint {
	d := item.Data
	if d == nil {
		d = map[string]any{}
	}
	cid := strings.TrimSpace(item.SourceCardID)
	if cid == "" {
		cid = strings.TrimSpace(anyString(d["card_id"]))
	}
	eid := strings.TrimSpace(item.SourceEventID)
	if eid == "" {
		eid = strings.TrimSpace(anyString(d["source_event_id"]))
	}
	tid := strings.TrimSpace(item.ThreadID)
	if tid == "" {
		tid = strings.TrimSpace(anyString(d["thread_id"]))
	}
	cat := strings.TrimSpace(item.Category)
	if cat == "" {
		cat = strings.TrimSpace(anyString(d["category"]))
	}
	return inboxContractHint{
		ThreadID:      tid,
		Category:      cat,
		SourceEventID: eid,
		SourceCardID:  cid,
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
	cat := strings.TrimSpace(h.Category)
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
	if cat == "risk_exception" || cat == "work_item_risk" {
		if bid := strings.TrimSpace(anyString(m["board_id"])); bid != "" {
			merged = append(merged, "board:"+bid)
		}
	}
	if tid != "" {
		merged = append(merged, "thread:"+tid)
	}
	if cat == "risk_exception" || cat == "work_item_risk" {
		if docRef := strings.TrimSpace(anyString(m["document_ref"])); docRef != "" {
			if _, docID, err := schema.SplitTypedRef(docRef); err == nil && docID != "" {
				merged = append(merged, "document:"+docID)
			}
		}
	}
	return typedRefStringsToAnyList(mergeUniqueSortedRefs(merged...))
}

// normalizeInboxCategoryForContract maps legacy stored categories and pre-simplification
// values onto enums.inbox_category so HTTP payloads stay OpenAPI-valid.
func normalizeInboxCategoryForContract(category string) string {
	category = strings.TrimSpace(category)
	switch category {
	case "action_needed", "risk_exception", "attention":
		return category
	case "decision_needed":
		return "action_needed"
	case "intervention_needed", "stale_topic", "work_item_risk", "risk_review":
		return "risk_exception"
	case "document_attention":
		return "attention"
	default:
		return category
	}
}

// applyInboxContractShape ensures OpenAPI-required InboxItem fields (subject_ref, related_refs)
// and optional source_event_ref are present for list/get/stream payloads. Callers may rely on
// this for legacy derived rows that predate contract-first shaping. Drops deprecated
// recommended_action so older stored rows do not leak removed contract fields.
func applyInboxContractShape(m map[string]any, h inboxContractHint) {
	if m == nil {
		return
	}
	cat := strings.TrimSpace(h.Category)
	tid := strings.TrimSpace(h.ThreadID)

	if strings.TrimSpace(anyString(m["subject_ref"])) == "" {
		if cat == "risk_exception" || cat == "work_item_risk" {
			if cid := strings.TrimSpace(h.SourceCardID); cid != "" {
				m["subject_ref"] = "card:" + cid
			}
		}
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

	if isEventSourcedInboxCategory(cat) {
		if eid := strings.TrimSpace(h.SourceEventID); eid != "" {
			if strings.TrimSpace(anyString(m["source_event_ref"])) == "" {
				m["source_event_ref"] = "event:" + eid
			}
		}
	}

	rawCat := strings.TrimSpace(anyString(m["category"]))
	if rawCat == "" {
		rawCat = cat
	}
	if rawCat != "" {
		m["category"] = normalizeInboxCategoryForContract(rawCat)
	}

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
			payloadItems = append(payloadItems, payloadFromLocalDerivedInboxItem(item))
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
		payloadItems = append(payloadItems, payloadFromDerivedInboxItem(item))
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
			writeJSON(w, http.StatusOK, map[string]any{
				"item":         payloadFromLocalDerivedInboxItem(item),
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
		"item":                 payloadFromDerivedInboxItem(item),
		"generated_at":         now.Format(time.RFC3339Nano),
		"projection_freshness": cloneWorkspaceMap(states[item.ThreadID].Freshness),
	})
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
			SystemActorID:    "oar-core",
		})
	}
	if err := maintainer.RunFullRebuild(r.Context(), time.Now().UTC(), actorID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild derived views")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleAckInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, pathInboxItemID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string `json:"actor_id"`
		SubjectRef  string `json:"subject_ref"`
		InboxItemID string `json:"inbox_item_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	subjectRef := strings.TrimSpace(req.SubjectRef)
	if subjectRef == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "subject_ref is required")
		return
	}
	correlationID, err := resolveInboxAckBackingThreadID(r.Context(), opts.primitiveStore, subjectRef)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	pathInboxItemID = strings.TrimSpace(pathInboxItemID)
	bodyItemID := strings.TrimSpace(req.InboxItemID)
	effectiveItemID := bodyItemID
	if pathInboxItemID != "" {
		if bodyItemID != "" && bodyItemID != pathInboxItemID {
			writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id must match path inbox_id")
			return
		}
		effectiveItemID = pathInboxItemID
	}
	if effectiveItemID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "inbox_item_id is required (body or path)")
		return
	}
	req.InboxItemID = effectiveItemID

	event := map[string]any{
		"type":      "inbox_item_acknowledged",
		"thread_id": correlationID,
		"refs":      []string{"inbox:" + req.InboxItemID},
		"summary":   "inbox item acknowledged",
		"payload": map[string]any{
			"inbox_item_id": req.InboxItemID,
		},
		"provenance": actorStatementProvenance(),
	}

	if err := validateEventReferenceConventions(opts.contract, event, []string{"inbox:" + req.InboxItemID}); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	stored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, event)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to acknowledge inbox item")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{correlationID}, time.Now().UTC())

	writeJSON(w, http.StatusCreated, map[string]any{"event": stored})
}

func resolveInboxAckBackingThreadID(ctx context.Context, store PrimitiveStore, subjectRef string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("store is not configured")
	}
	prefix, id, err := schema.SplitTypedRef(subjectRef)
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("subject_ref id is required")
	}
	switch prefix {
	case "thread":
		return id, nil
	case "topic":
		topic, err := store.GetTopic(ctx, id)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", fmt.Errorf(
					"topic not found for subject_ref %q; topic: must be a real topic id (use thread:<thread_id> when the anchor is the backing thread only)",
					subjectRef,
				)
			}
			return "", err
		}
		tid := strings.TrimSpace(anyString(topic["thread_id"]))
		if tid == "" {
			return "", fmt.Errorf("topic %q has no backing thread_id", id)
		}
		return tid, nil
	case "card":
		cards, err := store.ListCards(ctx, primitives.CardListFilter{})
		if err != nil {
			return "", err
		}
		for _, card := range cards {
			if strings.TrimSpace(anyString(card["id"])) != id {
				continue
			}
			tid := strings.TrimSpace(primaryRelatedThreadID(card))
			if tid == "" {
				return "", fmt.Errorf("card %q has no backing thread_id", id)
			}
			return tid, nil
		}
		return "", fmt.Errorf("card not found for subject_ref %q", subjectRef)
	case "board":
		board, err := store.GetBoard(ctx, id)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", fmt.Errorf("board not found for subject_ref %q", subjectRef)
			}
			return "", err
		}
		tid := strings.TrimSpace(anyString(board["thread_id"]))
		if tid == "" {
			return "", fmt.Errorf("board %q has no backing thread_id", id)
		}
		return tid, nil
	default:
		return "", fmt.Errorf(
			"subject_ref prefix %q is not supported for inbox acknowledgment (supported: thread:, topic:, card:, board:)",
			prefix,
		)
	}
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
		Types: []string{"decision_needed", "intervention_needed", "exception_raised", "inbox_item_acknowledged", "receipt_added", "decision_made"},
	})
	if err != nil {
		return nil, err
	}

	ackedAt := latestInboxAcknowledgments(events)
	decidedIDs := decidedInboxItemIDs(events)
	latestActivity := latestThreadActivityFromEvents(events)
	items := make([]derivedInboxItem, 0)

	for _, event := range events {
		eventType, _ := event["type"].(string)
		switch eventType {
		case "decision_needed", "intervention_needed", "exception_raised":
			item, ok := deriveEventBackedInboxItem(event)
			if !ok {
				continue
			}
			if eventType == "exception_raised" && isStaleTopicException(event) {
				threadID, _ := event["thread_id"].(string)
				if activityAt, exists := latestActivity[threadID]; exists && activityAt.After(item.TriggerAt) {
					continue
				}
			}
			if isSuppressedByAck(item, ackedAt) {
				continue
			}
			if _, decided := decidedIDs[item.ID]; decided {
				continue
			}
			items = append(items, item)
		}
	}

	cards, err := opts.primitiveStore.ListCards(ctx, primitives.CardListFilter{})
	if err != nil {
		return nil, err
	}

	for _, card := range cards {
		item, ok := deriveWorkItemRiskInboxItem(card, now, riskHorizon)
		if !ok {
			continue
		}
		if isSuppressedByAck(item, ackedAt) {
			continue
		}
		if _, decided := decidedIDs[item.ID]; decided {
			continue
		}
		items = append(items, item)
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
		if eventType != "decision_made" {
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
			for _, variantID := range inboxItemIDVariants(value) {
				out[variantID] = struct{}{}
			}
		}
	}
	return out
}

func latestInboxAcknowledgments(events []map[string]any) map[string]time.Time {
	ackedAt := make(map[string]time.Time)
	for _, event := range events {
		eventType, _ := event["type"].(string)
		if eventType != "inbox_item_acknowledged" {
			continue
		}

		ts, ok := parseTimestamp(event["ts"])
		if !ok {
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
			for _, candidate := range inboxAckSuppressionIDs(value) {
				if current, exists := ackedAt[candidate]; !exists || ts.After(current) {
					ackedAt[candidate] = ts
				}
			}
		}
	}
	return ackedAt
}

// equivalentInboxCategories lists category strings that share the same inbox id tuple
// (thread, subject, source event) across contract renames, so acks and decision_made
// refs remain correlated after category enum changes.
func equivalentInboxCategories(category string) []string {
	category = strings.TrimSpace(category)
	switch category {
	case "action_needed", "decision_needed":
		return []string{"action_needed", "decision_needed"}
	case "risk_exception", "intervention_needed", "stale_topic", "work_item_risk", "risk_review":
		return []string{"risk_exception", "intervention_needed", "stale_topic", "work_item_risk", "risk_review"}
	case "attention", "document_attention":
		return []string{"attention", "document_attention"}
	default:
		return []string{category}
	}
}

func inboxItemIDVariants(inboxItemID string) []string {
	inboxItemID = strings.TrimSpace(inboxItemID)
	if inboxItemID == "" {
		return nil
	}
	parts := strings.SplitN(inboxItemID, ":", 5)
	if len(parts) != 5 || parts[0] != "inbox" {
		return []string{inboxItemID}
	}
	category := strings.TrimSpace(parts[1])
	threadID := strings.TrimSpace(parts[2])
	subjectID := strings.TrimSpace(parts[3])
	sourceEventID := strings.TrimSpace(parts[4])

	out := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, cat := range equivalentInboxCategories(category) {
		add(makeInboxItemID(cat, threadID, subjectID, sourceEventID))
	}
	return out
}

func inboxAckSuppressionIDs(inboxItemID string) []string {
	return inboxItemIDVariants(inboxItemID)
}

func deriveEventBackedInboxItem(event map[string]any) (derivedInboxItem, bool) {
	eventType, _ := event["type"].(string)
	threadID, _ := event["thread_id"].(string)
	sourceEventID, _ := event["id"].(string)
	triggerAt, ok := parseTimestamp(event["ts"])
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(sourceEventID) == "" || !ok {
		return derivedInboxItem{}, false
	}

	category := ""
	titleFallback := ""
	switch eventType {
	case "decision_needed":
		category = "action_needed"
		titleFallback = "Decision needed"
	case "intervention_needed":
		category = "risk_exception"
		titleFallback = "Intervention needed"
	case "exception_raised":
		category = "risk_exception"
		if isStaleTopicException(event) {
			titleFallback = "Topic appears stale"
		} else {
			titleFallback = "Exception raised"
		}
	default:
		return derivedInboxItem{}, false
	}

	title, _ := event["summary"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = titleFallback
	}

	rawRefs, _ := extractStringSlice(event["refs"])
	subjectRef := pickSubjectRefFromEventRefs(rawRefs, threadID)
	related := eventBackedInboxRelatedRefs(rawRefs, threadID)

	id := makeInboxItemID(category, threadID, "", sourceEventID)
	data := map[string]any{
		"id":              id,
		"category":        category,
		"thread_id":       threadID,
		"source_event_id": sourceEventID,
		"subject_ref":     subjectRef,
		"related_refs":    typedRefStringsToAnyList(related),
		"title":           title,
	}
	if strings.TrimSpace(sourceEventID) != "" {
		data["source_event_ref"] = "event:" + strings.TrimSpace(sourceEventID)
	}

	return derivedInboxItem{
		Data:      data,
		Category:  category,
		ID:        id,
		TriggerAt: triggerAt,
	}, true
}

func deriveWorkItemRiskInboxItem(card map[string]any, now time.Time, riskHorizon time.Duration) (derivedInboxItem, bool) {
	threadID := strings.TrimSpace(primaryRelatedThreadID(card))
	cardID := strings.TrimSpace(anyString(card["id"]))
	if threadID == "" || cardID == "" {
		return derivedInboxItem{}, false
	}

	if !boardCardCountsAsOpenWorkItem(card) {
		return derivedInboxItem{}, false
	}

	riskState, dueAt, hasDueAt := boardCardRiskState(card, now, riskHorizon)
	if riskState == "" {
		return derivedInboxItem{}, false
	}

	triggerAt, ok := parseTimestamp(card["updated_at"])
	if !ok {
		triggerAt = now
	}

	title, _ := card["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Work item risk"
	}

	subjectRef := "card:" + cardID
	related := workItemRiskInboxRelatedRefs(card, threadID)

	id := makeInboxItemID("risk_exception", threadID, cardID, "")
	data := map[string]any{
		"id":           id,
		"category":     "risk_exception",
		"thread_id":    threadID,
		"card_id":      cardID,
		"board_id":     nullableStringValue(anyString(card["board_id"])),
		"subject_ref":  subjectRef,
		"related_refs": typedRefStringsToAnyList(related),
		"title":        title,
		"risk_state":   riskState,
	}
	if hasDueAt {
		data["due_at"] = dueAt.Format(time.RFC3339)
	}
	if pid := pinnedDocumentIDFromCard(card); pid != "" {
		data["pinned_document_id"] = pid
	}

	return derivedInboxItem{
		Data:      data,
		Category:  "risk_exception",
		ID:        id,
		TriggerAt: triggerAt,
		DueAt:     dueAt,
		HasDueAt:  hasDueAt,
	}, true
}

func boardCardCountsAsOpenWorkItem(card map[string]any) bool {
	if res := strings.TrimSpace(anyString(card["resolution"])); res != "" {
		return false
	}
	if strings.TrimSpace(anyString(card["column_key"])) == "done" {
		return false
	}
	switch strings.TrimSpace(anyString(card["status"])) {
	case "done", "cancelled":
		return false
	default:
		return true
	}
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

func isSuppressedByAck(item derivedInboxItem, ackedAt map[string]time.Time) bool {
	acked, ok := ackedAt[item.ID]
	if !ok {
		return false
	}
	return !item.TriggerAt.After(acked)
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
		"action_needed":       0,
		"decision_needed":     0,
		"risk_exception":      1,
		"intervention_needed": 1,
		"stale_topic":         1,
		"work_item_risk":      1,
		"attention":           2,
		"document_attention":  2,
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

		if isWorkItemRiskStyleCategory(left.Category) && isWorkItemRiskStyleCategory(right.Category) {
			if left.HasDueAt && right.HasDueAt && !left.DueAt.Equal(right.DueAt) {
				return left.DueAt.Before(right.DueAt)
			}
		}

		if !left.TriggerAt.Equal(right.TriggerAt) {
			return left.TriggerAt.After(right.TriggerAt)
		}

		return left.ID < right.ID
	})
}
