package pm

import (
	"encoding/json"
	"errors"
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
		writeError(w, ErrForbidden)
		return
	}
	if err = h.Service.authorize(r.Context(), p, "pm.access", ""); err != nil {
		writeError(w, err)
		return
	}
	path := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/pm"), "/"), "/")
	ctx := r.Context()
	s := h.Service
	var out any
	status := http.StatusOK
	decode := func(v any) error { return decodeBody(w, r, v) }
	switch {
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
			out, err = s.CreateConversation(ctx, p, in)
			status = http.StatusCreated
		}
	case len(path) == 2 && path[0] == "conversations" && r.Method == http.MethodGet:
		out, err = s.GetConversation(ctx, p, path[1])
	case len(path) == 3 && path[0] == "conversations" && path[2] == "messages" && r.Method == http.MethodPost:
		var in MessageInput
		if err = decode(&in); err == nil {
			out, err = s.PostMessage(ctx, p, path[1], in)
			status = http.StatusAccepted
		}
	case len(path) == 1 && path[0] == "context" && r.Method == http.MethodGet:
		var limit int
		limit, err = queryLimit(r)
		if err == nil {
			out, err = s.QueryContext(ctx, p, r.URL.Query().Get("work_ref"), r.URL.Query().Get("query"), limit)
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
			out, err = s.ProposeDecision(ctx, p, in)
			status = http.StatusCreated
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
	case len(path) == 1 && path[0] == "bindings" && r.Method == http.MethodPost:
		var in Binding
		if err = decode(&in); err == nil {
			out, err = s.BindChannel(ctx, p, in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "context" && r.Method == http.MethodGet:
		var limit int
		limit, err = queryLimit(r)
		if err == nil {
			out, err = s.GetTurnContext(ctx, p, path[1], r.URL.Query().Get("query"), limit)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "decisions" && r.Method == http.MethodPost:
		var in DecisionInput
		if err = decode(&in); err == nil {
			out, err = s.ProposeForTurn(ctx, p, path[1], in)
		}
	case len(path) == 3 && path[0] == "turns" && path[2] == "complete" && r.Method == http.MethodPost:
		var in struct {
			Text         string   `json:"text"`
			EvidenceRefs []string `json:"evidence_refs"`
		}
		if err = decode(&in); err == nil {
			out, err = s.CompleteTurn(ctx, p, path[1], in.Text, in.EvidenceRefs)
		}
	default:
		err = ErrNotFound
	}
	if err != nil {
		writeError(w, err)
		return
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
	d := json.NewDecoder(r.Body)
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
	}{{ErrInvalid, 400, "invalid_request"}, {ErrForbidden, 403, "forbidden"}, {ErrNotFound, 404, "not_found"}, {ErrConflict, 409, "conflict"}, {ErrStale, 409, "source_revision_changed"}, {ErrBusy, 429, "busy"}, {ErrUnavailable, 503, "unavailable"}} {
		if errors.Is(err, e.err) {
			status = e.status
			code = e.code
			message = e.err.Error()
			break
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
