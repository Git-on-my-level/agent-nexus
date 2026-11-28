package app

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/registry"
)

func TestRulingsCheckLegacyCitationBoundaries(t *testing.T) {
	names := map[string]map[string]any{}
	for i, name := range []string{"R17", "W3-R17", "hostctl-R17", "R17 exception", "SCA-2104-table"} {
		names[name] = map[string]any{"kind": "operating", "status": "superseded", "ruling_id": fmt.Sprintf("R-20261127-%02d", i+1)}
	}
	scanner := newRulingReferenceScanner(names)
	text := "$R17 $R17.3 R17suffix R17_ R17.3suffix R17 P1 R17.3 P9 PR17 prefix-R17 /R17 éR17 R17é\n" +
		"R17 R17.3 R17 PLAIN W3-R17 hostctl-R17 R17 exception SCA-2104-table"
	var tokens []string
	for _, f := range scanner.scan("fixture", text, names, false) {
		tokens = append(tokens, f.Token)
	}
	want := []string{"R17", "R17.3", "R17", "W3-R17", "hostctl-R17", "R17 exception", "SCA-2104-table"}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("citation tokens = %v, want %v", tokens, want)
	}
}

func TestRulingsCheckDateIDBoundaries(t *testing.T) {
	scanner := newRulingReferenceScanner(nil)
	for _, line := range []string{"PR-20261127-01", "W3-R-20261127-01", "éR-20261127-01", "_R-20261127-01", "R-20261127-011", "ruling: R-20261127-011"} {
		if got := scanner.scan("fixture", line, nil, false); len(got) != 0 {
			t.Fatalf("%q produced %v", line, got)
		}
	}
	for _, line := range []string{"R-20261127-01", "/R-20261127-01", "ruling: R-20261127-01"} {
		if got := scanner.scan("fixture", line, nil, false); len(got) != 1 {
			t.Fatalf("%q produced %v", line, got)
		}
	}
}

func TestRulingsCheckRejectsUnmatchedSurfaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"rulings":[],"source_revision":"fixture"}`)
	}))
	defer server.Close()
	home, root := t.TempDir(), t.TempDir()
	fixtureGit(t, root, "init", "-q")
	writeRulingReviewFile(t, root, "tracked.md", "tracked")
	fixtureGit(t, root, "add", "--", "tracked.md")
	writeRulingReviewFile(t, root, "untracked.md", "untracked")
	for _, tc := range []struct{ include, msg string }{
		{`["no/such/**"]`, "match no tracked regular files"},
		{`["untracked.md"]`, "match no tracked regular files"},
		{`[]`, "non-empty include array"},
	} {
		file := writeRulingReviewFile(t, root, "surfaces.json", `{"include":`+tc.include+`}`)
		code, _, stderr := runRulingCLI(t, home, root, server.URL, nil, "rulings", "check", "--surfaces", file)
		if code != 2 || !strings.Contains(stderr, tc.msg) {
			t.Fatalf("include %s returned %d, %q", tc.include, code, stderr)
		}
	}
}

func TestRulingsCheckJSONFindingStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"rulings":[],"source_revision":"fixture"}`)
	}))
	defer server.Close()
	home, root := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		text string
		want int
	}{{"ruling: R-20261127-01", 1}, {"Clean text.", 0}} {
		file := writeRulingReviewFile(t, root, "fixture.md", tc.text)
		code, stdout, stderr := runRulingCLI(t, home, root, server.URL, nil, "--json", "rulings", "check", file)
		var envelope struct {
			OK     bool `json:"ok"`
			Result struct {
				Count int `json:"finding_count"`
			}
			Error *struct {
				Code     string `json:"code"`
				ExitCode int    `json:"exit_code"`
				Details  struct {
					Count    int             `json:"finding_count"`
					Findings []rulingFinding `json:"findings"`
				} `json:"details"`
			} `json:"error"`
		}
		err := json.Unmarshal([]byte(stdout), &envelope)
		if err != nil || code != tc.want || envelope.OK != (code == 0) {
			t.Fatalf("JSON and exit disagree: code=%d stdout=%q stderr=%q err=%v", code, stdout, stderr, err)
		}
		if code == 1 && (envelope.Error == nil || envelope.Error.Code != "rulings_check_findings" || envelope.Error.ExitCode != 1 || envelope.Error.Details.Count != 1 || len(envelope.Error.Details.Findings) != 1) {
			t.Fatalf("findings failed to produce a complete error envelope: %s", stdout)
		}
		if code == 0 && (envelope.Error != nil || envelope.Result.Count != 0) {
			t.Fatalf("clean report failed to produce a success envelope: %s", stdout)
		}
	}
}

// Agents confirm only through the hand-written command, which requires a
// recorded decision; the server enforces the same rule for raw HTTP.
func TestRulingsConfirmationRequiresRecordedDecision(t *testing.T) {
	found := false
	for _, spec := range registry.CommandSpecs() {
		if spec.CommandID == "rulings.confirm" {
			found = true
			if spec.CLIPath != "rulings confirm" || spec.Method != "POST" || spec.Path != "/rulings/{ruling_id}/confirm" {
				t.Fatalf("unexpected confirmation contract: %+v", spec)
			}
		}
	}
	if !found {
		t.Fatal("confirmation HTTP contract is missing")
	}
	for _, args := range [][]string{
		{"confirm", "R-20261128-02"},
		{"confirm", "R-20261128-02", "--decision", "note-1"},
		{"confirm", "--decision", "event:1"},
		{"withdraw"},
		{"withdraw", "R-20261128-02", "R-20261128-02"},
		{"withdraw", "R1"},
	} {
		if _, err := parseRulingsArgs(args); err == nil {
			t.Fatalf("parseRulingsArgs(%q) accepted invalid input", args)
		}
	}
	if p, err := parseRulingsArgs([]string{"confirm", "R-20261128-02", "R-20261128-03", "--decision", "event:ev-1"}); err != nil || len(p.ids) != 2 || p.decision != "event:ev-1" {
		t.Fatalf("confirm parse: %+v %v", p, err)
	}
	if err := validateRulingAliases([]string{"R17", "R-20261114-01"}); err != nil {
		t.Fatalf("a withdrawn date ID should be a valid alias: %v", err)
	}
	if err := validateRulingAliases([]string{"R-20261114-00"}); err == nil {
		t.Fatal("an invalid date ID alias was accepted")
	}
}

func TestRulingsProjectionRetriesLegacy404(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rulings/projection" {
			if reads.Add(1) == 1 {
				http.NotFound(w, r)
				return
			}
			_, _ = fmt.Fprint(w, `{"source_revision":"upgraded","rulings":[{"ruling_id":"R-20261127-01","kind":"operating","scope":"workspace","active":true,"question":"New projection"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"rulings":[]}`)
	}))
	defer server.Close()
	home, root := t.TempDir(), t.TempDir()
	if code, _, _ := runRulingCLI(t, home, root, server.URL, nil, "rulings", "list"); code != 0 {
		t.Fatalf("legacy list failed with %d", code)
	}
	code, stdout, stderr := runRulingCLI(t, home, root, server.URL, nil, "rulings", "show", "R-20261127-01")
	if code != 0 || reads.Load() != 2 || !strings.Contains(stdout, "New projection") {
		t.Fatalf("next read did not reach recovered endpoint: %d %q %q reads=%d", code, stdout, stderr, reads.Load())
	}
}

func TestRulingsWorkspaceNamesAreScoped(t *testing.T) {
	testRulingWorkspaceNames(t, "workspace")
}

func TestRulingsAmbiguousNamesWarn(t *testing.T) {
	testRulingWorkspaceNames(t, "account")
}

func testRulingWorkspaceNames(t *testing.T, secondaryScope string) {
	t.Helper()
	const id = "R-20261127-01"
	var writes atomic.Int32
	makeServer := func(scope, title string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/auth/token":
				_, _ = fmt.Fprint(w, `{"tokens":{"access_token":"secondary-token","expires_in":300}}`)
			case "/rulings/projection":
				_, _ = fmt.Fprintf(w, `{"source_revision":"fixture","rulings":[{"ruling_id":%q,"aliases":["R17"],"kind":"operating","scope":%q,"active":true,"status":"active","question":%q}]}`, id, scope, title)
			case "/rulings/propose":
				writes.Add(1)
				w.WriteHeader(http.StatusCreated)
				_, _ = fmt.Fprint(w, `{"ruling":{"ruling_id":"R-20261127-02","kind":"operating","question":"Independent"}}`)
			default:
				_, _ = fmt.Fprint(w, `{"rulings":[]}`)
			}
		}))
	}
	home, root := t.TempDir(), t.TempDir()
	primary := makeServer("workspace", "Selected rule")
	defer primary.Close()
	secondary := makeServer(secondaryScope, "Other rule")
	defer secondary.Close()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	host := hostidentity.Host{ID: "host-second", KeyID: "key-second", WorkspaceID: "second", BaseURL: secondary.URL}
	if err := hostidentity.SaveAt(filepath.Join(home, ".config", "anx"), host, key); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string, string) {
		return runRulingCLI(t, home, root, primary.URL, nil, append([]string{"--as", "reviewer", "rulings"}, args...)...)
	}
	code, stdout, stderr := run("list")
	if code != 0 || !strings.Contains(stdout, "Selected rule") {
		t.Fatalf("selected list failed: %d %q %q", code, stdout, stderr)
	}
	file := writeRulingReviewFile(t, root, "fixture.md", "ruling: "+id+"\nruling: R17")
	code, stdout, stderr = run("check", file)
	if secondaryScope == "workspace" {
		if code != 0 || strings.Contains(stderr, "ruling_name_collision") {
			t.Fatalf("hidden workspace name affected check: %d %q %q", code, stdout, stderr)
		}
		if code, stdout, _ = run("show", "R17"); code != 0 || !strings.Contains(stdout, "Selected rule") || strings.Contains(stdout, "Other rule") {
			t.Fatalf("hidden workspace name affected lookup: %d %q", code, stdout)
		}
	} else {
		if code != 1 || strings.Count(stdout, "(ambiguous ruling)") != 2 || !strings.Contains(stderr, "ruling_name_collision") || !strings.Contains(stderr, primary.URL) || !strings.Contains(stderr, secondary.URL) {
			t.Fatalf("visible collision did not identify both workspaces: %d %q %q", code, stdout, stderr)
		}
		if code, _, stderr = run("show", "R17"); code != 4 || !strings.Contains(stderr, secondary.URL) {
			t.Fatalf("ambiguous alias resolved or hid its conflict: %d %q", code, stderr)
		}
	}
	body := writeRulingReviewFile(t, root, "body.md", "Independent")
	code, stdout, stderr = run("propose", "--id-date", "R-20261127-02", "--scope", "workspace", "--topic", "test", "--body-file", body, "--quote", "Independent")
	if code != 0 || writes.Load() != 1 {
		t.Fatalf("unrelated proposal blocked: %d %q %q writes=%d", code, stdout, stderr, writes.Load())
	}
}

func TestRulingsImportRejectsRetiredSupersessionBeforeWrites(t *testing.T) {
	for _, status := range []string{"revoked", "spent"} {
		t.Run(status, func(t *testing.T) {
			entries := reviewImportEntries(2)
			entries[1].Status, entries[1].Supersedes = status, []string{entries[0].ID}
			testRulingImportWithCount(t, entries, nil, 2, 0, "cannot supersede another ruling")
		})
	}
}

func TestRulingsImportEntryLimit(t *testing.T) {
	entries := reviewImportEntries(1001)
	testRulingImportWithCount(t, entries, nil, 2, 0, "exceeding --max-entries 1000")
	testRulingImportWithCount(t, entries, []string{"--max-entries", "1001"}, 0, 1001, `"submitted": 1001`)
}

func reviewImportEntries(n int) []rulingIndexEntry {
	entries := make([]rulingIndexEntry, 0, n)
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		date := day.AddDate(0, 0, i)
		entries = append(entries, rulingIndexEntry{ID: date.Format("R-20060102-01"), Date: date.Format(time.DateOnly), Aliases: []string{}, Supersedes: []string{}, Status: "active", Origin: "register", Title: "Fixture rule", Source: "register.md"})
	}
	return entries
}

func testRulingImportWithCount(t *testing.T, entries []rulingIndexEntry, flags []string, wantCode int, wantWrites int32, message string) {
	t.Helper()
	var requests, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == http.MethodPost {
			writes.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"ruling":{"status":"proposed"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"rulings":[],"source_revision":"fixture"}`)
	}))
	defer server.Close()
	home, root := t.TempDir(), t.TempDir()
	index, err := json.Marshal(map[string]any{"schema_version": 1, "generated_from": []string{"register.md"}, "rulings": entries})
	if err != nil {
		t.Fatal(err)
	}
	file := writeRulingReviewFile(t, root, "index.json", string(index))
	code, stdout, stderr := runRulingCLI(t, home, root, server.URL, nil, append([]string{"--json", "rulings", "import", "--file", file}, flags...)...)
	if code != wantCode || writes.Load() != wantWrites || (wantCode != 0 && requests.Load() != 0) || !strings.Contains(stdout+stderr, message) {
		t.Fatalf("import code=%d writes=%d requests=%d output=%q", code, writes.Load(), requests.Load(), (stdout + stderr)[:min(500, len(stdout+stderr))])
	}
}

func writeRulingReviewFile(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
