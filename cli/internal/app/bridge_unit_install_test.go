package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func stubCoreServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
}

type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "unit" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// The unit file must be installed through a temp write, an explicit mode, and
// a rename, so a preexisting world-readable unit can never survive a
// reinstall; symlinked targets are refused before anything is written.
func TestBridgeUnitInstallWritesTempChmodRenameAndSkipsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unit install targets launchd/systemd hosts")
	}
	server := stubCoreServer()
	defer server.Close()

	originalMkdir, originalWrite, originalRead := bridgeMkdirAll, bridgeWriteFile, bridgeReadFile
	originalLstat, originalChmod, originalRename, originalStat := bridgeLstat, bridgeChmod, bridgeRename, bridgeStat
	originalRun := bridgeCommandRun
	t.Cleanup(func() {
		bridgeMkdirAll, bridgeWriteFile, bridgeReadFile = originalMkdir, originalWrite, originalRead
		bridgeLstat, bridgeChmod, bridgeRename, bridgeStat = originalLstat, originalChmod, originalRename, originalStat
		bridgeCommandRun = originalRun
	})

	a, _ := deliveryTestApp(t, server.URL, map[string]string{"ANX_BASE_URL": server.URL})
	var tempWrites []string
	var chmodPerms []os.FileMode
	var renamePairs [][2]string
	bridgeMkdirAll = func(string, os.FileMode) error { return nil }
	bridgeReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	bridgeLstat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	bridgeStat = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	bridgeWriteFile = func(path string, _ []byte, _ os.FileMode) error {
		tempWrites = append(tempWrites, path)
		return nil
	}
	bridgeChmod = func(_ string, perm os.FileMode) error {
		chmodPerms = append(chmodPerms, perm)
		return nil
	}
	bridgeRename = func(from, to string) error {
		renamePairs = append(renamePairs, [2]string{from, to})
		return nil
	}
	bridgeCommandRun = func(context.Context, string, ...string) (string, string, error) { return "", "", nil }

	if _, err := a.runBridgeUnitInstall(context.Background(), []string{"--as", "worker"}, deliveryTestCfg(server.URL)); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(tempWrites) != 1 || !strings.HasSuffix(tempWrites[0], ".anx-install_tmp") {
		t.Fatalf("unit must be written through a temp file: %v", tempWrites)
	}
	if len(renamePairs) != 1 || renamePairs[0][0] != tempWrites[0] {
		t.Fatalf("unit must be installed by renaming the temp file: %v", renamePairs)
	}
	if len(chmodPerms) == 0 || chmodPerms[len(chmodPerms)-1] != 0o644 {
		t.Fatalf("final mode must be forced to 0644: %v", chmodPerms)
	}

	// A symlink at the unit path must be refused before any write happens.
	bridgeLstat = func(string) (os.FileInfo, error) { return fakeFileInfo{mode: os.ModeSymlink}, nil }
	if _, err := a.runBridgeUnitInstall(context.Background(), []string{"--as", "worker"}, deliveryTestCfg(server.URL)); err == nil {
		t.Fatal("symlinked unit path must be refused")
	}
	if len(tempWrites) != 1 {
		t.Fatalf("refusal must happen before any write: %v", tempWrites)
	}
}

func TestBridgeUnitPlanRejectsUnsafeCharacters(t *testing.T) {
	cases := []struct {
		name      string
		execPath  string
		logPath   string
		env       map[string]string
		wantError bool
	}{
		{"plain", "/usr/bin/anx", "/logs/bridge.log", nil, false},
		{"newline in binary", "/usr/bin/an\nx", "/logs/bridge.log", nil, true},
		{"quote in binary", `/usr/bin/an"x`, "/logs/bridge.log", nil, true},
		{"backslash in binary", `/usr/bin/an\x`, "/logs/bridge.log", nil, true},
		{"quote in log path", "/usr/bin/anx", `/lo"gs/bridge.log`, nil, true},
		{"control char in env", "/usr/bin/anx", "/logs/bridge.log", map[string]string{"ANX_BASE_URL": "https://core.example.test\nEVIL=1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := answerBridgeUnitPlan("/srv/anx-it", "worker", tc.execPath, tc.logPath, tc.env)
			if tc.wantError && err == nil {
				t.Fatal("expected rejection")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBridgeUnitPlanSystemdQuotingAndLogPathRules(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd rendering rules apply on linux")
	}
	plan, err := answerBridgeUnitPlan("/srv/anx-it", "worker", "/opt/My Tools/anx", "/logs/bridge.log", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Content, `ExecStart="/opt/My Tools/anx" bridge run --as worker`) {
		t.Fatalf("spaced binary path must be quoted: %s", plan.Content)
	}
	if _, err := answerBridgeUnitPlan("/srv/anx-it", "worker", "/opt/anx", "/logs/My Bridge.log", nil); err == nil {
		t.Fatal("spaced log path must be rejected: systemd append: ends the assignment at whitespace")
	}
}
