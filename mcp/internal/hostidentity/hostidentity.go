package hostidentity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "http://127.0.0.1:8000"
	DefaultTimeout = 30 * time.Second
)

type Options struct {
	As        string
	BaseURL   string
	ConfigDir string
	AnxPath   string
	Timeout   time.Duration
}

type Environment struct {
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	RunToken    func(context.Context, string, string, string, string) ([]byte, error)
}

type Resolved struct {
	Agent       string
	BaseURL     string
	AccessToken string
	Timeout     time.Duration
	ConfigDir   string
}

type tokenEnvelope struct {
	OK     bool `json:"ok"`
	Result struct {
		Token string `json:"token"`
		Agent struct {
			Handle string `json:"handle"`
		} `json:"agent"`
	} `json:"result"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func Resolve(ctx context.Context, opts Options, env Environment) (Resolved, error) {
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	userHomeDir := env.UserHomeDir
	if userHomeDir == nil {
		userHomeDir = os.UserHomeDir
	}
	runToken := env.RunToken
	if runToken == nil {
		runToken = runAnxToken
	}

	resolved := Resolved{BaseURL: DefaultBaseURL, Timeout: DefaultTimeout}
	if strings.TrimSpace(getenv("ANX_BASE_URL")) != "" {
		resolved.BaseURL = strings.TrimSpace(getenv("ANX_BASE_URL"))
	}
	if strings.TrimSpace(opts.BaseURL) != "" {
		resolved.BaseURL = strings.TrimSpace(opts.BaseURL)
	}
	resolved.Agent = strings.TrimSpace(getenv("ANX_AS"))
	if strings.TrimSpace(opts.As) != "" {
		resolved.Agent = strings.TrimSpace(opts.As)
	}
	if resolved.Agent == "" {
		return Resolved{}, fmt.Errorf("select a derived agent with --as <name> or ANX_AS")
	}
	if opts.Timeout > 0 {
		resolved.Timeout = opts.Timeout
	}
	resolved.ConfigDir = strings.TrimSpace(opts.ConfigDir)
	if resolved.ConfigDir == "" {
		resolved.ConfigDir = strings.TrimSpace(getenv("ANX_CONFIG_DIR"))
	}
	if resolved.ConfigDir == "" {
		home, err := userHomeDir()
		if err != nil {
			return Resolved{}, fmt.Errorf("resolve home directory: %w", err)
		}
		resolved.ConfigDir = filepath.Join(home, ".config", "anx")
	}
	if !filepath.IsAbs(resolved.ConfigDir) {
		return Resolved{}, fmt.Errorf("config directory must be an absolute path: %q", resolved.ConfigDir)
	}
	anxPath := strings.TrimSpace(opts.AnxPath)
	if anxPath == "" {
		anxPath = "anx"
	}
	raw, err := runToken(ctx, anxPath, resolved.ConfigDir, resolved.BaseURL, resolved.Agent)
	if err != nil {
		return Resolved{}, fmt.Errorf("obtain derived-agent token with anx host token: %w", err)
	}
	var envelope tokenEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Resolved{}, fmt.Errorf("decode anx host token response: %w", err)
	}
	if !envelope.OK {
		message := strings.TrimSpace(envelope.Error.Message)
		if message == "" {
			message = "anx host token failed"
		}
		return Resolved{}, fmt.Errorf("obtain derived-agent token: %s", message)
	}
	resolved.AccessToken = strings.TrimSpace(envelope.Result.Token)
	if resolved.AccessToken == "" {
		return Resolved{}, fmt.Errorf("anx host token response did not include a token")
	}
	wantHandle := resolved.Agent
	if got := strings.TrimSpace(envelope.Result.Agent.Handle); got != "" {
		if !strings.HasPrefix(got, wantHandle+".") {
			return Resolved{}, fmt.Errorf("anx host token returned unexpected agent handle %q", got)
		}
	}
	return resolved, nil
}

func runAnxToken(ctx context.Context, anxPath, configDir, baseURL, agent string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, anxPath, "--json", "--config-dir", configDir, "--base-url", baseURL, "host", "token", "--as", agent)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("%s", stderr)
			}
		}
		return nil, err
	}
	return output, nil
}
