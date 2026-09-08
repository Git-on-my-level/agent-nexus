package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/netip"
	"os"
	"regexp"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/primitives"
)

// ObservationBinding is trusted deployment wiring. The API never accepts
// executable paths, credentials, hostnames or a reader from observation reports.
type ObservationBinding struct {
	WorkRef        string
	SourceNativeID string
	Target         observation.Target
	Reader         observation.Reader
	Policy         observation.RefreshPolicy
}
type ObservationRuntime struct {
	store    *primitives.Store
	bindings []ObservationBinding
	worker   string
}
type observationConfig struct {
	Targets []struct {
		WorkRef           string             `json:"work_ref"`
		SourceNativeID    string             `json:"source_native_id"`
		Target            observation.Target `json:"target"`
		BaseURL           string             `json:"base_url"`
		SourceWorkspaceID string             `json:"source_workspace_id"`
		CredentialEnv     string             `json:"credential_env"`
		AllowedNetworks   []string           `json:"allowed_networks"`
		KnownHostsFile    string             `json:"known_hosts_file"`
		IdentityFile      string             `json:"identity_file"`
		SSHPort           int                `json:"ssh_port"`
		IntervalSeconds   int                `json:"interval_seconds"`
		StaleAfterSeconds int                `json:"stale_after_seconds"`
		TimeoutSeconds    int                `json:"timeout_seconds"`
		MaxBackoffSeconds int                `json:"max_backoff_seconds"`
	} `json:"targets"`
}

var credentialEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

// LoadObservationRuntime reads a bounded operator-owned configuration. Secrets
// are resolved only from the explicitly named environment variable, in memory.
func LoadObservationRuntime(path, workspaceID string, store *primitives.Store) (*ObservationRuntime, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 65536 {
		return nil, fmt.Errorf("observation config must be a regular non-group/world-writable file under 64KiB")
	}
	var cfg observationConfig
	decoder := json.NewDecoder(io.LimitReader(file, 65537))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("invalid observation configuration")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("observation configuration must contain one JSON value")
	}
	bindings := []ObservationBinding{}
	for _, c := range cfg.Targets {
		if c.Target.WorkspaceID != "" && c.Target.WorkspaceID != workspaceID {
			return nil, fmt.Errorf("observation target workspace mismatch")
		}
		c.Target.WorkspaceID = workspaceID
		if c.IntervalSeconds == 0 {
			c.IntervalSeconds = 300
		}
		if c.StaleAfterSeconds == 0 {
			c.StaleAfterSeconds = 900
		}
		if c.TimeoutSeconds == 0 {
			c.TimeoutSeconds = 30
		}
		if c.MaxBackoffSeconds == 0 {
			c.MaxBackoffSeconds = 3600
		}
		policy := observation.RefreshPolicy{Interval: time.Duration(c.IntervalSeconds) * time.Second, StaleAfter: time.Duration(c.StaleAfterSeconds) * time.Second, Timeout: time.Duration(c.TimeoutSeconds) * time.Second, MaxBackoff: time.Duration(c.MaxBackoffSeconds) * time.Second}
		var reader observation.Reader
		switch c.Target.Source {
		case "github", "multica":
			networks := []netip.Prefix{}
			for _, raw := range c.AllowedNetworks {
				prefix, e := netip.ParsePrefix(raw)
				if e != nil {
					return nil, fmt.Errorf("invalid approved network")
				}
				networks = append(networks, prefix)
			}
			if c.CredentialEnv != "" && !credentialEnvName.MatchString(c.CredentialEnv) {
				return nil, fmt.Errorf("invalid credential environment handle")
			}
			handle := c.CredentialEnv
			httpConfig := observation.HTTPConfig{BaseURL: c.BaseURL, WorkspaceID: workspaceID, ConnectionID: c.Target.ConnectionID, SourceWorkspaceID: c.SourceWorkspaceID, CredentialHandle: handle, AllowedNetworks: networks, Timeout: policy.Timeout}
			if handle != "" {
				httpConfig.ResolveCredential = func(_ context.Context, requested string) (string, error) {
					if requested != handle {
						return "", fmt.Errorf("credential handle mismatch")
					}
					value := os.Getenv(handle)
					if value == "" {
						return "", fmt.Errorf("configured source credential unavailable")
					}
					return value, nil
				}
			}
			if c.Target.Source == "github" {
				if httpConfig.BaseURL == "" {
					httpConfig.BaseURL = "https://api.github.com"
				}
				reader, err = observation.NewGitHubReader(httpConfig)
			} else {
				reader, err = observation.NewMulticaReader(httpConfig)
			}
		case "ssh_git":
			reader, err = observation.NewSSHGitReader(observation.SSHConfig{WorkspaceID: workspaceID, ConnectionID: c.Target.ConnectionID, Host: c.Target.Host, Repository: c.Target.Path, KnownHostsFile: c.KnownHostsFile, IdentityFile: c.IdentityFile, Port: c.SSHPort, Timeout: policy.Timeout})
		default:
			return nil, fmt.Errorf("unsupported configured observation source")
		}
		if err != nil {
			return nil, fmt.Errorf("invalid reader configuration: %w", err)
		}
		bindings = append(bindings, ObservationBinding{WorkRef: c.WorkRef, SourceNativeID: c.SourceNativeID, Target: c.Target, Reader: reader, Policy: policy})
	}
	return NewObservationRuntime(store, bindings)
}
func NewObservationRuntime(store *primitives.Store, bindings []ObservationBinding) (*ObservationRuntime, error) {
	if store == nil || len(bindings) > 200 {
		return nil, fmt.Errorf("observation runtime requires store and at most 200 targets")
	}
	seen := map[string]bool{}
	for _, b := range bindings {
		if b.WorkRef == "" || b.SourceNativeID == "" || b.Reader == nil || seen[b.WorkRef] {
			return nil, fmt.Errorf("unique work ref, source identity and reader required")
		}
		if err := b.Target.Validate(); err != nil {
			return nil, err
		}
		if err := b.Policy.Validate(); err != nil {
			return nil, err
		}
		seen[b.WorkRef] = true
	}
	return &ObservationRuntime{store: store, bindings: append([]ObservationBinding(nil), bindings...), worker: fmt.Sprintf("core-reader-%d", time.Now().UnixNano())}, nil
}
func (rt *ObservationRuntime) Run(ctx context.Context) {
	if rt == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := rt.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("observation refresh: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (rt *ObservationRuntime) Tick(ctx context.Context) error {
	var failures []error
	for _, b := range rt.bindings {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := rt.refresh(ctx, b); err != nil && !errors.Is(err, primitives.ErrConflict) {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (rt *ObservationRuntime) refresh(ctx context.Context, b ObservationBinding) error {
	w, err := rt.store.GetWork(ctx, b.WorkRef)
	if err != nil {
		return err
	}
	source := workSourceMap(w)
	if anyString(source["authority"]) != b.Target.Source || anyString(source["connection_id"]) != b.Target.ConnectionID || anyString(source["native_id"]) != b.SourceNativeID {
		return fmt.Errorf("registered work/source binding mismatch")
	}
	refresh, _ := w["refresh"].(map[string]any)
	next, _ := time.Parse(time.RFC3339Nano, anyString(refresh["next_due_at"]))
	failCount := 0
	if n, ok := refresh["failures"].(float64); ok {
		failCount = int(n)
	}
	// Manual refresh bypasses a successful interval, never rate-limit/backoff.
	if time.Now().Before(next) && (refresh["state"] != "queued" || failCount > 0) {
		return nil
	}
	if refresh["state"] != "queued" && refresh["state"] != "running" {
		if _, err = rt.store.RequestWorkRefresh(ctx, actors.SystemActorID, b.WorkRef); err != nil {
			return err
		}
	}
	lease, err := rt.store.ClaimWorkRefresh(ctx, b.WorkRef, rt.worker, b.Policy.Timeout+30*time.Second)
	if err != nil {
		return err
	}
	token := anyString(lease["lease_token"])
	scheduler, err := observation.NewScheduler(1)
	if err != nil {
		return err
	}
	health := observation.RefreshHealth{Target: b.Target, Failures: failCount, NextDue: next}
	if err = scheduler.Restore(map[string]observation.RefreshHealth{b.WorkRef: health}); err != nil {
		return err
	}
	result, readErr := scheduler.Refresh(ctx, b.WorkRef, b.Policy, b.Reader, b.Target, true)
	finished := map[string]any{"state": "failed", "next_due_at": result.Health.NextDue.Format(time.RFC3339Nano), "failures": result.Health.Failures}
	if readErr == nil && result.Health.LastGood != nil && !result.Skipped {
		report := result.Health.LastGood.Observation(b.Policy.StaleAfter)
		// Delivery identity is per actual leased read, source semantic identity is
		// source_revision. An unchanged poll advances freshness without progress.
		report["idempotency_key"] = "refresh:" + token
		_, err = rt.store.SubmitWorkObservation(ctx, actors.SystemActorID, b.WorkRef, report)
		if err == nil {
			finished["state"] = "succeeded"
		} else {
			readErr = err
		}
	}
	if finished["state"] != "succeeded" {
		finished["last_error"] = map[string]any{"code": "read_failed", "message": "Source read or observation persistence failed"}
		if result.Health.NextDue.IsZero() {
			finished["next_due_at"] = time.Now().Add(b.Policy.Interval).UTC().Format(time.RFC3339Nano)
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, finishErr := rt.store.FinishWorkRefresh(finishCtx, b.WorkRef, token, finished)
	return errors.Join(readErr, finishErr)
}
