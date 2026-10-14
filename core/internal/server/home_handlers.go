package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

func handleGetHomeUnread(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	readerID, ok := resolveHomeReaderID(w, r, opts, "")
	if !ok {
		return
	}
	limit := 200
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
	}
	if err != nil || limit < 1 || limit > 200 {
		writeError(w, 400, "invalid_request", "limit must be between 1 and 200")
		return
	}
	var groups []primitives.HomeUnreadGroup
	var unreadCount int
	var next string
	if reader, ok := opts.primitiveStore.(interface {
		ListHomeUnreadPage(context.Context, string, string, int) (primitives.HomeUnreadPage, error)
	}); ok {
		var page primitives.HomeUnreadPage
		page, err = reader.ListHomeUnreadPage(r.Context(), readerID, r.URL.Query().Get("cursor"), limit)
		groups, unreadCount, next = page.Groups, page.UnreadCount, page.NextCursor
	} else {
		groups, unreadCount, err = opts.primitiveStore.ListHomeUnread(r.Context(), readerID)
	}
	if errors.Is(err, primitives.ErrInvalidCursor) {
		writeError(w, 400, "invalid_request", "invalid cursor")
		return
	}
	if err != nil {
		log.Printf("anx-core: home unread failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load home unread activity")
		return
	}
	payloadGroups := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		payloadGroups = append(payloadGroups, map[string]any{
			"group_ref":    group.GroupRef,
			"group_type":   group.GroupType,
			"display_name": group.DisplayName,
			"priority":     group.Priority,
			"unread_count": group.UnreadCount,
			"newest_event": group.NewestEvent,
			"events":       group.Events,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"groups":       payloadGroups,
		"unread_count": unreadCount,
		"group_count":  len(payloadGroups),
		"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"next_cursor":  next, "has_more": next != "",
	})
}

func handleMarkHomeRead(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		RequestKey                string   `json:"request_key,omitempty"`
		ReaderID                  string   `json:"reader_id"`
		ActorID                   string   `json:"actor_id"`
		GroupRef                  string   `json:"group_ref"`
		GroupRefs                 []string `json:"group_refs"`
		ExpectedNewestEventCursor struct {
			TS string `json:"ts"`
			ID string `json:"id"`
		} `json:"expected_newest_event_cursor"`
		GroupCursors map[string]struct {
			TS string `json:"ts"`
			ID string `json:"id"`
		} `json:"group_cursors"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.RequestKey != "" && (strings.TrimSpace(req.RequestKey) == "" || len(req.RequestKey) > 256 || strings.ContainsRune(req.RequestKey, 0)) {
		writeError(w, 400, "invalid_request", "invalid saved read key")
		return
	}
	readerID, ok := resolveHomeReaderID(w, r, opts, firstNonEmptyString(req.ReaderID, req.ActorID))
	if !ok {
		return
	}
	groupRefs := append([]string{}, req.GroupRefs...)
	if strings.TrimSpace(req.GroupRef) != "" {
		groupRefs = append(groupRefs, strings.TrimSpace(req.GroupRef))
	}
	if len(groupRefs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "group_ref or group_refs is required")
		return
	}
	expected := map[string]primitives.EventCursor{}
	if strings.TrimSpace(req.GroupRef) != "" &&
		strings.TrimSpace(req.ExpectedNewestEventCursor.TS) != "" &&
		strings.TrimSpace(req.ExpectedNewestEventCursor.ID) != "" {
		expected[strings.TrimSpace(req.GroupRef)] = primitives.EventCursor{
			TS: strings.TrimSpace(req.ExpectedNewestEventCursor.TS),
			ID: strings.TrimSpace(req.ExpectedNewestEventCursor.ID),
		}
	}
	for groupRef, cursor := range req.GroupCursors {
		groupRef = strings.TrimSpace(groupRef)
		if groupRef == "" || strings.TrimSpace(cursor.TS) == "" || strings.TrimSpace(cursor.ID) == "" {
			continue
		}
		expected[groupRef] = primitives.EventCursor{TS: strings.TrimSpace(cursor.TS), ID: strings.TrimSpace(cursor.ID)}
	}

	if req.RequestKey != "" {
		frozen, ok := opts.primitiveStore.(interface {
			CheckHomeReadCursors(context.Context, []string, map[string]primitives.EventCursor) error
			MarkHomeReadFrozen(context.Context, string, []string, map[string]primitives.EventCursor) error
		})
		if !ok {
			writeError(w, 503, "unavailable", "frozen read cursors are unavailable")
			return
		}
		// Current visibility is checked even on replay. The receipt contains no
		// projection counts, and later arrivals are never folded into this request.
		if err := frozen.CheckHomeReadCursors(r.Context(), groupRefs, expected); err != nil {
			writeFrozenHomeReadError(w, r, err)
			return
		}
		status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "home.read", readerID, req.RequestKey, req, 200, func(ctx context.Context) (map[string]any, error) {
			if err := frozen.MarkHomeReadFrozen(ctx, readerID, groupRefs, expected); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
		if err != nil {
			writeFrozenHomeReadError(w, r, err)
			return
		}
		writeJSON(w, status, response)
		return
	}
	if err := opts.primitiveStore.MarkHomeReadAt(r.Context(), readerID, groupRefs, expected); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to mark home activity read")
		return
	}
	groups, unreadCount, err := opts.primitiveStore.ListHomeUnread(r.Context(), readerID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"unread_count": unreadCount,
		"group_count":  len(groups),
	})
}

func resolveHomeReaderID(w http.ResponseWriter, r *http.Request, opts handlerOptions, requested string) (string, bool) {
	principal, ok := resolveOptionalPrincipal(w, r, opts)
	if !ok {
		return "", false
	}
	if principal != nil {
		return firstNonEmptyString(strings.TrimSpace(principal.AgentID), strings.TrimSpace(principal.ActorID)), true
	}
	readerID := strings.TrimSpace(requested)
	if readerID == "" {
		readerID = strings.TrimSpace(r.URL.Query().Get("reader_id"))
	}
	if readerID == "" {
		readerID = strings.TrimSpace(r.URL.Query().Get("actor_id"))
	}
	if readerID == "" {
		readerID = strings.TrimSpace(r.Header.Get("X-ANX-Reader-ID"))
	}
	if readerID == "" {
		writeError(w, http.StatusUnauthorized, "auth_required", "authorization header is required")
		return "", false
	}
	return readerID, true
}

func writeFrozenHomeReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case writeStoreContentionError(w, r, err):
	case errors.Is(err, primitives.ErrInvalidBoardRequest):
		writeError(w, 400, "invalid_request", "a frozen visible event cursor is required for each of at most 100 groups")
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, 404, "not_found", "activity is no longer available")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, 409, "conflict", "saved read request has changed")
	default:
		writeError(w, 500, "internal_error", "failed to mark saved activity read")
	}
}
