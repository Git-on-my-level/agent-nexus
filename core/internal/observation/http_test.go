package observation

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testHTTPConfig(t *testing.T, h http.HandlerFunc) (HTTPConfig, *httptest.Server) {
	t.Helper()
	s := httptest.NewTLSServer(h)
	t.Cleanup(s.Close)
	roots := x509.NewCertPool()
	roots.AddCert(s.Certificate())
	return HTTPConfig{BaseURL: s.URL, WorkspaceID: "w", ConnectionID: "c", SourceWorkspaceID: "source-w", RootCAs: roots, AllowedNetworks: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, MaxPages: 2, MaxBytes: 65536, Timeout: time.Second}, s
}
func TestGitHubSelectedTargetAndBoundedPagination(t *testing.T) {
	calls := 0
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Errorf("write method %s", r.Method)
		}
		switch r.URL.Path {
		case "/repos/o/r/issues/1":
			fmt.Fprint(w, `{"number":1,"title":"Fixture","state":"unfamiliar","updated_at":"2026-09-07T00:00:00Z","html_url":"https://github.com/o/r/issues/1","comments":101}`)
		case "/repos/o/r/issues/1/comments":
			w.Header().Set("Link", `<https://unapproved.invalid/private>; rel="next"`)
			fmt.Fprint(w, `[{"id":1,"html_url":"https://github.com/o/r/issues/1#issuecomment-1","updated_at":"2026-09-07T00:00:00Z"}]`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	reader, err := NewGitHubReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "o/r", NativeID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.NativeStatus != "unfamiliar" || got.Coverage.Complete || got.Coverage.NextCursor == "" || calls != 2 {
		t.Fatalf("wrong report/calls: %+v %d", got, calls)
	}
	if got.SourceActivityAt == nil || got.SourceActivityAt.Equal(got.ObservedAt) {
		t.Fatal("read clock became activity")
	}
}
func TestHTTPFailsClosedAndSanitizesFailures(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://unapproved.invalid")
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(status)
				fmt.Fprint(w, "fixture-private-body")
			})
			reader, err := NewGitHubReader(cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "o/r", NativeID: "1"})
			var re *ReadError
			if !errors.As(err, &re) || strings.Contains(err.Error(), "fixture-private-body") {
				t.Fatalf("unsanitized error %v", err)
			}
			if status == 429 && re.RetryAfter != 120*time.Second {
				t.Fatalf("retry hint lost: %+v", re)
			}
		})
	}
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unapproved network reached") })
	cfg.AllowedNetworks = nil
	reader, _ := NewGitHubReader(cfg)
	if _, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "o/r", NativeID: "1"}); err == nil {
		t.Fatal("private address allowed by default")
	}
}
func TestMulticaPreservesNativeStateAndRunClaims(t *testing.T) {
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Workspace-ID") != "source-w" {
			t.Error("source workspace binding missing")
		}
		switch r.URL.Path {
		case "/api/issues/SCA-1":
			fmt.Fprint(w, `{"id":"fixture-id","identifier":"SCA-1","workspace_id":"source-w","title":"Fixture","status":"custom-review","revision":7,"updated_at":"2026-09-07T00:00:00Z"}`)
		case "/api/issues/fixture-id/task-runs":
			fmt.Fprint(w, `[{"id":"run-1","status":"completed","issue_id":"fixture-id"}]`)
		default:
			w.WriteHeader(404)
		}
	})
	reader, err := NewMulticaReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(context.Background(), Target{WorkspaceID: "w", ConnectionID: "c", Source: "multica", Kind: "issue", NativeID: "SCA-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.NativeStatus != "custom-review" || got.Knowledge != "reported" || got.Facts["phase"] != "unknown" {
		t.Fatalf("run success changed work status: %+v", got)
	}
}
func TestTargetCannotChooseUnregisteredConnectionOrPath(t *testing.T) {
	cfg, _ := testHTTPConfig(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid target reached source") })
	reader, _ := NewGitHubReader(cfg)
	target := Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "o/../r", NativeID: "1"}
	if _, err := reader.Read(context.Background(), target); err == nil {
		t.Fatal("path traversal accepted")
	}
	target.Repository = "o/r"
	target.ConnectionID = "other"
	if _, err := reader.Read(context.Background(), target); err == nil {
		t.Fatal("connection crossing accepted")
	}
}

func TestHTTPSRejectsSpecialUseDestinationsWithoutExplicitApproval(t *testing.T) {
	source := &httpSource{}
	for _, address := range []string{"100.64.0.1", "198.18.0.1", "192.0.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "::ffff:127.0.0.1"} {
		if source.allowedAddress(netip.MustParseAddr(address).Unmap()) {
			t.Errorf("special-use destination allowed: %s", address)
		}
	}
	source.config.AllowedNetworks = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10")}
	if !source.allowedAddress(netip.MustParseAddr("100.64.0.1")) {
		t.Fatal("approved Tailnet range rejected")
	}
	source.config.AllowedNetworks = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	if source.allowedAddress(netip.MustParseAddr("169.254.169.254")) {
		t.Fatal("metadata address allowed by broad approval")
	}
}
