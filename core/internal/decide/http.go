package decide

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	if err = h.Service.authorize(r.Context(), p, "pm.access", ""); err != nil {
		writeError(w, err)
		return
	}
	ctx := r.Context()
	s := h.Service
	var out any
	status := http.StatusOK
	decode := func(v any) error { return decodeBody(w, r, v) }
	switch {
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
		var in ControlInput
		if err = decode(&in); err == nil {
			out, err = s.control(ctx, p, "dispatch", path[1], in)
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
		var in ControlInput
		if err = decode(&in); err == nil {
			out, err = s.control(ctx, p, "reconcile", path[1], in)
		}
	case len(path) == 3 && path[0] == "actions" && path[2] == "acknowledge" && r.Method == http.MethodPost:
		var in ControlInput
		if err = decode(&in); err == nil {
			out, err = s.control(ctx, p, "acknowledge", path[1], in)
		}
	default:
		err = ErrNotFound
	}
	if err != nil {
		writeError(w, err)
		return
	}
	switch value := out.(type) {
	case Decision:
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
	if _, ok := v.(*DecisionInput); ok {
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
	message := "Decision operation failed"
	for _, e := range []struct {
		err    error
		status int
		code   string
	}{{ErrInvalid, 400, "invalid_request"}, {ErrForbidden, 403, "forbidden"}, {ErrNotFound, 404, "not_found"}, {ErrConflict, 409, "conflict"}, {ErrStale, 409, "source_revision_changed"}, {ErrUnavailable, 503, "unavailable"}} {
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
