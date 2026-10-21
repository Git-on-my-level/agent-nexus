package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomaticUpdateWorkersLeaveCommandOutputAndExitUnchanged(t *testing.T) {
	_, cfg, _ := managedUpdateFixture(t)
	newApp := func() (*App, *bytes.Buffer) {
		a := newTestApp(t)
		stdout := &bytes.Buffer{}
		a.Stdout = stdout
		a.Stdin = strings.NewReader("")
		a.StdinIsTTY = func() bool { return false }
		a.StdoutIsTTY = func() bool { return false }
		a.startSkillsRefresh = func(string, string, string) error { return nil }
		return a, stdout
	}
	args := []string{"--config-dir", cfg.ConfigDir, "--as", "worker", "version"}
	baseline, baselineOutput := newApp()
	baselineCode := baseline.Run(args)

	auto, autoOutput := newApp()
	auto.runScheduledMaintenanceInTests = true
	auto.startUpdateWorker = func(string, string) error {
		if autoOutput.Len() == 0 {
			t.Error("automatic update worker started before command output")
		}
		return errors.New("simulated update worker start failure")
	}
	auto.startServerUpdateWorker = func(string, string, string) error {
		if autoOutput.Len() == 0 {
			t.Error("server-version worker started before command output")
		}
		return errors.New("simulated server worker start failure")
	}
	autoCode := auto.Run(args)
	if autoCode != baselineCode || autoCode != 0 {
		t.Fatalf("exit code changed: baseline=%d automatic=%d", baselineCode, autoCode)
	}
	if autoOutput.String() != baselineOutput.String() {
		t.Fatalf("automatic update changed command output\nbaseline: %q\nautomatic: %q", baselineOutput.String(), autoOutput.String())
	}
}

func TestUpdateFollowUpWarningUsesLeadTokenText(t *testing.T) {
	a, cfg, _ := managedUpdateFixture(t)
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeUpdateJSON(filepath.Join(dir, "state.json"), updateState{
		Rollback:              "not_needed",
		SkillsSynced:          true,
		FollowUpNoticePending: true,
		FollowUpNoticeID:      "v0.12.11-install",
		LatestVersion:         "v0.12.11",
	}); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	a.Stdout = stdout
	a.Stdin = strings.NewReader("")
	a.StdinIsTTY = func() bool { return false }
	a.StdoutIsTTY = func() bool { return false }
	a.startSkillsRefresh = func(string, string, string) error { return nil }
	a.runScheduledMaintenanceInTests = true
	a.startUpdateWorker = nil
	a.startServerUpdateWorker = nil
	if code := a.Run([]string{"--config-dir", cfg.ConfigDir, "--as", "worker", "version"}); code != 0 {
		t.Fatalf("exit=%d output=%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "warning code=cli_update_follow_up") || !strings.Contains(stdout.String(), "reload the anx skill") {
		t.Fatalf("notice is not a lead-token warning: %s", stdout.String())
	}
}
