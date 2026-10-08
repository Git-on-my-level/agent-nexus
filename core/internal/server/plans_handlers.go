package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

type planStore interface {
	SetCardPlan(context.Context, string, string, string, plans.Plan) error
	EnrichCardPlans(context.Context, []map[string]any, func(string, string) bool, time.Time, time.Duration) error
	ResolveRefs(context.Context, []string, func(string, string) bool, time.Time, time.Duration) ([]primitives.RefPreview, error)
}

func planStalledAfter() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("ANX_PLAN_STALLED_AFTER")); err == nil && d > 0 {
		return d
	}
	return plans.DefaultStalledAfter
}

func planVisibility(r *http.Request, opts handlerOptions) func(string, string) bool {
	return func(threadID, privateOwner string) bool {
		return canAccessPMThread(r, opts, map[string]any{"id": threadID, "pm_actor_id": privateOwner})
	}
}

func filterPlanCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, cards []map[string]any) ([]map[string]any, bool) {
	store, ok := opts.primitiveStore.(interface {
		FilterCardAccess(context.Context, []map[string]any, func(string, string) bool) ([]map[string]any, error)
	})
	if !ok {
		return cards, true
	}
	out, err := store.FilterCardAccess(r.Context(), cards, planVisibility(r, opts))
	if err != nil {
		workStoreError(w, r, err)
		return nil, false
	}
	return out, true
}

func requirePlanCardAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, card map[string]any) bool {
	cards, ok := filterPlanCards(w, r, opts, []map[string]any{card})
	if !ok {
		return false
	}
	if len(cards) == 0 {
		denyPMNotFound(w, "card")
		return false
	}
	return true
}

func enrichPlans(w http.ResponseWriter, r *http.Request, opts handlerOptions, cards []map[string]any) bool {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		return true
	}
	if err := store.EnrichCardPlans(r.Context(), cards, planVisibility(r, opts), time.Now().UTC(), planStalledAfter()); err != nil {
		workStoreError(w, r, err)
		return false
	}
	if r.URL.Query().Get("summary") == "1" {
		for _, card := range cards {
			card["summary_format"] = true
		}
	}
	return true
}

func handleCardPlan(w http.ResponseWriter, r *http.Request, opts handlerOptions, identifier string) {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		writeError(w, 503, "unavailable", "plans are unavailable")
		return
	}
	id, ok := resolveHTTPResourceID(w, r, opts, "card", identifier, "card")
	if !ok {
		return
	}
	card, err := opts.primitiveStore.GetBoardCard(r.Context(), "", id)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	if !requirePlanCardAccess(w, r, opts, card) {
		return
	}
	if r.Method == http.MethodPut {
		var req struct {
			Plan        json.RawMessage `json:"plan"`
			IfUpdatedAt string          `json:"if_updated_at"`
			ActorID     string          `json:"actor_id"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		var p plans.Plan
		decoder := json.NewDecoder(strings.NewReader(string(req.Plan)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil || string(req.Plan) == "null" {
			writeError(w, 400, "invalid_request", "plan must be an object containing steps")
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, req.IfUpdatedAt); err != nil {
			writeError(w, 400, "invalid_request", "if_updated_at must be the timestamp from plan show or cards get")
			return
		}
		actor, ok := resolveWriteActorID(w, r, opts, req.ActorID)
		if !ok {
			return
		}
		if err = store.SetCardPlan(r.Context(), actor, id, req.IfUpdatedAt, p); err != nil {
			workStoreError(w, r, err)
			return
		}
		card, err = opts.primitiveStore.GetBoardCard(r.Context(), "", id)
		if err != nil {
			workStoreError(w, r, err)
			return
		}
	}
	if !enrichPlans(w, r, opts, []map[string]any{card}) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"summary": card["work_summary"], "card_ref": card["ref"], "if_updated_at": card["updated_at"], "plan": card["plan"], "plan_state": card["plan_state"], "plan_health": card["plan_health"], "next_step": card["next_step"], "status_mismatch": card["status_mismatch"]})
}

func handleResolveRefs(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		writeError(w, 503, "unavailable", "ref resolution is unavailable")
		return
	}
	var req struct {
		Refs []string `json:"refs"`
	}
	if !decodeJSONReadBody(w, r, &req) {
		return
	}
	for _, ref := range req.Refs {
		if len(ref) > 2048 {
			writeError(w, 400, "invalid_request", "refs must be at most 2048 bytes each")
			return
		}
	}
	items, err := store.ResolveRefs(r.Context(), req.Refs, planVisibility(r, opts), time.Now().UTC(), planStalledAfter())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}

func scopedThreadVisible(ctx context.Context, opts handlerOptions, id string) bool {
	if opts.readVisibility == nil || id == "" {
		return true
	}
	thread, err := opts.primitiveStore.GetThread(ctx, id)
	if err != nil {
		return errors.Is(err, primitives.ErrNotFound)
	}
	return opts.readVisibility(id, anyString(thread["pm_actor_id"]))
}
func scopedCards(ctx context.Context, opts handlerOptions, cards []map[string]any) ([]map[string]any, error) {
	if opts.readVisibility == nil {
		return cards, nil
	}
	if store, ok := opts.primitiveStore.(interface {
		FilterCardAccess(context.Context, []map[string]any, func(string, string) bool) ([]map[string]any, error)
	}); ok {
		return store.FilterCardAccess(ctx, cards, opts.readVisibility)
	}
	return cards, nil
}
