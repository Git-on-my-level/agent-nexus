package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

const (
	agentNotificationStatusUnread    = "unread"
	agentNotificationStatusRead      = "read"
	agentNotificationStatusDismissed = "dismissed"
	notificationStatusUnread         = agentNotificationStatusUnread
	notificationStatusRead           = agentNotificationStatusRead
	notificationStatusDismissed      = agentNotificationStatusDismissed
)

type agentNotificationItem struct {
	WakeupID         string
	Status           string
	TargetHandle     string
	TargetActorID    string
	WorkspaceID      string
	WorkspaceName    string
	ThreadID         string
	ThreadTitle      string
	TriggerEventID   string
	TriggerCreatedAt string
	TriggerText      string
	CreatedAt        string
	ReadAt           string
	DismissedAt      string
	ClaimedAt        string
	CompletedAt      string
	FailedAt         string
	RequestEventID   string
	ReadEventID      string
	DismissEventID   string
	BridgeInstanceID string
	DeliveryStatus   string
	FailureReason    string
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
	handleMutateAgentNotification(w, r, opts, agentNotificationStatusRead)
}

func handleDismissAgentNotification(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	handleMutateAgentNotification(w, r, opts, agentNotificationStatusDismissed)
}

func handleClaimAgentWakeup(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, req, ok := decodeAgentWakeupMutation(w, r, opts)
	if !ok {
		return
	}
	wakeup, err := opts.primitiveStore.ClaimAgentWakeup(r.Context(), req.WakeupID, principal.ActorID, req.BridgeInstanceID)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "agent wakeup is already claimed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to claim agent wakeup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": agentNotificationFromWakeup(wakeup).toMap()})
}

func handleCompleteAgentWakeup(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, req, ok := decodeAgentWakeupMutation(w, r, opts)
	if !ok {
		return
	}
	wakeup, err := opts.primitiveStore.CompleteAgentWakeup(r.Context(), req.WakeupID, principal.ActorID, req.BridgeInstanceID)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "agent wakeup is not claimed by this bridge")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to complete agent wakeup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": agentNotificationFromWakeup(wakeup).toMap()})
}

func handleFailAgentWakeup(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, req, ok := decodeAgentWakeupMutation(w, r, opts)
	if !ok {
		return
	}
	wakeup, err := opts.primitiveStore.FailAgentWakeup(r.Context(), req.WakeupID, principal.ActorID, req.BridgeInstanceID, req.Error)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "agent wakeup is not claimed by this bridge")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to fail agent wakeup")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notification": agentNotificationFromWakeup(wakeup).toMap()})
}

type agentWakeupMutationRequest struct {
	WakeupID         string `json:"wakeup_id"`
	BridgeInstanceID string `json:"bridge_instance_id"`
	Error            string `json:"error"`
}

func decodeAgentWakeupMutation(w http.ResponseWriter, r *http.Request, opts handlerOptions) (*auth.Principal, agentWakeupMutationRequest, bool) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return nil, agentWakeupMutationRequest{}, false
	}
	if !isAgentPrincipal(principal) {
		writeError(w, http.StatusForbidden, "invalid_request", "agent wakeups are only available to authenticated agents")
		return nil, agentWakeupMutationRequest{}, false
	}
	var req agentWakeupMutationRequest
	if !decodeJSONBody(w, r, &req) {
		return nil, agentWakeupMutationRequest{}, false
	}
	req.WakeupID = strings.TrimSpace(req.WakeupID)
	req.BridgeInstanceID = strings.TrimSpace(req.BridgeInstanceID)
	req.Error = strings.TrimSpace(req.Error)
	if req.WakeupID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "wakeup_id is required")
		return nil, agentWakeupMutationRequest{}, false
	}
	return principal, req, true
}

func handleMutateAgentNotification(
	w http.ResponseWriter,
	r *http.Request,
	opts handlerOptions,
	targetStatus string,
) {
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

	if targetStatus == agentNotificationStatusRead && notification.Status == agentNotificationStatusRead {
		writeJSON(w, http.StatusOK, map[string]any{"notification": notification.toMap()})
		return
	}
	if targetStatus == agentNotificationStatusDismissed && notification.Status == agentNotificationStatusDismissed {
		writeJSON(w, http.StatusOK, map[string]any{"notification": notification.toMap()})
		return
	}

	wakeup, err := opts.primitiveStore.MarkAgentWakeupNotification(r.Context(), notificationID, actorID, targetStatus)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "agent notification not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update agent notification")
		return
	}

	updated := agentNotificationFromWakeup(wakeup)
	writeJSON(w, http.StatusCreated, map[string]any{
		"notification": updated.toMap(),
	})
}

func deriveAgentNotifications(ctx context.Context, opts handlerOptions, actorID string) ([]map[string]any, error) {
	wakeups, err := opts.primitiveStore.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{
		TargetActorID: actorID,
		Order:         "asc",
	})
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(wakeups))
	for _, wakeup := range wakeups {
		item := agentNotificationFromWakeup(wakeup)
		if item.TriggerText == "" || item.ThreadTitle == "" {
			hydrateAgentNotificationFromArtifact(ctx, opts, item)
		}
		items = append(items, item.toMap())
	}
	return items, nil
}

func deriveAgentNotificationReceiptsByEvent(ctx context.Context, opts handlerOptions, threadID string) (map[string][]map[string]any, error) {
	threadID = strings.TrimSpace(threadID)
	if opts.primitiveStore == nil || threadID == "" {
		return map[string][]map[string]any{}, nil
	}
	wakeups, err := opts.primitiveStore.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{
		ThreadID: threadID,
		Order:    "asc",
	})
	if err != nil {
		return nil, err
	}
	receipts := make(map[string][]map[string]any)
	for _, wakeup := range wakeups {
		eventID := strings.TrimSpace(wakeup.TriggerEventID)
		if eventID == "" {
			continue
		}
		item := agentNotificationFromWakeup(wakeup).toMap()
		item["notification_status"] = item["status"]
		receipts[eventID] = append(receipts[eventID], item)
	}
	return receipts, nil
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
			WakeupID:         strings.TrimSpace(anyString(item["wakeup_id"])),
			Status:           strings.TrimSpace(anyString(item["status"])),
			TargetHandle:     strings.TrimSpace(anyString(item["target_handle"])),
			TargetActorID:    strings.TrimSpace(anyString(item["target_actor_id"])),
			WorkspaceID:      strings.TrimSpace(anyString(item["workspace_id"])),
			WorkspaceName:    strings.TrimSpace(anyString(item["workspace_name"])),
			ThreadID:         strings.TrimSpace(anyString(item["thread_id"])),
			ThreadTitle:      strings.TrimSpace(anyString(item["thread_title"])),
			TriggerEventID:   strings.TrimSpace(anyString(item["trigger_event_id"])),
			TriggerCreatedAt: strings.TrimSpace(anyString(item["trigger_created_at"])),
			TriggerText:      strings.TrimSpace(anyString(item["trigger_text"])),
			CreatedAt:        strings.TrimSpace(anyString(item["created_at"])),
			ReadAt:           strings.TrimSpace(anyString(item["read_at"])),
			DismissedAt:      strings.TrimSpace(anyString(item["dismissed_at"])),
			ClaimedAt:        strings.TrimSpace(anyString(item["claimed_at"])),
			CompletedAt:      strings.TrimSpace(anyString(item["completed_at"])),
			FailedAt:         strings.TrimSpace(anyString(item["failed_at"])),
			RequestEventID:   strings.TrimSpace(anyString(item["request_event_id"])),
			BridgeInstanceID: strings.TrimSpace(anyString(item["bridge_instance_id"])),
			DeliveryStatus:   strings.TrimSpace(anyString(item["delivery_status"])),
			FailureReason:    strings.TrimSpace(anyString(item["failure_reason"])),
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

func agentNotificationFromWakeup(wakeup primitives.AgentWakeup) *agentNotificationItem {
	return &agentNotificationItem{
		WakeupID:         wakeup.WakeupID,
		Status:           wakeup.NotificationStatus,
		TargetHandle:     wakeup.TargetHandle,
		TargetActorID:    wakeup.TargetActorID,
		WorkspaceID:      wakeup.WorkspaceID,
		WorkspaceName:    wakeup.WorkspaceName,
		ThreadID:         wakeup.ThreadID,
		ThreadTitle:      wakeup.ThreadTitle,
		TriggerEventID:   wakeup.TriggerEventID,
		TriggerCreatedAt: wakeup.TriggerCreatedAt,
		TriggerText:      wakeup.TriggerText,
		CreatedAt:        wakeup.CreatedAt,
		ReadAt:           wakeup.ReadAt,
		DismissedAt:      wakeup.DismissedAt,
		ClaimedAt:        wakeup.ClaimedAt,
		CompletedAt:      wakeup.CompletedAt,
		FailedAt:         wakeup.FailedAt,
		RequestEventID:   wakeup.TriggerEventID,
		BridgeInstanceID: wakeup.BridgeInstanceID,
		DeliveryStatus:   wakeup.Status,
		FailureReason:    wakeup.FailureReason,
	}
}

func (n *agentNotificationItem) toMap() map[string]any {
	item := map[string]any{
		"notification_id":    n.WakeupID,
		"wakeup_id":          n.WakeupID,
		"status":             n.Status,
		"target_handle":      n.TargetHandle,
		"target_actor_id":    n.TargetActorID,
		"workspace_id":       n.WorkspaceID,
		"workspace_name":     n.WorkspaceName,
		"thread_id":          n.ThreadID,
		"thread_title":       n.ThreadTitle,
		"trigger_event_id":   n.TriggerEventID,
		"trigger_created_at": n.TriggerCreatedAt,
		"trigger_text":       n.TriggerText,
		"created_at":         n.CreatedAt,
		"request_event_id":   n.RequestEventID,
		"delivery_status":    n.DeliveryStatus,
	}
	if n.ReadAt != "" {
		item["read_at"] = n.ReadAt
	}
	if n.DismissedAt != "" {
		item["dismissed_at"] = n.DismissedAt
	}
	if n.ClaimedAt != "" {
		item["claimed_at"] = n.ClaimedAt
	}
	if n.CompletedAt != "" {
		item["completed_at"] = n.CompletedAt
	}
	if n.FailedAt != "" {
		item["failed_at"] = n.FailedAt
	}
	if n.ReadEventID != "" {
		item["read_event_id"] = n.ReadEventID
	}
	if n.DismissEventID != "" {
		item["dismiss_event_id"] = n.DismissEventID
	}
	if n.BridgeInstanceID != "" {
		item["bridge_instance_id"] = n.BridgeInstanceID
	}
	if n.FailureReason != "" {
		item["failure_reason"] = n.FailureReason
	}
	return item
}
