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
		HandoffTo  string `json:"handoff_to"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	switch {
	case action != "acquire" && req.IfEpoch != nil:
		writeError(w, http.StatusBadRequest, "invalid_request", "if_epoch applies only to acquire")
		return
	case action != "release" && req.HandoffTo != "":
		writeError(w, http.StatusBadRequest, "invalid_request", "handoff_to applies only to release")
		return
	case action == "release" && req.TTLSeconds != nil && strings.TrimSpace(req.HandoffTo) == "":
		writeError(w, http.StatusBadRequest, "invalid_request", "ttl_seconds applies to release only with handoff_to")
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
		result, err = store.ReleaseCardLease(r.Context(), actorID, cardID, primitives.CardLeaseReleaseInput{Owner: req.Owner, Reason: req.Reason, HandoffTo: req.HandoffTo, HandoffTTL: ttl})
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
			payload["hint"] = "Nobody holds this lease. Acquire it before renewing or handing it off."
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

// CardLeaseClock acts on lease ends. Each tick it:
//
//   - records card_lease_expired once per expired epoch;
//   - sends the pending card_unowned wakes that releases without hand-off
//     left (TakeUnownedCardLeases).
//
// Wake routing:
//
//   - A lease end on live work (primitives cardLeaseLiveWork: in_progress, or
//     an open --next promise) sends card_unowned to the workspace lead, which
//     is the configured PM agent (PMActorID). With no PM configured, or a PM
//     that cannot read the card, it falls back to the card's agent assignee.
//     An expiry on live work also sends lease_expired to the agent assignee
//     when that is a different principal.
//   - An expiry on other work sends lease_expired to the card's agent
//     assignee, or the PM when the card has none.
//
// Every wake goes through the durable wake path (a wake artifact plus an
// agent_wakeups row, mirrored into the bridge delivery queue) and only to a
// principal that can read the card. Lease state never depends on this
// clock: reads compute expiry from expires_at.
type CardLeaseClock struct {
	Store         *primitives.Store
	AuthStore     *auth.Store
	PMActorID     func() string
	WorkspaceID   string
	WorkspaceName string
	Interval      time.Duration
	Logf          func(format string, args ...any)
}

// Card lease wake reasons, carried as wake_reason in the wake packet and in
// the wakeup id, so a bridge can tell an ownerless live card from a routine
// expiry.
const (
	CardLeaseWakeExpired = "lease_expired"
	CardLeaseWakeUnowned = "card_unowned"
)

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

// Step records one bounded batch of expiries, claims one bounded batch of
// pending card_unowned releases, and sends their wakes. A failed wake is
// logged and not retried: the lease event is already durable and visible on
// the card timeline.
func (c *CardLeaseClock) Step(ctx context.Context) error {
	expired, err := c.Store.ExpireDueCardLeases(ctx, actors.SystemActorID, 0)
	for _, e := range expired {
		c.notify(ctx, e)
	}
	if err != nil {
		return err
	}
	unowned, err := c.Store.TakeUnownedCardLeases(ctx, 0)
	for _, e := range unowned {
		c.notify(ctx, e)
	}
	return err
}

func (c *CardLeaseClock) notify(ctx context.Context, e primitives.ExpiredCardLease) {
	if e.DigestOnly {
		return
	}
	// A new tenure may have started after the store claimed this wake; its
	// owner holds the card, so the wake would be stale. This early check
	// avoids writing wake artifacts for it; the enqueue repeats it atomically.
	// A failed check falls through to that enqueue.
	if ended, err := c.Store.CardLeaseTenureEnded(ctx, e.CardID, e.Epoch); err != nil {
		c.logf("card lease clock: tenure check for %s epoch %d: %v", e.CardRef, e.Epoch, err)
	} else if !ended {
		return
	}
	assigneeID, assigneeHandle, err := c.assigneeTarget(ctx, e)
	if err != nil {
		c.logf("card lease clock: assignee for %s: %v", e.CardRef, err)
	}
	send := func(reason, actorID, handle string) bool {
		if actorID == "" {
			return false
		}
		sent, err := c.wake(ctx, e, reason, actorID, handle)
		if err != nil {
			c.logf("card lease clock: %s wake for %s epoch %d: %v", reason, e.CardRef, e.Epoch, err)
		}
		return sent
	}
	if e.Live == "" {
		if !send(CardLeaseWakeExpired, assigneeID, assigneeHandle) {
			leadID, leadHandle, _ := c.leadTarget(ctx)
			send(CardLeaseWakeExpired, leadID, leadHandle)
		}
		return
	}
	leadID, leadHandle, err := c.leadTarget(ctx)
	if err != nil {
		c.logf("card lease clock: lead for %s: %v", e.CardRef, err)
	}
	leadSent := send(CardLeaseWakeUnowned, leadID, leadHandle)
	switch {
	case !leadSent:
		send(CardLeaseWakeUnowned, assigneeID, assigneeHandle)
	case !e.Released && !e.LeadOnly && assigneeID != leadID:
		send(CardLeaseWakeExpired, assigneeID, assigneeHandle)
	}
}

func (c *CardLeaseClock) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// assigneeTarget resolves the card's first assignee to an agent principal.
func (c *CardLeaseClock) assigneeTarget(ctx context.Context, e primitives.ExpiredCardLease) (actorID, handle string, err error) {
	if c.AuthStore == nil {
		return "", "", nil
	}
	prefix, id, ok := strings.Cut(e.Assignee, ":")
	if !ok {
		return "", "", nil
	}
	var p auth.AuthPrincipalSummary
	var found bool
	switch prefix {
	case "agent":
		p, found, err = findAgentPrincipalByAgentID(ctx, c.AuthStore, id)
	case "actor":
		p, found, err = findAgentPrincipalByActorID(ctx, c.AuthStore, id)
	}
	if err != nil || !found {
		return "", "", err
	}
	return p.ActorID, strings.TrimSpace(p.Username), nil
}

// leadTarget resolves the workspace lead: the configured PM agent.
func (c *CardLeaseClock) leadTarget(ctx context.Context) (actorID, handle string, err error) {
	if c.AuthStore == nil || c.PMActorID == nil {
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

// wake writes one durable wake and reports whether it was sent. It sends
// nothing to a principal that cannot read the card.
func (c *CardLeaseClock) wake(ctx context.Context, e primitives.ExpiredCardLease, reason, actorID, handle string) (bool, error) {
	if actorID == "" || e.ThreadID == "" || e.EventID == "" || e.EventID == "-" {
		return false, nil
	}
	// Designation grants no read authority: never tell a principal about a
	// card it cannot read.
	if !c.Store.CanAccessResource(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actorID}), "card", e.CardID) {
		return false, nil
	}
	if handle == "" {
		handle = "agent"
	}
	workspaceID := strings.TrimSpace(c.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "ws_main"
	}
	var key, text, summary string
	switch {
	case reason == CardLeaseWakeUnowned && e.Released:
		key = fmt.Sprintf("card-unowned-%s-%d", e.CardID, e.Epoch)
		summary = "Unowned live card for @" + handle
		text = fmt.Sprintf("card_unowned: %s is live work (%s) but has no owner: %s released its lease (epoch %d) without a hand-off. Assign an owner with anx lease acquire, or close or park the card.", e.CardRef, e.Live, e.Owner, e.Epoch)
	case reason == CardLeaseWakeUnowned:
		key = fmt.Sprintf("card-unowned-%s-%d", e.CardID, e.Epoch)
		summary = "Unowned live card for @" + handle
		text = fmt.Sprintf("card_unowned: %s is live work (%s) but has no owner: the lease held by %s (epoch %d) expired at %s without renewal. Check whether the work is still alive; assign an owner with anx lease acquire, or close or park the card.", e.CardRef, e.Live, e.Owner, e.Epoch, e.Expires)
	default:
		key = fmt.Sprintf("lease-expired-%s-%d", e.CardID, e.Epoch)
		summary = "Expired work lease for @" + handle
		text = fmt.Sprintf("Work lease on %s held by %s (epoch %d) expired at %s without renewal. Check whether the work is still alive; re-acquire with anx lease acquire or reassign the card.", e.CardRef, e.Owner, e.Epoch, e.Expires)
	}
	wakeupID := router.WakeupArtifactID(workspaceID, e.ThreadID, key, actorID)
	refs := router.WakeArtifactRefs(e.ThreadID, e.EventID, e.CardRef)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := c.Store.CreateArtifact(ctx, actors.SystemActorID, map[string]any{
		"id":              wakeupID,
		"kind":            router.WakeArtifactKind,
		"summary":         summary,
		"refs":            refs,
		"target_handle":   handle,
		"target_actor_id": actorID,
		"workspace_id":    workspaceID,
		"thread_id":       e.ThreadID,
	}, map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"wake_reason":        reason,
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
		return false, err
	}
	// The enqueue re-checks the tenure in its own transaction: a re-acquire
	// since the claim (or since the check in notify) suppresses the wake.
	return c.Store.UpsertCardLeaseWakeup(ctx, e.CardID, e.Epoch, primitives.AgentWakeup{
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
}
