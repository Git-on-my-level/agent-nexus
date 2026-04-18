package bridgeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var sectionHeaderPattern = regexp.MustCompile(`^\s*\[([A-Za-z0-9_-]+)\]\s*(?:#.*)?$`)

type Config struct {
	Path    string
	BaseURL string
}

func RootDir(homeDir string) string {
	return filepath.Join(homeDir, ".config", "anx-bridge")
}

func Discover(homeDir string) ([]Config, error) {
	rootDir := RootDir(homeDir)
	paths, err := discoverConfigPaths(rootDir)
	if err != nil {
		return nil, err
	}

	configs := make([]Config, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read bridge config %s: %w", path, err)
		}
		baseURL := configStringValue(string(content), "anx", "base_url")
		if strings.TrimSpace(baseURL) == "" {
			continue
		}
		configs = append(configs, Config{Path: path, BaseURL: strings.TrimSpace(baseURL)})
	}
	return configs, nil
}

func discoverConfigPaths(rootDir string) ([]string, error) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bridge config root: %w", err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		entryPath := filepath.Join(rootDir, entry.Name())
		if entry.IsDir() {
			nested, err := os.ReadDir(entryPath)
			if err != nil {
				return nil, fmt.Errorf("read bridge config directory %s: %w", entryPath, err)
			}
			for _, child := range nested {
				if child.IsDir() || !strings.HasSuffix(child.Name(), ".toml") {
					continue
				}
				paths = append(paths, filepath.Join(entryPath, child.Name()))
			}
			continue
		}
		if strings.HasSuffix(entry.Name(), ".toml") {
			paths = append(paths, entryPath)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func configStringValue(content string, section string, key string) string {
	currentSection := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if matches := sectionHeaderPattern.FindStringSubmatch(line); len(matches) == 2 {
			currentSection = matches[1]
			continue
		}
		if currentSection != section {
			continue
		}
		name, rawValue, ok := parseAssignment(line)
		if !ok || name != key {
			continue
		}
		return rawValue
	}
	return ""
}

func parseAssignment(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(trimmed[:idx])
	rawValue := strings.TrimSpace(trimmed[idx+1:])
	if commentIdx := strings.Index(rawValue, "#"); commentIdx >= 0 {
		rawValue = strings.TrimSpace(rawValue[:commentIdx])
	}
	if len(rawValue) >= 2 && strings.HasPrefix(rawValue, "\"") && strings.HasSuffix(rawValue, "\"") {
		if unquoted, err := strconv.Unquote(rawValue); err == nil {
			rawValue = unquoted
		}
	}
	if name == "" || rawValue == "" {
		return "", "", false
	}
	return name, rawValue, true
}
