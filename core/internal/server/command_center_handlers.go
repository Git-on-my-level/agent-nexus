package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/primitives"
)

var runHeaderPattern = regexp.MustCompile(`^agentctl/[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func commandCenterRunRouteAccess(r *http.Request) routeAccessRequirement {
	path := r.URL.Path
	read := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
	write := routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
	if path == "/runs" {
		if r.Method == http.MethodGet {
			return read
		}
		if r.Method == http.MethodPost {
			return write
		}
	}
	if strings.HasPrefix(path, "/runs/") && strings.TrimPrefix(path, "/runs/") != "" && !strings.Contains(strings.TrimPrefix(path, "/runs/"), "/") && r.Method == http.MethodGet {
		return read
	}
	if path == "/agents" && r.Method == http.MethodGet {
		return read
	}
	if path == "/agents/me/presence" && r.Method == http.MethodPatch {
		return write
	}
	if strings.HasPrefix(path, "/agents/") && strings.TrimPrefix(path, "/agents/") != "" && !strings.Contains(strings.TrimPrefix(path, "/agents/"), "/") && r.Method == http.MethodGet {
		return read
	}
	return routeAccessRequirement{}
}
func agentIdentity(w http.ResponseWriter, r *http.Request, opts handlerOptions) (commandcenter.Identity, bool) {
	p, ok := authenticatePrincipalFromHeader(w, r, opts, true)
	if !ok {
		return commandcenter.Identity{}, false
	}
	if opts.runStore == nil {
		writeError(w, http.StatusServiceUnavailable, "runs_unavailable", "runs store is not configured")
		return commandcenter.Identity{}, false
	}
	identity, e := opts.runStore.Identity(r.Context(), p.AgentID)
	if e != nil || identity.ActorID != p.ActorID || identity.HostID == "" || (identity.Kind != "derived" && identity.Kind != "adopted") {
		writeError(w, http.StatusForbidden, "forbidden", "derived agent identity is required")
		return commandcenter.Identity{}, false
	}
	return identity, true
}
func attachRunAttribution(w http.ResponseWriter, r *http.Request, opts handlerOptions) bool {
	raw := r.Header.Get("X-ANX-Run-Id")
	if raw == "" {
		return true
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if !runHeaderPattern.MatchString(raw) {
		writeError(w, http.StatusBadRequest, "run_attribution_invalid", "run header is malformed")
		return false
	}
	principal, ok := authenticatePrincipalFromHeader(w, r, opts, true)
	if !ok {
		return false
	}
	if opts.runStore == nil {
		writeError(w, 503, "runs_unavailable", "runs store is not configured")
		return false
	}
	identity, err := opts.runStore.Identity(r.Context(), principal.AgentID)
	if err != nil || identity.ActorID != principal.ActorID || identity.HostID == "" || (identity.Kind != "derived" && identity.Kind != "adopted") {
		writeError(w, 400, "run_attribution_invalid", "run header requires a derived agent")
		return false
	}
	run, e := opts.runStore.Provisional(r.Context(), identity, strings.TrimPrefix(raw, "agentctl/"))
	if e != nil {
		if errors.Is(e, commandcenter.ErrIdentityConflict) {
			writeError(w, http.StatusBadRequest, "run_attribution_invalid", "run belongs to another agent")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve run attribution")
		}
		return false
	}
	ctx := commandcenter.WithAttribution(r.Context(), commandcenter.Attribution{RunID: run.ID, HostID: identity.HostID, AgentID: identity.AgentID, Adapter: identity.Name})
	*r = *r.WithContext(ctx)
	return true
}
func handleRuns(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.runStore == nil {
		writeError(w, http.StatusServiceUnavailable, "runs_unavailable", "runs store is not configured")
		return
	}
	if r.URL.Path == "/runs" {
		if r.Method == http.MethodGet {
			q := r.URL.Query()
			f := commandcenter.Filter{CardRef: q.Get("card_ref"), AgentID: q.Get("agent_id"), HostID: q.Get("host_id"), State: q.Get("state"), Cursor: q.Get("cursor"), Limit: 50}
			if q.Has("limit") {
				n, e := strconv.Atoi(q.Get("limit"))
				if e != nil || n < 1 || n > 200 {
					writeError(w, 400, "invalid_request", "limit must be 1..200")
					return
				}
				f.Limit = n
			}
			if q.Has("active") {
				b, e := strconv.ParseBool(q.Get("active"))
				if e != nil {
					writeError(w, 400, "invalid_request", "active must be boolean")
					return
				}
				f.Active = &b
			}
			if f.State != "" && !validRunState(f.State) {
				writeError(w, 400, "invalid_request", "state is invalid")
				return
			}
			runs, next, e := opts.runStore.ListRuns(r.Context(), f)
			if e != nil {
				writeError(w, 400, "invalid_request", "invalid run filter")
				return
			}
			body := map[string]any{"runs": runs}
			if next != "" {
				body["next_cursor"] = next
			}
			writeJSON(w, 200, body)
			return
		}
		if r.Method == http.MethodPost {
			identity, ok := agentIdentity(w, r, opts)
			if !ok {
				return
			}
			var raw map[string]json.RawMessage
			if !decodeJSONBody(w, r, &raw) {
				return
			}
			allowed := map[string]bool{"launcher": true, "external_id": true, "host_id": true, "agent_id": true, "adapter": true, "model": true, "state": true, "liveness": true, "result_collected": true, "labels": true, "card_ref": true, "repository": true, "branch": true, "started_at": true, "ended_at": true, "last_observed_at": true}
			for field := range raw {
				if !allowed[field] {
					writeError(w, 400, "invalid_request", "unknown run field: "+field)
					return
				}
			}
			for _, field := range []string{"launcher", "external_id", "host_id", "agent_id", "adapter", "state", "liveness", "result_collected", "labels", "last_observed_at"} {
				if _, exists := raw[field]; !exists {
					writeError(w, 400, "invalid_request", "missing run field: "+field)
					return
				}
			}
			encoded, _ := json.Marshal(raw)
			var in commandcenter.Run
			if json.Unmarshal(encoded, &in) != nil {
				writeError(w, 400, "invalid_request", "invalid run field type")
				return
			}
			if in.AgentID != identity.AgentID || in.HostID != identity.HostID {
				writeError(w, 403, "forbidden", "run agent and host must match caller")
				return
			}
			run, created, replayed, e := opts.runStore.UpsertRun(r.Context(), in)
			if e != nil {
				switch {
				case errors.Is(e, commandcenter.ErrInvalid):
					writeError(w, 400, "invalid_request", "invalid run observation")
				case errors.Is(e, commandcenter.ErrIdentityConflict):
					writeError(w, 409, "run_identity_conflict", "run identity conflicts with existing run")
				case errors.Is(e, commandcenter.ErrStateRegression):
					writeError(w, 409, "run_state_regression", "run state cannot regress")
				default:
					writeError(w, 500, "internal_error", "failed to upsert run")
				}
				return
			}
			status := 200
			if created {
				status = 201
			}
			writeJSON(w, status, map[string]any{"run": run, "created": created, "replayed": replayed})
			return
		}
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/runs/") {
		id := strings.TrimPrefix(r.URL.Path, "/runs/")
		run, e := opts.runStore.GetRun(r.Context(), id)
		if errors.Is(e, commandcenter.ErrNotFound) {
			writeError(w, 404, "not_found", "run not found")
			return
		}
		if e != nil {
			writeError(w, 500, "internal_error", "failed to get run")
			return
		}
		writeJSON(w, 200, map[string]any{"run": run})
		return
	}
	writeError(w, 404, "not_found", "endpoint not found")
}
func validRunState(s string) bool {
	switch s {
	case "unknown", "starting", "running", "completed", "failed", "cancelled":
		return true
	}
	return false
}
func handleAgents(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.runStore == nil {
		writeError(w, 503, "runs_unavailable", "runs store is not configured")
		return
	}
	if r.URL.Path == "/agents" && r.Method == http.MethodGet {
		v, e := opts.runStore.Roster(r.Context(), time.Now().UTC())
		if e != nil {
			writeError(w, 500, "internal_error", "failed to load roster")
			return
		}
		writeJSON(w, 200, map[string]any{"agents": v})
		return
	}
	if r.URL.Path == "/agents/me/presence" && r.Method == http.MethodPatch {
		identity, ok := agentIdentity(w, r, opts)
		if !ok {
			return
		}
		var raw map[string]json.RawMessage
		if !decodeJSONBody(w, r, &raw) {
			return
		}
		if len(raw) == 0 {
			writeError(w, 400, "invalid_request", "presence patch is empty")
			return
		}
		var patch commandcenter.PresencePatch
		for k, v := range raw {
			switch k {
			case "current_card_ref":
				patch.SetCard = true
				if string(v) != "null" {
					var s string
					if json.Unmarshal(v, &s) != nil || !strings.HasPrefix(s, "card:") {
						writeError(w, 400, "invalid_request", "current_card_ref must be a card ref")
						return
					}
					patch.CardRef = &s
				}
			case "note":
				patch.SetNote = true
				if string(v) != "null" {
					var s string
					if json.Unmarshal(v, &s) != nil || len([]rune(s)) > 500 {
						writeError(w, 400, "invalid_request", "note must be at most 500 characters")
						return
					}
					patch.Note = &s
				}
			default:
				writeError(w, 400, "invalid_request", "unknown presence field")
				return
			}
		}
		if patch.CardRef != nil {
			if !cardExists(r, opts, *patch.CardRef) {
				writeError(w, 404, "not_found", "card not found")
				return
			}
		}
		p, e := opts.runStore.PatchPresence(r.Context(), identity.AgentID, patch, time.Now().UTC())
		if e != nil {
			writeError(w, 500, "internal_error", "failed to set presence")
			return
		}
		writeJSON(w, 200, map[string]any{"presence": p})
		return
	}
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/agents/") {
		id := strings.TrimPrefix(r.URL.Path, "/agents/")
		d, e := opts.runStore.AgentDetail(r.Context(), id, time.Now().UTC())
		if errors.Is(e, commandcenter.ErrNotFound) {
			writeError(w, 404, "not_found", "agent not found")
			return
		}
		if e != nil {
			writeError(w, 500, "internal_error", "failed to get agent")
			return
		}
		if opts.primitiveStore != nil {
			cards, cardErr := opts.primitiveStore.ListCards(r.Context(), primitives.CardListFilter{})
			if cardErr != nil {
				writeError(w, 500, "internal_error", "failed to load agent cards")
				return
			}
			wanted := map[string]bool{}
			if d.Agent.CurrentCardRef != nil {
				wanted[*d.Agent.CurrentCardRef] = true
			}
			for _, run := range d.RecentRuns {
				if run.CardRef != nil {
					wanted[*run.CardRef] = true
				}
			}
			d.RecentCards = []map[string]any{}
			for _, card := range cards {
				if wanted[anyString(card["ref"])] || anyString(card["assignee"]) == d.Agent.Ref {
					d.RecentCards = append(d.RecentCards, card)
				}
			}
		}
		writeJSON(w, 200, map[string]any{"agent": d.Agent, "recent_cards": d.RecentCards, "recent_runs": d.RecentRuns, "open_asks": d.OpenAsks, "recent_notes": d.RecentNotes})
		return
	}
	writeError(w, 404, "not_found", "endpoint not found")
}
func cardExists(r *http.Request, opts handlerOptions, ref string) bool {
	if opts.primitiveStore == nil {
		return false
	}
	_, e := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: "card", Ref: ref})
	return e == nil
}
