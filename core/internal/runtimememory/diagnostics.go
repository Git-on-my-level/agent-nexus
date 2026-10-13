package runtimememory

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Diagnostics struct {
	server *http.Server
	done   chan struct{}
	once   sync.Once
}

// StartDiagnostics never installs routes on the workspace mux or DefaultServeMux.
// The bind address cannot be configured: the only listener is IPv4 loopback.
func StartDiagnostics(enabled bool, port int, log io.Writer) (*Diagnostics, error) {
	if !enabled {
		return nil, nil
	}
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid memory diagnostic port")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	d := &Diagnostics{done: make(chan struct{})}
	d.server = &http.Server{
		Handler:           diagnosticHandler(listener.Addr().String()),
		ReadHeaderTimeout: 3 * time.Second, WriteTimeout: 15 * time.Second,
		IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8 << 10,
	}
	go func() { defer close(d.done); _ = d.server.Serve(listener) }()
	fmt.Fprintf(log, "memory diagnostics: loopback-only http://%s (/memory, /heap)\n", listener.Addr())
	return d, nil
}

func (d *Diagnostics) Close() {
	if d != nil {
		d.once.Do(func() { _ = d.server.Close(); <-d.done })
	}
}

func diagnosticHandler(host string) http.Handler {
	// Bound profiling to one operator request at a time; callers cannot force GC
	// or ask for a textual dump or arbitrary runtime profiles.
	busy := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(peer)
		if err != nil || ip == nil || !ip.IsLoopback() || r.Host != host || len(r.Header.Values("Origin")) != 0 || len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("Cookie")) != 0 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		for name := range r.Header {
			if strings.EqualFold(name, "Forwarded") || strings.HasPrefix(strings.ToLower(name), "x-forwarded-") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "none" && site != "same-origin" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "query not allowed", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/memory":
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			w.Header().Set("Content-Type", "application/json")
			// Explicit numeric whitelist: no environment, arguments, goroutines,
			// objects, labels, request URLs, credentials, or workspace identifiers.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"heap_alloc": m.HeapAlloc, "heap_inuse": m.HeapInuse,
				"heap_sys": m.HeapSys, "heap_idle": m.HeapIdle, "heap_released": m.HeapReleased,
				"stack_inuse": m.StackInuse, "sys": m.Sys, "gc_target": m.NextGC,
				"total_alloc": m.TotalAlloc, "num_gc": m.NumGC,
				"go_soft_limit": debug.SetMemoryLimit(-1), "goroutines": runtime.NumGoroutine(),
			})
		case "/heap":
			select {
			case busy <- struct{}{}:
				defer func() { <-busy }()
			default:
				http.Error(w, "busy", http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			// Binary heap samples contain allocation sizes and code stacks, never
			// heap object bytes. Do not mount net/http/pprof's broader debug surface.
			_ = pprof.WriteHeapProfile(w)
		default:
			http.NotFound(w, r)
		}
	})
}
