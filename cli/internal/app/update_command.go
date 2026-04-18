package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

var (
	updateReleaseBaseURL = "https://github.com/Git-on-my-level/agent-nexus/releases"
	updateExecutablePath = os.Executable
	updateMkdirTemp      = os.MkdirTemp
	updateRemoveAll      = os.RemoveAll
	updateStat           = os.Stat
	updateWriteFile      = os.WriteFile
	updateChmod          = os.Chmod
	updateRename         = os.Rename
	updateGOOS           = runtime.GOOS
	updateGOARCH         = runtime.GOARCH
)

type updatePlan struct {
	CurrentVersion  string
	TargetVersion   string
	InstallPath     string
	ArchiveName     string
	Source          string
	UpdateAvailable bool
	AlreadyCurrent  bool
}

func (a *App) runUpdate(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("update")
	var (
		checkFlag   trackedBool
		versionFlag trackedString
	)
	fs.Var(&checkFlag, "check", "Report update availability without downloading or replacing the binary")
	fs.Var(&versionFlag, "version", "Install a specific release tag (for example v1.2.3)")

	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_update_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_update_args", "unexpected positional arguments for `anx update`")
	}

	plan, err := buildUpdatePlan(ctx, cfg, strings.TrimSpace(versionFlag.value))
	if err != nil {
		return nil, err
	}
	if checkFlag.value || plan.AlreadyCurrent {
		return renderUpdatePlan(plan, false), nil
	}

	binaryBytes, mode, err := downloadUpdateBinary(ctx, cfg.Timeout, plan.TargetVersion, plan.ArchiveName)
	if err != nil {
		return nil, err
	}
	if err := replaceExecutable(plan.InstallPath, binaryBytes, mode); err != nil {
		return nil, err
	}
	return renderUpdatePlan(plan, true), nil
}

func buildUpdatePlan(ctx context.Context, cfg config.Resolved, requestedVersion string) (updatePlan, error) {
	installPath, err := updateExecutablePath()
	if err != nil {
		return updatePlan{}, errnorm.Wrap(errnorm.KindLocal, "install_path_unavailable", "failed to resolve current executable path", err)
	}

	plan := updatePlan{
		CurrentVersion: normalizeReleaseTag(httpclient.CLIVersion),
		InstallPath:    installPath,
	}

	if requestedVersion != "" {
		plan.TargetVersion = normalizeReleaseTag(requestedVersion)
		plan.Source = "explicit_version"
		plan.ArchiveName, err = updateArchiveName(plan.TargetVersion)
		if err != nil {
			return updatePlan{}, err
		}
		plan.AlreadyCurrent = strings.EqualFold(plan.CurrentVersion, plan.TargetVersion)
		plan.UpdateAvailable = !plan.AlreadyCurrent
		return plan, nil
	}

	latest, latestErr := resolveLatestReleaseTag(ctx, cfg.Timeout)
	if latestErr != nil {
		return updatePlan{}, errnorm.Wrap(errnorm.KindNetwork, "update_unavailable", "failed to resolve latest release version", latestErr)
	}
	plan.TargetVersion = latest
	plan.Source = "latest_release"
	plan.ArchiveName, err = updateArchiveName(plan.TargetVersion)
	if err != nil {
		return updatePlan{}, err
	}

	comparison, compareErr := compareSemanticVersions(plan.CurrentVersion, plan.TargetVersion)
	if compareErr == nil {
		plan.AlreadyCurrent = comparison >= 0
	} else {
		plan.AlreadyCurrent = strings.EqualFold(plan.CurrentVersion, plan.TargetVersion)
	}
	plan.UpdateAvailable = !plan.AlreadyCurrent
	return plan, nil
}

func renderUpdatePlan(plan updatePlan, updated bool) *commandResult {
	data := map[string]any{
		"current_version":  plan.CurrentVersion,
		"target_version":   plan.TargetVersion,
		"install_path":     plan.InstallPath,
		"archive_name":     plan.ArchiveName,
		"source":           plan.Source,
		"update_available": plan.UpdateAvailable,
		"already_current":  plan.AlreadyCurrent,
		"updated":          updated,
	}

	lines := []string{
		"CLI update",
		"Current version: " + displayValue(plan.CurrentVersion),
		"Target version: " + displayValue(plan.TargetVersion),
		"Source: " + displayValue(plan.Source),
		"Install path: " + displayValue(plan.InstallPath),
		"Archive: " + displayValue(plan.ArchiveName),
	}
	switch {
	case updated:
		lines = append(lines, "Status: updated in place; re-run `anx version` to confirm the active binary.")
	case plan.AlreadyCurrent:
		lines = append(lines, "Status: already at or above the selected target version.")
	default:
		lines = append(lines, "Status: update available.")
	}
	return &commandResult{Data: data, Text: strings.Join(lines, "\n")}
}

func resolveLatestReleaseTag(ctx context.Context, timeout time.Duration) (string, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(updateReleaseBaseURL, "/")+"/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tag := strings.TrimSpace(pathBase(resp.Request.URL.Path))
	if tag == "" || strings.EqualFold(tag, "latest") {
		return "", fmt.Errorf("latest release redirect did not resolve a tag")
	}
	return normalizeReleaseTag(tag), nil
}

func updateArchiveName(version string) (string, error) {
	goos := strings.TrimSpace(updateGOOS)
	goarch := strings.TrimSpace(updateGOARCH)
	version = normalizeReleaseTag(version)
	if version == "" {
		return "", errnorm.Local("version_required", "release version is required to resolve the update archive name")
	}
	switch goos {
	case "linux", "darwin", "windows":
	default:
		return "", errnorm.Local("unsupported_os", fmt.Sprintf("unsupported OS: %s", goos))
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", errnorm.Local("unsupported_arch", fmt.Sprintf("unsupported architecture: %s", goarch))
	}

	if goos == "windows" {
		return fmt.Sprintf("oar_%s_%s_%s.zip", version, goos, goarch), nil
	}
	return fmt.Sprintf("oar_%s_%s_%s.tar.gz", version, goos, goarch), nil
}

func downloadUpdateBinary(ctx context.Context, timeout time.Duration, version string, archiveName string) ([]byte, os.FileMode, error) {
	client := &http.Client{Timeout: timeout}
	baseURL := strings.TrimRight(updateReleaseBaseURL, "/") + "/download/" + normalizeReleaseTag(version)

	archiveBytes, err := fetchBytes(ctx, client, baseURL+"/"+archiveName)
	if err != nil {
		return nil, 0, errnorm.Wrap(errnorm.KindNetwork, "download_failed", "failed to download CLI release archive", err)
	}
	checksumBytes, err := fetchBytes(ctx, client, baseURL+"/checksums.txt")
	if err != nil {
		return nil, 0, errnorm.Wrap(errnorm.KindNetwork, "download_failed", "failed to download CLI checksum manifest", err)
	}
	if err := verifyReleaseChecksum(archiveName, archiveBytes, checksumBytes); err != nil {
		return nil, 0, err
	}
	return extractReleaseBinary(archiveName, archiveBytes)
}

func fetchBytes(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
	return body, nil
}

func verifyReleaseChecksum(archiveName string, archiveBytes []byte, checksumBytes []byte) error {
	expected := ""
	for _, line := range strings.Split(string(checksumBytes), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		if fields[len(fields)-1] == archiveName {
			expected = strings.TrimSpace(fields[0])
			break
		}
	}
	if expected == "" {
		return errnorm.Local("checksum_missing", fmt.Sprintf("archive %s not found in checksums.txt", archiveName))
	}
	actualBytes := sha256.Sum256(archiveBytes)
	actual := hex.EncodeToString(actualBytes[:])
	if !strings.EqualFold(expected, actual) {
		return errnorm.WithDetails(
			errnorm.Local("checksum_mismatch", "downloaded CLI archive checksum did not match checksums.txt"),
			map[string]any{"expected": expected, "actual": actual, "archive_name": archiveName},
		)
	}
	return nil
}

func extractReleaseBinary(archiveName string, archiveBytes []byte) ([]byte, os.FileMode, error) {
	if strings.HasSuffix(archiveName, ".zip") {
		return extractZIPBinary(archiveBytes)
	}
	return extractTarGZBinary(archiveBytes)
}

func extractZIPBinary(archiveBytes []byte) ([]byte, os.FileMode, error) {
	reader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to read CLI zip archive", err)
	}
	for _, file := range reader.File {
		if pathBase(file.Name) != "oar.exe" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to open CLI binary inside zip archive", err)
		}
		defer rc.Close()
		body, err := io.ReadAll(rc)
		if err != nil {
			return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to read CLI binary inside zip archive", err)
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o755
		}
		return body, mode, nil
	}
	return nil, 0, errnorm.Local("archive_invalid", "CLI zip archive did not contain oar.exe")
}

func extractTarGZBinary(archiveBytes []byte) ([]byte, os.FileMode, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to open CLI tar.gz archive", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to read CLI tar archive", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if pathBase(header.Name) != "oar" {
			continue
		}
		body, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, 0, errnorm.Wrap(errnorm.KindLocal, "archive_invalid", "failed to read CLI binary inside tar archive", err)
		}
		mode := os.FileMode(header.Mode)
		if mode == 0 {
			mode = 0o755
		}
		return body, mode, nil
	}
	return nil, 0, errnorm.Local("archive_invalid", "CLI tar.gz archive did not contain oar")
}

func replaceExecutable(installPath string, binaryBytes []byte, mode os.FileMode) error {
	info, err := updateStat(installPath)
	if err == nil && info.Mode() != 0 {
		mode = info.Mode().Perm()
	}
	if mode == 0 {
		mode = 0o755
	}

	tmpDir, err := updateMkdirTemp(filepath.Dir(installPath), ".anx-update-")
	if err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "update_write_failed", "failed to allocate a temporary install directory", err)
	}
	defer func() { _ = updateRemoveAll(tmpDir) }()

	tmpPath := filepath.Join(tmpDir, filepath.Base(installPath))
	if err := updateWriteFile(tmpPath, binaryBytes, mode); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "update_write_failed", "failed to write the updated CLI binary", err)
	}
	if err := updateChmod(tmpPath, mode); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "update_write_failed", "failed to set executable permissions on the updated CLI binary", err)
	}
	if err := updateRename(tmpPath, installPath); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "update_replace_failed", "failed to replace the current CLI binary in place", err)
	}
	return nil
}

func compareSemanticVersions(left string, right string) (int, error) {
	leftParts, err := parseSemanticVersion(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parseSemanticVersion(right)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		if leftParts[i] < rightParts[i] {
			return -1, nil
		}
		if leftParts[i] > rightParts[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemanticVersion(raw string) ([3]int, error) {
	var out [3]int
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "v"))
	if raw == "" {
		return out, fmt.Errorf("empty version")
	}
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, fmt.Errorf("invalid semantic version: %s", raw)
	}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil || value < 0 {
			return out, fmt.Errorf("invalid semantic version: %s", raw)
		}
		out[i] = value
	}
	return out, nil
}

func normalizeReleaseTag(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "v") {
		return "v" + strings.TrimPrefix(raw[1:], "v")
	}
	return "v" + raw
}

func displayValue(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "(none)"
	}
	return strings.TrimSpace(raw)
}

func pathBase(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	return filepath.Base(raw)
}

func updateUsageText() string {
	return strings.TrimSpace(`Update the installed anx CLI binary in place.

Usage:
  anx update [--check] [--version <tag>]

Options:
  --check          report the selected target version without changing the binary
  --version <tag>  install a specific release tag instead of the recommended/latest version

Behavior:
  - resolves the latest release from GitHub when no explicit version is provided
  - downloads the matching release archive for the current OS/arch and replaces the current binary

Examples:
  anx update --check
  anx update
  anx update --version v1.2.3`)
}
