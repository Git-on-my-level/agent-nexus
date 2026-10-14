package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/delivery"
)

// WithDeliveryStore enables the delivery queue routes: channel adapters claim
// and receipt human notifications, and principals manage their own channel
// settings and read their own deliveries.
func WithDeliveryStore(store *delivery.Store) HandlerOption {
	return func(opts *handlerOptions) { opts.deliveryStore = store }
}

func deliveryRouteAccess(r *http.Request) routeAccessRequirement {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 1 && parts[0] == "deliveries":
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
	case len(parts) == 2 && parts[0] == "deliveries" && parts[1] == "claim":
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	case len(parts) == 3 && parts[0] == "deliveries" && parts[1] != "" && parts[2] == "receipt":
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost)(r)
	case len(parts) == 1 && parts[0] == "notification-settings":
		if r.Method == http.MethodGet {
			return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet)(r)
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPut)(r)
	}
	return routeAccessRequirement{}
}

func deliveryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, delivery.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "an active adapter declaration delivering this channel is required")
	case errors.Is(err, delivery.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "delivery not found")
	case errors.Is(err, delivery.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "delivery claim is no longer current")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "delivery operation failed")
	}
}

func decodeDeliveryJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object with known fields")
		return false
	}
	return true
}

type deliveryClaimRequest struct {
	Channel      string `json:"channel"`
	Limit        int    `json:"limit"`
	LeaseSeconds int    `json:"lease_seconds"`
}

type deliveryReceiptRequest struct {
	ClaimToken string `json:"claim_token"`
	Outcome    string `json:"outcome"`
	Error      string `json:"error"`
	Retryable  bool   `json:"retryable"`
}

type notificationSettingsRequest struct {
	Channels []delivery.ChannelSetting `json:"channels"`
}

func handleDeliveryRoutes(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.deliveryStore == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "delivery store unavailable")
		return
	}
	actor, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	store := opts.deliveryStore
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	ctx := r.Context()
	if parts[0] == "notification-settings" {
		if !isHumanPrincipal(actor) {
			writeError(w, http.StatusForbidden, "forbidden", "notification settings belong to human principals")
			return
		}
		if r.Method == http.MethodPut {
			var body notificationSettingsRequest
			if !decodeDeliveryJSON(w, r, &body) {
				return
			}
			declared, err := store.DeclaredChannels(ctx)
			if err != nil {
				deliveryError(w, err)
				return
			}
			for _, c := range body.Channels {
				if !containsString(declared, strings.TrimSpace(c.Channel)) {
					writeError(w, http.StatusBadRequest, "invalid_request", "channel is not delivered by any active adapter: "+c.Channel)
					return
				}
			}
			if _, err := store.ReplaceSettings(ctx, actor.ActorID, body.Channels); err != nil {
				deliveryError(w, err)
				return
			}
		}
		settings, err := store.Settings(ctx, actor.ActorID)
		if err != nil {
			deliveryError(w, err)
			return
		}
		declared, err := store.DeclaredChannels(ctx)
		if err != nil {
			deliveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"channels": settings, "available_channels": declared})
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, receipts, err := store.ListForPrincipal(ctx, actor.ActorID, limit)
		if err != nil {
			deliveryError(w, err)
			return
		}
		out := make([]map[string]any, 0, len(items))
		for _, d := range items {
			rs := receipts[d.DeliveryID]
			if rs == nil {
				rs = []delivery.Receipt{}
			}
			out = append(out, map[string]any{"delivery": d, "receipts": rs})
		}
		writeJSON(w, http.StatusOK, map[string]any{"deliveries": out})
	case len(parts) == 2 && parts[1] == "claim" && r.Method == http.MethodPost:
		var body deliveryClaimRequest
		if !decodeDeliveryJSON(w, r, &body) {
			return
		}
		if err := requireDeliveringAgent(r, store, actor, body.Channel); err != nil {
			deliveryError(w, err)
			return
		}
		items, err := store.Claim(ctx, strings.TrimSpace(body.Channel), actor.ActorID, body.Limit, time.Duration(body.LeaseSeconds)*time.Second)
		if err != nil {
			deliveryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deliveries": items})
	case len(parts) == 3 && parts[2] == "receipt" && r.Method == http.MethodPost:
		var body deliveryReceiptRequest
		if !decodeDeliveryJSON(w, r, &body) {
			return
		}
		if body.Outcome != "delivered" && body.Outcome != "failed" {
			writeError(w, http.StatusBadRequest, "invalid_request", "outcome must be delivered or failed")
			return
		}
		current, err := store.Get(ctx, parts[1])
		if err != nil {
			deliveryError(w, err)
			return
		}
		if err := requireDeliveringAgent(r, store, actor, current.Channel); err != nil {
			// Do not confirm a delivery on a channel this principal does not deliver.
			deliveryError(w, delivery.ErrNotFound)
			return
		}
		updated, err := store.RecordReceipt(ctx, current.DeliveryID, body.ClaimToken, current.Channel, body.Outcome == "delivered", body.Error, body.Retryable, actor.ActorID)
		if err != nil {
			deliveryError(w, err)
			return
		}
		updated.ClaimToken = ""
		writeJSON(w, http.StatusOK, map[string]any{"delivery": updated})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported delivery operation")
	}
}

// Only the owning agent of an active adapter declaration that delivers the
// channel may claim or receipt its deliveries. Scoped series tokens and human
// principals cannot.
func requireDeliveringAgent(r *http.Request, store *delivery.Store, actor *auth.Principal, channel string) error {
	channel = strings.TrimSpace(channel)
	if !delivery.ValidChannel(channel) {
		return delivery.ErrInvalid
	}
	if actor == nil || isHumanPrincipal(actor) || actor.SeriesAdapter != "" {
		return delivery.ErrForbidden
	}
	channels, err := store.ChannelsForAgent(r.Context(), actor.AgentID)
	if err != nil {
		return err
	}
	if !containsString(channels, channel) {
		return delivery.ErrForbidden
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
