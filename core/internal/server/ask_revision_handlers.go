package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"errors"
	"net/http"
	"strings"
)

func handleAskPresentation(w http.ResponseWriter, r *http.Request, opts handlerOptions, ref string, revise bool) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		writeError(w, 503, "primitives_unavailable", "ask presentations unavailable")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !revise {
		result, err := store.GetAskPresentation(r.Context(), ref)
		if err != nil {
			askRevisionError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	var body map[string]any
	if !decodeJSONBody(w, r, &body) {
		return
	}
	for k := range body {
		if k != "if_revision" && k != "request_key" && k != "presentation" && k != "undo" {
			writeError(w, 400, "invalid_request", "immutable or unknown request field: "+k)
			return
		}
	}
	rev, ok := body["if_revision"].(float64)
	if !ok || rev < 0 || rev > 2147483646 || rev != float64(int(rev)) {
		writeError(w, 400, "invalid_request", "if_revision must be a non-negative presentation revision")
		return
	}
	key, ok := body["request_key"].(string)
	if !ok || key == "" || len(key) > 128 || strings.TrimSpace(key) != key {
		writeError(w, 400, "invalid_request", "request_key must contain 1 to 128 characters without surrounding whitespace")
		return
	}
	undo := false
	if v, present := body["undo"]; present {
		undo, ok = v.(bool)
		if !ok {
			writeError(w, 400, "invalid_request", "undo must be a boolean")
			return
		}
	}
	patch, patchOK := body["presentation"].(map[string]any)
	if _, present := body["presentation"]; present && (!patchOK || undo) {
		writeError(w, 400, "invalid_request", "presentation must be an object and cannot accompany undo")
		return
	}
	hash, err := hashJSONPayload(map[string]any{"ask_ref": ref, "body": body})
	if err != nil {
		askRevisionError(w, r, err)
		return
	}
	result, err := store.ReviseAskPresentation(r.Context(), *principal, ref, int(rev), key, hash, patch, undo)
	if err != nil {
		askRevisionError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
func askRevisionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, primitives.ErrNotFound), errors.Is(err, primitives.ErrForbidden):
		writeError(w, 404, "not_found", "ask not found")
	case errors.Is(err, auth.ErrAuthAdminRequired):
		writeError(w, 403, "forbidden", "only the requester, a human member or an auth-admin agent may revise an ask")
	case errors.Is(err, primitives.ErrInvalidWorkRequest):
		writeError(w, 400, "invalid_request", err.Error())
	case errors.Is(err, primitives.ErrConflict), errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded):
		writeError(w, 409, "conflict", "ask is closed, presentation changed, or request_key was reused; refresh before revising")
	default:
		if !writePrimitiveQuotaViolationError(w, err) {
			writeReadInternalError(w, r, err, "cannot revise ask presentation")
		}
	}
}

// Apply only to admitted inbox rows, in one bounded batch. The immutable source
// event, option set and routing metadata remain untouched.
func enrichAskPresentations(r *http.Request, opts handlerOptions, items []map[string]any) error {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		return nil
	}
	asks := []map[string]any{}
	targets := map[string][]map[string]any{}
	var add func(map[string]any)
	add = func(item map[string]any) {
		if ref := strings.TrimPrefix(anyString(item["request_event_ref"]), "event:"); ref != "" {
			asks = append(asks, map[string]any{"id": ref, "payload": item})
			targets[ref] = append(targets[ref], item)
		}
		if batch, ok := item["batch"].(map[string]any); ok {
			switch members := batch["asks"].(type) {
			case []map[string]any:
				for _, member := range members {
					add(member)
				}
			case []any:
				for _, member := range members {
					if m, ok := member.(map[string]any); ok {
						add(m)
					}
				}
			}
		}
	}
	for _, item := range items {
		add(item)
	}
	states, err := store.AskPresentationStates(r.Context(), asks)
	if err != nil {
		return err
	}
	for id, state := range states {
		for _, item := range targets[id] {
			primitives.ApplyAskPresentationState(item, state)
		}
	}
	return nil
}
