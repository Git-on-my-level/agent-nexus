package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filelock"
)

func (a *App) runRuns(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "runs", errnorm.Usage("subcommand_required", "use runs ingest|list|get")
	}
	switch args[0] {
	case "ingest":
		r, e := a.runIngest(ctx, args[1:], cfg)
		return r, "runs ingest", e
	case "list":
		fs := newSilentFlagSet("runs list")
		var card, agent, host, state, cursor trackedString
		var limit trackedInt
		var active bool
		fs.Var(&card, "card", "Card ref")
		fs.Var(&agent, "agent-id", "Agent ID")
		fs.Var(&host, "host-id", "Host ID")
		fs.Var(&state, "state", "Run state")
		fs.Var(&cursor, "cursor", "Cursor")
		fs.Var(&limit, "limit", "Page limit")
		fs.BoolVar(&active, "active", false, "Active runs")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "runs list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) != 0 {
			return nil, "runs list", errnorm.Usage("invalid_args", "unexpected arguments")
		}
		q := url.Values{}
		for k, v := range map[string]string{"card_ref": card.value, "agent_id": agent.value, "host_id": host.value, "state": state.value, "cursor": cursor.value} {
			if v != "" {
				q.Set(k, v)
			}
		}
		if active {
			q.Set("active", "true")
		}
		if limit.set {
			q.Set("limit", fmt.Sprint(limit.value))
		}
		path := "/runs"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		r, e := a.invokeRawJSON(ctx, cfg, "runs list", "GET", path, nil)
		return r, "runs list", e
	case "get":
		if len(args) != 2 {
			return nil, "runs get", errnorm.Usage("invalid_args", "expected run ID or ref")
		}
		id := strings.TrimPrefix(args[1], "run:")
		r, e := a.invokeRawJSON(ctx, cfg, "runs get", "GET", "/runs/"+url.PathEscape(id), nil)
		return r, "runs get", e
	default:
		return nil, "runs", errnorm.Usage("unknown_subcommand", "unknown runs command")
	}
}

func agentctlName(adapter string) string {
	switch adapter {
	case "claude-code":
		return "claude"
	case "cursor-agent":
		return "cursor"
	case "generic-process":
		return "generic"
	}
	return adapter
}
func runState(state string) string {
	switch state {
	case "created", "starting":
		return "starting"
	case "running", "waiting", "attention":
		return "running"
	case "completed", "failed", "cancelled":
		return state
	default:
		return "unknown"
	}
}
func runLiveness(value string) string {
	if value == "alive" || value == "blocked" {
		return "alive"
	}
	if value == "exited" || value == "unreachable" {
		return "stale"
	}
	return "unknown"
}

func (a *App) runIngest(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if cfg.ConfigDir == "" && strings.TrimSpace(a.Getenv("HOME")) == "" {
		return nil, errnorm.Local("config_dir_required", "runs ingest needs --config-dir <absolute-path> when HOME is unset")
	}
	result, err := a.runIngestInner(ctx, args, cfg)
	if err != nil {
		if dir, dirErr := a.configDir(cfg); dirErr == nil {
			_ = appendRunsIngestError(dir, err)
		}
	}
	return result, err
}

func appendRunsIngestError(configDir string, runErr error) error {
	dir := filepath.Join(configDir, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return fmt.Errorf("unsafe runs ingest log directory")
	}
	path := filepath.Join(dir, "runs-ingest.log")
	f, err := filelock.OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := filelock.Lock(f); err != nil {
		return err
	}
	defer filelock.Unlock(f)
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if stat.Mode().Perm()&0077 != 0 || !stat.Mode().IsRegular() {
		return fmt.Errorf("unsafe runs ingest log")
	}
	code := "error"
	if normalized := errnorm.Normalize(runErr); normalized != nil && normalized.Code != "" {
		code = normalized.Code
	}
	if !regexp.MustCompile(`^[a-z0-9_]{1,64}$`).MatchString(code) {
		code = "error"
	}
	entry := fmt.Sprintf("%s code=%s\n", time.Now().UTC().Format(time.RFC3339), code)
	if stat.Size()+int64(len(entry)) > 64*1024 {
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	} else if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = io.WriteString(f, entry)
	return err
}

func (a *App) runIngestInner(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if len(args) > 1 {
		return nil, errnorm.Usage("invalid_args", "expected one callback file path or stdin")
	}
	var raw []byte
	var err error
	if len(args) == 1 {
		a.markNonReplayableInput(args[0])
		var file *os.File
		file, err = os.Open(args[0])
		if err != nil {
			return nil, err
		}
		defer file.Close()
		raw, err = io.ReadAll(io.LimitReader(file, (2<<20)+1))
	} else {
		raw, err = a.readStdinBytes((2 << 20) + 1)
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > 2<<20 {
		return nil, errnorm.Usage("invalid_envelope", "expected one bounded agentctl envelope")
	}
	var outer map[string]any
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, errnorm.Usage("invalid_envelope", "invalid JSON envelope")
	}
	if hostInt(outer["schema_version"]) != 1 {
		return nil, errnorm.Usage("unsupported_envelope_version", "agentctl schema_version must be 1")
	}
	event := outer
	if nested, ok := outer["event"]; ok {
		for _, field := range []string{"delivery_id", "subscription_id", "event_id", "event_dedupe_key", "sent_at", "expires_at", "nonce"} {
			if anyString(outer[field]) == "" {
				return nil, errnorm.Usage("invalid_envelope", "callback envelope is missing "+field)
			}
		}
		if hostInt(outer["attempt"]) < 1 {
			return nil, errnorm.Usage("invalid_envelope", "callback envelope is missing delivery fields")
		}
		event = asMap(nested)
		for _, field := range []string{"id", "execution_id", "origin_host_id", "adapter", "observed_at", "kind", "authority", "ordering", "dedupe_key"} {
			if anyString(event[field]) == "" {
				return nil, errnorm.Usage("invalid_envelope", "callback event is missing "+field)
			}
		}
		if hostInt(event["sequence"]) < 1 || hostInt(event["dedupe_version"]) < 1 {
			return nil, errnorm.Usage("invalid_envelope", "callback event sequence is invalid")
		}
	}
	isExecution := anyString(event["origin_host_id"]) != "" && anyString(event["id"]) != "" && event["observation"] != nil
	executionID := anyString(event["execution_id"])
	if isExecution {
		executionID = anyString(event["id"])
	}
	adapter := anyString(event["adapter"])
	origin := anyString(event["origin_host_id"])
	if executionID == "" || adapter == "" || origin == "" || hostInt(event["schema_version"]) != 1 {
		return nil, errnorm.Usage("invalid_envelope", "agentctl event is missing execution, adapter or host fields")
	}
	if !strings.HasPrefix(executionID, "exec-") || !strings.HasPrefix(origin, "host-") {
		return nil, errnorm.Usage("invalid_envelope", "invalid agentctl execution or host ID")
	}
	// Query the local journal for this exact execution when available. A callback
	// process has no AGENTCTL_* environment; its envelope is the identity source.
	var journal map[string]any
	if bin, e := exec.LookPath("agentctl"); e == nil {
		if output, e := exec.CommandContext(ctx, bin, "status", executionID).Output(); e == nil {
			var wrapper map[string]any
			if json.Unmarshal(output, &wrapper) == nil {
				journal = asMap(wrapper["result"])
			}
		}
	}
	if len(journal) > 0 && anyString(journal["origin_host_id"]) != origin {
		return nil, errnorm.Usage("agentctl_host_mismatch", "envelope host differs from local agentctl journal")
	}
	name := agentctlName(adapter)
	if !agentNamePattern.MatchString(name) {
		return nil, errnorm.Usage("invalid_adapter", "agentctl adapter cannot be a derived agent name")
	}
	if cfg.As == "" {
		cfg.As = name
		cfg.IdentitySource = "agentctl:envelope"
	}
	auth, err := a.resolveHostAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	labels := asSlice(event["labels"])
	if len(labels) == 0 {
		labels = asSlice(journal["labels"])
	}
	if labels == nil {
		labels = []any{}
	}
	state := runState(firstNonEmpty(anyString(event["state"]), anyString(journal["state"])))
	liveness := runLiveness(firstNonEmpty(anyString(event["liveness"]), anyString(journal["liveness"])))
	observed := anyString(asMap(event["observation"])["observed_at"])
	if observed == "" {
		observed = anyString(event["observed_at"])
	}
	if observed == "" {
		return nil, errnorm.Usage("invalid_envelope", "agentctl observation timestamp is required")
	}
	repository := anyString(event["repository"])
	if repository == "" {
		repository = anyString(journal["repository"])
	}
	branch := anyString(asMap(event["workspace"])["head_ref"])
	if branch == "" {
		branch = anyString(asMap(journal["workspace"])["head_ref"])
	}
	resultCollected := event["result_collected"] == true || anyString(asMap(event["outcome"])["availability"]) == "stored" || anyString(asMap(journal["outcome"])["availability"]) == "stored"
	body := map[string]any{"launcher": "agentctl", "external_id": executionID, "host_id": auth.HostID, "agent_id": auth.AgentID, "adapter": adapter, "state": state, "liveness": liveness, "result_collected": resultCollected, "labels": labels, "last_observed_at": observed}
	if repository != "" {
		body["repository"] = repository
	}
	if branch != "" {
		body["branch"] = branch
	}
	if started := firstNonEmpty(anyString(event["started_at"]), anyString(journal["started_at"])); started != "" {
		body["started_at"] = started
	}
	if ended := firstNonEmpty(anyString(event["terminal_at"]), anyString(journal["terminal_at"])); ended != "" {
		body["ended_at"] = ended
	}
	if model := firstNonEmpty(anyString(event["model"]), anyString(journal["model"])); model != "" {
		body["model"] = model
	}
	return a.invokeRawJSON(ctx, auth, "runs ingest", "POST", "/runs", body)
}
