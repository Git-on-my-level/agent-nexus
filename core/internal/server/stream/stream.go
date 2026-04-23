package stream

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// Prefix is the single URL prefix under which all long-lived / streaming
// endpoints (WebSocket, SSE, chunked streaming) MUST be served.
//
// Hosted edge routing bypasses the control plane for paths under this prefix,
// forwarding directly to anx-core.
const Prefix = "/stream/"

var activeConnections atomic.Int64

// Mount registers a streaming handler at Prefix + sub.
// sub must be a relative path, e.g. "events" => "/stream/events".
func Mount(mux *http.ServeMux, sub string, handler http.Handler) {
	if mux == nil {
		panic("stream.Mount: mux must not be nil")
	}
	sub = strings.TrimSpace(sub)
	if sub == "" || sub[0] == '/' {
		panic("stream.Mount: sub must be a non-empty relative path")
	}
	if handler == nil {
		panic("stream.Mount: handler must not be nil")
	}
	mux.Handle(Prefix+sub, Wrap(handler))
}

// Wrap marks handler execution as an active stream connection for heartbeat snapshots.
func Wrap(handler http.Handler) http.Handler {
	if handler == nil {
		panic("stream.Wrap: handler must not be nil")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeConnections.Add(1)
		defer activeConnections.Add(-1)
		handler.ServeHTTP(w, r)
	})
}

// Snapshot returns current active stream connection count.
func Snapshot() int {
	n := activeConnections.Load()
	if n <= 0 {
		return 0
	}
	return int(n)
}
