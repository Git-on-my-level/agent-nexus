package heartbeat

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func DiscoverLastSuccessfulBackupAt(workspaceRoot string) (*string, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil, nil
	}

	candidates := []string{
		filepath.Join(workspaceRoot, "backups"),
		filepath.Join(filepath.Dir(workspaceRoot), "backups"),
	}

	var newest time.Time
	for _, backupRoot := range uniqueStrings(candidates) {
		entries, err := filepath.Glob(filepath.Join(backupRoot, "*", "manifest.env"))
		if err != nil {
			return nil, fmt.Errorf("scan backup manifests: %w", err)
		}
		for _, manifestPath := range entries {
			createdAt, ok, err := loadBackupCreatedAt(manifestPath, workspaceRoot)
			if err != nil {
				return nil, err
			}
			if !ok || createdAt.IsZero() || !createdAt.After(newest) {
				continue
			}
			newest = createdAt
		}
	}

	if newest.IsZero() {
		return nil, nil
	}
	formatted := newest.UTC().Format(time.RFC3339Nano)
	return &formatted, nil
}

func loadBackupCreatedAt(manifestPath string, workspaceRoot string) (time.Time, bool, error) {
	values, err := readManifestEnv(manifestPath)
	if err != nil {
		return time.Time{}, false, err
	}
	sourceWorkspaceRoot := strings.TrimSpace(values["SOURCE_WORKSPACE_ROOT"])
	if sourceWorkspaceRoot != "" && filepath.Clean(sourceWorkspaceRoot) != filepath.Clean(workspaceRoot) {
		return time.Time{}, false, nil
	}

	createdAt := strings.TrimSpace(values["CREATED_AT"])
	if createdAt != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			parsed, err := time.Parse(layout, createdAt)
			if err == nil {
				return parsed.UTC(), true, nil
			}
		}
	}

	info, err := os.Stat(manifestPath)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("stat backup manifest %s: %w", manifestPath, err)
	}
	return info.ModTime().UTC(), true, nil
}

func readManifestEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open backup manifest %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read backup manifest %s: %w", path, err)
	}
	return values, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		value = filepath.Clean(value)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
