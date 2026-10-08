package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"errors"
	"net/http"
	"strings"
)

type askSubscriptionStore interface {
	CreateAskSubscription(context.Context, string, string, primitives.AskSubscriptionInput) (map[string]any, error)
	RecordAskDelivery(context.Context, string, string, string, string, string, int) error
}

func handleAskSubscription(w http.ResponseWriter, r *http.Request, opts handlerOptions, askRef string) {
	p, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(p) {
		writeError(w, 403, "agent_required", "only an agent can subscribe to its asks")
		return
	}
	store, ok := opts.primitiveStore.(askSubscriptionStore)
	if !ok {
		writeError(w, 503, "primitives_unavailable", "subscriptions unavailable")
		return
	}
	var in primitives.AskSubscriptionInput
	if !decodeJSONBody(w, r, &in) {
		return
	}
	out, err := store.CreateAskSubscription(r.Context(), p.ActorID, askRef, in)
	if err != nil {
		if errors.Is(err, primitives.ErrForbidden) || errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "ask not found")
		} else if errors.Is(err, primitives.ErrInvalidWorkRequest) {
			writeError(w, 400, "invalid_request", err.Error())
		} else {
			writeError(w, 500, "internal_error", "cannot create subscription")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, out)
}
func handleAskDeliveryReceipt(w http.ResponseWriter, r *http.Request, opts handlerOptions, askRef string) {
	p, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := opts.primitiveStore.(askSubscriptionStore)
	if !ok {
		writeError(w, 503, "primitives_unavailable", "subscriptions unavailable")
		return
	}
	var in struct {
		Subscription string `json:"subscription_id"`
		State        string `json:"state"`
		Reason       string `json:"reason"`
		Attempts     int    `json:"attempts"`
	}
	if !decodeJSONBody(w, r, &in) {
		return
	}
	err := store.RecordAskDelivery(r.Context(), p.ActorID, in.Subscription, askRef, in.State, strings.TrimSpace(in.Reason), in.Attempts)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) || errors.Is(err, primitives.ErrForbidden) {
			writeError(w, 404, "not_found", "delivery not found")
		} else {
			writeError(w, 400, "invalid_request", "delivery receipt rejected")
		}
		return
	}
	writeJSON(w, 200, map[string]any{"recorded": true})
}
