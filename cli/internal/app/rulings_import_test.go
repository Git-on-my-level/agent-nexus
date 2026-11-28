package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRulingsImportV2PreservesMetadataAndRoutes(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	account, accountWrites := rulingImportServer(t, "fixture-bearer")
	workspace, workspaceWrites := rulingImportServer(t, "workspace-token")
	seedRulingEnrollment(t, home, "team", workspace.URL)

	entries := rulingImportV2Entries(2)
	entries[0].Scope = "workspace:team"
	entries[0].Aliases = []string{"Legacy 12"}
	entries[0].Body = "  # First line\n\nSecond line 🧭\n"
	entries[0].Quote = "  Exact words, including spaces.  "
	entries[0].Source = "record.md#first"
	entries[0].Supersedes = []string{entries[1].ID}
	entries[0].Relations = []rulingRelation{
		{Kind: "supersedes", ID: entries[1].ID},
		{Kind: "amends", ID: "R-20251230-01"},
		{Kind: "independent", ID: "R-20251231-01"},
	}
	file := writeRulingImportIndex(t, root, 2, entries)
	args := []string{"--as", "reviewer", "--json", "rulings", "import", "--file", file}
	code, stdout, stderr := runRulingCLI(t, home, root, account.URL, nil, args...)
	if code != 0 || len(accountWrites) != 1 || len(workspaceWrites) != 1 || !strings.Contains(stdout, `"submitted": 2`) {
		t.Fatalf("import exit=%d, account writes=%d, workspace writes=%d, stdout=%s, stderr=%s", code, len(accountWrites), len(workspaceWrites), stdout, stderr)
	}
	gotWorkspace, gotAccount := <-workspaceWrites, <-accountWrites
	if gotWorkspace.RequestKey == "" || gotAccount.RequestKey == "" {
		t.Fatal("proposals must have idempotency keys")
	}
	wantWorkspace := rulingProposal{
		ID: entries[0].ID, Aliases: entries[0].Aliases, Scope: "workspace", Topic: entries[0].Topic, Title: entries[0].Title,
		Body: entries[0].Body, Quote: entries[0].Quote, Source: entries[0].Source,
		StatusHint: "active", Relations: entries[0].Relations,
	}
	wantAccount := rulingProposal{
		ID: entries[1].ID, Scope: "account", Topic: entries[1].Topic, Title: entries[1].Title,
		Body: entries[1].Body, Quote: entries[1].Quote, Source: entries[1].Source,
		StatusHint: "active",
	}
	if !reflect.DeepEqual(gotWorkspace.rulingProposal, wantWorkspace) || !reflect.DeepEqual(gotAccount.rulingProposal, wantAccount) {
		t.Fatalf("workspace proposal = %+v, want %+v\naccount proposal = %+v, want %+v", gotWorkspace, wantWorkspace, gotAccount, wantAccount)
	}

	code, _, stderr = runRulingCLI(t, home, root, account.URL, nil, args...)
	if code != 0 || len(workspaceWrites) != 1 || len(accountWrites) != 1 {
		t.Fatalf("retry exit=%d, stderr=%s", code, stderr)
	}
	if (<-workspaceWrites).RequestKey != gotWorkspace.RequestKey || (<-accountWrites).RequestKey != gotAccount.RequestKey {
		t.Fatal("import retry changed the idempotency keys")
	}
}

func TestRulingsImportV2RejectsInvalidRowsBeforeAnyRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name, message string
		change        func(*rulingIndexEntry)
	}{
		{"unknown workspace", "missing-team", func(r *rulingIndexEntry) { r.Scope = "workspace:missing-team" }},
		{"missing scope", "scope must be", func(r *rulingIndexEntry) { r.Scope = "" }},
		{"missing topic", "topic must be", func(r *rulingIndexEntry) { r.Topic = "" }},
		{"missing body", "body must contain text", func(r *rulingIndexEntry) { r.Body = "" }},
		{"missing quote", "source_quote must contain text", func(r *rulingIndexEntry) { r.Quote = "" }},
		{"legacy supersession", "requires a matching supersedes relation", func(r *rulingIndexEntry) {
			r.Supersedes = []string{"R-20251231-01"}
		}},
		{"duplicate relation", "per other R-YYYYMMDD-NN", func(r *rulingIndexEntry) {
			r.Relations = []rulingRelation{{Kind: "amends", ID: "R-20251231-01"}, {Kind: "independent", ID: "R-20251231-01"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			entries := rulingImportV2Entries(5)
			last := &entries[len(entries)-1]
			tc.change(last)
			file := writeRulingImportIndex(t, root, 2, entries)
			for _, flags := range [][]string{nil, {"--dry-run"}} {
				code, _, stderr := runRulingCLI(t, home, root, server.URL, nil, append([]string{"rulings", "import", "--file", file}, flags...)...)
				if code != 2 || requests.Load() != 0 || !strings.Contains(stderr, tc.message) || !strings.Contains(stderr, last.ID) {
					t.Fatalf("exit=%d requests=%d stderr=%s", code, requests.Load(), stderr)
				}
			}
		})
	}
}

func TestRulingsImportV2ChecksNamesWithinTheirScope(t *testing.T) {
	for _, tc := range []struct {
		name, firstScope, secondScope string
		wantCode                      int
	}{
		{"separate workspace namespaces", "workspace:primary", "workspace:secondary", 0},
		{"same workspace", "workspace:secondary", "workspace:secondary", 2},
		{"account then workspace", "account", "workspace:secondary", 2},
		{"workspace then account", "workspace:secondary", "account", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			primary, primaryWrites := rulingImportServer(t, "fixture-bearer")
			secondary, secondaryWrites := rulingImportServer(t, "workspace-token")
			seedRulingEnrollment(t, home, "primary", primary.URL)
			seedRulingEnrollment(t, home, "secondary", secondary.URL)
			entries := rulingImportV2Entries(2)
			entries[1].ID, entries[1].Date = entries[0].ID, entries[0].Date
			entries[0].Aliases, entries[1].Aliases = []string{"Legacy 8"}, []string{"Legacy 8"}
			entries[0].Scope, entries[1].Scope = tc.firstScope, tc.secondScope
			file := writeRulingImportIndex(t, root, 2, entries)
			code, _, stderr := runRulingCLI(t, home, root, primary.URL, nil, "--as", "reviewer", "rulings", "import", "--file", file)
			if code != tc.wantCode {
				t.Fatalf("exit=%d, want %d, stderr=%s", code, tc.wantCode, stderr)
			}
			if code == 0 {
				if len(primaryWrites) != 1 || len(secondaryWrites) != 1 {
					t.Fatal("expected one proposal per workspace")
				}
			} else if len(primaryWrites) != 0 || len(secondaryWrites) != 0 || !strings.Contains(stderr, "duplicate ruling ID or alias") {
				t.Fatalf("duplicate should fail before any submission: %s", stderr)
			}
		})
	}
}

func TestRulingsImportV1WarnsAboutDefaults(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	server, writes := rulingImportServer(t, "fixture-bearer")
	entries := reviewImportEntries(2)
	entries[1].Supersedes = []string{entries[0].ID}
	file := writeRulingImportIndex(t, root, 1, entries)
	code, stdout, stderr := runRulingCLI(t, home, root, server.URL, nil, "rulings", "import", "--file", file)
	if code != 0 || len(writes) != 2 {
		t.Fatalf("exit=%d writes=%d stdout=%s stderr=%s", code, len(writes), stdout, stderr)
	}
	warning := ""
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "warning code=") {
			warning = line
			break
		}
	}
	for _, field := range []string{"rulings_import_v1_defaults", "scope", "topic", "body", "source_quote"} {
		if !strings.Contains(warning, field) {
			t.Fatalf("warning missing %q: %s", field, warning)
		}
	}
	<-writes
	got := <-writes
	wantRelations := []rulingRelation{{Kind: "supersedes", ID: entries[0].ID}}
	if got.Scope != "account" || got.Topic != "import-"+strings.ToLower(got.ID) || got.Body != entries[1].Title || got.Quote != entries[1].Title || !slices.Equal(got.Relations, wantRelations) {
		t.Fatalf("version 1 defaults changed: %+v", got)
	}
}

func TestRulingsImportV2RejectsLegacyTargetBeforeSubmitting(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	primary, primaryWrites := rulingImportServer(t, "fixture-bearer")
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/token":
			_, _ = fmt.Fprint(w, `{"tokens":{"access_token":"legacy-token","expires_in":300}}`)
		case "/rulings/projection":
			w.WriteHeader(http.StatusNotFound)
		case "/rulings":
			_, _ = fmt.Fprint(w, `{"rulings":[]}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer legacy.Close()
	seedRulingEnrollment(t, home, "legacy", legacy.URL)
	entries := rulingImportV2Entries(2)
	entries[1].Scope = "workspace:legacy"
	file := writeRulingImportIndex(t, root, 2, entries)
	code, _, stderr := runRulingCLI(t, home, root, primary.URL, nil, "--as", "reviewer", "rulings", "import", "--file", file)
	if code != 2 || len(primaryWrites) != 0 || !strings.Contains(stderr, "rulings_index_unavailable") || !strings.Contains(stderr, entries[1].ID) {
		t.Fatalf("exit=%d writes=%d stderr=%s", code, len(primaryWrites), stderr)
	}
}

func TestRulingsImportDryRunPreviewsEachTargetWithoutRequests(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	offline := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("dry-run sent %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	primary, secondary := httptest.NewServer(offline), httptest.NewServer(offline)
	defer primary.Close()
	defer secondary.Close()
	seedRulingEnrollment(t, home, "team", secondary.URL)

	entries := rulingImportV2Entries(6)
	for i := 1; i <= 4; i++ {
		entries[i].Scope = "workspace:team"
	}
	wantSecondary := []string{entries[1].ID, entries[2].ID, entries[3].ID}
	wantPrimary := []string{entries[0].ID, entries[5].ID}
	slices.Reverse(entries)
	file := writeRulingImportIndex(t, root, 2, entries)

	code, stdout, stderr := runRulingCLI(t, home, root, primary.URL, nil, "--json", "rulings", "import", "--dry-run", "--file", file)
	if code != 0 {
		t.Fatalf("dry-run exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	var envelope struct {
		Result struct {
			DryRun    bool `json:"dry_run"`
			Submitted int  `json:"submitted"`
			Targets   []struct {
				Workspace string           `json:"workspace_url"`
				Count     int              `json:"count"`
				Rows      []rulingProposal `json:"rows"`
			} `json:"targets"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	got := envelope.Result
	if !got.DryRun || got.Submitted != 0 || len(got.Targets) != 2 {
		t.Fatalf("unexpected preview: %s", stdout)
	}
	wantCounts := map[string]int{primary.URL: 2, secondary.URL: 4}
	wantIDs := map[string][]string{primary.URL: wantPrimary, secondary.URL: wantSecondary}
	for _, target := range got.Targets {
		var ids []string
		for _, row := range target.Rows {
			ids = append(ids, row.ID)
		}
		if target.Count != wantCounts[target.Workspace] || !slices.Equal(ids, wantIDs[target.Workspace]) {
			t.Fatalf("unexpected target preview: %+v", target)
		}
		delete(wantCounts, target.Workspace)
	}
	code, stdout, stderr = runRulingCLI(t, home, root, primary.URL, nil, "rulings", "import", "--dry-run", "--file", file)
	if code != 0 || !strings.Contains(stdout, "result.targets.1.count=4") || !strings.Contains(stdout, "result.targets.1.workspace_url="+secondary.URL) || strings.Count(stdout, ".ruling_id=") != 5 {
		t.Fatalf("text preview exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

func rulingImportV2Entries(n int) []rulingIndexEntry {
	entries := reviewImportEntries(n)
	for i := range entries {
		entry := &entries[i]
		entry.Scope, entry.Topic = "account", "routing"
		entry.Body, entry.Quote = "Use the enrolled host.", "Use the enrolled host for this task."
	}
	return entries
}

func writeRulingImportIndex(t *testing.T, root string, version int, entries []rulingIndexEntry) string {
	t.Helper()
	index, err := json.Marshal(rulingIndex{SchemaVersion: version, GeneratedFrom: []string{"fixture.md"}, Rulings: entries})
	if err != nil {
		t.Fatal(err)
	}
	return writeRulingReviewFile(t, root, "index.json", string(index))
}

func rulingImportServer(t *testing.T, token string) (*httptest.Server, <-chan rulingWrite) {
	t.Helper()
	writes := make(chan rulingWrite, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/token" && token != "fixture-bearer" && r.Header.Get("Authorization") == "" {
			_, _ = fmt.Fprintf(w, `{"tokens":{"access_token":%q,"expires_in":300}}`, token)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("wrong bearer for %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/rulings/projection":
			_, _ = fmt.Fprint(w, `{"source_revision":"fixture","rulings":[]}`)
		case "/rulings":
			_, _ = fmt.Fprint(w, `{"rulings":[]}`)
		case "/rulings/propose":
			var proposal rulingWrite
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&proposal) != nil {
				t.Errorf("invalid proposal request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writes <- proposal
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"ruling": proposal})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, writes
}
