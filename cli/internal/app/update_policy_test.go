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
	a.StdinIsTTY = func() bool { return true }
	a.StdoutIsTTY = func() bool { return true }
	a.now = func() time.Time { return time.Date(2026, 10, 5, 23, 59, 0, 0, time.FixedZone("east", 3600)) }
	cfg := config.Resolved{ConfigDir: filepath.Join(home, "config"), Timeout: time.Second}
	return a, cfg, path
}

func TestUpdateSchedulerRunsAfterBoundedReadsAndWrites(t *testing.T) {
	for _, command := range []string{"orient", "inbox list", "doctor", "version", "work list", "context", "bridge status", "skills status", "cards create", "work start"} {
		if !updateInvocationEligible(command, strings.Fields(command)) {
			t.Errorf("eligible command did not trigger update check: %s", command)
		}
	}
	for _, command := range []string{"await", "help", "update status", "update --check", "api call", "pm install"} {
		if updateInvocationEligible(command, strings.Fields(command)) {
			t.Errorf("ineligible command triggered update: %s", command)
		}
	}
	for _, args := range [][]string{{"cards", "create", "--dry-run"}, {"cards", "create", "--dry-run=true"}, {"host", "enroll", "--plan"}} {
		if updateInvocationEligible(strings.Join(args[:2], " "), args) {
			t.Errorf("dry run triggers: %v", args)
		}
	}
}

func TestUpdatePolicyAndConcurrentDailyClaim(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	var dailyStarts, serverStarts atomic.Int32
	a.startUpdateWorker = func(string, string) error { dailyStarts.Add(1); return nil }
	a.startServerUpdateWorker = func(string, string, string) error { serverStarts.Add(1); return nil }
	var group sync.WaitGroup
	for i := 0; i < 30; i++ {
		group.Add(1)
		go func() { defer group.Done(); a.maybeScheduleUpdate("orient", nil, cfg) }()
	}
	group.Wait()
	if dailyStarts.Load() != 1 || serverStarts.Load() != 1 {
		t.Fatalf("daily starts=%d server starts=%d", dailyStarts.Load(), serverStarts.Load())
	}
	// A different workspace/config cannot bypass the installation's daily claim.
	other := cfg
	other.ConfigDir = filepath.Join(t.TempDir(), "config")
	a.maybeScheduleUpdate("cards create", nil, other)
	if dailyStarts.Load() != 1 || serverStarts.Load() != 1 {
		t.Fatal("second config bypassed daily claim")
	}
	a.now = func() time.Time { return time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) }
	a.maybeScheduleUpdate("version", []string{"version"}, cfg)
	if dailyStarts.Load() != 2 || serverStarts.Load() != 2 {
		t.Fatal("read on next UTC day did not trigger both checks")
	}
	a.Getenv = func(key string) string {
		if key == "ANX_UPDATE_POLICY" {
			return "off"
		}
		return ""
	}
	a.now = func() time.Time { return time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) }
	a.maybeScheduleUpdate("cards create", nil, cfg)
	if dailyStarts.Load() != 2 || serverStarts.Load() != 2 {
		t.Fatal("off triggered worker")
	}
}

func TestUpdateSchedulerAgentAndCISkipMatrix(t *testing.T) {
	cases := []struct {
		name       string
		env        map[string]string
		stdinTTY   bool
		stdoutTTY  bool
		as         string
		omp        bool
		policy     string
		wantDaily  bool
		wantServer bool
	}{
		{name: "interactive human", stdinTTY: true, stdoutTTY: true, wantDaily: true, wantServer: true},
		{name: "explicit agent in noninteractive run", as: "worker", wantDaily: true, wantServer: true},
		{name: "ANX_AS agent in noninteractive run", env: map[string]string{"ANX_AS": "worker"}, wantDaily: true, wantServer: true},
		{name: "generic CI excludes ANX_AS", env: map[string]string{"ANX_AS": "worker", "CI": "1"}},
		{name: "agentctl caller overrides generic CI", env: map[string]string{"AGENTCTL_CALLER_HARNESS": "codex", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "agentctl execution identity overrides generic CI", env: map[string]string{"AGENTCTL_EXECUTION_ID": "execution", "AGENTCTL_ADAPTER": "codex", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Claude Code overrides generic CI", env: map[string]string{"CLAUDECODE": "1", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Codex thread overrides generic CI", env: map[string]string{"CODEX_THREAD_ID": "thread", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Cursor agent overrides generic CI", env: map[string]string{"CURSOR_AGENT": "1", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Cursor completion marker overrides generic CI", env: map[string]string{"CURSOR_AGENT_COMPLETED_PATH": "/tmp/cursor-agent", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Hermes agent marker overrides generic CI", env: map[string]string{"HERMES_AGENT": "true", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "AI agent Hermes marker overrides generic CI", env: map[string]string{"AI_AGENT": "hermes-agent", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "Hermes session overrides generic CI", env: map[string]string{"HERMES_SESSION_ID": "session", "CI": "1"}, wantDaily: true, wantServer: true},
		{name: "OMP agent marker overrides generic CI", env: map[string]string{"AGENT": "1", "CI": "1"}, omp: true, wantDaily: true, wantServer: true},
		{name: "PM agent turn", as: "pm", wantDaily: true, wantServer: true},
		{name: "generic CI excludes explicit principal", env: map[string]string{"CI": "1"}, as: "worker"},
		{name: "generic CI excludes interactive human", env: map[string]string{"CI": "1"}, stdinTTY: true, stdoutTTY: true},
		{name: "Jenkins URL excludes explicit principal", env: map[string]string{"JENKINS_URL": "https://jenkins.example"}, as: "ci-bot"},
		{name: "Jenkins build markers exclude explicit principal", env: map[string]string{"BUILD_ID": "42", "JENKINS_HOME": "/var/lib/jenkins"}, as: "ci-bot"},
		{name: "GitHub Actions beats agent harness", env: map[string]string{"GITHUB_ACTIONS": "true", "AGENTCTL_CALLER_HARNESS": "codex"}, as: "ci-bot"},
		{name: "GitLab CI excludes explicit principal", env: map[string]string{"GITLAB_CI": "true"}, as: "ci-bot"},
		{name: "Buildkite excludes explicit principal", env: map[string]string{"BUILDKITE": "true"}, as: "ci-bot"},
		{name: "CircleCI excludes explicit principal", env: map[string]string{"CIRCLECI": "true"}, as: "ci-bot"},
		{name: "Azure Pipelines excludes explicit principal", env: map[string]string{"TF_BUILD": "True"}, as: "ci-bot"},
		{name: "TeamCity excludes explicit principal", env: map[string]string{"TEAMCITY_VERSION": "2026.10"}, as: "ci-bot"},
		{name: "BUILD_ID without Jenkins home is not sufficient", env: map[string]string{"BUILD_ID": "42"}, as: "worker", wantDaily: true, wantServer: true},
		{name: "ordinary redirected script"},
		{name: "policy off", as: "worker", policy: "off"},
		{name: "policy notify for agent", as: "worker", policy: "notify"},
		{name: "policy notify for human", stdinTTY: true, stdoutTTY: true, policy: "notify", wantDaily: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, cfg, _ := managedUpdateFixture(t)
			var daily, server atomic.Bool
			a.startUpdateWorker = func(string, string) error { daily.Store(true); return nil }
			a.startServerUpdateWorker = func(string, string, string) error { server.Store(true); return nil }
			a.StdinIsTTY = func() bool { return tc.stdinTTY }
			a.StdoutIsTTY = func() bool { return tc.stdoutTTY }
			a.hasOMPAncestor = func() bool { return tc.omp }
			a.Getenv = func(key string) string { return tc.env[key] }
			if tc.policy != "" {
				a.Getenv = func(key string) string {
					if key == "ANX_UPDATE_POLICY" {
						return tc.policy
					}
					return tc.env[key]
				}
			}
			cfg.As = tc.as
			a.maybeScheduleUpdate("work list", []string{"work", "list"}, cfg)
			if daily.Load() != tc.wantDaily || server.Load() != tc.wantServer {
				t.Fatalf("daily started=%t server started=%t; want daily=%t server=%t", daily.Load(), server.Load(), tc.wantDaily, tc.wantServer)
			}
		})
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

func TestServerVersionTriggerCanRunAfterTheDailyClaim(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	a.startUpdateWorker = func(string, string) error { t.Fatal("daily claim already exists"); return nil }
	var serverStarts atomic.Int32
	a.startServerUpdateWorker = func(string, string, string) error { serverStarts.Add(1); return nil }
	claims := path + ".anx-update-days"
	if err := os.MkdirAll(claims, 0700); err != nil {
		t.Fatal(err)
	}
	if !claimUpdateDay(claims, a.clockNow().UTC().Format("2006-01-02")) {
		t.Fatal("failed to seed the regular daily claim")
	}
	a.maybeScheduleUpdate("orient", nil, cfg)
	a.maybeScheduleUpdate("work list", nil, cfg)
	if serverStarts.Load() != 1 {
		t.Fatalf("server trigger starts=%d; want one after the daily claim", serverStarts.Load())
	}
}

func TestScheduledServerVersionTriggerSkipsMissingVersionAndChecksNewerVersion(t *testing.T) {
	t.Run("missing version is recorded", func(t *testing.T) {
		a, cfg, path := managedUpdateFixture(t)
		core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/meta/handshake" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"workspace_id":"ws"}`))
		}))
		defer core.Close()
		cfg.BaseURL = core.URL
		oldAPI, oldBase := updateReleaseAPIURL, updateReleaseBaseURL
		t.Cleanup(func() { updateReleaseAPIURL = oldAPI; updateReleaseBaseURL = oldBase })
		updateReleaseAPIURL = ""
		updateReleaseBaseURL = "https://release.invalid/releases"
		result, err := a.runUpdate(context.Background(), []string{"now", "--scheduled", "--server-trigger"}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if anyString(asMap(result.Data)["skip_reason"]) != "no_server_version" {
			t.Fatalf("result=%v", result.Data)
		}
		state, err := readUpdateState(mustUpdateDir(t, a, cfg))
		if err != nil || state.ServerTriggerResult != "skipped_no_server_version" || state.LastAutomaticResult != "skipped_no_server_version" || state.ServerTriggerCheckedAt == "" {
			t.Fatalf("state=%+v err=%v", state, err)
		}
		binary, _ := os.ReadFile(path)
		if string(binary) != "original" {
			t.Fatal("missing server version changed the binary")
		}
	})

	t.Run("newer version checks release channel", func(t *testing.T) {
		a, cfg, path := managedUpdateFixture(t)
		core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/meta/handshake" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"core_version":"v0.12.11"}`))
		}))
		defer core.Close()
		cfg.BaseURL = core.URL
		version := "v0.12.11"
		archive, _ := updateArchiveName(version)
		content := buildReleaseArchiveForTest(t, archive, []byte("updated"))
		release := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api":
				_, _ = w.Write([]byte(`{"tag_name":"v0.12.11"}`))
			case "/releases/download/" + version + "/" + archive:
				_, _ = w.Write(content)
			case "/releases/download/" + version + "/checksums.txt":
				_, _ = w.Write([]byte(sha256HexForTest(content) + "  " + archive + "\n"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer release.Close()
		oldAPI, oldBase := updateReleaseAPIURL, updateReleaseBaseURL
		oldSync := updateSyncSkills
		t.Cleanup(func() { updateReleaseAPIURL = oldAPI; updateReleaseBaseURL = oldBase; updateSyncSkills = oldSync })
		updateReleaseAPIURL = release.URL + "/api"
		updateReleaseBaseURL = release.URL + "/releases"
		updateSyncSkills = func(context.Context, string, string) error { return nil }
		result, err := a.runUpdate(context.Background(), []string{"now", "--scheduled", "--server-trigger"}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !asBool(asMap(result.Data)["updated"]) {
			t.Fatalf("result=%v", result.Data)
		}
		binary, _ := os.ReadFile(path)
		if string(binary) != "updated" {
			t.Fatalf("binary=%q", binary)
		}
		state, err := readUpdateState(mustUpdateDir(t, a, cfg))
		if err != nil || state.ServerVersion != version || state.ServerTriggerResult != "updated" || state.LastAutomaticResult != "updated" {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	})
}

func TestScheduledSkillsSyncFailureIsRecordedAndSurfacedOnce(t *testing.T) {
	a, cfg, path := managedUpdateFixture(t)
	version := "v0.12.11"
	archive, _ := updateArchiveName(version)
	content := buildReleaseArchiveForTest(t, archive, []byte("updated"))
	release := newUpdateTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`{"tag_name":"v0.12.11"}`))
		case "/releases/download/" + version + "/" + archive:
			_, _ = w.Write(content)
		case "/releases/download/" + version + "/checksums.txt":
			_, _ = w.Write([]byte(sha256HexForTest(content) + "  " + archive + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer release.Close()
	oldAPI, oldBase := updateReleaseAPIURL, updateReleaseBaseURL
	oldSync := updateSyncSkills
	t.Cleanup(func() { updateReleaseAPIURL = oldAPI; updateReleaseBaseURL = oldBase; updateSyncSkills = oldSync })
	updateReleaseAPIURL = release.URL + "/api"
	updateReleaseBaseURL = release.URL + "/releases"
	var syncCalls atomic.Int32
	updateSyncSkills = func(context.Context, string, string) error {
		syncCalls.Add(1)
		return errors.New("managed skill drift")
	}
	result, err := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg)
	if err != nil || !asBool(asMap(result.Data)["updated"]) {
		t.Fatalf("scheduled update result=%v err=%v", result, err)
	}
	binary, err := os.ReadFile(path)
	if err != nil || string(binary) != "updated" {
		t.Fatalf("binary=%q err=%v", binary, err)
	}
	state, err := readUpdateState(mustUpdateDir(t, a, cfg))
	if err != nil || state.FailureStage != "" || state.LastAutomaticResult != "updated_skills_sync_attention" || state.SkillsSyncError == "" || !state.FollowUpNoticePending {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	status, err := a.runUpdateStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lastAttempt := asMap(asMap(status.Data)["last_automatic_attempt"])
	if anyString(lastAttempt["at"]) == "" || anyString(lastAttempt["result"]) != "updated_skills_sync_attention" {
		t.Fatalf("automatic attempt status=%v", lastAttempt)
	}
	if syncCalls.Load() != 1 {
		t.Fatalf("skills sync calls=%d", syncCalls.Load())
	}
	var starts atomic.Int32
	a.startUpdateWorker = func(string, string) error { starts.Add(1); return nil }
	a.startServerUpdateWorker = func(string, string, string) error { return nil }
	a.maybeScheduleUpdate("orient", nil, cfg)
	a.maybeScheduleUpdate("work list", nil, cfg)
	if starts.Load() != 1 || syncCalls.Load() != 1 {
		t.Fatalf("daily workers=%d skills sync calls=%d", starts.Load(), syncCalls.Load())
	}
	if warnings := a.updateFollowUpWarnings(cfg); len(warnings) != 1 || warnings[0].Code != "cli_update_follow_up" || !strings.Contains(warnings[0].Message, "anx skills status") || !strings.Contains(warnings[0].Message, "reload the anx skill") {
		t.Fatalf("warnings=%+v", warnings)
	}
	if warnings := a.updateFollowUpWarnings(cfg); len(warnings) != 0 {
		t.Fatalf("follow-up surfaced more than once: %+v", warnings)
	}
}

func mustUpdateDir(t *testing.T, a *App, cfg config.Resolved) string {
	t.Helper()
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return dir
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
	scheduled, scheduledErr := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg)
	if scheduledErr != nil || anyString(asMap(scheduled.Data)["skip_reason"]) != "no_installer_receipt" {
		t.Fatalf("scheduled unmanaged result=%v err=%v", scheduled, scheduledErr)
	}
	state, _ := readUpdateState(mustUpdateDir(t, a, cfg))
	if state.LastAutomaticResult != "skipped_unmanaged" {
		t.Fatalf("automatic skip result=%+v", state)
	}
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
