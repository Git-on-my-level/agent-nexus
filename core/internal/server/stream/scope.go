package stream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/scopestream"
)

// ScopeOptions is trusted dispatcher configuration. Nil Ready keeps the new
// reader disabled. Registration and the complete semantic cutover are A-owned.
type ScopeOptions struct {
	Repository   scopestream.Repository
	Tokens       *scopestream.Tokens
	Ready        func() bool
	Selection    func(*http.Request) (scopestream.Request, error)
	PollInterval time.Duration
}

// ScopeHandler emits prefix-safe encrypted IDs and constant idle keepalives.
// It must be registered through Mount, with the regular stream authentication
// dispatcher. It never serializes repository or maintenance errors.
func ScopeHandler(opts ScopeOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if opts.Ready == nil || !opts.Ready() || opts.Repository == nil || opts.Tokens == nil || opts.Selection == nil {
			http.Error(w, "scope stream unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		req, err := opts.Selection(r)
		if err != nil {
			http.Error(w, "selection unavailable", http.StatusNotFound)
			return
		}
		req.Continuation = r.Header.Get("Last-Event-ID")
		// Authorize and validate before committing streaming response headers.
		page, err := scopestream.Tick(r.Context(), opts.Repository, opts.Tokens, req)
		if err != nil {
			http.Error(w, "scope stream unavailable", http.StatusConflict)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unavailable", http.StatusInternalServerError)
			return
		}
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Time{})
		w.Header().Set("Content-Type", "text/event-stream")
		interval := opts.PollInterval
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if len(page.Items) == 0 {
				if _, err = fmt.Fprint(w, ": keepalive\n\n"); err != nil {
					return
				}
			} else {
				for _, item := range page.Items {
					var buffer bytes.Buffer
					encoder := json.NewEncoder(&buffer)
					encoder.SetEscapeHTML(false)
					err := encoder.Encode(map[string]any{"scope_id": item.Record.Key.Stream.Scope, "family": item.Record.Key.Stream.Family, "rid": item.Record.Key.Head.RID, "version": item.Record.Key.Head.Version, "payload": item.Record.Payload})
					if err != nil {
						return
					}
					// Marshaling JSON ensures source newlines cannot create frames.
					if strings.ContainsAny(item.Continuation, "\r\n") {
						return
					}
					data := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
					if _, err = fmt.Fprintf(w, "id: %s\nevent: change\ndata: %s\n\n", item.Continuation, data); err != nil {
						return
					}
				}
			}
			flusher.Flush()
			_ = controller.SetWriteDeadline(time.Time{})
			req.Continuation = page.Continuation
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
			page, err = scopestream.Tick(r.Context(), opts.Repository, opts.Tokens, req)
			if err != nil {
				_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
				_, _ = fmt.Fprint(w, "event: reset\ndata: {}\n\n")
				flusher.Flush()
				return
			}
		}
	})
}
