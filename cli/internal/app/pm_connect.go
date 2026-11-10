package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filelock"
)

const pmConnectAuthBackoffStart = time.Minute
const pmConnectAuthBackoffCap = 15 * time.Minute
const pmConnectRetryBackoffStart = time.Second
const pmConnectRetryBackoffCap = 2 * time.Minute

type pmConnectFailure struct {
	Code              string `json:"code"`
	At                string `json:"at"`
	Status            int    `json:"status,omitempty"`
	Retryable         bool   `json:"retryable"`
	Hint              string `json:"hint,omitempty"`
	ConfiguredActorID string `json:"configured_actor_id,omitempty"`
	ActingActorID     string `json:"acting_actor_id,omitempty"`
}

// Store only bounded diagnostics, never response bodies, tokens or runner argv.
func recordPMConnect(state string, err error) error {
	var failure *pmConnectFailure
	if err != nil {
		normalized := errnorm.Normalize(err)
		failure = &pmConnectFailure{Code: normalized.Code, At: nowFn().UTC().Format(time.RFC3339Nano), Status: httpStatusFromErr(err), Retryable: pmConnectErrorRetryable(err)}
		if failure.Code == "pm_actor_mismatch" {
			details := pmErrorDetails(err)
			failure.ConfiguredActorID, _ = details["configured_actor_id"].(string)
			failure.ActingActorID, _ = details["acting_actor_id"].(string)
		}
		failure.localize()
	}
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	return writePMConnectFile(state, raw)
}

func readPMConnectFailure(state string) (*pmConnectFailure, error) {
	path := filepath.Join(state, "last-connect.json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	file, err := filelock.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, nil
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil {
		return nil, nil
	}
	if len(raw) > 16*1024 {
		return nil, nil
	}
	var failure *pmConnectFailure
	if err = json.Unmarshal(raw, &failure); err != nil {
		return nil, nil
	}
	if failure != nil {
		if failure.Code == "" {
			return nil, nil
		}
		if _, err := time.Parse(time.RFC3339Nano, failure.At); err != nil {
			return nil, nil
		}
		// Also discard remote hints left by older CLI versions.
		failure.localize()
	}
	return failure, nil
}

func (a *App) connectPM(ctx context.Context, cfg config.Resolved, argv []string) error {
	host, _ := os.Hostname()
	_, err := a.invokeRawJSON(ctx, cfg, "pm connect", "POST", "/pm/connect", map[string]any{"runner": pmRunnerLabel(argv), "host": safePMConnectionLabel(host, "local computer")})
	// Old cores signal presence on accepted claims. Auth failures never fall back.
	if httpStatusFromErr(err) == 404 {
		return nil
	}
	return err
}

var pmServicePreflightConnect = func(a *App, ctx context.Context, cfg config.Resolved, argv []string) error {
	resolved, err := pmServiceResolveAuth(a, ctx, cfg)
	if err != nil {
		return err
	}
	return a.connectPM(ctx, resolved, argv)
}

func pmConnectErrorRetryable(err error) bool {
	// Connect conflicts require registration repair, unlike claim lease races.
	return httpStatusFromErr(err) != 409 && claimErrorRetryable(err)
}

// Retry connect failures in-process so service-manager KeepAlive cannot turn
// configuration faults or outages into a short crash loop.
func (a *App) waitPMConnect(ctx context.Context, cfg config.Resolved, argv []string, state string) error {
	authBackoff, retryBackoff := pmConnectAuthBackoffStart, pmConnectRetryBackoffStart
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.connectPM(ctx, cfg, argv)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if recordErr := recordPMConnect(state, err); recordErr != nil {
			a.pmLog("pm serve: cannot persist advisory connect status: %v\n", recordErr)
		}
		if err == nil {
			return nil
		}
		configurationFailure := errnorm.ExitCode(err) == 5 || httpStatusFromErr(err) == 409
		retryable := pmConnectErrorRetryable(err)
		if !configurationFailure && !retryable {
			return err
		}
		backoff := authBackoff
		if retryable {
			backoff = retryBackoff
			retryBackoff = min(2*retryBackoff, pmConnectRetryBackoffCap)
			authBackoff = pmConnectAuthBackoffStart
		} else {
			authBackoff = min(2*authBackoff, pmConnectAuthBackoffCap)
			retryBackoff = pmConnectRetryBackoffStart
		}
		diagnostic := &pmConnectFailure{Code: errnorm.Normalize(err).Code, Status: httpStatusFromErr(err)}
		diagnostic.localize()
		a.pmLog("pm serve: connect failed code=%s; %s; retrying in %s\n", diagnostic.Code, diagnostic.Hint, backoff)
		if err = sleepFn(ctx, backoff); err != nil {
			return err
		}
	}
}
