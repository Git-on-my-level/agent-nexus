package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "http://127.0.0.1:8000"
	DefaultAgent   = "default"
	DefaultTimeout = 30 * time.Second
)

type Options struct {
	Profile string
	Agent   string
	BaseURL string
	Timeout time.Duration
}

type Environment struct {
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	ReadFile    func(string) ([]byte, error)
	ReadDir     func(string) ([]os.DirEntry, error)
}

type Resolved struct {
	Agent       string
	BaseURL     string
	AccessToken string
	Timeout     time.Duration
	ProfilePath string
	Sources     map[string]string
}

type fileProfile struct {
	BaseURL     string `json:"base_url"`
	AccessToken string `json:"access_token"`
	Revoked     bool   `json:"revoked,omitempty"`
}

func Resolve(opts Options, env Environment) (Resolved, error) {
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	userHomeDir := env.UserHomeDir
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	readFile := env.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	readDir := env.ReadDir
	if readDir == nil {
		readDir = os.ReadDir
	}

	homeDir, err := userHomeDir()
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve home directory: %w", err)
	}

	resolved := Resolved{
		Agent:   DefaultAgent,
		BaseURL: DefaultBaseURL,
		Timeout: DefaultTimeout,
		Sources: map[string]string{
			"agent":   "default",
			"baseUrl": "default",
			"timeout": "default",
		},
	}
	if opts.Timeout > 0 {
		resolved.Timeout = opts.Timeout
		resolved.Sources["timeout"] = "flag:--timeout"
	}

	profileSelector := strings.TrimSpace(opts.Profile)
	explicitAgent := false
	if envAgent := strings.TrimSpace(getenv("ANX_AGENT")); envAgent != "" {
		resolved.Agent = envAgent
		resolved.Sources["agent"] = "env:ANX_AGENT"
		explicitAgent = true
	}
	if strings.TrimSpace(opts.Agent) != "" {
		resolved.Agent = strings.TrimSpace(opts.Agent)
		resolved.Sources["agent"] = "flag:--agent"
		explicitAgent = true
	}
	if profileSelector != "" && !looksLikePath(profileSelector) {
		resolved.Agent = profileSelector
		resolved.Sources["agent"] = "flag:--profile"
		explicitAgent = true
	}

	if !explicitAgent {
		agent, ok, err := loadDefaultAgent(homeDir, readFile)
		if err != nil {
			return Resolved{}, err
		}
		if ok && profileExists(homeDir, agent, readFile) {
			resolved.Agent = agent
			resolved.Sources["agent"] = "profile:default"
		} else if selected, ok, err := singleProfileAgent(homeDir, readDir); err != nil {
			return Resolved{}, err
		} else if ok {
			resolved.Agent = selected
			resolved.Sources["agent"] = "profile:auto-single"
		}
	}

	profilePath := strings.TrimSpace(getenv("ANX_PROFILE_PATH"))
	if profileSelector != "" && looksLikePath(profileSelector) {
		profilePath = profileSelector
	}
	if profilePath == "" {
		profilePath = ProfilePath(homeDir, resolved.Agent)
	}
	resolved.ProfilePath = profilePath

	if p, ok, err := loadProfile(profilePath, readFile); err != nil {
		return Resolved{}, err
	} else if ok {
		if p.Revoked {
			return Resolved{}, fmt.Errorf("profile %q is revoked", profilePath)
		}
		if strings.TrimSpace(p.BaseURL) != "" {
			resolved.BaseURL = strings.TrimSpace(p.BaseURL)
			resolved.Sources["baseUrl"] = "profile"
		}
		resolved.AccessToken = strings.TrimSpace(p.AccessToken)
	}

	if envBaseURL := strings.TrimSpace(getenv("ANX_BASE_URL")); envBaseURL != "" {
		resolved.BaseURL = envBaseURL
		resolved.Sources["baseUrl"] = "env:ANX_BASE_URL"
	}
	if strings.TrimSpace(opts.BaseURL) != "" {
		resolved.BaseURL = strings.TrimSpace(opts.BaseURL)
		resolved.Sources["baseUrl"] = "flag:--base-url"
	}
	if envAccessToken := strings.TrimSpace(getenv("ANX_ACCESS_TOKEN")); envAccessToken != "" {
		resolved.AccessToken = envAccessToken
	}
	return resolved, nil
}

func ProfilePath(homeDir, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = DefaultAgent
	}
	return filepath.Join(homeDir, ".config", "anx", "profiles", agent+".json")
}

func loadDefaultAgent(homeDir string, readFile func(string) ([]byte, error)) (string, bool, error) {
	content, err := readFile(filepath.Join(homeDir, ".config", "anx", "default-profile"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("load default profile: %w", err)
	}
	agent := strings.TrimSpace(string(content))
	return agent, agent != "", nil
}

func profileExists(homeDir, agent string, readFile func(string) ([]byte, error)) bool {
	_, err := readFile(ProfilePath(homeDir, agent))
	return err == nil
}

func singleProfileAgent(homeDir string, readDir func(string) ([]os.DirEntry, error)) (string, bool, error) {
	entries, err := readDir(filepath.Join(homeDir, ".config", "anx", "profiles"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("list local profiles: %w", err)
	}
	agents := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		agent := strings.TrimSuffix(entry.Name(), ".json")
		if strings.TrimSpace(agent) != "" {
			agents = append(agents, agent)
		}
	}
	sort.Strings(agents)
	if len(agents) == 1 {
		return agents[0], true, nil
	}
	if len(agents) > 1 {
		return "", false, fmt.Errorf("multiple local profiles found (%s); select one using --agent, --profile, ANX_AGENT, or `anx auth default <profile>`", strings.Join(agents, ", "))
	}
	return "", false, nil
}

func loadProfile(path string, readFile func(string) ([]byte, error)) (fileProfile, bool, error) {
	content, err := readFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileProfile{}, false, nil
		}
		return fileProfile{}, false, fmt.Errorf("read profile: %w", err)
	}
	var p fileProfile
	if err := json.Unmarshal(content, &p); err != nil {
		return fileProfile{}, false, fmt.Errorf("decode profile: %w", err)
	}
	return p, true, nil
}

func looksLikePath(value string) bool {
	value = strings.TrimSpace(value)
	return strings.ContainsRune(value, os.PathSeparator) || strings.HasSuffix(value, ".json") || strings.HasPrefix(value, ".")
}
