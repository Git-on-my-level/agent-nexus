package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agent-nexus-cli/internal/httpclient"
)

func TestRunUpdateCheckDoesNotCallHandshake(t *testing.T) {
	restoreVersion := httpclient.CLIVersion
	restoreBaseURL := updateReleaseBaseURL
	httpclient.CLIVersion = "v0.0.1"
	t.Cleanup(func() {
		httpclient.CLIVersion = restoreVersion
		updateReleaseBaseURL = restoreBaseURL
	})

	handshakeCalled := false
	coreServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meta/handshake" {
			handshakeCalled = true
		}
		http.NotFound(w, r)
	}))
	defer coreServer.Close()

	var releaseServer *httptest.Server
	releaseServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, releaseServer.URL+"/releases/tag/v0.0.3", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer releaseServer.Close()
	updateReleaseBaseURL = releaseServer.URL + "/releases"

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", coreServer.URL, "update", "--check"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "update" {
		t.Fatalf("unexpected command: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["target_version"]); got != "v0.0.3" {
		t.Fatalf("expected latest release target version, got %#v", data)
	}
	if got := anyStringValue(data["source"]); got != "latest_release" {
		t.Fatalf("expected latest release source, got %#v", data)
	}
	if got, _ := data["update_available"].(bool); !got {
		t.Fatalf("expected update_available=true, got %#v", data)
	}
	if handshakeCalled {
		t.Fatal("expected update check to avoid the core handshake endpoint")
	}
}

func TestRunUpdateCheckFallsBackToLatestRelease(t *testing.T) {
	restoreVersion := httpclient.CLIVersion
	restoreBaseURL := updateReleaseBaseURL
	httpclient.CLIVersion = "v0.0.1"
	t.Cleanup(func() {
		httpclient.CLIVersion = restoreVersion
		updateReleaseBaseURL = restoreBaseURL
	})

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			http.Redirect(w, r, server.URL+"/releases/tag/v0.0.3", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	updateReleaseBaseURL = server.URL + "/releases"

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "update", "--check"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["target_version"]); got != "v0.0.3" {
		t.Fatalf("expected latest release target version, got %#v", data)
	}
	if got := anyStringValue(data["source"]); got != "latest_release" {
		t.Fatalf("expected latest_release source, got %#v", data)
	}
}

func TestRunUpdateReplacesBinaryFromRequestedVersion(t *testing.T) {
	restoreVersion := httpclient.CLIVersion
	restoreBaseURL := updateReleaseBaseURL
	restoreExecPath := updateExecutablePath
	httpclient.CLIVersion = "v0.0.1"
	t.Cleanup(func() {
		httpclient.CLIVersion = restoreVersion
		updateReleaseBaseURL = restoreBaseURL
		updateExecutablePath = restoreExecPath
	})

	version := "v0.0.2"
	archiveName, err := updateArchiveName(version)
	if err != nil {
		t.Fatalf("archive name: %v", err)
	}
	archiveBytes := buildReleaseArchiveForTest(t, archiveName, []byte("new-binary-bytes"))
	checksum := sha256HexForTest(archiveBytes)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/download/" + version + "/" + archiveName:
			_, _ = w.Write(archiveBytes)
		case "/releases/download/" + version + "/checksums.txt":
			_, _ = w.Write([]byte(checksum + "  " + archiveName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	updateReleaseBaseURL = server.URL + "/releases"

	execDir := t.TempDir()
	execPath := filepath.Join(execDir, executableNameForTest())
	if err := os.WriteFile(execPath, []byte("old-binary-bytes"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}
	updateExecutablePath = func() (string, error) { return execPath, nil }

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "update", "--version", version})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got, _ := data["updated"].(bool); !got {
		t.Fatalf("expected updated=true, got %#v", data)
	}
	if got := anyStringValue(data["target_version"]); got != version {
		t.Fatalf("unexpected target version: %#v", data)
	}

	installedBytes, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installedBytes) != "new-binary-bytes" {
		t.Fatalf("expected replaced binary bytes, got %q", string(installedBytes))
	}
}

func TestHelpUpdateTopic(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = os.ReadFile

	exitCode := cli.Run([]string{"help", "update"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "anx update [--check] [--version <tag>]") {
		t.Fatalf("expected update help output, got %q", stdout.String())
	}
}

func buildReleaseArchiveForTest(t *testing.T, archiveName string, binary []byte) []byte {
	t.Helper()

	if strings.HasSuffix(archiveName, ".zip") {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		name := "oar.exe"
		file, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry: %v", err)
		}
		if _, err := file.Write(binary); err != nil {
			t.Fatalf("write zip entry: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("close zip: %v", err)
		}
		return buf.Bytes()
	}

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	header := &tar.Header{
		Name: "oar",
		Mode: 0o755,
		Size: int64(len(binary)),
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatalf("write tar payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return raw.Bytes()
}

func executableNameForTest() string {
	if runtime.GOOS == "windows" {
		return "oar.exe"
	}
	return "oar"
}

func sha256HexForTest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
