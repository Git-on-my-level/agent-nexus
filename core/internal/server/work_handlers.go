package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// WorkStore is an optional capability of the canonical primitive store; keeping
// it separate avoids widening legacy test doubles and independently hosted stores.
type WorkStore interface {
	CreateWork(context.Context, string, string, map[string]any) (map[string]any, error)
	GetWork(context.Context, string) (map[string]any, error)
	ListWork(context.Context, primitives.WorkListFilter) (primitives.WorkPage, error)
	PatchWork(context.Context, string, string, int64, map[string]any) (map[string]any, error)
	SubmitWorkObservation(context.Context, string, string, map[string]any) (map[string]any, error)
	ListWorkObservations(context.Context, string, int, string) ([]map[string]any, string, error)
	RequestWorkRefresh(context.Context, string, string) (map[string]any, error)
}

func workRouteAccess(r *http.Request) routeAccessRequirement {
	path := strings.TrimPrefix(r.URL.Path, "/work")
	valid := false
	switch {
	case path == "":
		valid = r.Method == http.MethodGet || r.Method == http.MethodPost
	case path == "/capabilities":
		valid = r.Method == http.MethodGet
	default:
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		if len(parts) == 1 && parts[0] != "" {
			valid = r.Method == http.MethodGet || r.Method == http.MethodPatch
		}
		if len(parts) == 2 && parts[0] != "" && (parts[1] == "observations" || parts[1] == "refresh") {
			valid = r.Method == http.MethodGet || r.Method == http.MethodPost
		}
	}
	return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: valid}
}
func workStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, 404, "not_found", "work not found")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, 409, "conflict", "work changed or idempotency key conflicts; reload before retry")
	case errors.Is(err, primitives.ErrInvalidWorkRequest), errors.Is(err, primitives.ErrInvalidBoardRequest), errors.Is(err, primitives.ErrInvalidCursor):
		writeError(w, 400, "invalid_request", err.Error())
	default:
		writeError(w, 500, "internal_error", "work operation failed")
	}
}
func publicWork(w map[string]any) map[string]any {
	out := copyStringAnyMap(w)
	for _, key := range []string{"id", "board_id", "thread_id", "board_handle"} {
		delete(out, key)
	}
	return out
}
func workLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, true
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < 1 || n > 200 {
		writeError(w, 400, "invalid_request", "limit must be 1..200")
		return 0, false
	}
	return n, true
}
func handleWork(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(WorkStore)
	if !ok {
		writeError(w, 503, "work_unavailable", "work store is not configured")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/work")
	if path == "/capabilities" {
		writeJSON(w, 200, map[string]any{"capabilities": map[string]any{"version": "1", "canonical_entity": "card", "projects": "topics", "observations": true, "remote_reports": "attributed_claims", "external_status_write": false, "refresh": "durable_queue", "refresh_executor_configured": false, "phases": []string{"backlog", "ready", "in_progress", "blocked", "review", "done", "cancelled", "unknown"}}})
		return
	}
	if path == "" && r.Method == http.MethodGet {
		q := r.URL.Query()
		limit, ok := workLimit(w, r)
		if !ok {
			return
		}
		page, err := store.ListWork(r.Context(), primitives.WorkListFilter{ProjectRef: q.Get("project_ref"), Source: q.Get("source"), Owner: q.Get("owner"), Phase: q.Get("phase"), Freshness: q.Get("freshness"), Query: q.Get("q"), Limit: limit, Cursor: q.Get("cursor")})
		if err != nil {
			workStoreError(w, err)
			return
		}
		items := make([]map[string]any, 0, len(page.Work))
		for _, item := range page.Work {
			items = append(items, publicWork(item))
		}
		writeJSON(w, 200, map[string]any{"work": items, "next_cursor": page.NextCursor})
		return
	}
	var raw map[string]any
	actor := ""
	if r.Method != http.MethodGet {
		if !decodeJSONBody(w, r, &raw) {
			return
		}
		actor, ok = resolveWriteActorID(w, r, opts, anyString(raw["actor_id"]))
		if !ok {
			return
		}
	}
	if path == "" {
		boardID, ok := resolveBoardIDForGlobalCardCreate(w, r, raw, opts)
		if !ok {
			return
		}
		item, err := store.CreateWork(r.Context(), actor, boardID, raw)
		if err != nil {
			workStoreError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"work": publicWork(item)})
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	id, ok := resolveHTTPResourceID(w, r, opts, "card", parts[0], "card")
	if !ok {
		return
	}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			item, err := store.GetWork(r.Context(), id)
			if err != nil {
				workStoreError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"work": publicWork(item)})
			return
		}
		version, ok := raw["if_version"].(float64)
		if !ok || version < 0 || version != float64(int64(version)) {
			writeError(w, 400, "invalid_request", "if_version must be a nonnegative integer")
			return
		}
		patch, ok := raw["patch"].(map[string]any)
		if !ok {
			writeError(w, 400, "invalid_request", "patch is required")
			return
		}
		item, err := store.PatchWork(r.Context(), actor, id, int64(version), patch)
		if err != nil {
			workStoreError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"work": publicWork(item)})
		return
	}
	switch parts[1] {
	case "observations":
		if r.Method == http.MethodGet {
			limit, ok := workLimit(w, r)
			if !ok {
				return
			}
			observations, next, err := store.ListWorkObservations(r.Context(), id, limit, r.URL.Query().Get("cursor"))
			if err != nil {
				workStoreError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"observations": observations, "next_cursor": next})
			return
		}
		observation, ok := raw["observation"].(map[string]any)
		if !ok {
			writeError(w, 400, "invalid_request", "observation is required")
			return
		}
		result, err := store.SubmitWorkObservation(r.Context(), actor, id, observation)
		if err != nil {
			workStoreError(w, err)
			return
		}
		if item, ok := result["work"].(map[string]any); ok {
			result["work"] = publicWork(item)
		}
		writeJSON(w, 200, result)
	case "refresh":
		if r.Method == http.MethodGet {
			item, err := store.GetWork(r.Context(), id)
			if err != nil {
				workStoreError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"refresh": item["refresh"]})
			return
		}
		refresh, err := store.RequestWorkRefresh(r.Context(), actor, id)
		if err != nil {
			workStoreError(w, err)
			return
		}
		writeJSON(w, 202, map[string]any{"refresh": refresh})
	}
}

func pmRouteAccess(r *http.Request) routeAccessRequirement {
	return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: r.Method == http.MethodGet || r.Method == http.MethodPost}
}
