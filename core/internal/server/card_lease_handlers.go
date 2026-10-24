package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
)

// cardLeaseStore is the primitive store surface the lease routes need. It is
// asserted rather than added to PrimitiveStore so test doubles stay valid.
type cardLeaseStore interface {
	GetCardLease(context.Context, string) (primitives.CardLease, error)
	AcquireCardLease(context.Context, string, string, primitives.CardLeaseAcquireInput) (primitives.CardLeaseResult, error)
	RenewCardLease(context.Context, string, string, primitives.CardLeaseRenewInput) (primitives.CardLeaseResult, error)
	ReleaseCardLease(context.Context, string, string, primitives.CardLeaseReleaseInput) (primitives.CardLeaseResult, error)
}

// cardLeaseAction maps a /cards/{id}/lease... remainder to its card id and
// action: "show" for GET .../lease, otherwise acquire, renew or release.
func cardLeaseAction(remainder string) (cardID, action string, ok bool) {
	if id, found := strings.CutSuffix(remainder, "/lease"); found {
		return id, "show", id != "" && !strings.Contains(id, "/")
	}
	for _, a := range []string{"acquire", "renew", "release"} {
		if id, found := strings.CutSuffix(remainder, "/lease/"+a); found {
			return id, a, id != "" && !strings.Contains(id, "/")
		}
	}
	return "", "", false
}

func handleCardLease(w http.ResponseWriter, r *http.Request, opts handlerOptions, cardID, action string) {
	store, ok := opts.primitiveStore.(cardLeaseStore)
	if opts.primitiveStore == nil || !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if action == "show" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
	} else if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}
	cardID, ok = resolveHTTPResourceID(w, r, opts, "card", cardID, "card")
	if !ok {
		return
	}
	if action == "show" {
		lease, err := store.GetCardLease(r.Context(), cardID)
		if err != nil {
			writeCardLeaseError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"lease": lease.Map()})
		return
	}
	var req struct {
		ActorID    string `json:"actor_id"`
		Owner      string `json:"owner"`
		TTLSeconds *int64 `json:"ttl_seconds"`
		Reason     string `json:"reason"`
		IfEpoch    *int64 `json:"if_epoch"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	switch {
	case action != "acquire" && req.IfEpoch != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "if_epoch applies only to acquire")
		return
	case action == "release" && req.TTLSeconds != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds does not apply to release")
		return
	case action == "renew" && req.Reason != "":
		writeError(w, http.StatusBadRequest, "invalid_request", "reason does not apply to renew")
		return
	case action == "acquire" && req.TTLSeconds == nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds is required")
		return
	case req.IfEpoch != nil && *req.IfEpoch < 0:
		writeError(w, http.StatusBadRequest, "invalid_request", "if_epoch must be zero or positive")
		return
	}
	var ttl time.Duration
	if req.TTLSeconds != nil {
		if *req.TTLSeconds < int64(primitives.CardLeaseMinTTL/time.Second) || *req.TTLSeconds > int64(primitives.CardLeaseMaxTTL/time.Second) {
			writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds must be between 60 and 604800")
			return
		}
		ttl = time.Duration(*req.TTLSeconds) * time.Second
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	var (
		result primitives.CardLeaseResult
		err    error
	)
	switch action {
	case "acquire":
		result, err = store.AcquireCardLease(r.Context(), actorID, cardID, primitives.CardLeaseAcquireInput{Owner: req.Owner, TTL: ttl, Reason: req.Reason, IfEpoch: req.IfEpoch})
	case "renew":
		result, err = store.RenewCardLease(r.Context(), actorID, cardID, primitives.CardLeaseRenewInput{Owner: req.Owner, TTL: ttl})
	default:
		result, err = store.ReleaseCardLease(r.Context(), actorID, cardID, primitives.CardLeaseReleaseInput{Owner: req.Owner, Reason: req.Reason})
	}
	if err != nil {
		writeCardLeaseError(w, r, err)
		return
	}
	response := map[string]any{"lease": result.Lease.Map(), "changed": result.Changed}
	if result.EventID != "" {
		response["event_id"] = result.EventID
	}
	if result.Changed {
		notifyProjectionMaintainer(opts)
	}
	writeJSON(w, http.StatusOK, response)
}

func writeCardLeaseError(w http.ResponseWriter, r *http.Request, err error) {
	var leaseErr *primitives.CardLeaseError
	switch {
	case errors.As(err, &leaseErr):
		payload := errorPayload(leaseErr.Code, leaseErr.Error())
		payload["recoverable"] = leaseErr.Code != primitives.CardLeaseErrHeld
		switch leaseErr.Code {
		case primitives.CardLeaseErrHeld:
			payload["hint"] = "Another owner holds this card. Do not start the work; wait for expires_at or ask the holder to release it."
		case primitives.CardLeaseErrExpired:
			payload["hint"] = "Your lease expired. Acquire it again to take a new epoch; another owner may have started."
		default:
			payload["hint"] = "Nobody holds this lease. Acquire it before renewing."
		}
		details := map[string]any{"state": leaseErr.Lease.State, "owner": leaseErr.Lease.Owner, "epoch": leaseErr.Lease.Epoch, "card_ref": leaseErr.Lease.CardRef}
		if leaseErr.Lease.ExpiresAt != "" {
			details["expires_at"] = leaseErr.Lease.ExpiresAt
		}
		if leaseErr.Lease.HolderActorID != "" {
			details["holder_actor_id"] = leaseErr.Lease.HolderActorID
		}
		payload["details"] = details
		writeJSON(w, http.StatusConflict, map[string]any{"error": payload})
	case writeStoreContentionError(w, r, err):
	case errors.Is(err, primitives.ErrInvalidWorkRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "card not found")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "card lease changed; read it with lease show and retry")
	default:
		writeInternalError(w, r, err, "failed to update card lease")
	}
}

// DefaultCardLeaseClockInterval is how often core records lease expiries.
const DefaultCardLeaseClockInterval = 30 * time.Second

// CardLeaseClock records card_lease_expired once per expired epoch and wakes
// the card's agent assignee (or, when the card has none, the workspace PM)
// through the durable wake path, which mirrors into the delivery queue. Lease
// state never depends on this clock: reads compute expiry from expires_at.
type CardLeaseClock struct {
	Store         *primitives.Store
	AuthStore     *auth.Store
	PMActorID     func() string
	WorkspaceID   string
	WorkspaceName string
	Interval      time.Duration
	Logf          func(format string, args ...any)
}

func (c *CardLeaseClock) Run(ctx context.Context) {
	if c == nil || c.Store == nil {
		return
	}
	interval := c.Interval
	if interval <= 0 {
		interval = DefaultCardLeaseClockInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.Step(ctx); err != nil && ctx.Err() == nil {
			c.logf("card lease clock: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Step records one bounded batch of expiries and sends their wakes. A failed
// wake is logged and not retried: the expiry event is already durable and
// visible on the card timeline.
func (c *CardLeaseClock) Step(ctx context.Context) error {
	expired, err := c.Store.ExpireDueCardLeases(ctx, actors.SystemActorID, 0)
	for _, e := range expired {
		if wakeErr := c.wake(ctx, e); wakeErr != nil {
			c.logf("card lease clock: wake for %s epoch %d: %v", e.CardRef, e.Epoch, wakeErr)
		}
	}
	return err
}

func (c *CardLeaseClock) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func (c *CardLeaseClock) wakeTarget(ctx context.Context, e primitives.ExpiredCardLease) (actorID, handle string, err error) {
	if c.AuthStore == nil {
		return "", "", nil
	}
	if prefix, id, ok := strings.Cut(e.Assignee, ":"); ok {
		var p auth.AuthPrincipalSummary
		var found bool
		switch prefix {
		case "agent":
			p, found, err = findAgentPrincipalByAgentID(ctx, c.AuthStore, id)
		case "actor":
			p, found, err = findAgentPrincipalByActorID(ctx, c.AuthStore, id)
		}
		if err != nil {
			return "", "", err
		}
		if found {
			return p.ActorID, strings.TrimSpace(p.Username), nil
		}
	}
	if c.PMActorID == nil {
		return "", "", nil
	}
	pm := strings.TrimSpace(c.PMActorID())
	if pm == "" {
		return "", "", nil
	}
	p, found, err := findAgentPrincipalByActorID(ctx, c.AuthStore, pm)
	if err != nil || !found {
		return "", "", err
	}
	return p.ActorID, strings.TrimSpace(p.Username), nil
}

func (c *CardLeaseClock) wake(ctx context.Context, e primitives.ExpiredCardLease) error {
	actorID, handle, err := c.wakeTarget(ctx, e)
	if err != nil || actorID == "" || e.ThreadID == "" || e.EventID == "" || e.EventID == "-" {
		return err
	}
	// Designation grants no read authority: never tell a principal about a
	// card it cannot read.
	if !c.Store.CanAccessResource(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actorID}), "card", e.CardID) {
		return nil
	}
	if handle == "" {
		handle = "agent"
	}
	workspaceID := strings.TrimSpace(c.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	wakeupID := router.WakeupArtifactID(workspaceID, e.ThreadID, fmt.Sprintf("lease-expired-%s-%d", e.CardID, e.Epoch), actorID)
	text := fmt.Sprintf("Work lease on %s held by %s (epoch %d) expired at %s without renewal. Check whether the work is still alive; re-acquire with anx lease acquire or reassign the card.", e.CardRef, e.Owner, e.Epoch, e.Expires)
	refs := router.WakeArtifactRefs(e.ThreadID, e.EventID, e.CardRef)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = c.Store.CreateArtifact(ctx, actors.SystemActorID, map[string]any{
		"id":              wakeupID,
		"kind":            router.WakeArtifactKind,
		"summary":         "Expired work lease for @" + handle,
		"refs":            refs,
		"target_handle":   handle,
		"target_actor_id": actorID,
		"workspace_id":    workspaceID,
		"thread_id":       e.ThreadID,
	}, map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"target_handle":      handle,
		"target_actor_id":    actorID,
		"workspace_id":       workspaceID,
		"thread_id":          e.ThreadID,
		"trigger_event_id":   e.EventID,
		"trigger_created_at": ts,
		"trigger_text":       text,
		"subject_ref":        e.CardRef,
	}, "structured")
	if err != nil && !errors.Is(err, primitives.ErrConflict) {
		return err
	}
	_, err = c.Store.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
		WakeupID:         wakeupID,
		Status:           primitives.AgentWakeupStatusRequested,
		TargetHandle:     handle,
		TargetActorID:    actorID,
		WorkspaceID:      workspaceID,
		WorkspaceName:    c.WorkspaceName,
		ThreadID:         e.ThreadID,
		TriggerEventID:   e.EventID,
		TriggerCreatedAt: ts,
		TriggerText:      text,
		Refs:             append(refs, "artifact:"+wakeupID),
	})
	return err
}
