package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"

	"github.com/google/uuid"
)

// A caller that gave up is not a server fault. A disconnect records as a
// distinct 499 client cancellation only when the request context died AND the
// failure is the cancellation itself: context.Canceled in the error chain, or
// no error at all. A genuine failure that lands after the caller disconnected
// stays a 5xx and keeps its diagnostics, and an internal cancel wrapped as
// context.Canceled while the request context is still live is a core bug that
// logs as the 5xx the connected client receives. Core-side deadlines
// (context.DeadlineExceeded) stay real faults.

func canceledFaultLine(t *testing.T, logged string) string {
	t.Helper()
	var line string
	for _, entry := range strings.Split(strings.TrimSpace(logged), "\n") {
		if strings.Contains(entry, "anx-core: client canceled") {
			if line != "" {
				t.Fatalf("more than one client canceled line: %s", logged)
			}
			line = entry
		}
	}
	return line
}

func logFieldValue(line, key string) string {
	for _, field := range strings.Fields(line) {
		if value, ok := strings.CutPrefix(field, key+"="); ok {
			return value
		}
	}
	return ""
}

func requireCanceledOutcome(t *testing.T, logged, wantStage string) {
	t.Helper()
	if strings.Contains(logged, "anx-core: server error") {
		t.Fatalf("canceled request logged a server error: %s", logged)
	}
	line := canceledFaultLine(t, logged)
	if line == "" {
		t.Fatalf("no client canceled line: %s", logged)
	}
	for _, want := range []string{
		"status=499",
		"method=GET",
		"stage=" + wantStage,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("client canceled line missing %q: %s", want, line)
		}
	}
	if _, err := uuid.Parse(logFieldValue(line, "request_id")); err != nil {
		t.Errorf("client canceled request_id %q: %s", logFieldValue(line, "request_id"), line)
	}
}

// The fake-store seams mirror inbox_read_fault_test.go: a disconnect is
// injected at each read stage by canceling the request context and failing the
// seam with context.Canceled in the chain — the shape a real caller
// disconnect produces.
func TestClientCanceledReadsAreNotServerErrors(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	for _, tc := range []struct {
		stage   string
		path    string
		seam    string
		message string
	}{
		{stage: "authorization", path: "/inbox/private-id", seam: "authorization", message: "resource authorization failed"},
		{stage: "status", path: "/inbox/private-id", seam: "inbox", message: "failed to load inbox projection status"},
		{stage: "ask", path: "/asks/private-id", seam: "ask", message: "cannot read ask"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			output.Reset()
			cause := fmt.Errorf("private wrapper value: %w", context.Canceled)
			opts := handlerOptions{primitiveStore: inboxReadFaultStore{stage: tc.stage, cause: cause}}
			req := httptest.NewRequest(http.MethodGet, tc.path+"?secret=private-query", nil)
			req.Header.Set("Authorization", "Bearer private-token")
			cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader", PrincipalKind: "human"})
			check := func(context.Context, any) error { return nil }
			if tc.stage == "authorization" {
				check = func(context.Context, any) error { return cause }
			}
			disconnected, disconnect := context.WithCancel(context.Background())
			disconnect()
			req = req.WithContext(context.WithValue(disconnected, resourceAccessCheckKey{}, check))
			rec := httptest.NewRecorder()
			w := &faultResponseWriter{ResponseWriter: rec, r: req}
			switch tc.seam {
			case "authorization":
				authorizeResourceValues(w, req, []string{"inbox:private-id"})
			case "ask":
				handleAskOutcome(w, req, opts, false)
			default:
				handleGetInboxItem(w, req, opts, "private-id")
			}
			// The response body is irrelevant once the caller is gone; only the
			// log classification is asserted here.
			requireCanceledOutcome(t, output.String(), tc.message)
			line := canceledFaultLine(t, output.String())
			if tc.stage == "ask" && logFieldValue(line, "route") != "/asks/{id}" {
				t.Errorf("route template %q: %s", logFieldValue(line, "route"), line)
			}
		})
	}
}

// The token query fails under a canceled request context on the real auth
// store, reproducing the anonymous "query access token: context canceled"
// production evidence without touching handler code.
func TestCanceledTokenQueryIsNotAServerError(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	db := workspace.DB()

	req := httptest.NewRequest(http.MethodGet, "/inbox/private-id", nil)
	req.Header.Set("Authorization", "Bearer canceled-query-token")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(canceled)
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	opts := handlerOptions{authStore: auth.NewStore(db)}
	authenticatePrincipalFromHeader(w, req, opts, true)

	requireCanceledOutcome(t, output.String(), "failed to authenticate token")
}

// The full route middleware must stamp the start time so the client-canceled
// line reports elapsed time for slow stages.
func TestClientCanceledThroughRouteMiddlewareKeepsElapsed(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	db := workspace.DB()
	seed := seedMachinePrincipalForLockoutTest(t, ctx, db, "canceled-agent", "canceled-actor", "canceled.agent", "canceled-access-token")
	store := primitives.NewStore(db, nil, "")

	handler := NewHandler("canceled-test", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
	req := httptest.NewRequest(http.MethodGet, "/inbox/private-id", nil)
	req.Header.Set("Authorization", "Bearer "+seed.AccessToken)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(canceled)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	requireCanceledOutcome(t, output.String(), "failed to authenticate token")
	line := canceledFaultLine(t, output.String())
	if route := logFieldValue(line, "route"); route != "/inbox/{id}" {
		t.Fatalf("route template %q: %s", route, line)
	}
	elapsed, err := strconv.ParseInt(logFieldValue(line, "elapsed_ms"), 10, 64)
	if err != nil || elapsed < 0 {
		t.Fatalf("elapsed_ms %q: %s", logFieldValue(line, "elapsed_ms"), line)
	}
}

// Core-side deadlines are real server faults even though they arrive through
// a context error.
func TestServerSideDeadlineIsStillAServerError(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	cause := fmt.Errorf("private wrapper value: %w", context.DeadlineExceeded)
	opts := handlerOptions{primitiveStore: inboxReadFaultStore{stage: "status", cause: cause}}
	req := httptest.NewRequest(http.MethodGet, "/inbox/private-id", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader", PrincipalKind: "human"})
	req = req.WithContext(context.WithValue(req.Context(), resourceAccessCheckKey{}, func(context.Context, any) error { return nil }))
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	handleGetInboxItem(w, req, opts, "private-id")

	logged := output.String()
	if strings.Contains(logged, "anx-core: client canceled") {
		t.Fatalf("deadline classified as client canceled: %s", logged)
	}
	if strings.Count(logged, "anx-core: server error") != 1 || !strings.Contains(logged, "status=500") || !strings.Contains(logged, "class=deadline_exceeded") {
		t.Fatalf("deadline must stay a server error: %s", logged)
	}
}

// Work operations route through their own fault logger; a canceled caller must
// not be recorded as a failed work operation there either.
func TestCanceledWorkOperationIsNotAServerError(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	req := httptest.NewRequest(http.MethodPost, "/work", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "writer", PrincipalKind: "agent"})
	disconnected, disconnect := context.WithCancel(req.Context())
	disconnect()
	req = req.WithContext(disconnected)
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	workStoreError(w, req, fmt.Errorf("create work: %w", context.Canceled))

	logged := output.String()
	if strings.Contains(logged, "anx-core: server error") || strings.Contains(logged, "anx-core: work operation failed") {
		t.Fatalf("canceled work operation logged a server error: %s", logged)
	}
	line := canceledFaultLine(t, logged)
	if line == "" {
		t.Fatalf("no client canceled line: %s", logged)
	}
	for _, want := range []string{"status=499", "method=POST"} {
		if !strings.Contains(line, want) {
			t.Errorf("client canceled line missing %q: %s", want, line)
		}
	}
}

// The work fault funnel keeps a genuine store failure (rolled-back statement,
// driver IOERR) on the failed-work record with its fingerprint even when the
// caller had already disconnected; only the disconnect itself is not enough to
// reclassify it.
func TestGenuineWorkErrorAfterDisconnectKeepsDiagnostics(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	req := httptest.NewRequest(http.MethodPost, "/work", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "writer", PrincipalKind: "agent"})
	disconnected, disconnect := context.WithCancel(req.Context())
	disconnect()
	req = req.WithContext(disconnected)
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	workStoreError(w, req, fmt.Errorf("create work: %w", &inboxReadProbeError{}))

	logged := output.String()
	if strings.Contains(logged, "anx-core: client canceled") || strings.Contains(logged, "anx-core: server error") {
		t.Fatalf("disconnect reclassified a genuine work failure: %s", logged)
	}
	if strings.Count(logged, "anx-core: work operation failed") != 1 {
		t.Fatalf("expected one work fault line: %s", logged)
	}
	for _, want := range []string{"diagnostic=unmapped_store_error", "*server.inboxReadProbeError", "store_code=1", "error_fingerprint="} {
		if !strings.Contains(logged, want) {
			t.Errorf("work fault line missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "private database value") {
		t.Fatalf("work fault line leaked cause text: %s", logged)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

// Classification needs both signals: a dead request context and a failure that
// is itself the cancellation. Everything else — including a canceled chain
// under a still-live request context and a deadline — stays a server fault.
func TestRequestCanceledByClientRequiresBothSignals(t *testing.T) {
	genuine := fmt.Errorf("scan failed: %w", &inboxReadProbeError{})
	canceledChain := fmt.Errorf("wrapper: %w", context.Canceled)
	deadlineChain := fmt.Errorf("wrapper: %w", context.DeadlineExceeded)

	live := httptest.NewRequest(http.MethodGet, "/", nil)
	canceled := httptest.NewRequest(http.MethodGet, "/", nil)
	done, disconnect := context.WithCancel(context.Background())
	disconnect()
	canceled = canceled.WithContext(done)

	for _, tc := range []struct {
		name string
		r    *http.Request
		err  error
		want bool
	}{
		{name: "live request and canceled chain is internal", r: live, err: canceledChain, want: false},
		{name: "live request and genuine failure", r: live, err: genuine, want: false},
		{name: "live request without a cause", r: live, err: nil, want: false},
		{name: "disconnected request without a cause", r: canceled, err: nil, want: true},
		{name: "disconnected request and canceled chain", r: canceled, err: canceledChain, want: true},
		{name: "disconnected request and genuine failure", r: canceled, err: genuine, want: false},
		{name: "disconnected request and deadline", r: canceled, err: deadlineChain, want: false},
		{name: "missing request", r: nil, err: canceledChain, want: false},
	} {
		if got := requestCanceledByClient(tc.r, tc.err); got != tc.want {
			t.Errorf("%s: requestCanceledByClient=%v want %v", tc.name, got, tc.want)
		}
	}
}

// A genuine read failure that lands after the caller disconnected — a driver
// IOERR mid-scan — keeps its 500 record and class diagnostics; the disconnect
// alone must not reclassify it as a client cancellation.
func TestGenuineReadErrorAfterDisconnectStaysServerError(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	cause := fmt.Errorf("private wrapper value: %w", &inboxReadProbeError{})
	opts := handlerOptions{primitiveStore: inboxReadFaultStore{stage: "status", cause: cause}}
	req := httptest.NewRequest(http.MethodGet, "/inbox/private-id", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader", PrincipalKind: "human"})
	req = req.WithContext(context.WithValue(req.Context(), resourceAccessCheckKey{}, func(context.Context, any) error { return nil }))
	disconnected, disconnect := context.WithCancel(req.Context())
	disconnect()
	req = req.WithContext(disconnected)
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	handleGetInboxItem(w, req, opts, "private-id")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	logged := output.String()
	if strings.Contains(logged, "anx-core: client canceled") {
		t.Fatalf("disconnect reclassified a genuine failure: %s", logged)
	}
	if strings.Count(logged, "anx-core: server error") != 1 {
		t.Fatalf("expected one server error line: %s", logged)
	}
	for _, want := range []string{"status=500", "class=store_error", "*server.inboxReadProbeError", "store_code=1"} {
		if !strings.Contains(logged, want) {
			t.Errorf("server error line missing %q: %s", want, logged)
		}
	}
	for _, secret := range []string{"private database value", "private wrapper value"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("leaked %q: %s", secret, logged)
		}
	}
}

// An internal cancel wrapped as context.Canceled while the request context is
// still live is a core bug: the connected client receives a 500, and the log
// must record that same 500 with the cancel class instead of a 499.
func TestInternalCancelWithLiveRequestContextIsServerError(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	cause := fmt.Errorf("private wrapper value: %w", context.Canceled)
	opts := handlerOptions{primitiveStore: inboxReadFaultStore{stage: "status", cause: cause}}
	req := httptest.NewRequest(http.MethodGet, "/inbox/private-id", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader", PrincipalKind: "human"})
	req = req.WithContext(context.WithValue(req.Context(), resourceAccessCheckKey{}, func(context.Context, any) error { return nil }))
	rec := httptest.NewRecorder()
	w := &faultResponseWriter{ResponseWriter: rec, r: req}
	handleGetInboxItem(w, req, opts, "private-id")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	logged := output.String()
	if strings.Contains(logged, "anx-core: client canceled") {
		t.Fatalf("internal cancel classified as client canceled: %s", logged)
	}
	if strings.Count(logged, "anx-core: server error") != 1 || !strings.Contains(logged, "status=500") || !strings.Contains(logged, "class=request_canceled") {
		t.Fatalf("internal cancel must log the 500 the client receives: %s", logged)
	}
	if strings.Contains(logged, "private wrapper value") {
		t.Fatalf("leaked cause text: %s", logged)
	}
}
