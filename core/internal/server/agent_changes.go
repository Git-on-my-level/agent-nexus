package server

import (
	"net/http"
	"sync"
	"time"
)

// agentChangeHub is an ephemeral, process-local invalidation signal. The
// canonical event log remains reserved for workspace evidence.
type agentChangeHub struct {
	mu       sync.Mutex
	revision uint64
	subs     map[chan uint64]struct{}
}

func newAgentChangeHub() *agentChangeHub {
	return &agentChangeHub{subs: map[chan uint64]struct{}{}}
}

func (h *agentChangeHub) subscribe() (<-chan uint64, func()) {
	ch := make(chan uint64, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	ch <- h.revision
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *agentChangeHub) publish() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.revision++
	for ch := range h.subs {
		select {
		case ch <- h.revision:
		default:
			<-ch
			ch <- h.revision
		}
	}
	h.mu.Unlock()
}

func handleAgentChangesStream(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.runStore == nil || opts.agentChanges == nil {
		writeError(w, http.StatusServiceUnavailable, "runs_unavailable", "agent roster is unavailable")
		return
	}
	changes, cancel := opts.agentChanges.subscribe()
	defer cancel()
	controller, flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming is not supported by this server")
		return
	}
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case revision := <-changes:
			if err := writeSSEEvent(controller, w, "", "agents_changed", map[string]any{"revision": revision}); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
			flushSSE(controller, flusher)
		case <-keepalive.C:
			if err := writeSSEKeepalive(controller, w); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
			flushSSE(controller, flusher)
		case <-r.Context().Done():
			return
		}
	}
}
