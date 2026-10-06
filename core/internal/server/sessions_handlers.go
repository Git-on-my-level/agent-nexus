package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// SessionStore is additive so existing primitive-store adapters remain compatible.
type SessionStore interface {
	UpsertSession(context.Context, string, string, primitives.SessionRegistration) (primitives.AgentSession, error)
	GetSession(context.Context, string, string) (primitives.AgentSession, error)
	UpsertWorkParticipant(context.Context, string, string, primitives.WorkParticipantRegistration) (primitives.WorkParticipant, error)
	ListWorkParticipants(context.Context, string, string, int, string) (primitives.WorkParticipantPage, error)
}

// Route classifiers select middleware policy; supported=false does not reject a
// request. Keep path/method validation shared with the handler's explicit guard.
func sessionRouteMethod(path string) string {
	if path == "/sessions" {
		return http.MethodPost
	}
	if strings.HasPrefix(path, "/sessions/") {
		id := strings.TrimPrefix(path, "/sessions/")
		if id != "" && !strings.Contains(id, "/") {
			return http.MethodGet
		}
	}
	return ""
}

func sessionsRouteAccess(r *http.Request) routeAccessRequirement {
	method := sessionRouteMethod(r.URL.Path)
	return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, supported: method != "" && r.Method == method}
}
func sessionStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, 404, "not_found", "session or task participation not found")
	case errors.Is(err, primitives.ErrForbidden):
		writeError(w, 403, "forbidden", "session identity does not match the authenticated agent and host")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, 409, "conflict", "sequence is stale or conflicts with recorded state, or session is closed; reload before retry")
	case errors.Is(err, primitives.ErrInvalidSessionRequest), errors.Is(err, primitives.ErrInvalidCursor):
		writeError(w, 400, "invalid_request", err.Error())
	default:
		writeError(w, 500, "internal_error", "session operation failed")
	}
}
func decodeSessionBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, 400, "invalid_json", "request body must be valid JSON")
		return false
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if !writeRequestTooLargeError(w, err) {
			writeError(w, 400, "invalid_request", "invalid session request; unknown fields and transcript content are not accepted")
		}
		return false
	}
	return ensureJSONBodyEOF(w, decoder) && validateResourceText(w, dst)
}
func requireSessionAgent(w http.ResponseWriter, p *auth.Principal) bool {
	if p.PrincipalKind != string(auth.PrincipalKindAgent) {
		writeError(w, 403, "forbidden", "an authenticated agent principal is required")
		return false
	}
	return true
}
func handleSessions(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	method := sessionRouteMethod(r.URL.Path)
	if method == "" {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only "+method+" is supported")
		return
	}
	p, ok := authenticatePrincipalFromHeader(w, r, opts, true)
	if !ok {
		return
	}
	store, ok := opts.primitiveStore.(SessionStore)
	if !ok {
		writeError(w, 503, "sessions_unavailable", "session store is not configured")
		return
	}
	if r.Method == http.MethodGet {
		session, err := store.GetSession(r.Context(), p.AgentID, strings.TrimPrefix(r.URL.Path, "/sessions/"))
		if err != nil {
			sessionStoreError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"session": session})
		return
	}
	if !requireSessionAgent(w, p) {
		return
	}
	var in primitives.SessionRegistration
	if !decodeSessionBody(w, r, &in) {
		return
	}
	session, err := store.UpsertSession(r.Context(), p.AgentID, p.ActorID, in)
	if err != nil {
		sessionStoreError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"session": session})
}

// Participation grants visibility only to metadata on the requested readable
// task. Check every existing backing scope before exposing even an empty list.
func sessionWorkAccessible(w http.ResponseWriter, r *http.Request, opts handlerOptions, work map[string]any) bool {
	readableThread := func(raw any) bool {
		id := strings.TrimSpace(anyString(raw))
		if id == "" {
			return false
		}
		thread, err := opts.primitiveStore.GetThread(r.Context(), id)
		return err == nil && canAccessPMThread(r, opts, thread)
	}
	allowed := readableThread(work["thread_id"])
	if boardID := strings.TrimSpace(anyString(work["board_id"])); boardID != "" {
		board, err := opts.primitiveStore.GetBoard(r.Context(), boardID)
		allowed = allowed && err == nil && readableThread(board["thread_id"])
	}
	if project := strings.TrimSpace(anyString(work["project_ref"])); project != "" {
		id, ok := resolveResourceIDForInternalUse(r.Context(), opts, "topic", project)
		if !ok {
			allowed = false
		} else {
			topic, err := opts.primitiveStore.GetTopic(r.Context(), id)
			allowed = allowed && err == nil && readableThread(topic["thread_id"])
		}
	}
	if work["trashed_at"] != nil && anyString(work["trashed_at"]) != "" {
		allowed = false
	}
	if !allowed {
		denyPMNotFound(w, "work")
	}
	return allowed
}
func handleWorkParticipants(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardRef string) {
	if cardRef == "" || strings.Contains(cardRef, "/") || r.URL.Path != "/work/"+cardRef+"/participants" {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		return
	}
	p, ok := authenticatePrincipalFromHeader(w, r, opts, true)
	if !ok {
		return
	}
	store, ok := opts.primitiveStore.(SessionStore)
	if !ok {
		writeError(w, 503, "sessions_unavailable", "session store is not configured")
		return
	}
	workStore, ok := opts.primitiveStore.(WorkStore)
	if !ok {
		writeError(w, 503, "work_unavailable", "work store is not configured")
		return
	}
	id, ok := resolveHTTPResourceID(w, r, opts, "card", cardRef, "card")
	if !ok {
		return
	}
	work, err := workStore.GetWork(r.Context(), id)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	if !sessionWorkAccessible(w, r, opts, work) {
		return
	}
	if r.Method == http.MethodGet {
		limit, ok := workLimit(w, r)
		if !ok {
			return
		}
		page, err := store.ListWorkParticipants(r.Context(), p.AgentID, id, limit, r.URL.Query().Get("cursor"))
		if err != nil {
			sessionStoreError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"participants": page.Participants, "next_cursor": page.NextCursor})
		return
	}
	if !requireSessionAgent(w, p) {
		return
	}
	var in primitives.WorkParticipantRegistration
	if !decodeSessionBody(w, r, &in) {
		return
	}
	participant, err := store.UpsertWorkParticipant(r.Context(), p.AgentID, id, in)
	if err != nil {
		sessionStoreError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"participant": participant})
}
