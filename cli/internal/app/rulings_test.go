package app

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/hostidentity"
)

func rulingFixture(t *testing.T, file string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata/rulings", file))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestRulingsCheckGolden(t *testing.T) {
	index, err := decodeRulingIndex(rulingFixture(t, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]map[string]any{}
	for _, row := range index {
		data := map[string]any{"ruling_id": row.ID, "kind": "operating", "status": row.Status, "superseded_by": row.SupersededBy}
		for _, name := range append([]string{row.ID}, row.Aliases...) {
			names[name] = data
		}
	}
	scanner := newRulingReferenceScanner(names)
	var out bytes.Buffer
	for _, f := range scanner.scan("fixture.md", string(rulingFixture(t, "surface.md")), names, false) {
		out.WriteString(f.String() + "\n")
	}
	if expected := rulingFixture(t, "findings.txt"); !bytes.Equal(out.Bytes(), expected) {
		t.Fatalf("findings:\n%s\nwant:\n%s", out.Bytes(), expected)
	}
	f := scanner.scan("strict", "ruling: R41, then R41 and R41.3; PR17 and prefix-R17", names, true)
	if len(f) != 2 || f[0].Token != "R41" || f[1].Token != "R41.3" {
		t.Fatalf("strict or compound handling: %+v", f)
	}
}

func runRulingCLI(t *testing.T, home, cwd, base string, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	a := New()
	a.Stdin, a.Stdout, a.Stderr = bytes.NewReader(nil), &stdout, &stderr
	a.Getenv = func(key string) string {
		if key == "HOME" {
			return home
		}
		if key == "ANX_ACCESS_TOKEN" && env[key] == "" {
			return "fixture-bearer"
		}
		return env[key]
	}
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getwd = func() (string, error) { return cwd, nil }
	return a.Run(append([]string{"--base-url", base, "--timeout", "1s"}, args...)), stdout.String(), stderr.String()
}

func TestRulingsProjectionAliasRefreshAndOffline(t *testing.T) {
	var reads atomic.Int32
	var superseded atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rulings/projection":
			reads.Add(1)
			ruling := map[string]any{"ruling_id": "R-20261126-08", "aliases": []string{"R41"}, "question": "Host may take new work", "kind": "operating", "status": "active", "active": true, "scope": "account"}
			if superseded.Load() {
				ruling["status"], ruling["active"], ruling["superseded_by"] = "superseded", false, "R-20261127-01"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"rulings": []any{ruling}, "source_revision": "fixture"})
		case "/rulings":
			_, _ = w.Write([]byte(`{"rulings":[]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	home, cwd := t.TempDir(), t.TempDir()
	defer server.Close()
	base := server.URL
	code, show, stderr := runRulingCLI(t, home, cwd, base, nil, "rulings", "show", "R41")
	if code != 0 || !strings.Contains(show, "R-20261126-08") || stderr != "" {
		t.Fatalf("show exit=%d stdout=%q stderr=%q", code, show, stderr)
	}
	code, same, _ := runRulingCLI(t, home, cwd, base, nil, "rulings", "show", "R-20261126-08")
	if code != 0 || same != show || reads.Load() != 1 {
		t.Fatalf("alias vs ID = %d %q vs %q, reads=%d", code, same, show, reads.Load())
	}
	superseded.Store(true)
	if code, _, _ := runRulingCLI(t, home, cwd, base, nil, "rulings", "pull"); code != 0 || reads.Load() != 2 {
		t.Fatalf("pull did not refresh: %d, %d reads", code, reads.Load())
	}
	if err := os.WriteFile(filepath.Join(cwd, "fixture.md"), []byte("ruling: R41\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runRulingCLI(t, home, cwd, base, nil, "rulings", "check", "fixture.md")
	if code != 1 || !strings.Contains(out, "R41 -> R-20261126-08 (superseded) superseded_by R-20261127-01") {
		t.Fatalf("check exit=%d output=%q", code, out)
	}
	if code, _, stderr := runRulingCLI(t, home, cwd, base, nil, "rulings", "check", "misspelled.md"); code != 2 || !strings.Contains(stderr, "misspelled.md") {
		t.Fatalf("missing explicit input exit=%d error=%q", code, stderr)
	}
	server.Close()
	code, stale, stderr := runRulingCLI(t, home, cwd, base, map[string]string{"ANX_RULINGS_TTL": "0s"}, "rulings", "show", "R41")
	if code != 0 || !strings.Contains(stale, "superseded") || !strings.Contains(stderr, "warning code=rulings_projection_stale") {
		t.Fatalf("offline exit=%d stdout=%q stderr=%q", code, stale, stderr)
	}
	if code, _, stderr := runRulingCLI(t, home, cwd, base, map[string]string{"ANX_RULINGS_TTL": "0s", "ANX_ACCESS_TOKEN": "another-principal"}, "rulings", "check", "fixture.md"); code != 2 || !strings.Contains(stderr, "rulings_index_unavailable") {
		t.Fatalf("cache leaked across bearer change: %d %q", code, stderr)
	}
}

func TestRulingsProjectionAtomicPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rulings", "workspace.json")
	now := time.Now().UTC()
	p := rulingProjection{BaseURL: "http://localhost", Principal: "fixture", Revision: "old", FetchedAt: now, Rulings: []map[string]any{}}
	fresh := p
	fresh.FetchedAt, fresh.Revision = now.Add(time.Second), "new"
	var wg sync.WaitGroup
	for _, snap := range []rulingProjection{fresh, p} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := saveRulingProjection(path, snap); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := readRulingProjection(path, p.BaseURL, p.Principal)
	if err != nil || got.Revision != "new" {
		t.Fatalf("published %+v, %v", got, err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions %v %v", stat, err)
	}
	if _, err := readRulingProjection(path, p.BaseURL, "another"); err == nil {
		t.Fatal("workspace cannot reuse another principal's snapshot")
	}
}

func TestRulingsImportReusesKeysAndPreservesIndex(t *testing.T) {
	var posts []rulingWrite
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rulings/projection":
			_, _ = w.Write([]byte(`{"rulings":[],"source_revision":"fixture"}`))
		case "/rulings":
			_, _ = w.Write([]byte(`{"rulings":[]}`))
		case "/rulings/propose":
			var req rulingWrite
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			posts = append(posts, req)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"ruling": map[string]any{"id": req.ID, "status": "proposed"}})
		}
	}))
	defer server.Close()
	home, cwd := t.TempDir(), t.TempDir()
	indexFile, err := filepath.Abs("testdata/rulings/index.json")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if code, out, stderr := runRulingCLI(t, home, cwd, server.URL, nil, "--json", "rulings", "import", "--file", indexFile); code != 0 || !strings.Contains(out, `"submitted": 4`) {
			t.Fatalf("import exit=%d output=%q error=%q", code, out, stderr)
		}
	}
	if len(posts) != 8 || !reflect.DeepEqual(posts[:4], posts[4:]) || posts[0].RequestKey == "" || posts[0].StatusHint != "spent" || posts[1].SupersededByHint != posts[3].ID || len(posts[3].Relations) != 2 || !reflect.DeepEqual(posts[3].Aliases, []string{"R41"}) {
		t.Fatalf("incorrect import: %+v", posts)
	}
	_, err = decodeRulingIndex(bytes.Replace(rulingFixture(t, "index.json"), []byte(`["R41"]`), []byte(`["R17"]`), 1))
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("duplicate alias accepted: %v", err)
	}
}

func TestRulingsProposeReportsRelationRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rulings/projection":
			_, _ = w.Write([]byte(`{"rulings":[],"source_revision":"fixture"}`))
		case "/rulings":
			_, _ = w.Write([]byte(`{"rulings":[]}`))
		case "/rulings/propose":
			_, _ = w.Write([]byte(`{"redirected":"needs_relation","missing_relations":["R-20261127-01"]}`))
		}
	}))
	defer server.Close()
	home, cwd := t.TempDir(), t.TempDir()
	file := filepath.Join(cwd, "body.md")
	if err := os.WriteFile(file, []byte("Use the enrolled host."), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runRulingCLI(t, home, cwd, server.URL, nil, "rulings", "propose", "--id-date", "2026-11-27", "--scope", "workspace", "--topic", "routing", "--quote", "Use the enrolled host.", "--body-file", file)
	if code != 2 || !strings.Contains(stderr, "R-20261127-01") {
		t.Fatalf("redirect reported as successful submission: %d %q", code, stderr)
	}
}

func TestRulingsOfflineHostAssertionAndAccountFanout(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-bearer" {
			t.Error("selected workspace did not use its supplied bearer")
		}
		if r.URL.Path == "/rulings/projection" {
			_, _ = w.Write([]byte(`{"rulings":[],"source_revision":"primary"}`))
		} else {
			_, _ = w.Write([]byte(`{"rulings":[]}`))
		}
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer fixture-bearer" {
			t.Error("selected bearer leaked to a different workspace")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer secondary.Close()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	host := hostidentity.Host{ID: "host-second", KeyID: "key-second", WorkspaceID: "second", BaseURL: secondary.URL}
	if err := hostidentity.SaveAt(dir, host, key); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runRulingCLI(t, home, cwd, primary.URL, nil, "--as", "reviewer", "rulings", "list"); code != 0 || !strings.Contains(stderr, "rulings_projection_unavailable") {
		t.Fatalf("secondary with no usable cache blocked selected read: %d %q", code, stderr)
	}
	p := rulingProjection{
		FetchedAt: time.Now().Add(-time.Hour), Revision: "secondary", BaseURL: secondary.URL,
		Principal: "host:host-second:key-second:reviewer",
		Rulings: []map[string]any{
			{"ruling_id": "R-20261126-08", "aliases": []string{"R41"}, "kind": "operating", "scope": "account", "status": "active", "active": true, "question": "Cross-workspace rule"},
			{"ruling_id": "R-20261126-09", "aliases": []string{"PrivateWorkspaceRule"}, "kind": "operating", "scope": "workspace", "status": "active", "active": true, "question": "Scoped rule"},
		},
	}
	if err := saveRulingProjection(filepath.Join(dir, "rulings", "second.json"), p); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := runRulingCLI(t, home, cwd, primary.URL, nil, "--as", "reviewer", "rulings", "show", "R41")
	if code != 0 || !strings.Contains(out, "Cross-workspace rule") || !strings.Contains(stderr, "rulings_projection_stale") {
		t.Fatalf("fan-out after host assertion failure: %d %q %q", code, out, stderr)
	}
	code, out, _ = runRulingCLI(t, home, cwd, primary.URL, nil, "--as", "reviewer", "rulings", "list")
	if code != 0 || strings.Contains(out, "Scoped rule") {
		t.Fatalf("remote workspace scope escaped its selected workspace: %d %q", code, out)
	}
}

func TestRulingsLegacyActivePageSurvivesHistoryLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rulings/projection":
			_, _ = w.Write([]byte(`{"rulings":[],"source_revision":"fixture"}`))
		case r.URL.Query().Get("include_revoked") == "true":
			_, _ = w.Write([]byte(`{"rulings":[{"id":"new-revoked","ref":"ruling:new-revoked","kind":"answer","active":false}]}`))
		default:
			_, _ = w.Write([]byte(`{"rulings":[{"id":"old-active","ref":"ruling:old-active","kind":"answer","active":true}]}`))
		}
	}))
	defer server.Close()
	home, cwd := t.TempDir(), t.TempDir()
	code, out, _ := runRulingCLI(t, home, cwd, server.URL, nil, "rulings", "list")
	if code != 0 || !strings.Contains(out, "ruling:old-active") || strings.Contains(out, "ruling:new-revoked") {
		t.Fatalf("active legacy list: %d %q", code, out)
	}
	code, out, _ = runRulingCLI(t, home, cwd, server.URL, nil, "rulings", "list", "--include-revoked")
	if code != 0 || !strings.Contains(out, "ruling:old-active") || !strings.Contains(out, "ruling:new-revoked") {
		t.Fatalf("combined legacy list: %d %q", code, out)
	}
}
