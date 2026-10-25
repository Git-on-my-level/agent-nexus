package server

import (
	"errors"
	"net/http"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func handleAskRetirement(w http.ResponseWriter, r *http.Request, opts handlerOptions, ref string) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		writeError(w, 503, "primitives_unavailable", "asks unavailable")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	var body map[string]any
	if !decodeJSONBody(w, r, &body) {
		return
	}
	for k := range body {
		if k != "request_key" && k != "reason" && k != "evidence" && k != "undo" && k != "if_retirement" {
			writeError(w, 400, "invalid_request", "unknown retirement field: "+k)
			return
		}
	}
	key, keyOK := body["request_key"].(string)
	reason, reasonOK := body["reason"].(string)
	retirement, retirementOK := body["if_retirement"].(string)
	undo, undoOK := body["undo"].(bool)
	evidence := []string{}
	valid := keyOK
	if _, present := body["undo"]; present && !undoOK {
		valid = false
	}
	if _, present := body["reason"]; present && (!reasonOK || undo) {
		valid = false
	}
	if _, present := body["if_retirement"]; present && !retirementOK {
		valid = false
	}
	if value, present := body["evidence"]; present {
		list, ok := value.([]any)
		if !ok || undo {
			valid = false
		}
		for _, v := range list {
			s, ok := v.(string)
			if !ok {
				valid = false
			}
			evidence = append(evidence, s)
		}
	}
	if !valid {
		writeError(w, 400, "invalid_request", "invalid retirement request fields")
		return
	}
	hash, err := hashJSONPayload(map[string]any{"ask_ref": ref, "body": body})
	if err != nil {
		writeReadInternalError(w, r, err, "cannot retire ask")
		return
	}
	result, err := store.RetireAsk(r.Context(), *principal, ref, key, hash, reason, evidence, undo, retirement)
	if err != nil {
		switch {
		case errors.Is(err, primitives.ErrNotFound), errors.Is(err, primitives.ErrForbidden):
			writeError(w, 404, "not_found", "ask not found")
		case errors.Is(err, auth.ErrAuthAdminRequired):
			writeError(w, 403, "forbidden", "only the requester, a human member or an auth-admin agent may retire an ask")
		case errors.Is(err, primitives.ErrInvalidWorkRequest), errors.Is(err, primitives.ErrInvalidBoardRequest):
			writeError(w, 400, "invalid_request", err.Error())
		case errors.Is(err, primitives.ErrConflict), errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded):
			writeError(w, 409, "conflict", "ask is no longer open, retirement changed, or request_key was reused; refresh before retrying")
		default:
			if !writePrimitiveQuotaViolationError(w, err) {
				writeReadInternalError(w, r, err, "cannot retire ask")
			}
		}
		return
	}
	notifyProjectionMaintainer(opts)
	writeJSON(w, 200, result)
}
