package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/bridgeconfig"
	"agent-nexus-cli/internal/profile"
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
	Verbose *bool
	Headers *bool
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
	Verbose              bool
	Headers              bool
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

func Defaults(overrides Overrides) Resolved {
	r := Resolved{
		BaseURL: DefaultBaseURL,
		Agent:   DefaultAgent,
		Timeout: DefaultTimeout,
		Sources: map[string]string{},
	}
	if overrides.JSON != nil {
		r.JSON = *overrides.JSON
	}
	if overrides.BaseURL != nil && strings.TrimSpace(*overrides.BaseURL) != "" {
		r.BaseURL = strings.TrimSpace(*overrides.BaseURL)
	}
	if overrides.Agent != nil && strings.TrimSpace(*overrides.Agent) != "" {
		r.Agent = strings.TrimSpace(*overrides.Agent)
	}
	if overrides.NoColor != nil {
		r.NoColor = *overrides.NoColor
	}
	if overrides.Verbose != nil {
		r.Verbose = *overrides.Verbose
	}
	if overrides.Headers != nil {
		r.Headers = *overrides.Headers
	}
	if overrides.Timeout != nil {
		r.Timeout = *overrides.Timeout
	}
	return r
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
		Verbose: false,
		Headers: false,
		Timeout: DefaultTimeout,
		Sources: map[string]string{
			"json":     "default",
			"base_url": "default",
			"agent":    "default",
			"no_color": "default",
			"verbose":  "default",
			"headers":  "default",
			"timeout":  "default",
		},
	}

	explicitAgent := false
	if envAgent := strings.TrimSpace(getenv("ANX_AGENT")); envAgent != "" {
		resolved.Agent = envAgent
		resolved.Sources["agent"] = "env:ANX_AGENT"
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
		defaultAgent, ok, err := profile.LoadDefaultAgent(homeDir)
		if err != nil {
			return Resolved{}, fmt.Errorf("load default profile: %w", err)
		}
		if ok {
			if _, profileExists, err := profile.Load(profile.ProfilePath(homeDir, defaultAgent)); err != nil {
				return Resolved{}, fmt.Errorf("load default profile %q: %w", defaultAgent, err)
			} else if profileExists {
				resolved.Agent = defaultAgent
				resolved.Sources["agent"] = "profile:default"
			} else {
				ok = false
			}
		}
		if !ok {
			agents, err := profile.ListAgents(homeDir)
			if err != nil {
				return Resolved{}, fmt.Errorf("list local profiles: %w", err)
			}
			if len(agents) == 1 {
				resolved.Agent = agents[0]
				resolved.Sources["agent"] = "profile:auto-single"
			}
			if len(agents) > 1 {
				return Resolved{}, fmt.Errorf("multiple local profiles found (%s); select one using --agent, ANX_AGENT, or `anx auth default <profile>`", strings.Join(agents, ", "))
			}
		}
	}

	profilePath := strings.TrimSpace(getenv("ANX_PROFILE_PATH"))
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

	envBaseURL := strings.TrimSpace(getenv("ANX_BASE_URL"))
	flagBaseURL := ""
	if overrides.BaseURL != nil {
		flagBaseURL = strings.TrimSpace(*overrides.BaseURL)
	}
	if resolved.Sources["base_url"] == "default" && envBaseURL == "" && flagBaseURL == "" {
		configs, err := bridgeconfig.Discover(homeDir)
		if err != nil {
			return Resolved{}, fmt.Errorf("scan bridge configs: %w", err)
		}
		switch len(configs) {
		case 1:
			resolved.BaseURL = configs[0].BaseURL
			resolved.Sources["base_url"] = "bridge:auto-single"
		case 0:
		default:
			labels := make([]string, 0, len(configs))
			for _, cfg := range configs {
				label, relErr := filepath.Rel(bridgeconfig.RootDir(homeDir), cfg.Path)
				if relErr != nil {
					label = cfg.Path
				}
				labels = append(labels, label)
			}
			return Resolved{}, fmt.Errorf("multiple bridge configs found (%s); select a profile with `--agent` or `anx auth default <profile>`, or pass --base-url", strings.Join(labels, ", "))
		}
	}
	if envBaseURL != "" {
		resolved.BaseURL = envBaseURL
		resolved.Sources["base_url"] = "env:ANX_BASE_URL"
	}
	if envNoColor := strings.TrimSpace(getenv("ANX_NO_COLOR")); envNoColor != "" {
		value, err := strconv.ParseBool(envNoColor)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse ANX_NO_COLOR: %w", err)
		}
		resolved.NoColor = value
		resolved.Sources["no_color"] = "env:ANX_NO_COLOR"
	}
	if envJSON := strings.TrimSpace(getenv("ANX_JSON")); envJSON != "" {
		value, err := strconv.ParseBool(envJSON)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse ANX_JSON: %w", err)
		}
		resolved.JSON = value
		resolved.Sources["json"] = "env:ANX_JSON"
	}
	if envTimeout := strings.TrimSpace(getenv("ANX_TIMEOUT")); envTimeout != "" {
		dur, err := time.ParseDuration(envTimeout)
		if err != nil {
			return Resolved{}, fmt.Errorf("parse ANX_TIMEOUT: %w", err)
		}
		resolved.Timeout = dur
		resolved.Sources["timeout"] = "env:ANX_TIMEOUT"
	}
	if envToken := strings.TrimSpace(getenv("ANX_ACCESS_TOKEN")); envToken != "" {
		resolved.AccessToken = envToken
	}

	if flagBaseURL != "" {
		resolved.BaseURL = flagBaseURL
		resolved.Sources["base_url"] = "flag:--base-url"
	}
	if overrides.NoColor != nil {
		resolved.NoColor = *overrides.NoColor
		resolved.Sources["no_color"] = "flag:--no-color"
	}
	if overrides.Verbose != nil {
		resolved.Verbose = *overrides.Verbose
		resolved.Sources["verbose"] = "flag:--verbose"
	}
	if overrides.Headers != nil {
		resolved.Headers = *overrides.Headers
		resolved.Sources["headers"] = "flag:--headers"
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
