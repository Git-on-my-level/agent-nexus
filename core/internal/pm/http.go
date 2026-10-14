package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler must be mounted behind the existing core authentication middleware.
// Authenticate derives a verified principal from that middleware; it must never
// accept an actor/workspace from client-controlled headers or request JSON.
type Handler struct {
	Service      *Service
	Authenticate func(*http.Request) (Principal, error)
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if h.Service == nil || h.Authenticate == nil {
		writeError(w, ErrUnavailable)
		return
	}
	p, err := h.Authenticate(r)
	if err != nil {
		writeAuthenticationError(w, r)
		return
	}
	path := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/pm"), "/"), "/")
	permission := "pm.access"
	if r.Method == http.MethodGet && len(path) == 1 && path[0] == "presence" {
		permission = "pm.presence"
	}
	if r.Method == http.MethodPost && len(path) == 1 && (path[0] == "connect" || path[0] == "disconnect") {
		permission = "pm." + path[0]
	}
	if err = h.Service.authorize(r.Context(), p, permission, ""); err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	bootstrap := len(path) == 2 && path[0] == "turns" && path[1] == "claim" && r.Method == http.MethodPost || len(path) == 3 && path[0] == "turns" && path[2] == "heartbeat" && r.Method == http.MethodPost
	existingRead := r.Method == http.MethodGet && len(path) > 0 && (path[0] == "conversations" || path[0] == "decisions" || path[0] == "actions" || path[0] == "turns")
	existingAnswer := r.Method == http.MethodPost && len(path) == 3 && (path[0] == "decisions" && (path[2] == "answer" || path[2] == "dispatch") || path[0] == "actions" && (path[2] == "reconcile" || path[2] == "acknowledge"))
	// Proposal creation owns its onboarding gate: an opt-in stored receipt can
	// be recovered after disconnect, while both legacy and fresh writes gate.
	proposal := r.Method == http.MethodPost && len(path) == 1 && path[0] == "decisions"
	// Chat services check existing keys before their fresh-write setup gate.
	// A lost confirmation must remain recoverable after PM disconnect.
	chat := r.Method == http.MethodPost && (len(path) == 1 && path[0] == "conversations" || len(path) == 3 && path[0] == "conversations" && path[2] == "messages")
	if permission == "pm.access" && !bootstrap && !existingRead && !existingAnswer && !proposal && !chat {
		if err = h.Service.RequireOnboarded(ctx); err != nil {
			writeError(w, err)
			return
		}
	}
	s := h.Service
	var out any
	status := http.StatusOK
	decode := func(v any) error { return decodeBody(w, r, v) }
	switch {
	case len(path) == 1 && path[0] == "disconnect" && r.Method == http.MethodPost:
		out, err = s.Disconnect(ctx, p)
	case len(path) == 1 && path[0] == "connect" && r.Method == http.MethodPost:
		var in ConnectionInput
		if err = decode(&in); err == nil {
			out, err = s.Connect(ctx, p, in)
		}
	case len(path) == 1 && path[0] == "presence" && r.Method == http.MethodGet:
		out, err = s.Presence(ctx, p)
	case len(path) == 1 && path[0] == "conversations" && r.Method == http.MethodGet:
		var limit int
		var cursor string
		limit, cursor, err = pageParams(r)
		if err == nil {
			out, err = s.ConversationPage(ctx, p, limit, cursor)
		}
	case len(path) == 1 && path[0] == "conversations" && r.Method == http.MethodPost:
		var in CreateConversation
		if err = decode(&in); err == nil {
			if in.ReplayKey == "" {
				err = s.requireChatReplayOrSetup(ctx, p, "conversation", stableID("conversation", p.WorkspaceID, p.ActorID, in.RequestKey), in.RequestKey)
			}
			if err == nil {
				out, err = s.CreateConversation(ctx, p, in)
			}
			status = http.StatusCreated
		} else if gate := s.RequireOnboarded(ctx); gate != nil {
			err = gate
		}
	case len(path) == 2 && path[0] == "conversations" && r.Method == http.MethodGet:
		var limit int
		var cursor string
		limit, cursor, err = pageParams(r)
		if r.URL.Query().Get("limit") == "" {
			limit = 200
		}
		if err == nil {
			out, err = s.ConversationHistory(ctx, p, path[1], limit, cursor)
		}
	case len(path) == 3 && path[0] == "conversations" && path[2] == "messages" && r.Method == http.MethodPost:
		var in MessageInput
		if err = decode(&in); err == nil {
			if in.ReplayKey == "" {
				err = s.requireChatReplayOrSetup(ctx, p, "turn", stableID("turn", path[1], in.RequestKey), in.RequestKey)
			}
			if err == nil {
				out, err = s.PostMessage(ctx, p, path[1], in)
			}
			status = http.StatusAccepted
		} else if gate := s.RequireOnboarded(ctx); gate != nil {
			err = gate
		}
	case len(path) == 1 && path[0] == "context" && r.Method == http.MethodGet:
		var limit int
		limit, err = queryLimit(r)
		if err == nil {
			out, err = s.QueryContextPage(ctx, p, r.URL.Query().Get("work_ref"), r.URL.Query().Get("query"), r.URL.Query().Get("cursor"), limit)
		}
	case len(path) == 1 && path[0] == "decisions" && r.Method == http.MethodGet:
		var limit int
		var cursor string
		limit, cursor, err = pageParams(r)
		if err == nil {
			out, err = s.DecisionPage(ctx, p, limit, cursor)
		}
	case len(path) == 1 && path[0] == "decisions" && r.Method == http.MethodPost:
		var in DecisionInput
		if err = decode(&in); err == nil {
			if in.ReplayKey == "" {
				// Retain the legacy route gate before work/domain preflight.
				err = s.RequireOnboarded(ctx)
			}
			if err != nil {
				break
			}
			var d Decision
			d, err = s.ProposeDecision(ctx, p, in)
			out = d
			if d.ProposalReplayStatus != 0 {
				status = d.ProposalReplayStatus
			} else if !d.Replayed {
				status = http.StatusCreated
			}
		}
	case len(path) == 2 && path[0] == "decisions" && r.Method == http.MethodGet:
		out, err = s.decision(ctx, p, path[1], "pm.read")
	case len(path) == 3 && path[0] == "decisions" && path[2] == "answer" && r.Method == http.MethodPost:
		var in AnswerInput
		if err = decode(&in); err == nil {
			out, err = s.AnswerDecision(ctx, p, path[1], in)
		}
	case len(path) == 3 && path[0] == "decisions" && path[2] == "dispatch" && r.Method == http.MethodPost:
		var in struct{}
		if err = decode(&in); err == nil {
			out, err = s.DispatchDecision(ctx, p, path[1])
		}
	case len(path) == 1 && path[0] == "actions" && r.Method == http.MethodGet:
		var limit int
		var cursor string
		limit, cursor, err = pageParams(r)
		if err == nil {
			out, err = s.ActionPage(ctx, p, limit, cursor)
		}
	case len(path) == 2 && path[0] == "actions" && r.Method == http.MethodGet:
		out, err = s.action(ctx, p, path[1], "pm.read")
	case len(path) == 3 && path[0] == "actions" && path[2] == "reconcile" && r.Method == http.MethodPost:
		var in struct{}
		if err = decode(&in); err == nil {
			out, err = s.ReconcileAction(ctx, p, path[1])
		}
	case len(path) == 3 && path[0] == "actions" && path[2] == "acknowledge" && r.Method == http.MethodPost:
		var in struct{}
		if err = decode(&in); err == nil {
			out, err = s.AcknowledgeAction(ctx, p, path[1])
		}
	case len(path) == 1 && path[0] == "bindings" && r.Method == http.MethodGet:
		var limit int
		var cursor string
		limit, cursor, err = pageParams(r)
		if err == nil {
			out, err = s.BindingPage(ctx, p, limit, cursor)
		}
	case len(path) == 1 && path[0] == "bindings" && r.Method == http.MethodPost:
		var in Binding
		if err = decode(&in); err == nil {
			out, err = s.BindChannel(ctx, p, in)
		}
	case len(path) == 2 && path[0] == "turns" && r.Method == http.MethodGet:
		out, err = s.GetTurnWithLease(ctx, p, path[1], r.Header.Get("X-ANX-PM-Lease-Token"))
	case len(path) == 3 && path[0] == "turns" && path[2] == "context" && r.Method == http.MethodPost:
		in := TurnContextInput{Limit: 20}
		if err = decode(&in); err == nil {
			if in.Limit < 1 || in.Limit > 50 {
				err = ErrInvalid
			} else {
				out, err = s.getTurnContextView(ctx, p, path[1], in.ContextRef, in.Query, in.Cursor, in.Limit, in.LeaseToken, in.View)
			}
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "decisions" && r.Method == http.MethodPost:
		var in TurnProposeInput
		if err = decode(&in); err == nil {
			out, err = s.ProposeForTurn(ctx, p, path[1], in.DecisionInput, in.LeaseToken)
		}
	case len(path) == 2 && path[0] == "turns" && path[1] == "claim" && r.Method == http.MethodPost:
		var in ClaimInput
		if r.Body == nil || r.ContentLength == 0 {
			out, err = s.ClaimTurn(ctx, p, in)
		} else if err = decode(&in); err == nil {
			out, err = s.ClaimTurn(ctx, p, in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "heartbeat" && r.Method == http.MethodPost:
		var in HeartbeatInput
		if err = decode(&in); err == nil {
			out, err = s.HeartbeatTurn(ctx, p, path[1], in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "release" && r.Method == http.MethodPost:
		var in ReleaseInput
		if err = decode(&in); err == nil {
			out, err = s.ReleaseTurn(ctx, p, path[1], in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "fail" && r.Method == http.MethodPost:
		var in FailInput
		if err = decode(&in); err == nil {
			out, err = s.FailTurn(ctx, p, path[1], in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "complete" && r.Method == http.MethodPost:
		var in struct {
			Text         string   `json:"text"`
			EvidenceRefs []string `json:"evidence_refs"`
			LeaseToken   string   `json:"lease_token"`
		}
		if err = decode(&in); err == nil {
			out, err = s.CompleteTurnWithLease(ctx, p, path[1], in.Text, in.EvidenceRefs, in.LeaseToken)
		}
	default:
		err = ErrNotFound
	}
	if errors.Is(err, ErrEmpty) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	// All turn-bearing endpoints share the same public projection, including
	// idempotent message replays. Only the runner claim returns credentials.
	switch value := out.(type) {
	case Conversation:
		if len(value.ReplayJSON) > 0 {
			out = value.ReplayJSON
		}
	case Decision:
		if len(path) == 3 && path[0] == "turns" && path[2] == "decisions" {
			// Proposal validation already established this conversation owner.
			// Keep live response evidence inside that same requester boundary.
			p = Principal{WorkspaceID: value.WorkspaceID, ActorID: value.ActorID, Human: true}
			if s.deps.BindRequesterContext != nil {
				ctx = s.deps.BindRequesterContext(ctx, p)
			}
		}
		if len(value.ProposalReplayJSON) > 0 {
			out = value.ProposalReplayJSON
		} else if len(value.AnswerReplayJSON) > 0 {
			out = value.AnswerReplayJSON
		} else {
			out = s.decisionResponse(ctx, p, value)
		}
	case Page[Decision]:
		if s.deps.BeginDeliveryRead != nil {
			refs := []string{}
			for _, d := range value.Items {
				if d.SourceAuthority == "" {
					refs = append(refs, d.WorkRef)
				}
			}
			ctx, err = s.deps.BeginDeliveryRead(ctx, refs)
			if err != nil {
				writeError(w, err)
				return
			}
		}
		items := make([]any, 0, len(value.Items))
		for _, d := range value.Items {
			items = append(items, s.decisionResponse(ctx, p, d))
		}
		out = Page[any]{Items: items, HasMore: value.HasMore, NextCursor: value.NextCursor}
	case Turn:
		if len(value.ReplayJSON) > 0 {
			out = value.ReplayJSON
			break
		}
		view := turnResponse(value, r.Method == http.MethodPost && len(path) == 2 && path[0] == "turns" && path[1] == "claim")
		if len(path) == 3 && path[0] == "turns" && path[2] == "heartbeat" {
			view.LeaseExpiresAt = &value.LeaseExpiresAt
		}
		out = view
	case ConversationDetail:
		turns := make([]any, 0, len(value.Turns))
		for _, turn := range value.Turns {
			turns = append(turns, turnResponse(turn, false))
		}
		out = struct {
			ConversationDetail
			Turns []any `json:"turns"`
		}{value, turns}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}
func queryLimit(r *http.Request) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 50 {
		return 0, ErrInvalid
	}
	return n, nil
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return ErrInvalid
	}
	switch v.(type) {
	case *DecisionInput, *TurnProposeInput:
		var body struct {
			Payload map[string]json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &body) == nil {
			if _, exists := body.Payload["resolution"]; exists {
				return fmt.Errorf("%w: payload.resolution is read-only; send payload.resolution_refs", ErrInvalid)
			}
		}
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return ErrInvalid
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return ErrInvalid
	}
	return nil
}
func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "PM operation failed"
	for _, e := range []struct {
		err    error
		status int
		code   string
	}{{ErrNotOnboarded, 409, "pm_not_onboarded"}, {ErrInvalid, 400, "invalid_request"}, {ErrForbidden, 403, "forbidden"}, {ErrNotFound, 404, "not_found"}, {ErrLeaseRequired, 409, "lease_required"}, {ErrLeaseMismatch, 409, "lease_mismatch"}, {ErrTurnNotClaimed, 409, "turn_not_claimed"}, {ErrConflict, 409, "conflict"}, {ErrTurnClosed, 409, "turn_closed"}, {ErrStale, 409, "source_revision_changed"}, {ErrBusy, 429, "busy"}, {ErrPMIdentity, 503, "unavailable"}, {ErrUnavailable, 503, "unavailable"}} {
		if errors.Is(err, e.err) {
			status = e.status
			code = e.code
			message = err.Error()
			break
		}
	}
	var humanPending *HumanProposalPendingError
	if errors.As(err, &humanPending) {
		status, code, message = http.StatusConflict, "human_proposal_pending", err.Error()
	}
	w.WriteHeader(status)
	body := map[string]any{"code": code, "message": message}
	if humanPending != nil {
		body["details"] = humanPending
	}
	var nonHuman *ConversationOwnerNotHumanError
	if errors.As(err, &nonHuman) {
		body["details"] = map[string]string{"reason": "conversation_owner_not_human"}
	}
	var target *ApprovalTargetError
	if errors.As(err, &target) {
		body["details"] = target
	}
	var busy *BusyError
	if errors.As(err, &busy) {
		body["details"] = busy
	}
	var closed *TurnClosedError
	if errors.As(err, &closed) {
		body["details"] = closed
	}
	var conflict *DecisionConflict
	if errors.As(err, &conflict) {
		body["details"] = map[string]string{"existing_decision_id": conflict.ExistingDecisionID}
	}
	var superseded *SupersededDecisionError
	if errors.As(err, &superseded) {
		body["details"] = map[string]string{"status": string(Superseded), "superseded_by": superseded.SupersededBy}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"error": body})
}

// Expose the active runner identity; reserve lease credentials for claims.
type turnView struct {
	ActivitySupported bool `json:"activity_supported,omitempty"`
	Turn
	Claimed        bool       `json:"claimed"`
	LeaseToken     string     `json:"lease_token,omitempty"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
}

func turnResponse(t Turn, includeLease bool) turnView {
	t.FailureKind = turnFailureKind(t)
	t.TerminalLeaseHash = ""
	out := turnView{Turn: t, Claimed: leaseHeld(t, time.Now().UTC())}
	if out.Claimed {
		out.LeaseOwner = t.LeaseOwner
	}
	if includeLease {
		out.ActivitySupported = true
		out.LeaseToken = t.LeaseToken
		out.LeaseOwner = t.LeaseOwner
		out.LeaseExpiresAt = &t.LeaseExpiresAt
	}
	return out
}

// Runtime wiring uses core's authentication writer. Keep standalone handlers
// compatible with that same auth error envelope.
func writeAuthenticationError(w http.ResponseWriter, r *http.Request) {
	code, message, hint := "invalid_token", "token is invalid, expired, or revoked", "Refresh or rotate credentials, then retry."
	if strings.TrimSpace(r.Header.Get("Authorization")) == "" {
		code, message, hint = "auth_required", "authorization header is required", "Attach a valid Bearer token and retry."
	}
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "recoverable": true, "hint": hint}})
}

// Keep the legacy fresh-write onboarding response (including invalid fresh
// bodies), but allow a known owner-bound key to reach normal body/access checks.
// This peek exposes no record and never grants resource access by itself.
func (s *Service) requireChatReplayOrSetup(ctx context.Context, p Principal, kind, id, key string) error {
	gate := s.RequireOnboarded(ctx)
	if gate == nil {
		return nil
	}
	if !errors.Is(gate, ErrNotOnboarded) || !validText(key, 256) {
		return gate
	}
	var prior struct {
		WorkspaceID string `json:"workspace_id"`
		ActorID     string `json:"actor_id"`
	}
	err := s.store.get(ctx, kind, id, &prior)
	if errors.Is(err, ErrNotFound) {
		return gate
	}
	if err != nil {
		return err
	}
	if prior.WorkspaceID != p.WorkspaceID || prior.ActorID != p.ActorID {
		return gate
	}
	return nil
}
