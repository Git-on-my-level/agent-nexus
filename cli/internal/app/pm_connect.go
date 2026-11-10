package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const pmConnectAuthBackoffStart = time.Minute
const pmConnectAuthBackoffCap = 15 * time.Minute

type pmConnectFailure struct {
	Code      string `json:"code"`
	At        string `json:"at"`
	Status    int    `json:"status,omitempty"`
	Retryable bool   `json:"retryable"`
	Hint      string `json:"hint,omitempty"`
}

// Store only bounded diagnostics, never response bodies, tokens or runner argv.
func recordPMConnect(state string, err error) error {
	var failure *pmConnectFailure
	if err != nil {
		normalized := errnorm.Normalize(err)
		failure = &pmConnectFailure{Code: truncateToMaxBytes(normalized.Code, 128), At: nowFn().UTC().Format(time.RFC3339Nano), Status: httpStatusFromErr(err), Retryable: claimErrorRetryable(err), Hint: truncateToMaxBytes(normalized.Hint, 2048)}
	}
	raw, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	return writePMPrivateFile(filepath.Join(state, "last-connect.json"), raw)
}

func readPMConnectFailure(state string) (*pmConnectFailure, error) {
	file, err := os.Open(filepath.Join(state, "last-connect.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16*1024 {
		return nil, fmt.Errorf("PM connect diagnostic exceeds 16 KiB")
	}
	var failure *pmConnectFailure
	if err = json.Unmarshal(raw, &failure); err != nil {
		return nil, err
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

// Keep the service alive on authorization denial: service-manager KeepAlive
// must not turn a permanent configuration fault into a ten-second crash loop.
func (a *App) waitPMConnect(ctx context.Context, cfg config.Resolved, argv []string, state string) error {
	backoff := pmConnectAuthBackoffStart
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.connectPM(ctx, cfg, argv)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if recordErr := recordPMConnect(state, err); recordErr != nil {
			return fmt.Errorf("persist PM connect status: %w", recordErr)
		}
		if err == nil {
			return nil
		}
		authFailure := errnorm.ExitCode(err) == 5
		if !authFailure || claimErrorRetryable(err) {
			return err
		}
		normalized := errnorm.Normalize(err)
		a.pmLog("pm serve: connect failed code=%s: %s; %s; retrying in %s\n", normalized.Code, normalized.Message, normalized.Hint, backoff)
		if err = sleepFn(ctx, backoff); err != nil {
			return err
		}
		backoff *= 2
		if backoff > pmConnectAuthBackoffCap {
			backoff = pmConnectAuthBackoffCap
		}
	}
}
