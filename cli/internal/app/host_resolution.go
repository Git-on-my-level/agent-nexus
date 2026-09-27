package app

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/httpclient"
)

var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

type tokenCache struct {
	Token     string         `json:"token"`
	ExpiresAt time.Time      `json:"expires_at"`
	Agent     map[string]any `json:"agent"`
}

func (a *App) identityName(cfg config.Resolved) (string, string, error) {
	if cfg.As != "" {
		if !agentNamePattern.MatchString(cfg.As) {
			return "", "", errnorm.Usage("invalid_agent_name", "--as requires a lowercase agent name")
		}
		return cfg.As, cfg.IdentitySource, nil
	}
	// Verified with installed agentctl v0.11.1 on 2026-09-27 by running
	// `agentctl run -- /bin/sh -c 'env'`: children receive ADAPTER,
	// EXECUTION_ID, HOST_ID, LABELS and AUTHORITY with the AGENTCTL_ prefix.
	if adapter := strings.TrimSpace(a.Getenv("AGENTCTL_ADAPTER")); adapter != "" && a.Getenv("AGENTCTL_EXECUTION_ID") != "" {
		adapter = agentctlName(adapter)
		if !agentNamePattern.MatchString(adapter) {
			return "", "", errnorm.Usage("invalid_agent_name", "agentctl adapter cannot be used as an agent name; pass --as")
		}
		return adapter, "agentctl", nil
	}
	// Observed in child tool shells of installed Claude Code 2.1.283 and Codex 0.156.0
	// on 2026-09-27. CLAUDECODE=1 and CODEX_THREAD_ID are actual exported markers.
	if a.Getenv("CLAUDECODE") == "1" {
		return "claude", "harness:claude", nil
	}
	if a.Getenv("CODEX_THREAD_ID") != "" {
		return "codex", "harness:codex", nil
	}
	// Cursor's installed 2026.09.26 shell runtime exports CURSOR_AGENT_COMPLETED_PATH
	// to tool child shells (src/commands/record.ts in its installed bundle).
	if a.Getenv("CURSOR_AGENT_COMPLETED_PATH") != "" {
		return "cursor", "harness:cursor", nil
	}
	// Installed OMP 17.4.0 uses generic AGENT=1 for bash children
	// (pi-coding-agent/src/exec/non-interactive-env.ts); it has no exclusive
	// environment marker. Identify its installed omp launcher in parent argv.
	if a.Getenv("AGENT") == "1" && a.hasOMPAncestor != nil && a.hasOMPAncestor() {
		return "omp", "harness:omp", nil
	}
	return "", "", errnorm.WithDetails(errnorm.Usage("identity_unresolved", "cannot resolve agent identity; pass --as <name> or set ANX_AS"), map[string]any{"next_argv": []string{"anx", "--as", "codex", "auth", "whoami"}})
}

func ompAncestor() bool {
	pid := os.Getppid()
	for i := 0; i < 5 && pid > 1; i++ {
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
		if err != nil {
			return false
		}
		fields := strings.Fields(string(out))
		if len(fields) > 0 {
			if filepath.Base(fields[0]) == "omp" {
				return true
			}
			if len(fields) > 1 && strings.Contains(filepath.Base(fields[0]), "bun") && filepath.Base(fields[1]) == "omp" {
				return true
			}
		}
		parent, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "ppid=").Output()
		if err != nil {
			return false
		}
		next, err := strconv.Atoi(strings.TrimSpace(string(parent)))
		if err != nil || next == pid {
			return false
		}
		pid = next
	}
	return false
}

func (a *App) resolvedHost(cfg config.Resolved) (hostidentity.Host, error) {
	configDir, err := a.configDir(cfg)
	if err != nil {
		return hostidentity.Host{}, err
	}
	baseURL := cfg.BaseURL
	if cfg.Sources["base_url"] == "default" {
		baseURL = ""
	}
	host, ok, err := hostidentity.LoadAt(configDir, baseURL)
	if err != nil {
		return hostidentity.Host{}, err
	}
	if !ok {
		return hostidentity.Host{}, errnorm.Local("host_not_enrolled", "host is not enrolled; run anx host enroll")
	}
	return host, nil
}

func (a *App) configDir(cfg config.Resolved) (string, error) {
	if cfg.ConfigDir != "" {
		return cfg.ConfigDir, nil
	}
	if strings.TrimSpace(a.Getenv("HOME")) == "" {
		return "", errnorm.Local("config_dir_required", "set --config-dir <absolute-path> or ANX_CONFIG_DIR when HOME is unset")
	}
	home, err := a.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errnorm.Local("config_dir_required", "set --config-dir <absolute-path> or ANX_CONFIG_DIR")
	}
	return filepath.Join(home, ".config", "anx"), nil
}

func (a *App) resolveHostAgent(ctx context.Context, cfg config.Resolved) (config.Resolved, error) {
	if cfg.AccessToken != "" && cfg.As == "" {
		return cfg, nil
	} // Human test and explicit bearer context.
	name, source, err := a.identityName(cfg)
	if err != nil {
		return cfg, err
	}
	host, err := a.resolvedHost(cfg)
	if err != nil {
		return cfg, err
	}
	if cfg.Sources["base_url"] == "default" {
		cfg.BaseURL = host.BaseURL
	}
	key, err := hostidentity.Key(host)
	if err != nil {
		return cfg, errnorm.Wrap(errnorm.KindLocal, "key_load_failed", "host key unavailable or unsafe", err)
	}
	dir := filepath.Dir(host.PrivateKeyPath)
	lockPath := filepath.Join(dir, "token-"+name+".lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return cfg, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return cfg, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	cachePath := filepath.Join(dir, "token-"+name+".json")
	var cached tokenCache
	if raw, readErr := os.ReadFile(cachePath); readErr == nil {
		if st, statErr := os.Stat(cachePath); statErr == nil && st.Mode().Perm()&0077 == 0 {
			_ = json.Unmarshal(raw, &cached)
		}
	}
	if cached.Token == "" || time.Until(cached.ExpiresAt) < 30*time.Second {
		signed := time.Now().UTC().Format(time.RFC3339Nano)
		msg := "anx-host-agent-token|" + host.ID + "|" + host.KeyID + "|" + name + "|" + signed
		body, _ := json.Marshal(map[string]any{"grant_type": "host_assertion", "host_id": host.ID, "key_id": host.KeyID, "agent_name": name, "signed_at": signed, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(msg)))})
		client, err := httpclient.New(cfg)
		if err != nil {
			return cfg, err
		}
		resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: http.MethodPost, Path: "/auth/token", Body: body})
		if err != nil {
			return cfg, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "host assertion failed", err)
		}
		if resp.StatusCode >= 400 {
			return cfg, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
		}
		var grant struct {
			Agent  map[string]any `json:"agent"`
			Tokens struct {
				AccessToken string `json:"access_token"`
				ExpiresIn   int    `json:"expires_in"`
			} `json:"tokens"`
		}
		if err := json.Unmarshal(resp.Body, &grant); err != nil {
			return cfg, err
		}
		if grant.Tokens.AccessToken == "" || grant.Tokens.ExpiresIn < 1 {
			return cfg, fmt.Errorf("invalid host token response")
		}
		cached = tokenCache{Token: grant.Tokens.AccessToken, ExpiresAt: time.Now().Add(time.Duration(grant.Tokens.ExpiresIn) * time.Second), Agent: grant.Agent}
		raw, _ := json.Marshal(cached)
		tmp, err := os.CreateTemp(dir, ".token-"+name+"-")
		if err != nil {
			return cfg, err
		}
		_ = tmp.Chmod(0600)
		_, err = tmp.Write(raw)
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(tmp.Name())
			return cfg, err
		}
		if err := os.Rename(tmp.Name(), cachePath); err != nil {
			return cfg, err
		}
	}
	cfg.AccessToken = cached.Token
	cfg.AccessTokenExpiresAt = cached.ExpiresAt.Format(time.RFC3339Nano)
	cfg.Agent = name
	cfg.As = name
	cfg.IdentitySource = source
	cfg.HostID = host.ID
	cfg.HostKeyID = host.KeyID
	cfg.HostKeyPath = host.PrivateKeyPath
	cfg.AgentID = anyString(cached.Agent["id"])
	cfg.ActorID = anyString(cached.Agent["actor_id"])
	cfg.Username = anyString(cached.Agent["handle"])
	if cfg.Username == "" {
		cfg.Username = anyString(cached.Agent["username"])
	}
	if runID := strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")); runID != "" && a.Getenv("AGENTCTL_ADAPTER") != "" {
		cfg.RunID = "agentctl/" + runID
	}
	return cfg, nil
}

func (a *App) runHostWhoAmI(ctx context.Context, cfg config.Resolved) (*commandResult, error) {
	auth, err := a.resolveHostAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	result, err := a.invokeRawJSON(ctx, auth, "auth whoami", "GET", "/agents/me", nil)
	if err != nil {
		return nil, err
	}
	body := commandResultBody(result)
	return &commandResult{Data: map[string]any{"host": map[string]any{"id": auth.HostID}, "agent": body["agent"], "resolution": auth.IdentitySource}}, nil
}
