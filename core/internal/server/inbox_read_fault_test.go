package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

type inboxReadFaultStore struct {
	PrimitiveStore
	stage string
	cause error
}

func (s inboxReadFaultStore) GetDerivedInboxItem(context.Context, string) (primitives.DerivedInboxItem, error) {
	if s.stage == "item" {
		return primitives.DerivedInboxItem{}, s.cause
	}
	return primitives.DerivedInboxItem{ID: "probe", ThreadID: "thread", Data: map[string]any{"kind": "ask"}}, nil
}
func (s inboxReadFaultStore) ListDerivedTopicProjections(context.Context, []string) (map[string]primitives.DerivedTopicProjection, error) {
	if s.stage == "projection" {
		return nil, s.cause
	}
	return nil, nil
}
func (s inboxReadFaultStore) GetTopicProjectionRefreshStatuses(context.Context, []string) (map[string]primitives.TopicProjectionRefreshStatus, error) {
	return nil, s.cause
}
func (s inboxReadFaultStore) AskOutcome(context.Context, string) (map[string]any, error) {
	return nil, s.cause
}

type inboxReadProbeError struct{}

func (*inboxReadProbeError) Error() string { return "private database value" }
func (*inboxReadProbeError) Code() int     { return 1 }

func TestInboxReadFaultsLogWrappedClassesOnly(t *testing.T) {
	// Serial: capture process-wide logging. Faults are injected at each read seam;
	// lifecycle absence is tested with the real authenticated HTTP/store stack.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, stage := range []string{"item", "projection", "status", "authorization", "ask"} {
		t.Run(stage, func(t *testing.T) {
			output.Reset()
			cause := fmt.Errorf("private wrapper value: %w", &inboxReadProbeError{})
			opts := handlerOptions{primitiveStore: inboxReadFaultStore{stage: stage, cause: cause}}
			path := "/inbox/private-id"
			if stage == "ask" {
				path = "/asks/private-id"
			}
			req := httptest.NewRequest(http.MethodGet, path+"?secret=private-query", nil)
			cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader", PrincipalKind: "human"})
			// Supply the same selector policy seam as the canonical store, without
			// depending on unrelated thread presentation in this diagnostic test.
			check := func(context.Context, any) error { return nil }
			if stage == "authorization" {
				check = func(context.Context, any) error { return cause }
			}
			req = req.WithContext(context.WithValue(req.Context(), resourceAccessCheckKey{}, check))
			rec := httptest.NewRecorder()
			w := &faultResponseWriter{ResponseWriter: rec, r: req}
			switch stage {
			case "authorization":
				authorizeResourceValues(w, req, []string{"inbox:private-id"})
			case "ask":
				handleAskOutcome(w, req, opts, false)
			default:
				handleGetInboxItem(w, req, opts, "private-id")
			}
			if rec.Code != 500 {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			line := output.String()
			for _, want := range []string{"anx-core: server error", "actor_kind=human", "*fmt.wrapError", "*server.inboxReadProbeError", "store_code=1"} {
				if !strings.Contains(line, want) {
					t.Errorf("missing %q: %s", want, line)
				}
			}
			if strings.Count(line, "anx-core: server error") != 1 || strings.Contains(line, "error=-") {
				t.Errorf("diagnostic: %s", line)
			}
			for _, secret := range []string{"private database value", "private wrapper value", "private-id", "private-query"} {
				if strings.Contains(line+rec.Body.String(), secret) {
					t.Errorf("leaked %q: %s", secret, line)
				}
			}
			if stage == "ask" && !strings.Contains(line, "route=/asks/{id}") {
				t.Errorf("route: %s", line)
			}
		})
	}
}
