package server

import (
	"container/heap"
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

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

const (
	defaultJSONRequestBodyLimit    int64 = 1 << 20
	defaultAuthRequestBodyLimit    int64 = 256 << 10
	defaultContentRequestBodyLimit int64 = 8 << 20

	defaultAuthRequestsPerMinute       = 600
	defaultAuthRequestsBurst           = 100
	defaultEnrollmentRequestsPerMinute = 12
	defaultEnrollmentRequestsBurst     = 8
	defaultWriteRequestsPerMinute      = 1200
	defaultWriteRequestsBurst          = 200
	requestTooLargeRetryAfterSecs      = 1
	// One person or managed agent gets the budget a single caller address
	// had before verified exchanges were keyed by subject.
	defaultWorkspaceGrantSubjectRequestsPerMinute       = 10
	defaultWorkspaceGrantSubjectBurst                   = 10
	defaultWorkspaceGrantPrincipalMaxKeys               = 65536
	defaultRouteRateLimitMaxKeys                        = 4096
	defaultRouteRateLimitBucketTTL                      = 15 * time.Minute
	routeRateLimitPruneInterval                         = time.Minute
	defaultAttachmentRequestBodyLimit             int64 = 50 << 20
)

type RequestBodyLimits struct {
	Default    int64
	Auth       int64
	Content    int64
	Attachment int64
}

type RouteRateLimits struct {
	AuthRequestsPerMinute       int
	AuthBurst                   int
	EnrollmentRequestsPerMinute int
	EnrollmentBurst             int
	WriteRequestsPerMinute      int
	WriteBurst                  int
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
	if l.Attachment <= 0 {
		l.Attachment = defaultAttachmentRequestBodyLimit
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
	if l.EnrollmentRequestsPerMinute <= 0 {
		l.EnrollmentRequestsPerMinute = defaultEnrollmentRequestsPerMinute
	}
	if l.EnrollmentBurst <= 0 {
		l.EnrollmentBurst = defaultEnrollmentRequestsBurst
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
	mu      sync.Mutex
	buckets map[string]*rateLimitBucket
	// byFullAt orders buckets by when they refill to their burst, so idle
	// buckets are found at the top without scanning.
	byFullAt  rateLimitBucketHeap
	limits    RouteRateLimits
	maxKeys   int
	bucketTTL time.Duration
	lastPrune time.Time
	// failClosedWhenFull refuses a new scope when every tracked bucket
	// still carries state, instead of evicting one and handing that scope a
	// fresh burst.
	failClosedWhenFull bool
}

type rateLimitBucket struct {
	key        string
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
	// fullAt is when the bucket will have refilled to its burst; from then
	// on forgetting it loses nothing.
	fullAt time.Time
	index  int
}

type rateLimitBucketHeap []*rateLimitBucket

func (h rateLimitBucketHeap) Len() int           { return len(h) }
func (h rateLimitBucketHeap) Less(i, j int) bool { return h[i].fullAt.Before(h[j].fullAt) }
func (h rateLimitBucketHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *rateLimitBucketHeap) Push(x any) {
	bucket := x.(*rateLimitBucket)
	bucket.index = len(*h)
	*h = append(*h, bucket)
}
func (h *rateLimitBucketHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	old[len(old)-1] = nil
	*h = old[:len(old)-1]
	last.index = -1
	return last
}

func newRouteRateLimiter(limits RouteRateLimits) *routeRateLimiter {
	return &routeRateLimiter{
		buckets:   make(map[string]*rateLimitBucket, 2),
		limits:    limits.normalize(),
		maxKeys:   defaultRouteRateLimitMaxKeys,
		bucketTTL: defaultRouteRateLimitBucketTTL,
	}
}

// allow spends one request from the scope's budget when one is left.
// Its cost is O(log n) in the tracked scopes, full or not.
func (l *routeRateLimiter) allow(bucket string, scope string, now time.Time) (bool, time.Duration) {
	_, allowed, retryAfter := l.reserve(bucket, scope, now)
	return allowed, retryAfter
}

// rateLimitReservation records the bucket one allowed request was charged
// to, so a refund can return exactly that request.
type rateLimitReservation struct {
	limiter *routeRateLimiter
	state   *rateLimitBucket
	limit   int
	burst   int
}

// reserve is allow that also returns a reservation for a later refund.
func (l *routeRateLimiter) reserve(bucket string, scope string, now time.Time) (rateLimitReservation, bool, time.Duration) {
	if l == nil || strings.TrimSpace(bucket) == "" {
		return rateLimitReservation{}, true, 0
	}
	limit, burst := l.limitForBucket(bucket)
	if limit <= 0 || burst <= 0 {
		return rateLimitReservation{}, true, 0
	}
	ratePerSecond := float64(limit) / 60.0

	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.bucketLocked(rateLimitStateKey(bucket, scope), limit, burst, now)
	if state == nil {
		// No bucket could be tracked without discarding another scope's
		// state; refuse until the nearest one refills.
		retryAfter := time.Second
		if len(l.byFullAt) > 0 {
			if wait := l.byFullAt[0].fullAt.Sub(now); wait > retryAfter {
				retryAfter = wait
			}
		}
		return rateLimitReservation{}, false, retryAfter
	}
	state.lastSeen = now
	if state.tokens >= 1 {
		state.tokens -= 1
		l.updateFullAtLocked(state, limit, burst)
		return rateLimitReservation{limiter: l, state: state, limit: limit, burst: burst}, true, 0
	}
	return rateLimitReservation{}, false, rateLimitRetryAfter(state.tokens, ratePerSecond)
}

// refund returns the one request this reservation took. The bucket ends
// where it would be had the request never been charged: refilled to now,
// plus one, never above its burst. A refund to a bucket that was evicted
// (and possibly recreated) since the charge does nothing, and a
// reservation refunds at most once.
func (r *rateLimitReservation) refund(now time.Time) {
	if r == nil || r.limiter == nil || r.state == nil {
		return
	}
	l, state := r.limiter, r.state
	r.state = nil
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets[state.key] != state {
		return
	}
	refillRateLimitBucket(state, r.limit, r.burst, now)
	state.tokens = math.Min(float64(r.burst), state.tokens+1)
	l.updateFullAtLocked(state, r.limit, r.burst)
}

func rateLimitStateKey(bucket string, scope string) string {
	if scope = strings.TrimSpace(scope); scope != "" {
		return bucket + ":" + scope
	}
	return bucket
}

func rateLimitRetryAfter(tokens float64, ratePerSecond float64) time.Duration {
	if ratePerSecond <= 0 {
		return requestTooLargeRetryAfterSecs * time.Second
	}
	retryAfter := time.Duration(math.Ceil((1 - tokens) / ratePerSecond * float64(time.Second)))
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return retryAfter
}

func refillRateLimitBucket(state *rateLimitBucket, limit int, burst int, now time.Time) {
	if now.Before(state.lastRefill) {
		state.lastRefill = now
	}
	ratePerSecond := float64(limit) / 60.0
	if elapsed := now.Sub(state.lastRefill).Seconds(); elapsed > 0 && ratePerSecond > 0 {
		state.tokens = math.Min(float64(burst), state.tokens+elapsed*ratePerSecond)
		state.lastRefill = now
	}
}

// updateFullAtLocked recomputes when a just-refilled bucket reaches its
// burst and restores heap order.
func (l *routeRateLimiter) updateFullAtLocked(state *rateLimitBucket, limit int, burst int) {
	missing := float64(burst) - state.tokens
	state.fullAt = state.lastRefill
	if missing > 0 {
		ratePerSecond := float64(limit) / 60.0
		state.fullAt = state.lastRefill.Add(time.Duration(math.Ceil(missing / ratePerSecond * float64(time.Second))))
	}
	if state.index >= 0 && state.index < len(l.byFullAt) && l.byFullAt[state.index] == state {
		heap.Fix(&l.byFullAt, state.index)
	}
}

func (l *routeRateLimiter) removeLocked(state *rateLimitBucket) {
	if state.index >= 0 && state.index < len(l.byFullAt) && l.byFullAt[state.index] == state {
		heap.Remove(&l.byFullAt, state.index)
	}
	delete(l.buckets, state.key)
}

// bucketLocked returns the refilled bucket for a key, creating it when
// there is room. It returns nil only for a fail-closed limiter whose
// buckets all still carry state.
func (l *routeRateLimiter) bucketLocked(stateKey string, limit int, burst int, now time.Time) *rateLimitBucket {
	if state, ok := l.buckets[stateKey]; ok {
		refillRateLimitBucket(state, limit, burst, now)
		return state
	}
	l.pruneBuckets(now)
	if l.maxKeys > 0 && len(l.buckets) >= l.maxKeys && !l.evictBucketLocked(now) {
		return nil
	}
	state := &rateLimitBucket{key: stateKey, tokens: float64(burst), lastRefill: now, lastSeen: now, fullAt: now}
	l.buckets[stateKey] = state
	heap.Push(&l.byFullAt, state)
	return state
}

// evictBucketLocked frees one slot, in O(log n). The bucket nearest to
// refilled goes first; if it has refilled, nothing is lost. If none has, a
// fail-closed limiter keeps every bucket and reports false; otherwise the
// nearest-to-refilled bucket, which carries the least state, is evicted.
func (l *routeRateLimiter) evictBucketLocked(now time.Time) bool {
	if len(l.byFullAt) == 0 {
		return false
	}
	nearest := l.byFullAt[0]
	if nearest.fullAt.After(now) && l.failClosedWhenFull {
		return false
	}
	l.removeLocked(nearest)
	return true
}

func (l *routeRateLimiter) limitForBucket(bucket string) (int, int) {
	switch bucket {
	case "auth":
		return l.limits.AuthRequestsPerMinute, l.limits.AuthBurst
	case "enrollment":
		return l.limits.EnrollmentRequestsPerMinute, l.limits.EnrollmentBurst
	case "write":
		return l.limits.WriteRequestsPerMinute, l.limits.WriteBurst
	default:
		return 0, 0
	}
}

// pruneBuckets forgets buckets that have been refilled for at least the
// bucket TTL, at most once per prune interval, popping them from the top of
// the refill order.
func (l *routeRateLimiter) pruneBuckets(now time.Time) {
	if l == nil || len(l.buckets) == 0 || l.bucketTTL <= 0 {
		return
	}
	if now.Sub(l.lastPrune) < routeRateLimitPruneInterval {
		return
	}
	cutoff := now.Add(-l.bucketTTL)
	for len(l.byFullAt) > 0 && !l.byFullAt[0].fullAt.After(cutoff) {
		l.removeLocked(l.byFullAt[0])
	}
	l.lastPrune = now
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
	case "/artifacts", "/docs":
		if method == http.MethodPost || method == http.MethodPatch {
			return limits.Content
		}
	case "/artifacts/attachments":
		if method == http.MethodPost {
			return limits.Attachment
		}
	}

	if strings.HasPrefix(path, "/docs/") || strings.HasPrefix(path, "/artifacts/") || strings.HasPrefix(path, "/threads/") || strings.HasPrefix(path, "/boards/") {
		if strings.HasSuffix(path, "/trash") || strings.HasSuffix(path, "/archive") || strings.HasSuffix(path, "/unarchive") ||
			strings.HasSuffix(path, "/restore") || strings.HasSuffix(path, "/purge") ||
			strings.HasSuffix(path, "/history") || strings.Contains(path, "/revisions/") || strings.HasSuffix(path, "/content") {
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
		if path == "/auth/hosts/enrollments" || path == "/auth/hosts/enrollments/headless" {
			return "enrollment"
		}
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
	return decodeJSONBodySyntax(w, r, dst) && authorizeResourceValues(w, r, dst)
}

// Audited map-valued replay bodies must retain integer precision both for
// persistence and changed-body key rejection. Keep legacy decoder behavior for
// callers that have not yet been audited for json.Number values.
func decodeJSONBodyPreserveNumbers(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodySyntaxWithNumbers(w, r, dst, true) && authorizeResourceValues(w, r, dst)
}

// Read-side batches may include inaccessible or shared keys. Their scoped
// loaders authorize each candidate, while writes retain whole-body ownership
// checks. Text validation, body limits and single-document parsing still apply.
func decodeJSONReadBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodySyntax(w, r, dst) && validateResourceText(w, dst)
}

func decodeJSONBodySyntax(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONBodySyntaxWithNumbers(w, r, dst, false)
}

func decodeJSONBodySyntaxWithNumbers(w http.ResponseWriter, r *http.Request, dst any, preserveNumbers bool) bool {
	if r == nil || r.Body == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	decoder := json.NewDecoder(r.Body)
	if preserveNumbers {
		decoder.UseNumber()
	}
	if err := decoder.Decode(dst); err != nil {
		if writeRequestTooLargeError(w, err) {
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return ensureJSONBodyEOF(w, decoder)
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
	return ensureJSONBodyEOF(w, decoder) && authorizeResourceValues(w, r, dst)
}

func ensureJSONBodyEOF(w http.ResponseWriter, decoder *json.Decoder) bool {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		if writeRequestTooLargeError(w, err) {
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
	return false
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
	metric := violation.Metric
	label := metric
	switch metric {
	case "artifact_count":
		metric, label = "artifacts", "artifact"
	case "document_count":
		metric, label = "documents", "document"
	case "revision_count":
		metric, label = "revisions", "revision"
	case "storage_bytes":
		metric, label = "blob_bytes", "storage byte"
	case "upload_bytes":
		label = "upload byte"
	}
	quota := map[string]any{"metric": metric, "limit": violation.Limit}
	message := fmt.Sprintf("Workspace %s limit reached (limit %d). Remove content or raise the configured workspace limit.", label, violation.Limit)
	// Enforcement uses canonical totals, but these totals are reader-visible.
	// Include them only when they explain why the write was rejected.
	if violation.Projected > violation.Limit {
		delta := violation.Projected - violation.Current
		quota["current"] = violation.Current
		quota["delta"] = delta
		quota["projected"] = violation.Projected
		message = fmt.Sprintf("Workspace %s limit reached (%d/%d). This operation needs %d more. Remove content or raise the configured workspace limit.", label, violation.Current, violation.Limit, delta)
	}
	writeDetailedError(w, http.StatusConflict, "quota_exceeded", message, map[string]any{
		"quota": quota,
	})
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
