package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

func managedUpdateFixture(t *testing.T) (*App, config.Resolved, string) {
	t.Helper()
	oldProbe := updateProbeBinary
	updateProbeBinary = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() { updateProbeBinary = oldProbe })
	oldVersion, oldPath, oldOS := httpclient.CLIVersion, updateExecutablePath, updateGOOS
	httpclient.CLIVersion = "v0.12.10"
	updateGOOS = "darwin"
	t.Cleanup(func() { httpclient.CLIVersion = oldVersion; updateExecutablePath = oldPath; updateGOOS = oldOS })
	home := t.TempDir()
	path := filepath.Join(home, "bin", "anx")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	digest, _ := binaryDigest(path)
	if err := writeUpdateJSON(installRecordPath(path), updateInstallRecord{ManagedBy: "anx", Version: "v0.12.10", SHA256: digest, InstalledAt: "2026-10-05T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	updateExecutablePath = func() (string, error) { return path, nil }
	a := newTestApp(t)
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getenv = func(string) string { return "" }
	a.now = func() time.Time { return time.Date(2026, 10, 5, 23, 59, 0, 0, time.FixedZone("east", 3600)) }
	cfg := config.Resolved{ConfigDir: filepath.Join(home, "config"), Timeout: time.Second}
	return a, cfg, path
}

func TestUpdateReadOnlyAndDryRunExemptions(t *testing.T) {
	for _, command := range []string{"orient", "help", "inbox list", "await", "doctor", "update status", "version", "work list", "work context", "bridge status", "skills status", "update --check"} {
		if updateInvocationEligible(command, strings.Fields(command)) {
			t.Errorf("read-only command triggers update: %s", command)
		}
	}
	for _, args := range [][]string{{"cards", "create", "--dry-run"}, {"cards", "create", "--dry-run=true"}, {"host", "enroll", "--plan"}} {
		if updateInvocationEligible(strings.Join(args[:2], " "), args) {
			t.Errorf("dry run triggers: %v", args)
		}
	}
	if !updateInvocationEligible("work start", []string{"work", "start", "card:task"}) {
		t.Fatal("work write is exempt")
	}
}

func TestUpdatePolicyAndConcurrentDailyClaim(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	var starts atomic.Int32
	a.startUpdateWorker = func(string, string) error { starts.Add(1); return nil }
	var group sync.WaitGroup
	for i := 0; i < 30; i++ {
		group.Add(1)
		go func() { defer group.Done(); a.maybeScheduleUpdate("cards create", nil, cfg) }()
	}
	group.Wait()
	if starts.Load() != 1 {
		t.Fatalf("starts=%d", starts.Load())
	}
	// A different workspace/config cannot bypass the installation's daily claim.
	other := cfg
	other.ConfigDir = filepath.Join(t.TempDir(), "config")
	a.maybeScheduleUpdate("cards create", nil, other)
	if starts.Load() != 1 {
		t.Fatal("second config bypassed daily claim")
	}
	a.now = func() time.Time { return time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) }
	a.maybeScheduleUpdate("cards create", nil, cfg)
	if starts.Load() != 2 {
		t.Fatal("next UTC day did not trigger")
	}
	a.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "off"
		}
		return ""
	}
	a.now = func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) }
	a.maybeScheduleUpdate("cards create", nil, cfg)
	if starts.Load() != 2 {
		t.Fatal("off triggered worker")
	}
}

func TestNotifyWarnsOnceWithoutInstalling(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	if _, err := a.runUpdate(context.Background(), []string{"policy", "notify"}, cfg); err != nil {
		t.Fatal(err)
	}
	a.startUpdateWorker = func(string, string) error { return nil }
	if warnings := a.maybeScheduleUpdate("cards create", nil, cfg); len(warnings) != 0 {
		t.Fatal("warned without release observation")
	}
	dir, _ := a.updateDirectory(cfg)
	_ = writeUpdateJSON(filepath.Join(dir, "state.json"), updateState{LatestVersion: "v0.12.11"})
	if warnings := a.maybeScheduleUpdate("cards create", nil, cfg); len(warnings) != 1 {
		t.Fatalf("warning count=%d", len(warnings))
	}
	if warnings := a.maybeScheduleUpdate("cards create", nil, cfg); len(warnings) != 0 {
		t.Fatal("duplicate daily warning")
	}
}

func TestUnmanagedInstallIsOffline(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	_ = os.Remove(installRecordPath(path))
	status, err := a.runUpdateStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data := asMap(status.Data)
	if asBool(data["managed"]) || anyString(data["skip_reason"]) != "no_installer_receipt" {
		t.Fatalf("status=%v", data)
	}
	a.startUpdateWorker = func(string, string) error { t.Fatal("unmanaged worker started"); return nil }
	a.maybeScheduleUpdate("cards create", nil, cfg)
	if _, err := a.runUpdate(context.Background(), []string{"now"}, cfg); errnorm.Normalize(err).Code != "unmanaged_install" {
		t.Fatalf("err=%v", err)
	}
}

func TestUpdateOwnershipAndObservedReceiptSeparation(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	var receipt updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &receipt)
	receipt.Version = "v0.12.9"
	_ = writeUpdateJSON(installRecordPath(path), receipt)
	status, _ := a.runUpdateStatus(cfg)
	data := asMap(status.Data)
	if asBool(asMap(data["observed_binary"])["bookkeeping_matches"]) {
		t.Fatal("concealed stale installer version")
	}
	_ = os.WriteFile(path, []byte("source build"), 0755)
	status, _ = a.runUpdateStatus(cfg)
	if anyString(asMap(status.Data)["skip_reason"]) != "binary_differs_from_installer_receipt" {
		t.Fatal("source overwrite was accepted")
	}
	brew := filepath.Join(t.TempDir(), "Cellar", "anx", "v0.12.10", "bin", "anx")
	_ = os.MkdirAll(filepath.Dir(brew), 0700)
	_ = os.WriteFile(brew, []byte("brew"), 0755)
	updateExecutablePath = func() (string, error) { return brew, nil }
	if _, _, _, reason := inspectUpdateInstall(); reason != "package_manager_install" {
		t.Fatalf("reason=%s", reason)
	}
}

func TestReleaseAPIRateLimitFallbackAndRedirectOrigin(t *testing.T) {
	oldAPI, oldBase := updateReleaseAPIURL, updateReleaseBaseURL
	t.Cleanup(func() { updateReleaseAPIURL = oldAPI; updateReleaseBaseURL = oldBase })
	for _, code := range []int{403, 429} {
		var server *httptest.Server
		server = newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api":
				w.WriteHeader(code)
			case "/releases/latest":
				http.Redirect(w, r, server.URL+"/releases/tag/v0.12.11", 302)
			default:
				w.WriteHeader(200)
			}
		}))
		updateReleaseAPIURL = server.URL + "/api"
		updateReleaseBaseURL = server.URL + "/releases"
		tag, err := resolveLatestReleaseTag(context.Background(), time.Second)
		server.Close()
		if err != nil || tag != "v0.12.11" {
			t.Fatalf("code=%d tag=%s err=%v", code, tag, err)
		}
	}
	target := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("cross-origin redirect followed") }))
	defer target.Close()
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/releases/tag/v0.12.11", 302)
	}))
	defer server.Close()
	updateReleaseAPIURL = ""
	updateReleaseBaseURL = server.URL + "/releases"
	if _, err := resolveLatestReleaseTag(context.Background(), time.Second); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
}

func TestManagedUpdateChecksumFailurePreservesBinary(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	oldBase := updateReleaseBaseURL
	t.Cleanup(func() { updateReleaseBaseURL = oldBase })
	archive, _ := updateArchiveName("v0.12.11")
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = w.Write([]byte(strings.Repeat("0", 64) + "  " + archive))
		} else {
			_, _ = w.Write([]byte("tampered archive"))
		}
	}))
	defer server.Close()
	updateReleaseBaseURL = server.URL + "/releases"
	_, err := a.runUpdate(context.Background(), []string{"--version", "v0.12.11"}, cfg)
	if errnorm.Normalize(err).Code != "checksum_mismatch" {
		t.Fatalf("err=%v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("checksum failure changed executable")
	}
	dir, _ := a.updateDirectory(cfg)
	s, _ := readUpdateState(dir)
	if s.FailureStage != "download_verify" || s.FailureCode != "checksum_mismatch" || s.Rollback != "not_needed" {
		t.Fatalf("state=%+v", s)
	}
}

func TestAtomicReplacementRollbackAndFailedRollbackRetainsBackup(t *testing.T) {
	for _, failRollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "restored", true: "retained"}[failRollback], func(t *testing.T) {
			_, _, path := managedUpdateFixture(t)
			var record updateInstallRecord
			_ = readUpdateJSON(installRecordPath(path), &record)
			oldProbe, oldRename := updateProbeBinary, updateRename
			t.Cleanup(func() { updateProbeBinary = oldProbe; updateRename = oldRename })
			updateProbeBinary = func(ctx context.Context, p, v string) error {
				if p == path {
					return errors.New("post-replace probe failed")
				}
				return nil
			}
			updateRename = func(from, to string) error {
				if failRollback && strings.Contains(from, ".anx-rollback") {
					return errors.New("rollback denied")
				}
				return os.Rename(from, to)
			}
			outcome, backup, err := replaceManagedExecutable(context.Background(), path, []byte("candidate"), 0755, "v0.12.11", record)
			if err == nil {
				t.Fatal("failure lost")
			}
			b, _ := os.ReadFile(path)
			if failRollback {
				if outcome != "failed" || backup == "" {
					t.Fatalf("%s %s", outcome, backup)
				}
				original, _ := os.ReadFile(backup)
				if string(original) != "original" {
					t.Fatal("original not recoverable")
				}
			} else if outcome != "succeeded" || string(b) != "original" {
				t.Fatalf("outcome=%s bytes=%s", outcome, b)
			}
		})
	}
}

func TestUpdatePoliciesAndUsageBeforeWorkspaceResolution(t *testing.T) {
	for _, args := range [][]string{{"update", "policy", "bad"}, {"update", "status", "--version", "v1.0.0"}, {"update", "now", "--bad"}, {"update", "--version", "v1.0.0/evil"}, {"update", "bogus"}} {
		if _, err := preflightConfigIndependentUsage(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	a, cfg, _ := managedUpdateFixture(t)
	for _, policy := range []string{"auto", "notify", "off"} {
		result, err := a.runUpdate(context.Background(), []string{"policy", policy}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if anyString(asMap(result.Data)["policy"]) != policy {
			t.Fatalf("policy=%v", result.Data)
		}
	}
	a.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "notify"
		}
		return ""
	}
	result, _ := a.runUpdateStatus(cfg)
	if anyString(asMap(result.Data)["policy_source"]) != "environment" {
		t.Fatal("env override not reported")
	}
}

func TestScheduledOffPerformsNoReleaseCheck(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	a.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "off"
		}
		return ""
	}
	oldBase := updateReleaseBaseURL
	t.Cleanup(func() { updateReleaseBaseURL = oldBase })
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("off contacted release server") }))
	defer server.Close()
	updateReleaseBaseURL = server.URL
	if _, err := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestScheduledNotifyChecksButDoesNotDownloadOrReplace(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	a.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "notify"
		}
		return ""
	}
	oldAPI, oldBase := updateReleaseAPIURL, updateReleaseBaseURL
	t.Cleanup(func() { updateReleaseAPIURL = oldAPI; updateReleaseBaseURL = oldBase })
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			t.Error("notify downloaded a release archive")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.12.11"}`))
	}))
	defer server.Close()
	updateReleaseAPIURL = server.URL + "/api"
	updateReleaseBaseURL = server.URL + "/releases"
	result, err := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if asBool(asMap(result.Data)["updated"]) {
		t.Fatal("notify installed")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("notify changed binary")
	}
	dir, _ := a.updateDirectory(cfg)
	state, _ := readUpdateState(dir)
	if state.LatestVersion != "v0.12.11" {
		t.Fatalf("state=%+v", state)
	}
}

func TestManagedUpdateSkillsFailureKeepsVerifiedRelease(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	oldBase, oldSync := updateReleaseBaseURL, updateSyncSkills
	t.Cleanup(func() { updateReleaseBaseURL = oldBase; updateSyncSkills = oldSync })
	updateSyncSkills = func(context.Context, string, string) error { return errors.New("skill conflict") }
	archive, _ := updateArchiveName("v0.12.11")
	content := buildReleaseArchiveForTest(t, archive, []byte("updated"))
	server := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "checksums.txt") {
			_, _ = w.Write([]byte(sha256HexForTest(content) + "  " + archive))
		} else {
			_, _ = w.Write(content)
		}
	}))
	defer server.Close()
	updateReleaseBaseURL = server.URL + "/releases"
	_, err := a.runUpdate(context.Background(), []string{"--version", "v0.12.11"}, cfg)
	if errnorm.Normalize(err).Code != "update_skills_failed" {
		t.Fatalf("err=%v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "updated" {
		t.Fatal("skill conflict rolled back healthy binary")
	}
	var receipt updateInstallRecord
	_ = readUpdateJSON(installRecordPath(path), &receipt)
	if receipt.Version != "v0.12.11" || receipt.SHA256 != sha256HexForTest(b) {
		t.Fatalf("receipt=%+v", receipt)
	}
	dir, _ := a.updateDirectory(cfg)
	state, _ := readUpdateState(dir)
	if state.FailureStage != "skills_sync" || state.SkillsSynced {
		t.Fatalf("state=%+v", state)
	}
}

func TestStaleWorkerCannotReplaceNewerImage(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	updateProbeBinary = func(context.Context, string, string) error { return errors.New("version changed") }
	_, err := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg)
	if errnorm.Normalize(err).Code != "update_binary_changed" {
		t.Fatalf("err=%v", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "original" {
		t.Fatal("stale worker replaced binary")
	}
}

func TestUpdateStatusDoesNotRequireHomeWithExplicitConfigDir(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	a.UserHomeDir = func() (string, error) { return "", errors.New("no home") }
	if _, err := a.runUpdateStatus(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestJSONHelpCatalogClassifiesUpdateAndReads(t *testing.T) {
	topics := runtimeHelpDocTopics()
	wanted := map[string]string{"update status": "read_only", "update now": "local_operational_write", "update policy": "local_operational_write", "doctor": "read_only"}
	for _, topic := range topics {
		if class, exists := wanted[topic.Path]; exists {
			if class != topic.SideEffectClass {
				t.Fatalf("topic=%+v", topic)
			}
			delete(wanted, topic.Path)
		}
	}
	if len(wanted) != 0 {
		t.Fatalf("missing topics=%v", wanted)
	}
	status := action("status", "anx", "update", "status")
	if status.Mutates || status.SideEffectClass != "read_only" {
		t.Fatalf("status=%+v", status)
	}
}

func TestUpdateSideEffectClassificationUsesFinalCheckValue(t *testing.T) {
	for command, expected := range map[string]string{
		"update --check":               "read_only",
		"update --check=true":          "read_only",
		"update --check=false":         "local_operational_write",
		"update --check --check=false": "local_operational_write",
		"update --check=false --check": "read_only",
	} {
		if actual := commandSideEffectClass(command); actual != expected {
			t.Fatalf("%s: got %s want %s", command, actual, expected)
		}
	}
}
