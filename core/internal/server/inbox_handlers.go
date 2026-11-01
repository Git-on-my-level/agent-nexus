// HTTP handlers for reading the derived inbox: list, get one item, item
// payload conversion, notification status enrichment, and the open-ask
// summary. Respond flows live in inbox_ask_handlers.go; ref contract shaping
// in inbox_contract_shape.go; item derivation in inbox_derivation.go.

package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

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
	status := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("status")))
	if status == "" {
		status = "open"
	}
	if status == "completed" {
		handleGetCompletedInbox(w, r, opts, now)
		return
	}
	if status != "open" {
		writeError(w, http.StatusBadRequest, "invalid_request", "status must be open or completed")
		return
	}

	payload, err := loadOpenInbox(w, r, opts, now)
	if errors.Is(err, primitives.ErrInvalidCursor) {
		writeError(w, 400, "invalid_request", "invalid open inbox pagination")
		return
	}
	if err != nil {
		writeReadInternalError(w, r, err, "failed to load inbox projections")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func loadOpenInbox(w http.ResponseWriter, r *http.Request, opts handlerOptions, now time.Time) (map[string]any, error) {
	if canonical, ok := opts.primitiveStore.(*primitives.Store); ok {
		ctx, closeRead, err := canonical.BeginResourceRead(r.Context())
		if err != nil {
			return nil, err
		}
		defer closeRead()
		r = r.WithContext(ctx)
	}
	stage := time.Now()
	var threadIDs []string
	var err error
	if sampler, ok := opts.primitiveStore.(interface {
		InboxThreadIDs(context.Context, int) ([]string, error)
	}); ok {
		threadIDs, err = sampler.InboxThreadIDs(r.Context(), 100)
	} else {
		limit := 100
		var threads []map[string]any
		threads, _, err = opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{Limit: &limit})
		for _, thread := range threads {
			threadIDs = append(threadIDs, anyString(thread["id"]))
		}
	}
	if err != nil {
		return nil, err
	}
	addServerTiming(w, "threads", stage)
	stage = time.Now()
	states, err := loadTopicProjectionStates(r.Context(), opts, threadIDs)
	addServerTiming(w, "topic_projections", stage)
	if err != nil {
		return nil, err
	}
	stage = time.Now()
	items, next, more, err := loadInboxPage(r, opts)
	addServerTiming(w, "inbox", stage)
	if err != nil {
		return nil, err
	}
	stage = time.Now()
	freshness := aggregateTopicProjectionFreshness(states, threadIDs)
	if summary, ok := opts.primitiveStore.(interface {
		InboxFreshnessSummary(context.Context) (int, int, error)
	}); ok {
		count, rank, err := summary.InboxFreshnessSummary(r.Context())
		if err != nil {
			return nil, err
		}
		freshness["thread_count"] = count
		freshness["status"] = []string{"current", "missing", "pending", "error"}[rank]
		freshness["truncated"] = count > len(threadIDs)
	}
	addServerTiming(w, "freshness", stage)
	return map[string]any{"status": "open", "items": items, "generated_at": now.Format(time.RFC3339Nano), "projection_freshness": freshness, "next_cursor": next, "has_more": more}, nil
}

const inboxStreamTickLimit = 200

// One bounded, freshly authorized page per tick. The long-lived stream never
// retains a read snapshot; SQL validates the new tick's snapshot on every read.
func loadInboxStreamPage(r *http.Request, opts handlerOptions, filter primitives.DerivedInboxListFilter) ([]map[string]any, inboxReadPage, error) {
	filter.Limit = inboxStreamTickLimit
	filter.ActiveNotifications = true
	tick := r.WithContext(primitives.WithReadTickSnapshot(r.Context()))
	page := inboxReadPage{}
	items, err := loadVisibleInboxItemsFiltered(tick, opts, true, filter, &page)
	return items, page, err
}

// Overview uses an explicit truncated window of the same authorized state.
func loadOverviewInboxItems(r *http.Request, opts handlerOptions) ([]map[string]any, bool, error) {
	page := &inboxReadPage{}
	items, err := loadVisibleInboxItemsFiltered(r, opts, true, primitives.DerivedInboxListFilter{Limit: 100}, page)
	return items, page.More, err
}

func loadInboxPage(r *http.Request, opts handlerOptions) ([]map[string]any, string, bool, error) {
	limit := 50
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 100 {
		return nil, "", false, primitives.ErrInvalidCursor
	}
	scope := "anonymous"
	if p, ok := cachedAuthenticatedPrincipal(r); ok && p != nil {
		scope = p.AgentID
	}
	var before struct {
		Scope       string
		Category    int
		Trigger, ID string
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 2048 {
		return nil, "", false, primitives.ErrInvalidCursor
	}
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil || json.Unmarshal(raw, &before) != nil || before.Scope != scope || before.ID == "" {
			return nil, "", false, primitives.ErrInvalidCursor
		}
	}
	page := &inboxReadPage{}
	items, err := loadVisibleInboxItemsFiltered(r, opts, true, primitives.DerivedInboxListFilter{Limit: limit, BeforeCategory: before.Category, BeforeTrigger: before.Trigger, BeforeID: before.ID}, page)
	if err != nil {
		return nil, "", false, err
	}
	next := ""
	if page.More {
		before.Scope = scope
		before.Category = primitives.InboxCategoryRank(page.Last.Category)
		before.Trigger = page.Last.TriggerAt
		before.ID = page.Last.ID
		raw, _ := json.Marshal(before)
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return items, next, page.More, nil
}

func loadVisibleInboxItems(r *http.Request, opts handlerOptions, notifications bool) ([]map[string]any, error) {
	return loadVisibleInboxItemsFiltered(r, opts, notifications, primitives.DerivedInboxListFilter{}, nil)
}

type inboxReadPage struct {
	More bool
	Last primitives.DerivedInboxItem
}

func loadVisibleInboxItemsFiltered(r *http.Request, opts handlerOptions, notifications bool, filter primitives.DerivedInboxListFilter, page *inboxReadPage) ([]map[string]any, error) {
	recipient := ""
	if principal, ok := cachedAuthenticatedPrincipal(r); ok && principal != nil {
		recipient = principal.ActorID
	}
	filter.RecipientActorID = recipient
	projected, err := opts.primitiveStore.ListDerivedInboxItems(r.Context(), filter)
	if err != nil {
		return nil, err
	}
	if filter.Limit > 0 && len(projected) > filter.Limit {
		if page != nil {
			page.More = true
		}
		projected = projected[:filter.Limit]
	}
	if page != nil && len(projected) > 0 {
		page.Last = projected[len(projected)-1]
	}
	accessRequests := map[string]primitives.AccessRequest{}
	if store, ok := opts.primitiveStore.(*primitives.Store); notifications && ok {
		var ids []string
		for _, item := range projected {
			if item.SourceEventID != "" {
				ids = append(ids, item.SourceEventID)
			}
		}
		accessRequests, err = store.AccessRequestsForEvents(r.Context(), ids)
		if err != nil {
			return nil, err
		}
	}
	targets := map[string]auth.AuthPrincipalSummary{}
	var targetErr error
	if notifications && opts.authStore != nil {
		actorIDs, agentIDs := []string{}, []string{}
		for _, item := range projected {
			payload := payloadFromDerivedInboxItem(item)
			if canonicalHumanAttentionKind(anyString(payload["kind"])) == "" {
				continue
			}
			if id := strings.TrimSpace(anyString(payload["requester_agent_id"])); id != "" {
				agentIDs = append(agentIDs, id)
			} else if id := strings.TrimSpace(anyString(payload["requester_actor_id"])); id != "" {
				actorIDs = append(actorIDs, id)
			}
		}
		if len(actorIDs)+len(agentIDs) > 0 {
			targets, targetErr = opts.authStore.NotificationTargets(r.Context(), uniqueServerStrings(actorIDs), uniqueServerStrings(agentIDs))
		}
	}
	payloadItems := make([]map[string]any, 0, len(projected))
	for _, item := range projected {
		payload := payloadFromDerivedInboxItem(item)
		if notifications {
			if canonicalHumanAttentionKind(anyString(payload["kind"])) != "" {
				key := "actor:" + strings.TrimSpace(anyString(payload["requester_actor_id"]))
				if id := strings.TrimSpace(anyString(payload["requester_agent_id"])); id != "" {
					key = "agent:" + id
				}
				p, found := targets[key]
				applyNotificationTargetStatus(payload, humanAttentionResponseTarget{ActorID: p.ActorID, AgentID: p.AgentID, Handle: p.Username}, found, targetErr)
			}
			if request, ok := accessRequests["event:"+item.SourceEventID]; ok {
				applyAccessRequestInboxMetadata(payload, request)
			}
		}
		payloadItems = append(payloadItems, payload)
	}
	// Authorization is already applied by the scoped inbox relation. Lifecycle
	// filtering is only needed for ordinary notifications; requests keep their
	// own response/withdrawal lifecycle when linked context is archived.
	pendingLifecycle := map[string]bool{}
	for _, item := range projected {
		if !item.LifecycleReady {
			pendingLifecycle[item.ID] = true
		}
	}
	if notifications && (!filter.ActiveNotifications || len(pendingLifecycle) > 0) {
		if store, ok := opts.primitiveStore.(interface {
			HiddenSubjectRefs(context.Context) (map[string]bool, error)
		}); ok {
			var hidden map[string]bool
			var err error
			if selected, ok := opts.primitiveStore.(interface {
				HiddenSubjectRefsFor(context.Context, []string) (map[string]bool, error)
			}); ok {
				refs := []string{}
				for _, item := range payloadItems {
					if filter.ActiveNotifications && !pendingLifecycle[anyString(item["id"])] {
						continue
					}
					if canonicalHumanAttentionKind(anyString(item["kind"])) == "" {
						related, _ := extractStringSlice(item["related_refs"])
						refs = append(refs, related...)
					}
				}
				hidden, err = selected.HiddenSubjectRefsFor(r.Context(), uniqueServerStrings(refs))
			} else {
				hidden, err = store.HiddenSubjectRefs(r.Context())
			}
			if err != nil {
				return nil, err
			}
			visible := payloadItems[:0]
			for _, item := range payloadItems {
				if filter.ActiveNotifications && !pendingLifecycle[anyString(item["id"])] {
					visible = append(visible, item)
					continue
				}
				if canonicalHumanAttentionKind(anyString(item["kind"])) != "" {
					visible = append(visible, item)
					continue
				}
				refs, _ := extractStringSlice(item["related_refs"])
				hide := false
				for _, ref := range refs {
					hide = hide || hidden[ref]
				}
				if !hide {
					visible = append(visible, item)
				}
			}
			payloadItems = visible
		}
	}
	if err := enrichInboxCardSummaries(r, opts, payloadItems); err != nil {
		return nil, err
	}
	if err := enrichAskPresentations(r, opts, payloadItems); err != nil {
		return nil, err
	}
	return payloadItems, nil
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
	if strings.HasPrefix(inboxItemID, "completed:") {
		handleGetCompletedInboxItem(w, r, opts, inboxItemID, now)
		return
	}

	// Keep the generic projection loader available to mutation admission;
	// canonical HTTP reads also reject already-resolved source requests.
	get := opts.primitiveStore.GetDerivedInboxItem
	if reader, ok := opts.primitiveStore.(interface {
		GetOpenDerivedInboxItem(context.Context, string) (primitives.DerivedInboxItem, error)
	}); ok {
		get = reader.GetOpenDerivedInboxItem
	}
	var item primitives.DerivedInboxItem
	var err error
	for _, candidate := range inboxItemIDVariants(inboxItemID) {
		item, err = get(r.Context(), candidate)
		if err == nil {
			break
		}
		if !errors.Is(err, primitives.ErrNotFound) {
			writeReadInternalError(w, r, err, "failed to load inbox projections")
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Inbox item not found")
		return
	}
	payload := payloadFromDerivedInboxItem(item)
	if !inboxItemAccessible(r, opts, item.ThreadID, payload) {
		denyPMNotFound(w, "inbox item")
		return
	}

	states, err := loadTopicProjectionStates(r.Context(), opts, []string{item.ThreadID})
	if err != nil {
		writeReadInternalError(w, r, err, "failed to load inbox projection status")
		return
	}

	if err := enrichAskPresentations(r, opts, []map[string]any{payload}); err != nil {
		writeReadInternalError(w, r, err, "cannot read ask presentation")
		return
	}
	enrichHumanAttentionNotificationStatus(r.Context(), opts, payload)
	enrichAccessRequestInboxItem(r.Context(), opts, payload)
	if err := enrichInboxCardSummaries(r, opts, []map[string]any{payload}); err != nil {
		workStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item":                 payload,
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
	return applyNotificationTargetStatus(item, target, found, err)
}

func applyNotificationTargetStatus(item map[string]any, target humanAttentionResponseTarget, found bool, err error) map[string]any {
	requesterActorID := strings.TrimSpace(anyString(item["requester_actor_id"]))
	requesterAgentID := strings.TrimSpace(anyString(item["requester_agent_id"]))
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

// Summary reuses permission and lifecycle filtering without scanning all
// workspace threads for freshness or hydrating every subject ref.
func handleGetInboxSummary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, 503, "primitives_unavailable", "primitives store is not configured")
		return
	}
	limit := 5
	if values, ok := r.URL.Query()["limit"]; ok {
		var err error
		if len(values) != 1 {
			writeError(w, 400, "invalid_request", "limit must be an integer from 0 to 50")
			return
		}
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 0 || limit > 50 {
			writeError(w, 400, "invalid_request", "limit must be an integer from 0 to 50")
			return
		}
	}
	projected, count, err := opts.primitiveStore.ReadInbox(r.Context(), primitives.InboxReadOptions{AsksOnly: true, Limit: &limit})
	if err != nil {
		writeReadInternalError(w, r, err, "failed to load inbox projections")
		return
	}
	asks := make([]map[string]any, 0, len(projected))
	for _, item := range projected {
		asks = append(asks, payloadFromDerivedInboxItem(item))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"open_ask_count": count, "asks": asks, "generated_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
