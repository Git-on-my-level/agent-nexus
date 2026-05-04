package server

import (
	"log"
	"net/http"
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
	groups, unreadCount, err := opts.primitiveStore.ListHomeUnread(r.Context(), readerID)
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
	})
}

func handleMarkHomeRead(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
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
