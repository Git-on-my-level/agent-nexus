package bridgeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type Config struct {
	Path    string
	BaseURL string
}

type bridgeConfigFile struct {
	AgentHome string `toml:"agent_home"`
	ANX       struct {
		BaseURL string `toml:"base_url"`
	} `toml:"anx"`
}

type agentManifestFile struct {
	Identity struct {
		BaseURL string `toml:"base_url"`
	} `toml:"identity"`
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
		baseURL := bridgeConfigBaseURL(path, string(content))
		if strings.TrimSpace(baseURL) == "" {
			continue
		}
		configs = append(configs, Config{Path: path, BaseURL: strings.TrimSpace(baseURL)})
	}
	return configs, nil
}

func bridgeConfigBaseURL(configPath string, content string) string {
	var config bridgeConfigFile
	if err := toml.Unmarshal([]byte(content), &config); err != nil {
		return ""
	}
	if baseURL := strings.TrimSpace(config.ANX.BaseURL); baseURL != "" {
		return baseURL
	}
	agentHome := strings.TrimSpace(config.AgentHome)
	if agentHome == "" {
		return ""
	}
	agentHomePath := expandConfigPath(filepath.Dir(configPath), agentHome)
	manifestPath := filepath.Join(agentHomePath, "agent.toml")
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		return ""
	}
	var manifest agentManifestFile
	if err := toml.Unmarshal(manifestContent, &manifest); err != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Identity.BaseURL)
}

func expandConfigPath(baseDir string, raw string) string {
	value := os.ExpandEnv(strings.TrimSpace(raw))
	if value == "~" || strings.HasPrefix(value, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if value == "~" {
				value = home
			} else {
				value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
			}
		}
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(baseDir, value)
	}
	return filepath.Clean(value)
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
