package app

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-cli/internal/hostidentity"
)

func TestRulingsUnresolvedSecondaryIdentityFailsClosed(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"source_revision":"fixture","rulings":[]}`)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("unresolved identity made a secondary request")
	}))
	defer secondary.Close()
	home, root := t.TempDir(), t.TempDir()
	seedRulingEnrollment(t, home, "secondary", secondary.URL)
	env := map[string]string{"AGENTCTL_AUTHORITY": "fixture-incomplete"}
	file := writeRulingReviewFile(t, root, "refs.md", "ruling: R-20261127-01")
	code, out, stderr := runRulingCLI(t, home, root, primary.URL, env, "rulings", "check", file)
	if code != 2 || out != "" || !strings.Contains(stderr, "rulings_index_unavailable") || !strings.Contains(stderr, secondary.URL) {
		t.Fatalf("check used incomplete coverage: %d %q %q", code, out, stderr)
	}
	code, _, stderr = runRulingCLI(t, home, root, primary.URL, env, "rulings", "pull")
	if code == 0 || !strings.Contains(stderr, secondary.URL) {
		t.Fatalf("pull claimed complete coverage: %d %q", code, stderr)
	}
	code, _, stderr = runRulingCLI(t, home, root, primary.URL, env, "rulings", "list")
	if code != 0 || !strings.Contains(stderr, "rulings_projection_unavailable") || !strings.Contains(stderr, secondary.URL) {
		t.Fatalf("list hid missing coverage: %d %q", code, stderr)
	}
}

func TestRulingsEquivalentWorkspaceBaseFetchedOnce(t *testing.T) {
	var projectionReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			t.Error("selected workspace was opened a second time with host credentials")
		case "/rulings/projection":
			projectionReads.Add(1)
			_, _ = fmt.Fprint(w, `{"source_revision":"fixture","rulings":[{"ruling_id":"R-20261127-01","kind":"operating","scope":"account","active":true,"question":"One workspace"}]}`)
		default:
			_, _ = fmt.Fprint(w, `{"rulings":[]}`)
		}
	}))
	defer srv.Close()
	home, root := t.TempDir(), t.TempDir()
	seedRulingEnrollment(t, home, "workspace", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	base := strings.Replace(srv.URL, "127.0.0.1", "LOCALHOST", 1)
	code, out, stderr := runRulingCLI(t, home, root, base, nil, "--as", "reader", "rulings", "show", "R-20261127-01")
	if code != 0 || projectionReads.Load() != 1 || !strings.Contains(out, "One workspace") || strings.Contains(stderr, "rulings_projection_unavailable") {
		t.Fatalf("same workspace was treated as two: %d %q %q (%d reads)", code, out, stderr, projectionReads.Load())
	}
}

func TestRulingsPullRequiresOperatingProjection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rulings/projection" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprint(w, `{"rulings":[]}`)
	}))
	defer srv.Close()
	code, out, stderr := runRulingCLI(t, t.TempDir(), t.TempDir(), srv.URL, nil, "rulings", "pull")
	if code != 2 || out != "" || !strings.Contains(stderr, "rulings_index_unavailable") {
		t.Fatalf("legacy pull claimed an operating projection: %d %q %q", code, out, stderr)
	}
}

func TestRulingsLegacyPagesReuseShortCacheAndProbeProjection(t *testing.T) {
	var projectionReads, legacyReads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rulings/projection" {
			projectionReads.Add(1)
			http.NotFound(w, r)
			return
		}
		legacyReads.Add(1)
		_, _ = fmt.Fprint(w, `{"rulings":[{"ref":"ruling:answer-one","kind":"answer","active":true,"question":"Legacy answer"}]}`)
	}))
	defer srv.Close()
	home, root := t.TempDir(), t.TempDir()
	for range 2 {
		code, out, _ := runRulingCLI(t, home, root, srv.URL, nil, "rulings", "list")
		if code != 0 || !strings.Contains(out, "Legacy answer") {
			t.Fatalf("legacy list: %d %q", code, out)
		}
	}
	if projectionReads.Load() != 2 || legacyReads.Load() != 2 {
		t.Fatalf("wanted one probe per read and shared legacy pages, got %d and %d", projectionReads.Load(), legacyReads.Load())
	}
	code, _, stderr := runRulingCLI(t, home, root, srv.URL, map[string]string{"ANX_RULINGS_TTL": "0s"}, "rulings", "list")
	if code != 0 || projectionReads.Load() != 3 || legacyReads.Load() != 4 {
		t.Fatalf("zero TTL failed to refresh old pages: %d %q", code, stderr)
	}
}

func TestRulingsLegacyAliasConflictIsIsolated(t *testing.T) {
	var collision atomic.Bool
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rulings/projection":
			aliases := "[]"
			if collision.Load() {
				aliases = `["ruling_old123"]`
			}
			_, _ = fmt.Fprintf(w, `{"source_revision":"fixture","rulings":[{"ruling_id":"R-20261127-01","aliases":%s,"kind":"operating","scope":"workspace","active":true,"question":"Operating rule"}]}`, aliases)
		case "/rulings/propose":
			writes.Add(1)
		default:
			_, _ = fmt.Fprint(w, `{"rulings":[{"ref":"ruling:ruling_old123","kind":"answer","question":"Legacy answer"}]}`)
		}
	}))
	defer srv.Close()
	home, root := t.TempDir(), t.TempDir()
	body := writeRulingReviewFile(t, root, "body.md", "Independent")
	propose := []string{"rulings", "propose", "--id-date", "R-20261127-02", "--scope", "workspace", "--topic", "test", "--body-file", body, "--quote", "Independent", "--alias", "ruling_old123"}
	code, _, stderr := runRulingCLI(t, home, root, srv.URL, nil, propose...)
	if code != 4 || !strings.Contains(stderr, "existing answer ruling") || writes.Load() != 0 {
		t.Fatalf("legacy name not reserved: %d %q", code, stderr)
	}
	collision.Store(true)
	env := map[string]string{"ANX_RULINGS_TTL": "0s"}
	code, out, stderr := runRulingCLI(t, home, root, srv.URL, env, "rulings", "list")
	if code != 0 || !strings.Contains(out, "Operating rule") || !strings.Contains(stderr, "ruling_name_collision") {
		t.Fatalf("existing collision broke list or disappeared: %d %q %q", code, out, stderr)
	}
	code, _, stderr = runRulingCLI(t, home, root, srv.URL, env, "rulings", "show", "ruling_old123")
	if code != 4 || !strings.Contains(stderr, "ruling_old123") {
		t.Fatalf("ambiguous name resolved: %d %q", code, stderr)
	}
	code, out, _ = runRulingCLI(t, home, root, srv.URL, env, "rulings", "show", "R-20261127-01")
	if code != 0 || !strings.Contains(out, "Operating rule") {
		t.Fatalf("unambiguous name was blocked: %d %q", code, out)
	}
}

func TestRulingsReproposeAfterTerminalStatus(t *testing.T) {
	for _, status := range []string{"revoked", "superseded", "proposed"} {
		t.Run(status, func(t *testing.T) {
			reqs := make(chan rulingWrite, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/rulings/projection":
					_, _ = fmt.Fprintf(w, `{"source_revision":"fixture","rulings":[{"ruling_id":"R-20261127-01","kind":"operating","scope":"workspace","topic":"test","body":"Routing","source_quote":"Quote","question":"Routing","status":%q}]}`, status)
				case "/rulings/propose":
					var proposal rulingWrite
					if err := json.NewDecoder(r.Body).Decode(&proposal); err != nil {
						t.Error(err)
					}
					reqs <- proposal
					w.WriteHeader(201)
					_, _ = fmt.Fprintf(w, `{"ruling":{"ruling_id":%q,"kind":"operating","status":"proposed"}}`, proposal.ID)
				default:
					_, _ = fmt.Fprint(w, `{"rulings":[]}`)
				}
			}))
			defer srv.Close()
			home, root := t.TempDir(), t.TempDir()
			file := writeRulingReviewFile(t, root, "body.md", "Routing")
			code, _, stderr := runRulingCLI(t, home, root, srv.URL, nil, "rulings", "propose", "--id-date", "2026-11-27", "--scope", "workspace", "--topic", "test", "--body-file", file, "--quote", "Quote")
			if code != 0 || len(reqs) != 1 {
				t.Fatalf("expected proposal: %d %q", code, stderr)
			}
			want := "R-20261127-02"
			if status == "proposed" {
				want = "R-20261127-01"
			}
			if got := (<-reqs).ID; got != want {
				t.Fatalf("selected ID %s, want %s", got, want)
			}
		})
	}
}

func TestRulingsProposeRuneLimits(t *testing.T) {
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			writes.Add(1)
			w.WriteHeader(201)
			_, _ = fmt.Fprint(w, `{"ruling":{"ruling_id":"R-20261127-01","kind":"operating","status":"proposed"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"source_revision":"fixture","rulings":[]}`)
	}))
	defer srv.Close()
	home, root := t.TempDir(), t.TempDir()
	run := func(body string) (int, string) {
		file := writeRulingReviewFile(t, root, "body.md", body)
		code, _, stderr := runRulingCLI(t, home, root, srv.URL, nil, "rulings", "propose", "--id-date", "R-20261127-01", "--scope", "workspace", "--topic", "test", "--title", strings.Repeat("😀", 1024), "--quote", strings.Repeat("😀", 8192), "--body-file", file)
		return code, stderr
	}
	if code, stderr := run(strings.Repeat("😀", 32768)); code != 0 || writes.Load() != 1 {
		t.Fatalf("valid rune lengths rejected: %d %q", code, stderr)
	}
	if code, stderr := run(strings.Repeat("é", 32769)); code != 2 || !strings.Contains(stderr, "invalid_ruling_proposal") || writes.Load() != 1 {
		t.Fatalf("over-limit body passed local validation: %d %q", code, stderr)
	}
	entry := reviewImportEntries(1)[0]
	entry.Title, entry.Source = strings.Repeat("😀", 1024), strings.Repeat("😀", 2048)
	index, err := json.Marshal(map[string]any{"schema_version": 1, "generated_from": []string{"source"}, "rulings": []rulingIndexEntry{entry}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRulingIndex(index, 1)
	if err != nil {
		t.Fatalf("valid rune lengths rejected by index decoder: %v", err)
	}
	if _, err := decoded.importRows(); err != nil {
		t.Fatalf("valid rune lengths rejected by import: %v", err)
	}
}

func seedRulingEnrollment(t *testing.T, home, id, base string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	host := hostidentity.Host{ID: "host-" + id, KeyID: "key-" + id, WorkspaceID: id, BaseURL: base}
	if err := hostidentity.SaveAt(filepath.Join(home, ".config", "anx"), host, key); err != nil {
		t.Fatal(err)
	}
}
