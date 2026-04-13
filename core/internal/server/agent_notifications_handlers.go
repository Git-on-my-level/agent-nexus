package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/router"
)

const (
	agentWakeRequestEvent            = router.WakeRequestEvent
	agentNotificationReadEvent       = "agent_notification_read"
	agentNotificationDismissedEvent  = "agent_notification_dismissed"
	agentNotificationStatusUnread    = "unread"
	agentNotificationStatusRead      = "read"
	agentNotificationStatusDismissed = "dismissed"
	notificationStatusUnread         = agentNotificationStatusUnread
	notificationStatusRead           = agentNotificationStatusRead
	notificationStatusDismissed      = agentNotificationStatusDismissed
)

type agentNotificationItem struct {
	WakeupID       string
	Status         string
	TargetHandle   string
	TargetActorID  string
	ThreadID       string
	ThreadTitle    string
	TriggerEventID string
	TriggerText    string
	CreatedAt      string
	ReadAt         string
	DismissedAt    string
	RequestEventID string
	ReadEventID    string
	DismissEventID string
}

func handleListAgentNotifications(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "invalid_request", "agent notifications are only available to authenticated agents")
		return
	}

	items, err := deriveAgentNotifications(r.Context(), opts, principal.ActorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to derive agent notifications")
		return
	}

	statusFilter, ok := parseAgentNotificationStatusFilter(w, r)
	if !ok {
		return
	}
	order := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("order")))
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		writeError(w, http.StatusBadRequest, "invalid_request", "order must be asc or desc")
		return
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		status := strings.TrimSpace(anyString(item["status"]))
		if len(statusFilter) > 0 {
			if _, exists := statusFilter[status]; !exists {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		left := strings.TrimSpace(anyString(filtered[i]["created_at"]))
		right := strings.TrimSpace(anyString(filtered[j]["created_at"]))
		if left == right {
			if order == "asc" {
				return strings.TrimSpace(anyString(filtered[i]["wakeup_id"])) < strings.TrimSpace(anyString(filtered[j]["wakeup_id"]))
			}
			return strings.TrimSpace(anyString(filtered[i]["wakeup_id"])) > strings.TrimSpace(anyString(filtered[j]["wakeup_id"]))
		}
		if order == "asc" {
			return left < right
		}
		return left > right
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"items":        filtered,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func handleReadAgentNotification(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	handleMutateAgentNotification(w, r, opts, agentNotificationReadEvent, agentNotificationStatusRead, "agent notification marked read")
}

func handleDismissAgentNotification(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	handleMutateAgentNotification(w, r, opts, agentNotificationDismissedEvent, agentNotificationStatusDismissed, "agent notification dismissed")
}

func handleMutateAgentNotification(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	eventType string,
	targetStatus string,
	summary string,
) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "invalid_request", "agent notifications are only available to authenticated agents")
		return
	}

	var req struct {
		ActorID        string `json:"actor_id"`
		NotificationID string `json:"notification_id"`
		WakeupID       string `json:"wakeup_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	notificationID := strings.TrimSpace(req.NotificationID)
	if notificationID == "" {
		notificationID = strings.TrimSpace(req.WakeupID)
	}
	if notificationID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "notification_id is required")
		return
	}

	notification, err := loadAgentNotificationByWakeupID(r.Context(), opts, actorID, notificationID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "agent notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load agent notification")
		return
	}
	if notification.TargetActorID != actorID {
		writeError(w, http.StatusForbidden, "invalid_request", "only the target agent can update this notification")
		return
	}
	if notification.Status == agentNotificationStatusDismissed && targetStatus == agentNotificationStatusRead {
		writeError(w, http.StatusConflict, "conflict", "dismissed notifications cannot be marked read")
		return
	}

	if targetStatus == agentNotificationStatusRead && notification.Status == agentNotificationStatusRead && notification.ReadEventID != "" {
		existing, err := opts.primitiveStore.GetEvent(r.Context(), notification.ReadEventID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load existing read event")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"event":        existing,
			"notification": notification.toMap(),
		})
		return
	}
	if targetStatus == agentNotificationStatusDismissed && notification.DismissEventID != "" {
		existing, err := opts.primitiveStore.GetEvent(r.Context(), notification.DismissEventID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load existing dismiss event")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"event":        existing,
			"notification": notification.toMap(),
		})
		return
	}

	requestKey := agentNotificationRequestKey(eventType, actorID, notificationID)
	event := map[string]any{
		"id":        deriveRequestScopedID(eventType, actorID, requestKey, "ev"),
		"type":      eventType,
		"thread_id": notification.ThreadID,
		"refs":      notification.eventRefs(),
		"summary":   summary,
		"payload": map[string]any{
			"wakeup_id":       notification.WakeupID,
			"target_handle":   notification.TargetHandle,
			"target_actor_id": notification.TargetActorID,
		},
		"provenance": actorStatementProvenance(),
	}
	if err := validateEventReferenceConventions(opts.contract, event, notification.eventRefs()); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	stored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, event)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) {
			existing, loadErr := opts.primitiveStore.GetEvent(r.Context(), anyString(event["id"]))
			if loadErr != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to load existing notification event")
				return
			}
			updated, refreshErr := loadAgentNotificationByWakeupID(r.Context(), opts, actorID, notificationID)
			if refreshErr != nil {
				writeError(w, http.StatusInternalServerError, "internal_error", "failed to refresh agent notification")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"event":        existing,
				"notification": updated.toMap(),
			})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update agent notification")
		return
	}

	updated, err := loadAgentNotificationByWakeupID(r.Context(), opts, actorID, notificationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to refresh agent notification")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"event":        stored,
		"notification": updated.toMap(),
	})
}

func deriveAgentNotifications(ctx context.Context, opts handlerOptions, actorID string) ([]map[string]any, error) {
	events, err := opts.primitiveStore.ListEvents(ctx, primitives.EventListFilter{
		Types: []string{
			router.WakeRequestEvent,
			agentNotificationReadEvent,
			agentNotificationDismissedEvent,
		},
	})
	if err != nil {
		return nil, err
	}
	sortEventsAscending(events)

	itemsByWakeup := map[string]*agentNotificationItem{}
	orderedWakeups := make([]string, 0)
	for _, event := range events {
		payload, _ := event["payload"].(map[string]any)
		wakeupID := strings.TrimSpace(anyString(payload["wakeup_id"]))
		if wakeupID == "" {
			continue
		}
		eventActorID := strings.TrimSpace(anyString(event["actor_id"]))
		targetActorID := strings.TrimSpace(anyString(payload["target_actor_id"]))
		switch strings.TrimSpace(anyString(event["type"])) {
		case router.WakeRequestEvent:
			if targetActorID != actorID {
				continue
			}
			if _, exists := itemsByWakeup[wakeupID]; exists {
				continue
			}
			item := &agentNotificationItem{
				WakeupID:       wakeupID,
				Status:         agentNotificationStatusUnread,
				TargetHandle:   strings.TrimSpace(anyString(payload["target_handle"])),
				TargetActorID:  targetActorID,
				ThreadID:       strings.TrimSpace(anyString(payload["thread_id"])),
				TriggerEventID: strings.TrimSpace(anyString(payload["trigger_event_id"])),
				CreatedAt:      strings.TrimSpace(anyString(event["ts"])),
				RequestEventID: strings.TrimSpace(anyString(event["id"])),
			}
			if item.TriggerText == "" || item.ThreadTitle == "" {
				hydrateAgentNotificationFromArtifact(ctx, opts, item)
			}
			itemsByWakeup[wakeupID] = item
			orderedWakeups = append(orderedWakeups, wakeupID)
		case agentNotificationReadEvent:
			if targetActorID != actorID {
				continue
			}
			if eventActorID == "" || eventActorID != targetActorID {
				continue
			}
			item, exists := itemsByWakeup[wakeupID]
			if !exists {
				continue
			}
			if item.Status == agentNotificationStatusDismissed {
				continue
			}
			item.Status = agentNotificationStatusRead
			item.ReadAt = strings.TrimSpace(anyString(event["ts"]))
			item.ReadEventID = strings.TrimSpace(anyString(event["id"]))
		case agentNotificationDismissedEvent:
			if targetActorID != actorID {
				continue
			}
			if eventActorID == "" || eventActorID != targetActorID {
				continue
			}
			item, exists := itemsByWakeup[wakeupID]
			if !exists {
				continue
			}
			item.Status = agentNotificationStatusDismissed
			item.DismissedAt = strings.TrimSpace(anyString(event["ts"]))
			item.DismissEventID = strings.TrimSpace(anyString(event["id"]))
		}
	}

	items := make([]map[string]any, 0, len(orderedWakeups))
	for _, wakeupID := range orderedWakeups {
		item := itemsByWakeup[wakeupID]
		if item == nil {
			continue
		}
		items = append(items, item.toMap())
	}
	return items, nil
}

func loadAgentNotificationByWakeupID(ctx context.Context, opts handlerOptions, actorID string, wakeupID string) (*agentNotificationItem, error) {
	items, err := deriveAgentNotifications(ctx, opts, actorID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.TrimSpace(anyString(item["wakeup_id"])) != wakeupID {
			continue
		}
		return &agentNotificationItem{
			WakeupID:       strings.TrimSpace(anyString(item["wakeup_id"])),
			Status:         strings.TrimSpace(anyString(item["status"])),
			TargetHandle:   strings.TrimSpace(anyString(item["target_handle"])),
			TargetActorID:  strings.TrimSpace(anyString(item["target_actor_id"])),
			ThreadID:       strings.TrimSpace(anyString(item["thread_id"])),
			ThreadTitle:    strings.TrimSpace(anyString(item["thread_title"])),
			TriggerEventID: strings.TrimSpace(anyString(item["trigger_event_id"])),
			TriggerText:    strings.TrimSpace(anyString(item["trigger_text"])),
			CreatedAt:      strings.TrimSpace(anyString(item["created_at"])),
			ReadAt:         strings.TrimSpace(anyString(item["read_at"])),
			DismissedAt:    strings.TrimSpace(anyString(item["dismissed_at"])),
			RequestEventID: strings.TrimSpace(anyString(item["request_event_id"])),
			ReadEventID:    strings.TrimSpace(anyString(item["read_event_id"])),
			DismissEventID: strings.TrimSpace(anyString(item["dismiss_event_id"])),
		}, nil
	}
	return nil, primitives.ErrNotFound
}

func hydrateAgentNotificationFromArtifact(ctx context.Context, opts handlerOptions, item *agentNotificationItem) {
	if item == nil || item.WakeupID == "" {
		return
	}
	contentBytes, contentType, err := opts.primitiveStore.GetArtifactContent(ctx, item.WakeupID)
	if err != nil || !strings.Contains(contentType, "json") || len(contentBytes) == 0 {
		return
	}

	var content map[string]any
	if err := json.Unmarshal(contentBytes, &content); err != nil {
		return
	}
	thread, _ := content["thread"].(map[string]any)
	trigger, _ := content["trigger"].(map[string]any)
	if item.ThreadTitle == "" {
		item.ThreadTitle = strings.TrimSpace(anyString(thread["title"]))
	}
	if item.TriggerText == "" {
		item.TriggerText = strings.TrimSpace(anyString(trigger["text"]))
	}
}

func parseAgentNotificationStatusFilter(w http.ResponseWriter, r *http.Request) (map[string]struct{}, bool) {
	values := make([]string, 0)
	for _, raw := range r.URL.Query()["status"] {
		values = append(values, splitCommaSeparated(raw)...)
	}
	if len(values) == 0 {
		return nil, true
	}

	out := make(map[string]struct{}, len(values))
	for _, raw := range values {
		status := strings.ToLower(strings.TrimSpace(raw))
		switch status {
		case agentNotificationStatusUnread, agentNotificationStatusRead, agentNotificationStatusDismissed:
			out[status] = struct{}{}
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", "status must be one of unread, read, dismissed")
			return nil, false
		}
	}
	return out, true
}

func isAgentPrincipal(principal *auth.Principal) bool {
	if principal == nil {
		return false
	}
	return strings.TrimSpace(principal.PrincipalKind) == string(auth.PrincipalKindAgent)
}

func isHumanPrincipal(principal *auth.Principal) bool {
	if principal == nil {
		return false
	}
	return strings.TrimSpace(principal.PrincipalKind) == string(auth.PrincipalKindHuman)
}

func agentNotificationRequestKey(action string, actorID string, wakeupID string) string {
	sum := sha256.Sum256([]byte(action + "\n" + actorID + "\n" + wakeupID))
	return hex.EncodeToString(sum[:])[:24]
}

func (n *agentNotificationItem) eventRefs() []string {
	refs := []string{}
	if n.ThreadID != "" {
		refs = append(refs, "thread:"+n.ThreadID)
	}
	if n.RequestEventID != "" {
		refs = append(refs, "event:"+n.RequestEventID)
	}
	if n.WakeupID != "" {
		refs = append(refs, "artifact:"+n.WakeupID)
	}
	return refs
}

func (n *agentNotificationItem) toMap() map[string]any {
	item := map[string]any{
		"notification_id":  n.WakeupID,
		"wakeup_id":        n.WakeupID,
		"status":           n.Status,
		"target_handle":    n.TargetHandle,
		"target_actor_id":  n.TargetActorID,
		"thread_id":        n.ThreadID,
		"thread_title":     n.ThreadTitle,
		"trigger_event_id": n.TriggerEventID,
		"trigger_text":     n.TriggerText,
		"created_at":       n.CreatedAt,
		"request_event_id": n.RequestEventID,
	}
	if n.ReadAt != "" {
		item["read_at"] = n.ReadAt
	}
	if n.DismissedAt != "" {
		item["dismissed_at"] = n.DismissedAt
	}
	if n.ReadEventID != "" {
		item["read_event_id"] = n.ReadEventID
	}
	if n.DismissEventID != "" {
		item["dismiss_event_id"] = n.DismissEventID
	}
	return item
}
