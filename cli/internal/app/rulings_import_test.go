package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRulingsImportV2PreservesMetadataAndRoutes(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	account, accountWrites := rulingImportServer(t, "fixture-bearer")
	workspace, workspaceWrites := rulingImportServer(t, "workspace-token",
		importProjectionRuling("R-20251229-01", "workspace"),
		importProjectionRuling("R-20251230-01", "workspace"),
		importProjectionRuling("R-20251231-01", "workspace"),
	)
	seedRulingEnrollment(t, home, "team", workspace.URL)

	entries := rulingImportV2Entries(2)
	entries[0].Scope = "workspace:team"
	entries[0].Aliases = []string{"Legacy 12"}
	entries[0].Body = "  # First line\n\nSecond line 🧭\n"
	entries[0].Quote = "  Exact words, including spaces.  "
	entries[0].Source = "record.md#first"
	entries[0].Supersedes = []string{"R-20251229-01"}
	entries[0].Relations = []rulingRelation{
		{Kind: "supersedes", ID: "R-20251229-01"},
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
	legacy := rulingImportLegacyServer(t)
	seedRulingEnrollment(t, home, "legacy", legacy.URL)
	entries := rulingImportV2Entries(2)
	entries[1].Scope = "workspace:legacy"
	file := writeRulingImportIndex(t, root, 2, entries)
	for _, flags := range [][]string{nil, {"--dry-run"}} {
		args := append([]string{"--as", "reviewer", "rulings", "import", "--file", file}, flags...)
		code, _, stderr := runRulingCLI(t, home, root, primary.URL, nil, args...)
		if code != 2 || len(primaryWrites) != 0 || !strings.Contains(stderr, "rulings_index_unavailable") || !strings.Contains(stderr, entries[1].ID) {
			t.Fatalf("flags=%v exit=%d writes=%d stderr=%s", flags, code, len(primaryWrites), stderr)
		}
	}
}

func TestRulingsImportDryRunPreviewsEachTargetWithoutWrites(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	primary, primaryWrites := rulingImportServer(t, "fixture-bearer")
	secondary, secondaryWrites := rulingImportServer(t, "workspace-token")
	seedRulingEnrollment(t, home, "team", secondary.URL)

	entries := rulingImportV2Entries(6)
	for i := 1; i <= 4; i++ {
		entries[i].Scope = "workspace:team"
	}
	wantSecondary := []string{entries[1].ID, entries[2].ID, entries[3].ID}
	wantPrimary := []string{entries[0].ID, entries[5].ID}
	slices.Reverse(entries)
	file := writeRulingImportIndex(t, root, 2, entries)

	code, stdout, stderr := runRulingCLI(t, home, root, primary.URL, nil, "--as", "reviewer", "--json", "rulings", "import", "--dry-run", "--file", file)
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
	code, stdout, stderr = runRulingCLI(t, home, root, primary.URL, nil, "--as", "reviewer", "rulings", "import", "--dry-run", "--file", file)
	if code != 0 || !strings.Contains(stdout, "result.targets.1.count=4") || !strings.Contains(stdout, "result.targets.1.workspace_url="+secondary.URL) || strings.Count(stdout, ".ruling_id=") != 5 {
		t.Fatalf("text preview exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if len(primaryWrites) != 0 || len(secondaryWrites) != 0 {
		t.Fatal("dry-run submitted proposals")
	}
	err := filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if err == nil && (entry.Name() == "rulings" || strings.HasPrefix(entry.Name(), "token-")) {
			t.Errorf("dry-run persisted a cache: %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
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

func rulingImportServer(t *testing.T, token string, existing ...map[string]any) (*httptest.Server, <-chan rulingWrite) {
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
			rows := append([]map[string]any{}, existing...)
			_ = json.NewEncoder(w).Encode(map[string]any{"source_revision": "fixture", "rulings": rows})
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

func TestRulingsImportV2ResolvesTargetsInDestination(t *testing.T) {
	const projected = "R-20251231-01"
	for _, tc := range []struct {
		name, projectedIn    string
		relation, superseder bool
		targetPlanned        bool
		wantCode             int
	}{
		{name: "local projection", projectedIn: "team", relation: true},
		{name: "later index row", relation: true, targetPlanned: true},
		{name: "local superseder", projectedIn: "team", superseder: true},
		{name: "later superseder", superseder: true, targetPlanned: true},
		{name: "relation missing", relation: true, wantCode: 2},
		{name: "superseder missing", superseder: true, wantCode: 2},
		{name: "relation in foreign projection", projectedIn: "primary", relation: true, wantCode: 2},
		{name: "superseder in foreign projection", projectedIn: "primary", superseder: true, wantCode: 2},
		{name: "relation in foreign index row", relation: true, targetPlanned: true, wantCode: 2},
		{name: "superseder in foreign index row", superseder: true, targetPlanned: true, wantCode: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			project := func(workspace string) []map[string]any {
				if tc.projectedIn == workspace {
					return []map[string]any{importProjectionRuling(projected, "workspace")}
				}
				return nil
			}
			primary, primaryWrites := rulingImportServer(t, "fixture-bearer", project("primary")...)
			team, teamWrites := rulingImportServer(t, "workspace-token", project("team")...)
			seedRulingEnrollment(t, home, "team", team.URL)
			entries := rulingImportV2Entries(3)
			entries[1].Scope = "workspace:team"
			target := projected
			if tc.targetPlanned {
				target = entries[2].ID
				if tc.wantCode == 0 {
					entries[2].Scope = "workspace:team"
				}
			}
			if tc.relation {
				entries[1].Relations = []rulingRelation{{Kind: "amends", ID: target}}
			}
			if tc.superseder {
				entries[1].Status, entries[1].SupersededBy = "superseded", target
			}
			file := writeRulingImportIndex(t, root, 2, entries)
			for _, flags := range [][]string{{"--dry-run"}, nil} {
				args := append([]string{"--as", "reviewer", "rulings", "import", "--file", file}, flags...)
				code, _, stderr := runRulingCLI(t, home, root, primary.URL, nil, args...)
				if code != tc.wantCode {
					t.Fatalf("flags=%v exit=%d want=%d stderr=%s", flags, code, tc.wantCode, stderr)
				}
				if len(flags) != 0 || code != 0 {
					if len(primaryWrites)+len(teamWrites) != 0 {
						t.Fatal("preflight submitted a proposal")
					}
				}
				if code != 0 && (!strings.Contains(stderr, target) || !strings.Contains(stderr, team.URL)) {
					t.Fatalf("error omitted target or destination: %s", stderr)
				}
			}
			if tc.wantCode == 0 && len(primaryWrites)+len(teamWrites) != len(entries) {
				t.Fatal("accepted import did not submit every row")
			}
		})
	}
}

func TestRulingsImportV2AccountNamesCheckEveryEnrolledWorkspace(t *testing.T) {
	for _, collision := range []string{"id", "alias"} {
		t.Run(collision, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			entries := rulingImportV2Entries(2)
			entries[1].Aliases = []string{"Legacy 8"}
			id, name := "R-20251231-01", entries[1].Aliases[0]
			if collision == "id" {
				id, name = entries[1].ID, entries[1].ID
			}
			row := importProjectionRuling(id, "workspace")
			if collision == "alias" {
				row["aliases"] = entries[1].Aliases
			}
			primary, primaryWrites := rulingImportServer(t, "fixture-bearer")
			other, otherWrites := rulingImportServer(t, "workspace-token", row)
			seedRulingEnrollment(t, home, "other", other.URL)
			file := writeRulingImportIndex(t, root, 2, entries)
			for _, flags := range [][]string{{"--dry-run"}, nil} {
				args := append([]string{"--as", "reviewer", "rulings", "import", "--file", file}, flags...)
				code, _, stderr := runRulingCLI(t, home, root, primary.URL, nil, args...)
				if code != 4 || len(primaryWrites)+len(otherWrites) != 0 || !strings.Contains(stderr, name) || !strings.Contains(stderr, other.URL) {
					t.Fatalf("flags=%v exit=%d writes=%d/%d stderr=%s", flags, code, len(primaryWrites), len(otherWrites), stderr)
				}
			}
		})
	}
}

func TestRulingsImportDeduplicatesProjectionWarnings(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	row := importProjectionRuling("R-20251231-01", "account")
	primary, primaryWrites := rulingImportServer(t, "fixture-bearer", row)
	team, teamWrites := rulingImportServer(t, "workspace-token", row)
	legacy := rulingImportLegacyServer(t)
	seedRulingEnrollment(t, home, "team", team.URL)
	seedRulingEnrollment(t, home, "legacy", legacy.URL)
	entries := rulingImportV2Entries(2)
	entries[1].Scope = "workspace:team"
	file := writeRulingImportIndex(t, root, 2, entries)
	for _, flags := range [][]string{{"--dry-run"}, nil} {
		args := append([]string{"--as", "reviewer", "rulings", "import", "--file", file}, flags...)
		code, stdout, stderr := runRulingCLI(t, home, root, primary.URL, nil, args...)
		if code != 0 || strings.Count(stdout, "warning code=rulings_projection_unavailable") != 1 ||
			strings.Count(stdout, "warning code=ruling_name_collision") != 1 {
			t.Fatalf("flags=%v exit=%d stdout=%s stderr=%s", flags, code, stdout, stderr)
		}
		if len(flags) > 0 && len(primaryWrites)+len(teamWrites) != 0 {
			t.Fatal("dry-run submitted proposals")
		}
	}
}

func TestRulingsImportDryRunSkipsEnabledScheduledMaintenance(t *testing.T) {
	home, root := skillTestDir(t), t.TempDir()
	server, writes := rulingImportServer(t, "fixture-bearer")
	file := writeRulingImportIndex(t, root, 2, rulingImportV2Entries(1))
	configDir := filepath.Join(home, ".config", "anx")
	prefs := skillsSyncConfig{SchemaVersion: 1, ManagedBy: "anx", AutoSync: true}
	if err := writeSkillsSyncConfig(configDir, prefs); err != nil {
		t.Fatal(err)
	}
	a, stdout := workspaceTestApp(t, home, root, map[string]string{"ANX_ACCESS_TOKEN": "fixture-bearer"})
	a.runScheduledMaintenanceInTests = true
	refreshes := 0
	a.startSkillsRefresh = func(_, dir, _ string) error {
		refreshes++
		updated := prefs
		updated.LastAutoSyncAt = "2026-11-28T00:00:00Z"
		return writeSkillsSyncConfig(dir, updated)
	}
	args := []string{"--base-url", server.URL, "rulings", "import", "--file", file}
	if code := a.Run(append(slices.Clone(args), "--dry-run")); code != 0 {
		t.Fatalf("dry-run exit=%d stdout=%s stderr=%v", code, stdout, a.Stderr)
	}
	lock := filepath.Join(configDir, ".skills-auto-sync.lock")
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("dry-run touched maintenance lock: %v", err)
	}
	stored, _, err := readSkillsSyncConfig(configDir)
	if err != nil || stored.LastAutoSyncAt != "" || refreshes != 0 || len(writes) != 0 {
		t.Fatalf("dry-run changed state: prefs=%+v refreshes=%d writes=%d err=%v", stored, refreshes, len(writes), err)
	}

	if code := a.Run(args); code != 0 {
		t.Fatalf("import exit=%d stdout=%s stderr=%v", code, stdout, a.Stderr)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("real import did not acquire the maintenance lock: %v", err)
	}
	stored, _, err = readSkillsSyncConfig(configDir)
	if err != nil || stored.LastAutoSyncAt == "" || refreshes != 1 || len(writes) != 1 {
		t.Fatalf("real import did not exercise maintenance: prefs=%+v refreshes=%d writes=%d err=%v", stored, refreshes, len(writes), err)
	}
}

func importProjectionRuling(id, scope string) map[string]any {
	return map[string]any{"ruling_id": id, "scope": scope, "kind": "operating", "status": "active"}
}

func rulingImportLegacyServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	t.Cleanup(server.Close)
	return server
}
