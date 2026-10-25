package runtimememory

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"
)

// StartPprof uses a dedicated mux and is disabled for an empty address. Only
// literal loopback IPs are accepted unless the operator explicitly opts out.
func StartPprof(addr string, allowNonLoopback bool) (*Diagnostics, error) {
	if addr == "" {
		return nil, nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid debug pprof address: %w", err)
	}
	ip := net.ParseIP(host)
	if !allowNonLoopback && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("debug pprof requires a literal loopback address unless ANX_DEBUG_PPROF_ALLOW_NON_LOOPBACK=true")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	d := &Diagnostics{done: make(chan struct{})}
	d.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" || strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	}), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 10 * time.Second}
	go func() { defer close(d.done); _ = d.server.Serve(listener) }()
	return d, nil
}
