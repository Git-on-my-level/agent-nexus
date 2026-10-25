package runtimememory

import (
	"net"
	"net/http"
	"testing"
)

func TestPprofExplicitAddress(t *testing.T) {
	if d, err := StartPprof("", false); d != nil || err != nil {
		t.Fatalf("disabled: %v %v", d, err)
	}
	for _, addr := range []string{":6060", "0.0.0.0:6060", "[::]:6060", "192.0.2.1:6060", "localhost:6060", "invalid"} {
		if d, err := StartPprof(addr, false); err == nil {
			d.Close()
			t.Fatalf("accepted %s", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "0.0.0.0:0"} {
		d, err := StartPprof(addr, addr == "0.0.0.0:0")
		if err != nil {
			t.Fatal(err)
		}
		d.Close()
	}
}

func TestPprofDedicatedMux(t *testing.T) {
	// No handler on the workspace/default mux. Importing net/http/pprof does
	// register default routes, so core must always use its explicit server mux.
	d, err := StartPprof("127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: d.server.Handler}
	go server.Serve(listener)
	defer server.Close()
	base := "http://" + listener.Addr().String()
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine?debug=0", "/debug/pprof/heap"} {
		r, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("%s: %d", path, r.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", base+"/debug/pprof/", nil)
	req.Header.Set("Origin", "https://example.com")
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal(r.StatusCode)
	}
}
