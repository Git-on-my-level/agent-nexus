package observation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CredentialResolver is supplied by trusted connection registration. It must resolve
// only the configured read-only source handle, never a general agent secret API.
type CredentialResolver func(context.Context, string) (string, error)

// HTTPConfig is operator-owned configuration, not reader output or request input.
// Private destinations require an explicit network approval. No proxies or redirects.
type HTTPConfig struct {
	BaseURL           string
	WorkspaceID       string
	ConnectionID      string
	SourceWorkspaceID string
	CredentialHandle  string
	ResolveCredential CredentialResolver
	RootCAs           *x509.CertPool
	AllowedNetworks   []netip.Prefix
	MaxPages          int
	MaxBytes          int64
	Timeout           time.Duration
}
type httpSource struct {
	config HTTPConfig
	base   *url.URL
	client *http.Client
}

func newHTTPSource(c HTTPConfig) (*httpSource, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return nil, failure(ErrConfiguration, "source needs an HTTPS origin without userinfo, query or fragment")
	}
	if c.WorkspaceID == "" || c.ConnectionID == "" {
		return nil, failure(ErrConfiguration, "workspace and connection binding required")
	}
	if c.MaxPages == 0 {
		c.MaxPages = 5
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 1 << 20
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxPages < 1 || c.MaxPages > 20 || c.MaxBytes < 1 || c.MaxBytes > 2<<20 || c.Timeout <= 0 || c.Timeout > time.Minute {
		return nil, failure(ErrConfiguration, "invalid HTTP resource bounds")
	}
	if c.CredentialHandle != "" && c.ResolveCredential == nil {
		return nil, failure(ErrConfiguration, "credential handle requires a scoped resolver")
	}
	c.AllowedNetworks = append([]netip.Prefix(nil), c.AllowedNetworks...)
	if c.RootCAs != nil {
		c.RootCAs = c.RootCAs.Clone()
	}
	source := &httpSource{config: c, base: u}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: c.RootCAs}, DisableCompression: true, ResponseHeaderTimeout: c.Timeout, MaxResponseHeaderBytes: 32768, MaxIdleConnsPerHost: 2}
	transport.DialContext = source.dial
	source.client = &http.Client{Transport: transport, Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return source, nil
}
func (s *httpSource) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(host, s.base.Hostname()) {
		return nil, failure(ErrPolicy, "connection destination differs from registered source")
	}
	expectedPort := s.base.Port()
	if expectedPort == "" {
		expectedPort = "443"
	}
	if port != expectedPort {
		return nil, failure(ErrPolicy, "unapproved destination port")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, failure(ErrUnavailable, "source DNS resolution failed")
	}
	for _, a := range addresses {
		if !s.allowedAddress(a.Unmap()) {
			return nil, failure(ErrPolicy, "source resolved to an unapproved address")
		}
	}
	var last error
	for _, a := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	_ = last
	return nil, failure(ErrUnavailable, "source connection failed")
}
func (s *httpSource) allowedAddress(a netip.Addr) bool {
	// Metadata and link-local endpoints are never enabled, even by broad network approval.
	if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
		return false
	}
	if len(s.config.AllowedNetworks) > 0 {
		for _, p := range s.config.AllowedNetworks {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	// Shared carrier/Tailnet space, benchmarking/documentation ranges and
	// transition mechanisms are not implicitly public source destinations.
	for _, prefix := range specialUseNetworks {
		if prefix.Contains(a) {
			return false
		}
	}
	return a.IsGlobalUnicast() && !a.IsPrivate() && !a.IsLoopback()
}

var specialUseNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func (s *httpSource) bind(t Target, source string) error {
	if err := t.Validate(); err != nil {
		return failure(ErrConfiguration, "invalid target")
	}
	if t.WorkspaceID != s.config.WorkspaceID || t.ConnectionID != s.config.ConnectionID || t.Source != source {
		return failure(ErrPermission, "target is outside registered source connection")
	}
	return nil
}

// A readSession has a cumulative response budget and request count. All endpoints
// are constructed by built-in source code. Source-supplied pagination URLs are never followed.
type readSession struct {
	source *httpSource
	pages  int
	bytes  int64
	token  string
}

func (s *httpSource) session(ctx context.Context) (*readSession, error) {
	token := ""
	if s.config.CredentialHandle != "" {
		var err error
		token, err = s.config.ResolveCredential(ctx, s.config.CredentialHandle)
		if err != nil || token == "" || strings.ContainsAny(token, "\r\n") {
			return nil, failure(ErrPermission, "source credential unavailable")
		}
	}
	return &readSession{source: s, token: token}, nil
}
func (s *readSession) get(ctx context.Context, endpoint string, out any) (http.Header, error) {
	if s.pages >= s.source.config.MaxPages {
		return nil, failure(ErrLimit, "source page limit reached")
	}
	if !strings.HasPrefix(endpoint, "/") || strings.Contains(endpoint, "..") || strings.ContainsAny(endpoint, "\r\n\\") {
		return nil, failure(ErrPolicy, "invalid built-in endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.source.base.String(), "/")+endpoint, nil)
	if err != nil {
		return nil, failure(ErrConfiguration, "invalid source endpoint")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Agent-Nexus-Observation/1")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	if s.source.config.SourceWorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", s.source.config.SourceWorkspaceID)
	}
	s.pages++
	response, err := s.source.client.Do(req)
	if err != nil {
		var re *ReadError
		if errors.As(err, &re) {
			return nil, re
		}
		if ctx.Err() != nil {
			return nil, failure(ErrUnavailable, "source read cancelled or timed out")
		}
		return nil, failure(ErrUnavailable, "source transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		kind := ErrUnavailable
		switch {
		case response.StatusCode >= 300 && response.StatusCode < 400:
			kind = ErrPolicy
		case response.StatusCode == 401:
			kind = ErrPermission
		case response.StatusCode == 403:
			kind = ErrPermission
		case response.StatusCode == 404 || response.StatusCode == 410:
			kind = ErrNotFound
		case response.StatusCode == 429:
			kind = ErrRateLimit
		}
		if response.StatusCode == 403 && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0") {
			kind = ErrRateLimit
		}
		return nil, &ReadError{Kind: kind, Message: "source request failed", Status: response.StatusCode, RetryAfter: retryAfter(response.Header, time.Now())}
	}
	remain := s.source.config.MaxBytes - s.bytes
	body, err := io.ReadAll(io.LimitReader(response.Body, remain+1))
	s.bytes += int64(len(body))
	if err != nil {
		return nil, failure(ErrUnavailable, "source response interrupted")
	}
	if int64(len(body)) > remain {
		return nil, failure(ErrLimit, "source response budget exceeded")
	}
	if err = json.Unmarshal(body, out); err != nil {
		return nil, failure(ErrInvalidOutput, "source returned invalid JSON")
	}
	return response.Header, nil
}
func retryAfter(h http.Header, now time.Time) time.Duration {
	var delay time.Duration
	if n, err := strconv.ParseInt(h.Get("Retry-After"), 10, 32); err == nil && n > 0 {
		delay = time.Duration(n) * time.Second
	} else if at, err := http.ParseTime(h.Get("Retry-After")); err == nil && at.After(now) {
		delay = at.Sub(now)
	}
	if epoch, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil && delay == 0 && h.Get("X-RateLimit-Remaining") == "0" {
		delay = time.Unix(epoch, 0).Sub(now)
	}
	if delay < 0 {
		return 0
	}
	if delay > 24*time.Hour {
		return 24 * time.Hour
	}
	return delay
}

var component = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,199}$`)

func validComponent(s string) bool { return component.MatchString(s) && !strings.Contains(s, "..") }
func validRepo(s string) bool {
	parts := strings.Split(s, "/")
	return len(parts) == 2 && validComponent(parts[0]) && validComponent(parts[1])
}
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func stamp(v string) *time.Time {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return &t
	}
	return nil
}
func phase(status string) string {
	switch status {
	case "open", "todo", "backlog":
		return "backlog"
	case "ready":
		return "ready"
	case "in_progress", "in-progress":
		return "in_progress"
	case "blocked":
		return "blocked"
	case "in_review", "review":
		return "review"
	case "done", "closed", "merged":
		return "done"
	case "cancelled", "canceled":
		return "cancelled"
	}
	return "unknown"
}
func newReport(t Target, id string) Report {
	now := time.Now().UTC()
	return Report{Target: t, ReaderID: id, ReaderRevision: ReaderRevision, ObservedAt: now, ReceivedAt: now, Knowledge: "reported", Facts: map[string]any{}, Evidence: []Evidence{}, Coverage: Coverage{Complete: true}}
}
func partial(r *Report, err error) {
	r.Coverage.Complete = false
	var re *ReadError
	if errors.As(err, &re) {
		r.Coverage.Limitations = append(r.Coverage.Limitations, re.Error())
	} else {
		r.Coverage.Limitations = append(r.Coverage.Limitations, "additional evidence unavailable")
	}
}
func selectFields(m map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}
