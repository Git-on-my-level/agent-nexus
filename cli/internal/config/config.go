package config

import (
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
	JSON      *bool
	BaseURL   *string
	Workspace *string
	As        *string
	ConfigDir *string
	NoColor   *bool
	Verbose   *bool
	Headers   *bool
	Timeout   *time.Duration
}

type Resolved struct {
	JSON                 bool
	BaseURL              string
	Agent                string
	As                   string
	ConfigDir            string
	IdentitySource       string
	HostID               string
	HostKeyID            string
	HostKeyPath          string
	RunID                string
	NoColor              bool
	Verbose              bool
	Headers              bool
	Timeout              time.Duration
	AccessToken          string
	AccessTokenExpiresAt string
	AgentID              string
	ActorID              string
	Username             string
	Sources              map[string]string
}

type Environment struct {
	Getenv      func(string) string
	UserHomeDir func() (string, error)
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
	if overrides.ConfigDir != nil {
		r.ConfigDir = strings.TrimSpace(*overrides.ConfigDir)
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

	resolved.As = strings.TrimSpace(getenv("ANX_AS"))
	if resolved.As != "" {
		resolved.IdentitySource = "env:ANX_AS"
	}
	if overrides.As != nil {
		resolved.As = strings.TrimSpace(*overrides.As)
		resolved.IdentitySource = "flag:--as"
	}
	resolved.ConfigDir = strings.TrimSpace(getenv("ANX_CONFIG_DIR"))
	if overrides.ConfigDir != nil {
		resolved.ConfigDir = strings.TrimSpace(*overrides.ConfigDir)
	}
	if resolved.ConfigDir != "" && (!filepath.IsAbs(resolved.ConfigDir) || filepath.Clean(resolved.ConfigDir) != resolved.ConfigDir) {
		return Resolved{}, fmt.Errorf("config dir must be an absolute clean path")
	}

	envBaseURL := strings.TrimSpace(getenv("ANX_BASE_URL"))
	flagBaseURL := ""
	if overrides.BaseURL != nil {
		flagBaseURL = strings.TrimSpace(*overrides.BaseURL)
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
	if resolved.As != "" {
		resolved.Agent = resolved.As
		resolved.Sources["agent"] = resolved.IdentitySource
	}
	return resolved, nil
}
