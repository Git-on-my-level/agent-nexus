package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

func resolveAwaitableAsk(w http.ResponseWriter, r *http.Request, opts handlerOptions, ref string) (string, string, bool) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		if strings.HasPrefix(strings.TrimSpace(ref), "access-request:") {
			writeError(w, 404, "not_found", "ask not found")
			return "", "", false
		}
		return strings.TrimPrefix(strings.TrimSpace(ref), "event:"), "", true
	}
	actorID, kind := "", ""
	if principal, ok := cachedAuthenticatedPrincipal(r); ok && principal != nil {
		actorID = principal.ActorID
		kind = principal.PrincipalKind
	}
	eventID, accessRef, err := store.ResolveAwaitableAsk(r.Context(), actorID, kind, ref)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "ask not found")
		} else {
			writeError(w, 500, "internal_error", "cannot read ask")
		}
		return "", "", false
	}
	return eventID, accessRef, true
}

type askOutcomeStore interface {
	AskOutcome(context.Context, string) (map[string]any, error)
}

func handleAskOutcome(w http.ResponseWriter, r *http.Request, opts handlerOptions, streaming bool) {
	prefix := "/asks/"
	if streaming {
		prefix = "/stream/asks/"
	}
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if id == "" || strings.Contains(id, "/") {
		writeError(w, 404, "not_found", "ask not found")
		return
	}
	id, accessRef, ok := resolveAwaitableAsk(w, r, opts, id)
	if !ok {
		return
	}
	store, ok := opts.primitiveStore.(askOutcomeStore)
	if !ok {
		writeError(w, 503, "primitives_unavailable", "ask outcomes unavailable")
		return
	}
	read := func() (map[string]any, error) {
		outcome, err := store.AskOutcome(primitives.WithReadTickSnapshot(r.Context()), id)
		if err != nil {
			return nil, err
		}
		if accessRef != "" {
			outcome["access_request_ref"] = accessRef
		}
		return outcome, nil
	}
	outcome, err := read()
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, 404, "not_found", "ask not found")
		} else {
			writeError(w, 500, "internal_error", "cannot read ask")
		}
		return
	}
	if !streaming {
		writeJSON(w, 200, outcome)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "stream unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(kind string, value any) bool {
		b, e := json.Marshal(value)
		if e != nil {
			return false
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
		_, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b)
		flusher.Flush()
		return e == nil
	}
	if !write("outcome", outcome) || outcome["status"] != "open" {
		return
	}
	tick := time.NewTicker(opts.streamPollInterval)
	defer tick.Stop()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-tick.C:
			outcome, err = read()
			if err != nil {
				write("error", map[string]any{"code": "not_found"})
				return
			}
			if !write("outcome", outcome) || outcome["status"] != "open" {
				return
			}
		}
	}
}
