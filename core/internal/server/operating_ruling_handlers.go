package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

type operatingRulingStore interface {
	OperatingRulingPage(context.Context, string) ([]primitives.OperatingRuling, string, string, error)
	OperatingRulingDecisions(context.Context, time.Time) ([]map[string]any, bool, error)
	ProposeOperatingRuling(context.Context, string, primitives.OperatingRulingProposal, time.Time) (primitives.OperatingRuling, error)
	ConfirmOperatingRuling(context.Context, string, string, primitives.OperatingRulingDecision, time.Time) (primitives.OperatingRuling, error)
	RevokeOperatingRuling(context.Context, string, string, time.Time) (primitives.OperatingRuling, error)
	WithdrawOperatingRulings(context.Context, string, bool, []string) ([]primitives.OperatingRuling, error)
	GetEvent(context.Context, string) (map[string]any, error)
}

func operatingRulingStoreFrom(w http.ResponseWriter, opts handlerOptions) (operatingRulingStore, bool) {
	store, ok := opts.primitiveStore.(operatingRulingStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "standing rulings store is not configured")
	}
	return store, ok
}

func handleOperatingRulingPage(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	store, ok := operatingRulingStoreFrom(w, opts)
	if !ok {
		return
	}
	after := r.URL.Query().Get("after")
	rulings, revision, next, err := store.OperatingRulingPage(r.Context(), after)
	if errors.Is(err, primitives.ErrInvalidWorkRequest) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err != nil {
		writeReadInternalError(w, r, err, "cannot read ruling projection")
		return
	}
	items := make([]map[string]any, 0, len(rulings))
	for _, ruling := range rulings {
		items = append(items, ruling.JSON())
	}
	body := map[string]any{"rulings": items, "source_revision": revision}
	if next != "" {
		body["next_cursor"] = next
	}
	writeJSON(w, http.StatusOK, body)
}

func handleProposeOperatingRuling(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := operatingRulingStoreFrom(w, opts)
	if !ok {
		return
	}
	var body primitives.OperatingRulingProposal
	if !decodeJSONBody(w, r, &body) || !requireRulingRequestKey(w, body.RequestKey) {
		return
	}
	status, payload, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "rulings.propose", principal.ActorID, body.RequestKey, body, http.StatusCreated, func(ctx context.Context) (map[string]any, error) {
		ruling, err := store.ProposeOperatingRuling(ctx, principal.ActorID, body, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]any{"ruling": ruling.JSON()}, nil
	})
	var missing *primitives.RulingRelationsRequired
	if errors.As(err, &missing) {
		writeJSON(w, http.StatusOK, map[string]any{"redirected": "needs_relation", "missing_relations": missing.Missing})
		return
	}
	writeOperatingRulingResult(w, r, status, payload, err)
}

// handleConfirmOperatingRuling makes a proposal authoritative. A human
// confirms in Decide. An agent confirms only by citing a human decision it
// recorded elsewhere (anx note --decided-by) whose text names this ruling ID;
// the ruling keeps that reference and who it was decided for.
func handleConfirmOperatingRuling(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := operatingRulingStoreFrom(w, opts)
	if !ok {
		return
	}
	var body struct {
		RequestKey  string `json:"idempotency_key"`
		DecisionRef string `json:"decision_ref"`
	}
	if !decodeJSONBody(w, r, &body) || !requireRulingRequestKey(w, body.RequestKey) {
		return
	}
	if !primitives.ValidOperatingRulingID(id) {
		writeError(w, http.StatusBadRequest, "invalid_request", "confirmation requires a ruling date ID")
		return
	}
	human := isHumanPrincipal(principal)
	if !human && strings.TrimSpace(body.DecisionRef) == "" {
		writeError(w, http.StatusForbidden, "human_required", "a human confirms in Decide; an agent must cite the human's recorded decision with decision_ref")
		return
	}
	var decision primitives.OperatingRulingDecision
	if strings.TrimSpace(body.DecisionRef) != "" {
		var problem string
		decision, problem = recordedRulingDecision(r.Context(), store, id, body.DecisionRef)
		if problem != "" {
			writeError(w, http.StatusBadRequest, "invalid_request", problem)
			return
		}
	}
	request := map[string]string{"ruling_id": id, "decision_ref": decision.Ref}
	status, payload, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "rulings.confirm", principal.ActorID, body.RequestKey, request, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		ruling, err := store.ConfirmOperatingRuling(ctx, principal.ActorID, id, decision, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]any{"ruling": ruling.JSON()}, nil
	})
	writeOperatingRulingResult(w, r, status, payload, err)
}

// recordedRulingDecision checks the human decision an agent cites. It must be
// a live event the caller can read, record who decided (decided_by), and name
// the ruling ID in its text, so one decision cannot confirm unrelated rulings.
func recordedRulingDecision(ctx context.Context, store operatingRulingStore, id, ref string) (primitives.OperatingRulingDecision, string) {
	ref = strings.TrimSpace(ref)
	eventID, found := strings.CutPrefix(ref, "event:")
	if !found || strings.TrimSpace(eventID) == "" || len(ref) > 256 {
		return primitives.OperatingRulingDecision{}, "decision_ref must be event:<id> of a recorded decision note"
	}
	event, err := store.GetEvent(ctx, eventID)
	if err != nil || event == nil {
		return primitives.OperatingRulingDecision{}, "decision_ref does not name a decision you can read"
	}
	if anyString(event["trashed_at"]) != "" || anyString(event["archived_at"]) != "" {
		return primitives.OperatingRulingDecision{}, "decision_ref names a retired event"
	}
	payload, _ := event["payload"].(map[string]any)
	decidedFor := strings.TrimSpace(anyString(payload["decided_by"]))
	if decidedFor == "" {
		return primitives.OperatingRulingDecision{}, "decision_ref must be a note recorded with --decided-by"
	}
	if !strings.Contains(anyString(payload["text"]), id) {
		return primitives.OperatingRulingDecision{}, "the recorded decision does not name " + id
	}
	return primitives.OperatingRulingDecision{Ref: "event:" + eventID, DecidedFor: decidedFor}, ""
}

// handleWithdrawOperatingRulings removes unconfirmed proposals and releases
// their names. An agent withdraws its own; a human may withdraw any.
func handleWithdrawOperatingRulings(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := operatingRulingStoreFrom(w, opts)
	if !ok {
		return
	}
	var body struct {
		RulingIDs  []string `json:"ruling_ids"`
		RequestKey string   `json:"idempotency_key"`
	}
	if !decodeJSONBody(w, r, &body) || !requireRulingRequestKey(w, body.RequestKey) {
		return
	}
	human := isHumanPrincipal(principal)
	status, payload, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "rulings.withdraw", principal.ActorID, body.RequestKey, body.RulingIDs, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		rulings, err := store.WithdrawOperatingRulings(ctx, principal.ActorID, human, body.RulingIDs)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(rulings))
		for _, ruling := range rulings {
			ids = append(ids, ruling.RulingID)
		}
		return map[string]any{"withdrawn": ids}, nil
	})
	switch {
	case errors.Is(err, primitives.ErrRulingNotYours):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, primitives.ErrConflict) && !errors.Is(err, primitives.ErrMutationReplayConflict):
		// The store names only rows visible through the caller's scope.
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeOperatingRulingResult(w, r, status, payload, err)
	}
}

func requireRulingRequestKey(w http.ResponseWriter, key string) bool {
	if strings.TrimSpace(key) == "" || len(key) > 256 || strings.ContainsRune(key, 0) {
		writeError(w, http.StatusBadRequest, "invalid_request", "a valid idempotency_key is required")
		return false
	}
	return true
}

func writeOperatingRulingResult(w http.ResponseWriter, r *http.Request, status int, payload map[string]any, err error) {
	var missing *primitives.RulingRelationsRequired
	switch {
	case err == nil:
		writeJSON(w, status, payload)
	case writePrimitiveQuotaViolationError(w, err):
	case errors.Is(err, primitives.ErrRulingPeerNotVisible):
		writeError(w, http.StatusConflict, "needs_relation", primitives.ErrRulingPeerNotVisible.Error())
	case errors.As(err, &missing):
		writeError(w, http.StatusConflict, "needs_relation", missing.Error())
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "ruling or relation target not found")
	case errors.Is(err, primitives.ErrInvalidWorkRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, primitives.ErrMutationReplayConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "request key conflicts with an existing receipt")
	case errors.Is(err, primitives.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "the ruling, alias, relation state or request key conflicts")
	default:
		writeReadInternalError(w, r, err, "cannot update operating ruling")
	}
}

func appendOperatingRulingDecisions(ctx context.Context, store PrimitiveStore, brief map[string]any) error {
	rulings, ok := store.(operatingRulingStore)
	if !ok {
		return nil
	}
	pending, partial, err := rulings.OperatingRulingDecisions(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	items, _ := brief["decisions"].([]map[string]any)
	items = append(items, pending...)
	primitives.SortBriefDecisions(items)
	if len(items) > primitives.BriefSectionLimit {
		items = items[:primitives.BriefSectionLimit]
		partial = true
	}
	if truncated, _ := brief["truncated"].(map[string]bool); partial && truncated != nil {
		truncated["decisions"] = true
	}
	brief["decisions"] = items
	return nil
}
