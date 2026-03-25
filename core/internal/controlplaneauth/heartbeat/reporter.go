package heartbeat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"organization-autorunner-core/internal/controlplane"
	"organization-autorunner-core/internal/controlplaneauth"
)

const (
	DefaultInterval       = 30 * time.Second
	defaultRequestTimeout = 15 * time.Second
	defaultJitterFraction = 0.2
)

type ReporterConfig struct {
	BaseURL                      string
	WorkspaceID                  string
	Interval                     time.Duration
	Version                      string
	Build                        string
	Identity                     *controlplaneauth.WorkspaceServiceIdentity
	ReadinessSummary             func(ctx context.Context) map[string]any
	ProjectionMaintenanceSummary func(ctx context.Context, now time.Time) map[string]any
	UsageSummary                 func(ctx context.Context) (map[string]any, error)
	LastSuccessfulBackupAt       func(ctx context.Context) (*string, error)
	Client                       *http.Client
	Now                          func() time.Time
	Logf                         func(format string, args ...any)
	JitterFraction               float64
}

type Reporter struct {
	baseURL                      *url.URL
	workspaceID                  string
	interval                     time.Duration
	version                      string
	build                        string
	identity                     *controlplaneauth.WorkspaceServiceIdentity
	readinessSummary             func(ctx context.Context) map[string]any
	projectionMaintenanceSummary func(ctx context.Context, now time.Time) map[string]any
	usageSummary                 func(ctx context.Context) (map[string]any, error)
	lastSuccessfulBackupAt       func(ctx context.Context) (*string, error)
	client                       *http.Client
	now                          func() time.Time
	logf                         func(format string, args ...any)
	jitterFraction               float64
	random                       *rand.Rand
}

func NewReporter(config ReporterConfig) (*Reporter, error) {
	baseURLText := strings.TrimSpace(config.BaseURL)
	if baseURLText == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	baseURL, err := url.Parse(baseURLText)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("base URL must include scheme and host")
	}
	if strings.TrimSpace(config.WorkspaceID) == "" {
		return nil, fmt.Errorf("workspace ID is required")
	}
	if config.Identity == nil {
		return nil, fmt.Errorf("workspace service identity is required")
	}
	version := strings.TrimSpace(config.Version)
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}
	build := strings.TrimSpace(config.Build)
	if build == "" {
		build = version
	}
	interval := config.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: defaultRequestTimeout}
	}
	logf := config.Logf
	if logf == nil {
		logf = log.Printf
	}
	jitterFraction := config.JitterFraction
	if jitterFraction < 0 {
		jitterFraction = 0
	}
	if jitterFraction == 0 {
		jitterFraction = defaultJitterFraction
	}

	return &Reporter{
		baseURL:                      baseURL,
		workspaceID:                  strings.TrimSpace(config.WorkspaceID),
		interval:                     interval,
		version:                      version,
		build:                        build,
		identity:                     config.Identity,
		readinessSummary:             config.ReadinessSummary,
		projectionMaintenanceSummary: config.ProjectionMaintenanceSummary,
		usageSummary:                 config.UsageSummary,
		lastSuccessfulBackupAt:       config.LastSuccessfulBackupAt,
		client:                       client,
		now:                          nowFn,
		logf:                         logf,
		jitterFraction:               jitterFraction,
		random:                       rand.New(rand.NewSource(nowFn().UnixNano())),
	}, nil
}

func (r *Reporter) Run(ctx context.Context) {
	if r == nil {
		return
	}
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if err := r.ReportOnce(ctx); err != nil && ctx.Err() == nil {
			r.logf("control-plane heartbeat failed: %v", err)
		}

		next := r.intervalWithJitter()
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(next)
	}
}

func (r *Reporter) ReportOnce(ctx context.Context) error {
	if r == nil {
		return nil
	}
	payload, err := r.buildPayload(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal heartbeat payload: %w", err)
	}

	token, _, err := r.identity.SignClientAssertion(controlplaneauth.WorkspaceServiceAssertionAudience, controlplaneauth.DefaultClientAssertion, map[string]any{
		"workspace_id": r.workspaceID,
		"purpose":      "heartbeat",
	})
	if err != nil {
		return fmt.Errorf("sign heartbeat assertion: %w", err)
	}

	endpoint := *r.baseURL
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/workspaces/" + url.PathEscape(r.workspaceID) + "/heartbeat"
	endpoint.RawPath = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build heartbeat request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("post heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := strings.TrimSpace(readErrorBody(resp.Body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("heartbeat rejected with %s: %s", resp.Status, message)
	}
	return nil
}

func (r *Reporter) buildPayload(ctx context.Context) (controlplane.WorkspaceHeartbeatRequest, error) {
	healthSummary := map[string]any{"ok": true}
	if r.readinessSummary != nil {
		healthSummary = nonNilMap(r.readinessSummary(ctx))
	}

	projectionSummary := map[string]any{}
	if r.projectionMaintenanceSummary != nil {
		projectionSummary = nonNilMap(r.projectionMaintenanceSummary(ctx, r.now()))
	}

	usageSummary := map[string]any{}
	if r.usageSummary != nil {
		var err error
		usageSummary, err = r.usageSummary(ctx)
		if err != nil {
			return controlplane.WorkspaceHeartbeatRequest{}, fmt.Errorf("load usage summary: %w", err)
		}
	}

	var lastSuccessfulBackupAt *string
	if r.lastSuccessfulBackupAt != nil {
		value, err := r.lastSuccessfulBackupAt(ctx)
		if err != nil {
			return controlplane.WorkspaceHeartbeatRequest{}, fmt.Errorf("load last successful backup timestamp: %w", err)
		}
		lastSuccessfulBackupAt = value
	}

	return controlplane.WorkspaceHeartbeatRequest{
		Version:                      r.version,
		Build:                        r.build,
		HealthSummary:                healthSummary,
		ProjectionMaintenanceSummary: projectionSummary,
		UsageSummary:                 nonNilMap(usageSummary),
		LastSuccessfulBackupAt:       lastSuccessfulBackupAt,
	}, nil
}

func (r *Reporter) intervalWithJitter() time.Duration {
	if r == nil {
		return DefaultInterval
	}
	if r.jitterFraction <= 0 {
		return r.interval
	}
	maxJitter := float64(r.interval) * r.jitterFraction
	return r.interval + time.Duration(r.random.Float64()*maxJitter)
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func readErrorBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 2048))
	if err != nil {
		return ""
	}
	return string(raw)
}
