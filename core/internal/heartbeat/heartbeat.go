package heartbeat

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	DefaultInterval       = 30 * time.Second
	DefaultAudience       = "anx-control-plane"
	defaultAssertionTTL   = 60 * time.Second
	defaultRetryBackoff   = 200 * time.Millisecond
	defaultRequestTimeout = 15 * time.Second
	defaultMaxAttempts    = 3
)

type Identity struct {
	ID         string
	PrivateKey ed25519.PrivateKey
}

type Snapshot struct {
	Version                      string         `json:"version"`
	Build                        string         `json:"build"`
	HealthSummary                map[string]any `json:"health_summary"`
	ProjectionMaintenanceSummary map[string]any `json:"projection_maintenance_summary"`
	UsageSummary                 map[string]any `json:"usage_summary"`
	ActiveStreamCount            int            `json:"active_stream_count"`
	LastSuccessfulBackupAt       *string        `json:"last_successful_backup_at,omitempty"`
}

type Publisher struct {
	URL         string
	Audience    string
	WorkspaceID string
	Interval    time.Duration
	Identity    Identity
	Snapshot    func(ctx context.Context) Snapshot
	HTTPClient  *http.Client

	Now          func() time.Time
	Logf         func(format string, args ...any)
	RetryBackoff time.Duration
	MaxAttempts  int
}

// TODO(shared-auth-module): replace with import from shared auth module per TODOS.md.

func (p *Publisher) Run(ctx context.Context) {
	if p == nil || ctx == nil || ctx.Err() != nil {
		return
	}

	interval := p.effectiveInterval()
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if err := p.publishOnce(ctx); err != nil && ctx.Err() == nil {
			p.logf("heartbeat publisher: %v", err)
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(interval)
	}
}

func (p *Publisher) publishOnce(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}

	snapshot := p.Snapshot(ctx)
	snapshot.HealthSummary = nonNilMap(snapshot.HealthSummary)
	snapshot.ProjectionMaintenanceSummary = nonNilMap(snapshot.ProjectionMaintenanceSummary)
	snapshot.UsageSummary = nonNilMap(snapshot.UsageSummary)

	body, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("marshal heartbeat snapshot: %w", err)
	}

	token, err := p.signAssertion()
	if err != nil {
		return err
	}

	maxAttempts := p.maxAttempts()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		retry, postErr := p.post(ctx, body, token)
		if postErr == nil {
			return nil
		}
		if !retry || attempt >= maxAttempts {
			return postErr
		}
		if err := sleepWithContext(ctx, time.Duration(attempt)*p.retryBackoff()); err != nil {
			return err
		}
	}

	return nil
}

func (p *Publisher) signAssertion() (string, error) {
	now := p.now().UTC()
	claims := jwt.MapClaims{
		"iss":          p.Identity.ID,
		"sub":          p.Identity.ID,
		"aud":          p.audience(),
		"iat":          now.Unix(),
		"nbf":          now.Add(-30 * time.Second).Unix(),
		"exp":          now.Add(defaultAssertionTTL).Unix(),
		"workspace_id": p.WorkspaceID,
		"purpose":      "heartbeat",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := token.SignedString(p.Identity.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("sign heartbeat assertion: %w", err)
	}
	return signed, nil
}

func (p *Publisher) post(ctx context.Context, body []byte, assertion string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(p.URL), bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build heartbeat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+assertion)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return true, fmt.Errorf("post heartbeat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}

	message := strings.TrimSpace(readErrorBody(resp.Body))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	retry := resp.StatusCode >= 500
	return retry, fmt.Errorf("heartbeat rejected with %s: %s", resp.Status, message)
}

func (p *Publisher) validate() error {
	if strings.TrimSpace(p.URL) == "" {
		return fmt.Errorf("heartbeat URL is required")
	}
	if strings.TrimSpace(p.WorkspaceID) == "" {
		return fmt.Errorf("workspace ID is required")
	}
	if strings.TrimSpace(p.Identity.ID) == "" {
		return fmt.Errorf("service identity ID is required")
	}
	if len(p.Identity.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("service identity private key must be %d bytes", ed25519.PrivateKeySize)
	}
	if p.Snapshot == nil {
		return fmt.Errorf("heartbeat snapshot function is required")
	}
	return nil
}

func (p *Publisher) audience() string {
	if trimmed := strings.TrimSpace(p.Audience); trimmed != "" {
		return trimmed
	}
	return DefaultAudience
}

func (p *Publisher) effectiveInterval() time.Duration {
	if p.Interval > 0 {
		return p.Interval
	}
	return DefaultInterval
}

func (p *Publisher) maxAttempts() int {
	if p.MaxAttempts > 0 {
		return p.MaxAttempts
	}
	return defaultMaxAttempts
}

func (p *Publisher) retryBackoff() time.Duration {
	if p.RetryBackoff > 0 {
		return p.RetryBackoff
	}
	return defaultRetryBackoff
}

func (p *Publisher) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: defaultRequestTimeout}
}

func (p *Publisher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UTC()
}

func (p *Publisher) logf(format string, args ...any) {
	if p.Logf != nil {
		p.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func nonNilMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	return input
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func readErrorBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 2048))
	if err != nil {
		return ""
	}
	return string(raw)
}
