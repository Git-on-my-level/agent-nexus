package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/series"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func WithSeriesStore(store *series.Store) HandlerOption {
	return func(opts *handlerOptions) { opts.seriesStore = store }
}
func seriesRouteAccess(r *http.Request) routeAccessRequirement {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if parts[0] == "series" {
		if len(parts) == 1 || len(parts) == 2 || len(parts) == 3 && parts[2] == "query" {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
		}
		if len(parts) == 3 && parts[1] != "" && parts[2] == "points" {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
		}
	} else if parts[0] == "adapters" {
		if len(parts) == 1 {
			if r.Method == http.MethodGet {
				return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
			}
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
		}
		if len(parts) == 2 && parts[1] != "" {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodDelete)(r)
		}
		if len(parts) == 3 && parts[1] != "" && (parts[2] == "revoke" || parts[2] == "token") {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
		}
	}
	return routeAccessRequirement{}
}

func seriesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrSeriesForbidden):
		writeError(w, 403, "forbidden", "an active, explicitly granted series permission is required")
	case errors.Is(err, series.ErrInvalid):
		writeError(w, 400, "invalid_request", err.Error())
	case errors.Is(err, series.ErrCapacity):
		writeError(w, 429, "series_capacity", err.Error())
	case errors.Is(err, series.ErrNotFound):
		writeError(w, 404, "not_found", "series or adapter not found")
	case errors.Is(err, series.ErrConflict):
		writeError(w, 409, "conflict", "adapter or series name is already declared")
	default:
		writeError(w, 500, "internal_error", "series operation failed")
	}
}
func handleSeriesRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.seriesStore == nil || opts.authStore == nil {
		writeError(w, 503, "unavailable", "series store unavailable")
		return
	}
	actor, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	w.Header().Set("Cache-Control", "no-store")
	if parts[0] == "adapters" {
		// The owning agent may only exchange its own active declaration for a scoped
		// token. Declaration and lifecycle mutations require explicit administration.
		if len(parts) == 3 && parts[2] == "token" && r.Method == http.MethodPost {
			bundle, err := opts.authStore.IssueSeriesToken(r.Context(), parts[1], *actor)
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"tokens": bundle})
			return
		}
		if !isAuthAdminPrincipal(actor) || actor.SeriesAdapter != "" {
			seriesError(w, auth.ErrSeriesForbidden)
			return
		}
		if len(parts) == 1 && r.Method == http.MethodGet {
			items, err := opts.seriesStore.Adapters(r.Context())
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"adapters": items})
			return
		}
		if len(parts) == 1 && r.Method == http.MethodPost {
			var d series.Declaration
			if !decodeSeriesJSON(w, r, &d) {
				return
			}
			out, err := opts.seriesStore.Declare(r.Context(), d, *actor)
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"adapter": out})
			return
		}
		if len(parts) == 2 && r.Method == http.MethodDelete || len(parts) == 3 && parts[2] == "revoke" && r.Method == http.MethodPost {
			err := opts.seriesStore.Remove(r.Context(), parts[1], r.Method == http.MethodDelete, *actor)
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"adapter": parts[1], "revoked": true, "deleted": r.Method == http.MethodDelete})
			return
		}
	} else if parts[0] == "series" {
		if len(parts) == 1 && r.Method == http.MethodGet {
			items, err := opts.seriesStore.List(r.Context())
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"series": items})
			return
		}
		if len(parts) == 3 && parts[2] == "points" && r.Method == http.MethodPost {
			var p series.Point
			if !decodeSeriesJSON(w, r, &p) {
				return
			}
			if err := opts.seriesStore.Push(r.Context(), parts[1], p, *actor, time.Now().UTC()); err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"series": parts[1], "accepted": true})
			return
		}
		if r.Method == http.MethodGet && (len(parts) == 2 || len(parts) == 3 && parts[2] == "query") {
			window, step := 24*time.Hour, time.Hour
			agg := "last"
			var err error
			if raw := r.URL.Query().Get("range"); raw != "" {
				window, err = series.Duration(raw)
				if err != nil {
					seriesError(w, series.ErrInvalid)
					return
				}
			}
			step = window / series.MaxBuckets
			if step < time.Second {
				step = time.Second
			}
			if window > series.Retention {
				step = 24 * time.Hour * ((window/24/time.Hour + series.MaxBuckets - 1) / series.MaxBuckets)
			}
			if raw := r.URL.Query().Get("step"); raw != "" {
				step, err = series.Duration(raw)
				if err != nil {
					seriesError(w, series.ErrInvalid)
					return
				}
			}
			if raw := r.URL.Query().Get("agg"); raw != "" {
				agg = raw
			}
			labels := map[string]string{}
			for _, raw := range r.URL.Query()["label"] {
				key, value, ok := strings.Cut(raw, "=")
				if !ok {
					seriesError(w, series.ErrInvalid)
					return
				}
				if _, exists := labels[key]; exists {
					seriesError(w, series.ErrInvalid)
					return
				}
				labels[key] = value
			}
			out, err := opts.seriesStore.Query(r.Context(), parts[1], labels, window, step, agg, time.Now().UTC())
			if err != nil {
				seriesError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"series": out})
			return
		}
	}
	writeError(w, 404, "not_found", "endpoint not found")
}

func decodeSeriesJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		if writeRequestTooLargeError(w, err) {
			return false
		}
		writeError(w, 400, "invalid_request", "body must match the declared series schema")
		return false
	}
	return ensureJSONBodyEOF(w, decoder)
}
