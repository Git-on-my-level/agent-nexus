package observation

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MulticaCLIConfig is an alternative trusted built-in transport. It uses the
// existing Multica profile without exposing its token to this package or JIT code.
// The approved CLI/profile owns destination and auth enforcement; this transport
// cannot claim the direct HTTP reader's DNS pinning or redirect policy.
type MulticaCLIConfig struct {
	Binary            string
	Profile           string
	BaseURL           string
	WorkspaceID       string
	ConnectionID      string
	SourceWorkspaceID string
	Timeout           time.Duration
	MaxBytes          int64
}
type MulticaCLIReader struct{ config MulticaCLIConfig }

func NewMulticaCLIReader(c MulticaCLIConfig) (*MulticaCLIReader, error) {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 1 << 20
	}
	if !filepath.IsAbs(c.Binary) || !validComponent(c.Profile) || !safeReference(c.BaseURL) || c.WorkspaceID == "" || c.ConnectionID == "" || !validComponent(c.SourceWorkspaceID) || c.Timeout <= 0 || c.Timeout > time.Minute || c.MaxBytes <= 0 || c.MaxBytes > 2<<20 {
		return nil, failure(ErrConfiguration, "Multica CLI requires an approved absolute binary, profile, source workspace and bounds")
	}
	return &MulticaCLIReader{config: c}, nil
}
func (*MulticaCLIReader) Capabilities() Capabilities {
	return Capabilities{Source: "multica", ReadOne: true, TrustedBuiltin: true, EvidenceKinds: []string{"issue", "task_run", "pull_request"}, Limitations: []string{"Trusted installed CLI/profile owns source auth and network policy; no token extraction; bounded selected-target commands; unpaginated source runs"}}
}
func (r *MulticaCLIReader) Read(ctx context.Context, t Target) (Report, error) {
	c := r.config
	if t.Validate() != nil || t.WorkspaceID != c.WorkspaceID || t.ConnectionID != c.ConnectionID || t.Source != "multica" || t.Kind != "issue" || !validComponent(t.NativeID) {
		return Report{}, failure(ErrPermission, "Multica CLI target is outside approved connection")
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	pages := 0
	bytes := int64(0)
	get := func(ctx context.Context, endpoint string, out any) (http.Header, error) {
		parts := strings.Split(strings.TrimPrefix(endpoint, "/api/issues/"), "/")
		if len(parts) < 1 || len(parts) > 2 || !validComponent(parts[0]) {
			return nil, failure(ErrPolicy, "invalid built-in Multica command")
		}
		action := "get"
		if len(parts) == 2 {
			switch parts[1] {
			case "task-runs":
				action = "runs"
			case "pull-requests":
				action = "pull-requests"
			default:
				return nil, failure(ErrPolicy, "unsupported Multica read")
			}
		}
		if pages >= 3 {
			return nil, failure(ErrLimit, "Multica CLI command budget exceeded")
		}
		pages++
		args := []string{"--profile", c.Profile, "--workspace-id", c.SourceWorkspaceID, "issue", action, parts[0], "--output", "json"}
		cmd := exec.CommandContext(ctx, c.Binary, args...)
		cmd.WaitDelay = time.Second
		// Preserve only the existing account home needed by the approved CLI profile.
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, failure(ErrConfiguration, "existing Multica profile home unavailable")
		}
		cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin", "LANG=C", "NO_COLOR=1"}
		stdout := &boundedBuffer{limit: c.MaxBytes - bytes}
		stderr := &boundedBuffer{limit: 4096}
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err = cmd.Run()
		raw := stdout.Bytes()
		bytes += int64(len(raw))
		if stdout.exceeded || stderr.exceeded {
			return nil, failure(ErrLimit, "Multica CLI output exceeded budget")
		}
		if err != nil {
			// Classify common public CLI diagnostics without returning private stderr.
			kind := ErrUnavailable
			detail := strings.ToLower(string(stderr.Bytes()))
			if strings.Contains(detail, "not signed in") || strings.Contains(detail, "unauthorized") || strings.Contains(detail, "session has expired") || strings.Contains(detail, "forbidden") {
				kind = ErrPermission
			}
			if strings.Contains(detail, "not found") {
				kind = ErrNotFound
			}
			if strings.Contains(detail, "rate limit") {
				kind = ErrRateLimit
			}
			return nil, failure(kind, "approved Multica CLI read failed")
		}
		if json.Unmarshal(raw, out) != nil {
			return nil, failure(ErrInvalidOutput, "Multica CLI returned invalid JSON")
		}
		return http.Header{}, nil
	}
	return readMultica(ctx, t, c.SourceWorkspaceID, c.BaseURL, "builtin:multica-cli", get, func() int { return pages }, 3)
}
