package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

type rulingStore interface {
	CreateStandingRuling(ctx context.Context, actorID, askID, kind, answer string, now time.Time) (primitives.StandingRuling, error)
	StandingRulings(ctx context.Context, includeRevoked bool) ([]primitives.StandingRuling, error)
	RevokeStandingRuling(ctx context.Context, actorID, id string, now time.Time) (primitives.StandingRuling, error)
	RecordStandingRulingUse(ctx context.Context, id string, now time.Time) error
}

func rulingStoreFrom(w http.ResponseWriter, opts handlerOptions) (rulingStore, bool) {
	store, ok := opts.primitiveStore.(rulingStore)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "standing rulings store is not configured")
	}
	return store, ok
}

// handleListRulings returns at most primitives.StandingRulingListLimit
// rulings, newest first: one indexed page however large the workspace is.
func handleListRulings(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	store, ok := rulingStoreFrom(w, opts)
	if !ok {
		return
	}
	rulings, err := store.StandingRulings(r.Context(), r.URL.Query().Get("include_revoked") == "true")
	if err != nil {
		writeReadInternalError(w, r, err, "cannot list standing rulings")
		return
	}
	out := make([]map[string]any, 0, len(rulings))
	for _, ruling := range rulings {
		out = append(out, ruling.JSON())
	}
	writeJSON(w, http.StatusOK, map[string]any{"rulings": out})
}

// handleCreateRuling makes a human's answer to one ask durable. Only a human
// may: a ruling answers future asks in the human's name.
func handleCreateRuling(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireHumanPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := rulingStoreFrom(w, opts)
	if !ok {
		return
	}
	var body struct {
		AskRef string `json:"ask_ref"`
		Kind   string `json:"kind"`
		Answer string `json:"answer"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(body.AskRef), "event:") {
		writeError(w, http.StatusBadRequest, "invalid_request", "ask_ref must be event:<ask-id>")
		return
	}
	ruling, err := store.CreateStandingRuling(r.Context(), principal.ActorID, body.AskRef, strings.TrimSpace(body.Kind), body.Answer, time.Now())
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "ask not found")
	case errors.Is(err, primitives.ErrInvalidWorkRequest):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, primitives.ErrStandingRulingAskOpen):
		writeError(w, http.StatusConflict, "ask_open", err.Error())
	case err != nil:
		writeReadInternalError(w, r, err, "cannot create standing ruling")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"ruling": ruling.JSON()})
	}
}

func handleRevokeRuling(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	principal, ok := requireHumanPrincipal(w, r, opts)
	if !ok {
		return
	}
	store, ok := rulingStoreFrom(w, opts)
	if !ok {
		return
	}
	ruling, err := store.RevokeStandingRuling(r.Context(), principal.ActorID, id, time.Now())
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "ruling not found")
	case err != nil:
		writeReadInternalError(w, r, err, "cannot revoke standing ruling")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ruling": ruling.JSON()})
	}
}

// writeRulingApplied answers an ask a standing ruling covers. Nothing reaches
// Decide. An answer ruling returns the human's answer. A not_a_decision ruling
// turns the ask into an fyi on its subject, so the information still arrives
// where the human reads, and the response says where it landed.
//
// A retry with the same request key replays the first response: the use is
// counted once and the fyi is stored once.
func writeRulingApplied(w http.ResponseWriter, r *http.Request, opts handlerOptions, actorID, requestKey string, ask map[string]any, req any, ruling primitives.StandingRuling) {
	if strings.TrimSpace(anyString(ask["id"])) == "" && strings.HasPrefix(requestKey, "ruled:") {
		requestKey = ""
	}
	if status, payload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, requestKey, req); err == nil && replayed {
		writeJSON(w, status, payload)
		return
	}
	finish := func(response map[string]any) {
		if store, ok := opts.primitiveStore.(rulingStore); ok {
			if err := store.RecordStandingRulingUse(r.Context(), ruling.ID, time.Now()); err != nil {
				log.Printf("record standing ruling use failed (ruling=%s): %v", ruling.ID, err)
			}
		}
		status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "events.create", actorID, requestKey, req, http.StatusOK, response)
		if err != nil {
			status, payload = http.StatusOK, response
		}
		writeJSON(w, status, payload)
	}
	response := map[string]any{"ruling": ruling.JSON()}
	if ruling.Kind != primitives.RulingKindNotADecision {
		response["answered_by_ruling"] = true
		finish(response)
		return
	}
	response["redirected"] = "tell"
	payload, _ := ask["payload"].(map[string]any)
	title := strings.TrimSpace(anyString(payload["title"]))
	text := firstNonEmptyString(payload["situation"], payload["body"], title)
	event := map[string]any{
		"type": "message_posted", "thread_id": ask["thread_id"], "refs": ask["refs"], "summary": title, "provenance": ask["provenance"],
		"payload": map[string]any{"kind": "fyi", "title": title, "text": text, "body": text, "redirected_by_ruling": "ruling:" + ruling.ID},
	}
	stored, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, event)
	if err != nil {
		// The ruling still holds; say so without claiming the fyi was stored.
		log.Printf("store fyi for redirected ask failed (ruling=%s): %v", ruling.ID, err)
		response["redirected"] = "dropped"
	} else {
		response["event"] = stored
		notifyProjectionMaintainer(opts)
	}
	finish(response)
}

// handleRulingApplied counts an ask a ruling answered before it was filed.
// The CLI settles an ask that names no card against the rulings list, so that
// no card is created for a question that is never asked; this keeps the use
// count, which the human reads, true for that path too.
func handleRulingApplied(w http.ResponseWriter, r *http.Request, opts handlerOptions, id string) {
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	store, ok := rulingStoreFrom(w, opts)
	if !ok {
		return
	}
	switch err := store.RecordStandingRulingUse(r.Context(), id, time.Now()); {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "ruling not found")
	case err != nil:
		writeReadInternalError(w, r, err, "cannot record standing ruling use")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"recorded": true})
	}
}
