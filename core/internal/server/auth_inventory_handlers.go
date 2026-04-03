package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/auth"
)

const (
	defaultAuthListLimit = 50
	maxAuthListLimit     = 200
)

func enrichAuthPrincipalSummary(item auth.AuthPrincipalSummary, workspaceID string, now time.Time) auth.AuthPrincipalSummary {
	wakeRouting := auth.DescribeWakeRouting(item, workspaceID, now)
	item.WakeRouting = &wakeRouting
	return item
}

func handleListAuthPrincipals(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}

	limit, cursor, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}

	principals, nextCursor, err := opts.authStore.ListPrincipals(r.Context(), auth.AuthPrincipalListFilter{
		Limit:  &limit,
		Cursor: cursor,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list auth principals")
		return
	}
	now := time.Now().UTC()
	for index := range principals {
		principals[index] = enrichAuthPrincipalSummary(principals[index], opts.workspaceID, now)
	}

	activeHumanCount, err := opts.authStore.CountActiveHumanPrincipals(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to count active human principals")
		return
	}

	response := map[string]any{
		"principals":                   principals,
		"active_human_principal_count": activeHumanCount,
	}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func handleListAuthAudit(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}

	limit, cursor, ok := parseAuthListParams(w, r)
	if !ok {
		return
	}

	events, nextCursor, err := opts.authStore.ListAuditEvents(r.Context(), auth.AuthAuditListFilter{
		Limit:  &limit,
		Cursor: cursor,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list auth audit events")
		return
	}

	response := map[string]any{"events": events}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func parseAuthListParams(w http.ResponseWriter, r *http.Request) (int, string, bool) {
	limit := defaultAuthListLimit
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > maxAuthListLimit {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 200")
			return 0, "", false
		}
		limit = parsed
	}
	return limit, strings.TrimSpace(r.URL.Query().Get("cursor")), true
}
