package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func handleAskWakeStream(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isAgentPrincipal(principal) {
		writeError(w, 403, "agent_required", "agent identity required")
		return
	}
	store, ok := opts.primitiveStore.(interface {
		AskWakePage(context.Context, string, int64) (primitives.AskWakePage, error)
		AskWakeSnapshotStart(context.Context, string, string) (int64, string, string, error)
		AskWakeSnapshotPage(context.Context, string, string, string) ([]primitives.AgentWakeup, string, string, bool, error)
	})
	if !ok {
		writeError(w, 503, "primitives_unavailable", "wake stream unavailable")
		return
	}
	cursor, at, id, err := store.AskWakeSnapshotStart(primitives.WithReadTickSnapshot(r.Context()), principal.ActorID, r.Header.Get("Last-Event-ID"))
	if err != nil {
		writeError(w, 500, "internal_error", "cannot read wake cursor")
		return
	}
	snapshot := r.Header.Get("Last-Event-ID") == ""

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, 500, "stream_unsupported", "stream unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher.Flush()
	ticker := time.NewTicker(opts.streamPollInterval)
	defer ticker.Stop()
	for r.Context().Err() == nil {
		var page primitives.AskWakePage
		if snapshot {
			page.Wakeups, at, id, page.More, err = store.AskWakeSnapshotPage(primitives.WithReadTickSnapshot(r.Context()), principal.ActorID, at, id)
			page.Cursor = cursor
			if !page.More {
				snapshot = false
				page.More = true
			}
		} else {
			page, err = store.AskWakePage(primitives.WithReadTickSnapshot(r.Context()), principal.ActorID, cursor)
		}
		if err != nil {
			return
		}
		cursor = page.Cursor
		for _, wake := range page.Wakeups {
			b, _ := json.Marshal(map[string]any{"receipt": agentNotificationFromWakeup(wake).toMap()})
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
			if token := page.Tokens[wake.WakeupID]; token != "" {
				if _, err = fmt.Fprintf(w, "id: %s\n", token); err != nil {
					return
				}
			}
			if _, err = fmt.Fprintf(w, "event: notification_receipt\ndata: %s\n\n", b); err != nil {
				return
			}
		}
		if len(page.Wakeups) > 0 {
			flusher.Flush()
		}
		if page.More {
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
