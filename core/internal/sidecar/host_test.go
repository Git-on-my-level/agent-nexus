package sidecar

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubService struct {
	name       string
	readyErr   error
	runErr     error
	runStarted chan struct{}
}

func (s *stubService) Name() string                            { return s.name }
func (s *stubService) Ready(context.Context) error             { return s.readyErr }
func (s *stubService) Snapshot(context.Context) map[string]any { return map[string]any{"kind": "stub"} }
func (s *stubService) Run(ctx context.Context) error {
	close(s.runStarted)
	if s.runErr != nil {
		return s.runErr
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestHostReadyAndSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := &stubService{name: "router", runStarted: make(chan struct{})}
	host := NewHost(Registration{Service: service, Enabled: true})
	host.Run(ctx)

	select {
	case <-service.runStarted:
	case <-time.After(time.Second):
		t.Fatal("service did not start")
	}

	if err := host.Ready(context.Background()); err != nil {
		t.Fatalf("expected host ready, got %v", err)
	}
	snapshot := host.Snapshot(context.Background())
	if snapshot["ok"] != true {
		t.Fatalf("expected snapshot ok, got %#v", snapshot)
	}
}

func TestHostReadyFailsWhenServiceIsUnhealthy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service := &stubService{
		name:       "router",
		readyErr:   errors.New("not ready"),
		runStarted: make(chan struct{}),
	}
	host := NewHost(Registration{Service: service, Enabled: true})
	host.Run(ctx)

	select {
	case <-service.runStarted:
	case <-time.After(time.Second):
		t.Fatal("service did not start")
	}

	if err := host.Ready(context.Background()); err == nil {
		t.Fatal("expected readiness error")
	}
}
