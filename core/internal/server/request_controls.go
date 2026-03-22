package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
)

const (
	defaultJSONRequestBodyLimit    int64 = 1 << 20
	defaultAuthRequestBodyLimit    int64 = 256 << 10
	defaultContentRequestBodyLimit int64 = 8 << 20

	defaultAuthRequestsPerMinute   = 600
	defaultAuthRequestsBurst       = 100
	defaultWriteRequestsPerMinute  = 1200
	defaultWriteRequestsBurst      = 200
	requestTooLargeRetryAfterSecs  = 1
	defaultRouteRateLimitMaxKeys   = 4096
	defaultRouteRateLimitBucketTTL = 15 * time.Minute
	routeRateLimitPruneInterval    = time.Minute
)

type RequestBodyLimits struct {
	Default int64
	Auth    int64
	Content int64
}

type RouteRateLimits struct {
	AuthRequestsPerMinute  int
	AuthBurst              int
	WriteRequestsPerMinute int
	WriteBurst             int
}

func (l RequestBodyLimits) normalize() RequestBodyLimits {
	if l.Default <= 0 {
		l.Default = defaultJSONRequestBodyLimit
	}
	if l.Auth <= 0 {
		l.Auth = defaultAuthRequestBodyLimit
	}
	if l.Content <= 0 {
		l.Content = defaultContentRequestBodyLimit
	}
	return l
}

func (l RouteRateLimits) normalize() RouteRateLimits {
	if l.AuthRequestsPerMinute <= 0 {
		l.AuthRequestsPerMinute = defaultAuthRequestsPerMinute
	}
	if l.AuthBurst <= 0 {
		l.AuthBurst = defaultAuthRequestsBurst
	}
	if l.WriteRequestsPerMinute <= 0 {
		l.WriteRequestsPerMinute = defaultWriteRequestsPerMinute
	}
	if l.WriteBurst <= 0 {
		l.WriteBurst = defaultWriteRequestsBurst
	}
	return l
}

type routeRateLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*rateLimitBucket
	limits    RouteRateLimits
	maxKeys   int
	bucketTTL time.Duration
	lastPrune time.Time
}

type rateLimitBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

func newRouteRateLimiter(limits RouteRateLimits) *routeRateLimiter {
	return &routeRateLimiter{
		buckets:   make(map[string]*rateLimitBucket, 2),
		limits:    limits.normalize(),
		maxKeys:   defaultRouteRateLimitMaxKeys,
		bucketTTL: defaultRouteRateLimitBucketTTL,
	}
}

func (l *routeRateLimiter) allow(bucket string, scope string, now time.Time) (bool, time.Duration) {
	if l == nil || strings.TrimSpace(bucket) == "" {
		return true, 0
	}

	limit, burst := l.limitForBucket(bucket)
	if limit <= 0 || burst <= 0 {
		return true, 0
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneBuckets(now)

	stateKey := bucket
	scope = strings.TrimSpace(scope)
	if scope != "" {
		stateKey = bucket + ":" + scope
	}

	state, ok := l.buckets[stateKey]
	if !ok {
		l.evictOldestBucketIfFull()
		state = &rateLimitBucket{
			tokens:     float64(burst),
			lastRefill: now,
			lastSeen:   now,
		}
		l.buckets[stateKey] = state
	}
	state.lastSeen = now

	if now.Before(state.lastRefill) {
		state.lastRefill = now
	}

	ratePerSecond := float64(limit) / 60.0
	elapsed := now.Sub(state.lastRefill).Seconds()
	if elapsed > 0 && ratePerSecond > 0 {
		state.tokens = math.Min(float64(burst), state.tokens+elapsed*ratePerSecond)
		state.lastRefill = now
	}

	if state.tokens >= 1 {
		state.tokens -= 1
		return true, 0
	}

	if ratePerSecond <= 0 {
		return false, requestTooLargeRetryAfterSecs * time.Second
	}

	deficit := 1 - state.tokens
	retryAfter := time.Duration(math.Ceil(deficit / ratePerSecond * float64(time.Second)))
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return false, retryAfter
}

func (l *routeRateLimiter) limitForBucket(bucket string) (int, int) {
	switch bucket {
	case "auth":
		return l.limits.AuthRequestsPerMinute, l.limits.AuthBurst
	case "write":
		return l.limits.WriteRequestsPerMinute, l.limits.WriteBurst
	default:
		return 0, 0
	}
}

func (l *routeRateLimiter) pruneBuckets(now time.Time) {
	if l == nil || len(l.buckets) == 0 {
		return
	}
	if now.Sub(l.lastPrune) < routeRateLimitPruneInterval && len(l.buckets) < l.maxKeys {
		return
	}
	if l.bucketTTL > 0 {
		for key, bucket := range l.buckets {
			lastTouched := bucket.lastSeen
			if lastTouched.Before(bucket.lastRefill) {
				lastTouched = bucket.lastRefill
			}
			if now.Sub(lastTouched) >= l.bucketTTL {
				delete(l.buckets, key)
			}
		}
	}
	l.lastPrune = now
}

func (l *routeRateLimiter) evictOldestBucketIfFull() {
	if l == nil || l.maxKeys <= 0 || len(l.buckets) < l.maxKeys {
		return
	}
	var oldestKey string
	var oldestSeen time.Time
	for key, bucket := range l.buckets {
		lastTouched := bucket.lastSeen
		if lastTouched.Before(bucket.lastRefill) {
			lastTouched = bucket.lastRefill
		}
		if oldestKey == "" || lastTouched.Before(oldestSeen) {
			oldestKey = key
			oldestSeen = lastTouched
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

func requestBodyLimitForRequest(path string, method string, bucket routeAccessRequirement, limits RequestBodyLimits) int64 {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if path == "" {
		return 0
	}
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return 0
	}

	limits = limits.normalize()
	if strings.HasPrefix(path, "/auth/") {
		return limits.Auth
	}

	switch path {
	case "/artifacts", "/docs", "/work_orders", "/receipts", "/reviews":
		if method == http.MethodPost || method == http.MethodPatch {
			return limits.Content
		}
	}

	if strings.HasPrefix(path, "/docs/") || strings.HasPrefix(path, "/artifacts/") {
		if strings.HasSuffix(path, "/tombstone") || strings.HasSuffix(path, "/history") || strings.Contains(path, "/revisions/") || strings.HasSuffix(path, "/content") {
			return limits.Default
		}
		if method == http.MethodPatch || method == http.MethodPost {
			return limits.Content
		}
	}

	_ = bucket
	return limits.Default
}

func routeRateLimitForRequest(r *http.Request, requirement routeAccessRequirement) (string, string) {
	if r == nil {
		return "", ""
	}
	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return "", ""
	}
	path := strings.TrimSpace(r.URL.Path)
	if path == "" {
		return "", ""
	}
	bucket := routeRateLimitBucketForPath(path, requirement)
	if bucket == "" {
		return "", ""
	}
	return bucket, routeRateLimitScopeForRequest(r)
}

func routeRateLimitBucketForPath(path string, requirement routeAccessRequirement) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "/auth/") {
		return "auth"
	}
	switch requirement.bucket {
	case routeAccessWorkspaceBusiness, routeAccessAuthenticatedPrincipal, routeAccessDevOnlyLegacyActor:
		return "write"
	default:
		return ""
	}
}

func routeRateLimitScopeForRequest(r *http.Request) string {
	if principal, ok := cachedAuthenticatedPrincipal(r); ok {
		return routeRateLimitScopeForPrincipal(principal)
	}
	host := requestClientHost(r)
	if host != "" {
		return "addr:" + host
	}
	return "anonymous"
}

func routeRateLimitScopeForPrincipal(principal *auth.Principal) string {
	if principal == nil {
		return "anonymous"
	}
	if agentID := strings.TrimSpace(principal.AgentID); agentID != "" {
		return "principal:" + agentID
	}
	if actorID := strings.TrimSpace(principal.ActorID); actorID != "" {
		return "actor:" + actorID
	}
	return "anonymous"
}

func requestClientHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if peerIP := requestPeerIP(r); peerIP != nil && peerIP.IsLoopback() {
		if forwarded := forwardedClientHost(r); forwarded != "" {
			return forwarded
		}
	}
	return requestRemoteHost(r)
}

func requestPeerIP(r *http.Request) net.IP {
	host := requestRemoteHost(r)
	if host == "" {
		return nil
	}
	return net.ParseIP(host)
}

func requestRemoteHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	return strings.Trim(host, "[]")
}

func forwardedClientHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if host := parseForwardedFor(firstHeaderValue(r.Header.Get("X-Forwarded-For"))); host != "" {
		return host
	}
	forwarded := firstHeaderValue(r.Header.Get("Forwarded"))
	if forwarded == "" {
		return ""
	}
	for _, field := range strings.Split(forwarded, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "for") {
			continue
		}
		return parseForwardedFor(value)
	}
	return ""
}

func parseForwardedFor(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, `"`))
	if raw == "" || strings.EqualFold(raw, "unknown") {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		host, _, err := net.SplitHostPort(raw)
		if err == nil {
			return strings.Trim(host, "[]")
		}
	}
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.Trim(raw, "[]")
	if net.ParseIP(raw) == nil {
		return ""
	}
	return raw
}

func routeRateLimitBucketForRequest(path string, method string, requirement routeAccessRequirement) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return ""
	}
	return routeRateLimitBucketForPath(path, requirement)
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r == nil || r.Body == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		if writeRequestTooLargeError(w, err) {
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return true
}

func decodeJSONBodyAllowEmpty(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r == nil || r.Body == nil {
		return true
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		if writeRequestTooLargeError(w, err) {
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return true
}

func writeRequestTooLargeError(w http.ResponseWriter, err error) bool {
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) && !strings.Contains(strings.ToLower(err.Error()), "request body too large") {
		return false
	}

	details := map[string]any{
		"request_body": map[string]any{
			"limit_bytes": maxErrLimit(maxErr),
		},
	}
	writeDetailedError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds the configured limit", details)
	return true
}

func maxErrLimit(err *http.MaxBytesError) int64 {
	if err == nil {
		return 0
	}
	return err.Limit
}

func writeDetailedError(w http.ResponseWriter, status int, code string, message string, details map[string]any) {
	payload := map[string]any{
		"error": errorPayload(code, message),
	}
	for key, value := range details {
		payload[key] = value
	}
	writeJSON(w, status, payload)
}

func writeQuotaViolationError(w http.ResponseWriter, violation primitives.QuotaViolation) {
	status := http.StatusInsufficientStorage
	if violation.Code == "request_too_large" {
		status = http.StatusRequestEntityTooLarge
	}
	details := map[string]any{
		"quota": map[string]any{
			"metric":    violation.Metric,
			"limit":     violation.Limit,
			"current":   violation.Current,
			"projected": violation.Projected,
		},
	}
	message := "workspace quota exceeded"
	if violation.Code == "request_too_large" {
		message = "request body exceeds the configured upload limit"
	}
	writeDetailedError(w, status, violation.Code, message, details)
}

func writePrimitiveQuotaViolationError(w http.ResponseWriter, err error) bool {
	var violation *primitives.QuotaViolation
	if !errors.As(err, &violation) || violation == nil {
		return false
	}
	writeQuotaViolationError(w, *violation)
	return true
}

func writeRateLimitedError(w http.ResponseWriter, bucket string, retryAfter time.Duration) {
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%d", int64(math.Ceil(retryAfter.Seconds()))))
	details := map[string]any{
		"rate_limit": map[string]any{
			"bucket":              bucket,
			"retry_after_seconds": int64(math.Ceil(retryAfter.Seconds())),
		},
	}
	writeDetailedError(w, http.StatusTooManyRequests, "rate_limited", "too many requests for this route class", details)
}
