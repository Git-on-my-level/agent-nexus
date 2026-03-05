package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "http://127.0.0.1:8000"
	DefaultAgent   = "default"
	DefaultTimeout = 10 * time.Second
)

type Overrides struct {
	JSON    *bool
	BaseURL *string
	Agent   *string
	NoColor *bool
	Timeout *time.Duration
}

type Profile struct {
	BaseURL     string `json:"base_url"`
	Timeout     string `json:"timeout"`
	NoColor     *bool  `json:"no_color,omitempty"`
	JSON        *bool  `json:"json,omitempty"`
	AccessToken string `json:"access_token"`
}

type Resolved struct {
	JSON        bool
	BaseURL     string
	Agent       string
	NoColor     bool
	Timeout     time.Duration
	AccessToken string
	ProfilePath string
	Sources     map[string]string
}

type Environment struct {
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	ReadFile    func(string) ([]byte, error)
}

func Resolve(overrides Overrides, env Environment) (Resolved, error) {
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

	resolved := Resolved{
		JSON:    false,
		BaseURL: DefaultBaseURL,
		Agent:   DefaultAgent,
		NoColor: false,
		Timeout: DefaultTimeout,
		Sources: map[string]string{
			"json":     "default",
			"base_url": "default",
			"agent":    "default",
			"no_color": "default",
			"timeout":  "default",
		},
	}

	if envAgent := strings.TrimSpace(getenv("OAR_AGENT")); envAgent != "" {
		resolved.Agent = envAgent
		resolved.Sources["agent"] = "env:OAR_AGENT"
	}
	if overrides.Agent != nil && strings.TrimSpace(*overrides.Agent) != "" {
		resolved.Agent = strings.TrimSpace(*overrides.Agent)
		resolved.Sources["agent"] = "flag:--agent"
	}

	homeDir, err := userHomeDir()
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve home directory: %w", err)
	}
	profilePath := strings.TrimSpace(getenv("OAR_PROFILE_PATH"))
	if profilePath == "" {
		profilePath = DefaultProfilePath(homeDir, resolved.Agent)
	}
	resolved.ProfilePath = profilePath

	profile, profileLoaded, err := loadProfile(readFile, profilePath)
	if err != nil {
		return Resolved{}, err
	}
	if profileLoaded {
		if strings.TrimSpace(profile.BaseURL) != "" {
			resolved.BaseURL = strings.TrimSpace(profile.BaseURL)
			resolved.Sources["base_url"] = "profile"
		}
		if strings.TrimSpace(profile.Timeout) != "" {
			dur, err := time.ParseDuration(strings.TrimSpace(profile.Timeout))
			if err != nil {
				return Resolved{}, fmt.Errorf("parse profile timeout %q: %w", profile.Timeout, err)
			}
			resolved.Timeout = dur
			resolved.Sources["timeout"] = "profile"
		}
		if profile.NoColor != nil {
			resolved.NoColor = *profile.NoColor
			resolved.Sources["no_color"] = "profile"
		}
		if profile.JSON != nil {
			resolved.JSON = *profile.JSON
			resolved.Sources["json"] = "profile"
		}
		if strings.TrimSpace(profile.AccessToken) != "" {
			resolved.AccessToken = strings.TrimSpace(profile.AccessToken)
		}
	}

	if envBaseURL := strings.TrimSpace(getenv("OAR_BASE_URL")); envBaseURL != "" {
		resolved.BaseURL = envBaseURL
		resolved.Sources["base_url"] = "env:OAR_BASE_URL"
	}
	if envNoColor := strings.TrimSpace(getenv("OAR_NO_COLOR")); envNoColor != "" {
		value, err := strconv.ParseBool(envNoColor)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse OAR_NO_COLOR: %w", err)
		}
		resolved.NoColor = value
		resolved.Sources["no_color"] = "env:OAR_NO_COLOR"
	}
	if envJSON := strings.TrimSpace(getenv("OAR_JSON")); envJSON != "" {
		value, err := strconv.ParseBool(envJSON)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse OAR_JSON: %w", err)
		}
		resolved.JSON = value
		resolved.Sources["json"] = "env:OAR_JSON"
	}
	if envTimeout := strings.TrimSpace(getenv("OAR_TIMEOUT")); envTimeout != "" {
		dur, err := time.ParseDuration(envTimeout)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse OAR_TIMEOUT: %w", err)
		}
		resolved.Timeout = dur
		resolved.Sources["timeout"] = "env:OAR_TIMEOUT"
	}
	if envToken := strings.TrimSpace(getenv("OAR_ACCESS_TOKEN")); envToken != "" {
		resolved.AccessToken = envToken
	}

	if overrides.BaseURL != nil && strings.TrimSpace(*overrides.BaseURL) != "" {
		resolved.BaseURL = strings.TrimSpace(*overrides.BaseURL)
		resolved.Sources["base_url"] = "flag:--base-url"
	}
	if overrides.NoColor != nil {
		resolved.NoColor = *overrides.NoColor
		resolved.Sources["no_color"] = "flag:--no-color"
	}
	if overrides.JSON != nil {
		resolved.JSON = *overrides.JSON
		resolved.Sources["json"] = "flag:--json"
	}
	if overrides.Timeout != nil {
		resolved.Timeout = *overrides.Timeout
		resolved.Sources["timeout"] = "flag:--timeout"
	}

	if strings.TrimSpace(resolved.BaseURL) == "" {
		return Resolved{}, fmt.Errorf("base url must not be empty")
	}
	if resolved.Timeout <= 0 {
		return Resolved{}, fmt.Errorf("timeout must be greater than zero")
	}
	return resolved, nil
}

func loadProfile(readFile func(string) ([]byte, error), path string) (Profile, bool, error) {
	content, err := readFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Profile{}, false, nil
		}
		return Profile{}, false, fmt.Errorf("read profile %s: %w", path, err)
	}
	var profile Profile
	if err := json.Unmarshal(content, &profile); err != nil {
		return Profile{}, false, fmt.Errorf("parse profile %s: %w", path, err)
	}
	return profile, true, nil
}

func DefaultProfilePath(homeDir string, agent string) string {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		agent = DefaultAgent
	}
	return filepath.Join(homeDir, ".config", "oar", "profiles", agent+".json")
}
