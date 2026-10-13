package runtimememory

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiagnosticBoundary(t *testing.T) {
	h := diagnosticHandler("127.0.0.1:6061")
	for _, tc := range []struct{ name, peer, host, header, value string }{
		{"public-peer", "192.0.2.1:42", "127.0.0.1:6061", "", ""},
		{"forwarded-loopback", "192.0.2.1:42", "127.0.0.1:6061", "X-Forwarded-For", "127.0.0.1"},
		{"proxy", "127.0.0.1:42", "127.0.0.1:6061", "Forwarded", "for=192.0.2.1"},
		{"proxy-host", "127.0.0.1:42", "127.0.0.1:6061", "X-Forwarded-Host", "workspace.example"},
		{"workspace-bearer", "127.0.0.1:42", "127.0.0.1:6061", "Authorization", "Bearer workspace-user"},
		{"workspace-cookie", "127.0.0.1:42", "127.0.0.1:6061", "Cookie", "workspace-session=synthetic"},
		{"browser", "127.0.0.1:42", "127.0.0.1:6061", "Origin", "https://workspace.example"},
		{"navigation", "127.0.0.1:42", "127.0.0.1:6061", "Sec-Fetch-Site", "cross-site"},
		{"rebinding", "127.0.0.1:42", "workspace.example:6061", "", ""},
		{"bad-peer", "malformed", "127.0.0.1:6061", "", ""},
	} {
		for _, path := range []string{"/memory", "/heap"} {
			t.Run(tc.name+path, func(t *testing.T) {
				r := httptest.NewRequest("GET", "http://127.0.0.1:6061"+path, nil)
				r.RemoteAddr = tc.peer
				r.Host = tc.host
				if tc.header != "" {
					r.Header.Set(tc.header, tc.value)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "heap_") {
					t.Fatalf("diagnostic disclosed to %s: %d", tc.name, w.Code)
				}
			})
		}
	}
	for _, path := range []string{"/debug/pprof/goroutine", "/heap?debug=1", "/heap?gc=1", "/memory?debug=1"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:6061"+path, nil)
		r.RemoteAddr = "127.0.0.1:42"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code == 200 {
			t.Fatalf("unbounded debug path accepted: %s", path)
		}
	}
}

func TestDiagnosticDefaultOffAndLoopback(t *testing.T) {
	var log bytes.Buffer
	if d, err := StartDiagnostics(false, -1, &log); d != nil || err != nil || log.Len() != 0 {
		t.Fatal("disabled diagnostic opened listener")
	}
	d, err := StartDiagnostics(true, 0, &log)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// The real listener must remain loopback even when the workspace API uses
	// 0.0.0.0. No workspace token or policy is accepted by this separate server.
	addr := strings.Split(strings.TrimSpace(log.String()), "http://")[1]
	addr = strings.Split(addr, " ")[0]
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("diagnostic not bound to loopback")
	}
	resp, err := http.Get("http://" + addr + "/memory")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stats map[string]json.Number
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" || json.NewDecoder(resp.Body).Decode(&stats) != nil || len(stats) != 12 {
		t.Fatal("invalid numeric summary")
	}
	for _, value := range stats {
		if _, err := value.Float64(); err != nil {
			t.Fatal("nonnumeric data in diagnostic")
		}
	}
	profile, err := http.Get("http://" + addr + "/heap")
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Body.Close()
	gz, err := gzip.NewReader(profile.Body)
	if err != nil {
		t.Fatal("not a binary pprof profile")
	}
	defer gz.Close()
	if raw, err := io.ReadAll(gz); err != nil || len(raw) == 0 {
		t.Fatal("empty heap profile")
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:6061/memory", nil)
	_, pattern := http.DefaultServeMux.Handler(r)
	if pattern != "" {
		t.Fatal("diagnostic registered on shared default mux")
	}
}
