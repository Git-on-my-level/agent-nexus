package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-cli/internal/profile"
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
	BaseURL              string `json:"base_url"`
	Timeout              string `json:"timeout"`
	NoColor              *bool  `json:"no_color,omitempty"`
	JSON                 *bool  `json:"json,omitempty"`
	AccessToken          string `json:"access_token"`
	RefreshToken         string `json:"refresh_token"`
	TokenType            string `json:"token_type,omitempty"`
	AccessTokenExpiresAt string `json:"access_token_expires_at,omitempty"`
	AgentID              string `json:"agent_id,omitempty"`
	ActorID              string `json:"actor_id,omitempty"`
	KeyID                string `json:"key_id,omitempty"`
	Username             string `json:"username,omitempty"`
	PrivateKeyPath       string `json:"private_key_path,omitempty"`
	Revoked              bool   `json:"revoked,omitempty"`
	CoreInstanceID       string `json:"core_instance_id,omitempty"`
}

type Resolved struct {
	JSON                 bool
	BaseURL              string
	Agent                string
	NoColor              bool
	Timeout              time.Duration
	AccessToken          string
	RefreshToken         string
	TokenType            string
	AccessTokenExpiresAt string
	AgentID              string
	ActorID              string
	KeyID                string
	Username             string
	PrivateKeyPath       string
	Revoked              bool
	CoreInstanceID       string
	ProfilePath          string
	Sources              map[string]string
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

	explicitAgent := false
	if envAgent := strings.TrimSpace(getenv("OAR_AGENT")); envAgent != "" {
		resolved.Agent = envAgent
		resolved.Sources["agent"] = "env:OAR_AGENT"
		explicitAgent = true
	}
	if overrides.Agent != nil && strings.TrimSpace(*overrides.Agent) != "" {
		resolved.Agent = strings.TrimSpace(*overrides.Agent)
		resolved.Sources["agent"] = "flag:--agent"
		explicitAgent = true
	}

	homeDir, err := userHomeDir()
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve home directory: %w", err)
	}

	if !explicitAgent {
		agents, err := profile.ListAgents(homeDir)
		if err != nil {
			return Resolved{}, fmt.Errorf("list local profiles: %w", err)
		}
		if len(agents) == 1 {
			resolved.Agent = agents[0]
			resolved.Sources["agent"] = "profile:auto-single"
		}
		if len(agents) > 1 {
			return Resolved{}, fmt.Errorf("multiple local profiles found (%s); select one using --agent or OAR_AGENT", strings.Join(agents, ", "))
		}
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
		if strings.TrimSpace(profile.RefreshToken) != "" {
			resolved.RefreshToken = strings.TrimSpace(profile.RefreshToken)
		}
		resolved.TokenType = strings.TrimSpace(profile.TokenType)
		resolved.AccessTokenExpiresAt = strings.TrimSpace(profile.AccessTokenExpiresAt)
		resolved.AgentID = strings.TrimSpace(profile.AgentID)
		resolved.ActorID = strings.TrimSpace(profile.ActorID)
		resolved.KeyID = strings.TrimSpace(profile.KeyID)
		resolved.Username = strings.TrimSpace(profile.Username)
		resolved.PrivateKeyPath = strings.TrimSpace(profile.PrivateKeyPath)
		resolved.Revoked = profile.Revoked
		resolved.CoreInstanceID = strings.TrimSpace(profile.CoreInstanceID)
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
	return profile.ProfilePath(homeDir, agent)
}
