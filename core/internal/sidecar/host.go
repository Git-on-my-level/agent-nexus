package sidecar

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Service interface {
	Name() string
	Run(ctx context.Context) error
	Ready(ctx context.Context) error
	Snapshot(ctx context.Context) map[string]any
}

type Registration struct {
	Service Service
	Enabled bool
}

type Host struct {
	services []Registration

	mu     sync.RWMutex
	states map[string]*runtimeState
}

type runtimeState struct {
	startedAt string
	running   bool
	lastError string
}

func NewHost(registrations ...Registration) *Host {
	states := make(map[string]*runtimeState, len(registrations))
	for _, registration := range registrations {
		if registration.Service == nil {
			continue
		}
		states[registration.Service.Name()] = &runtimeState{}
	}
	return &Host{
		services: registrations,
		states:   states,
	}
}

func (h *Host) Run(ctx context.Context) {
	for _, registration := range h.services {
		if registration.Service == nil || !registration.Enabled {
			continue
		}
		name := registration.Service.Name()
		h.markStarted(name)
		go func(service Service) {
			err := service.Run(ctx)
			if err != nil && ctx.Err() == nil {
				h.markStopped(service.Name(), err)
				return
			}
			h.markStopped(service.Name(), nil)
		}(registration.Service)
	}
}

func (h *Host) Ready(ctx context.Context) error {
	for _, registration := range h.services {
		if registration.Service == nil || !registration.Enabled {
			continue
		}
		state := h.getState(registration.Service.Name())
		if state == nil || !state.running {
			return fmt.Errorf("sidecar %s is not running", registration.Service.Name())
		}
		if strings.TrimSpace(state.lastError) != "" {
			return fmt.Errorf("sidecar %s failed: %s", registration.Service.Name(), state.lastError)
		}
		if err := registration.Service.Ready(ctx); err != nil {
			return fmt.Errorf("sidecar %s not ready: %w", registration.Service.Name(), err)
		}
	}
	return nil
}

func (h *Host) Snapshot(ctx context.Context) map[string]any {
	services := make([]map[string]any, 0, len(h.services))
	ready := true
	for _, registration := range h.services {
		if registration.Service == nil {
			continue
		}
		state := h.getState(registration.Service.Name())
		entry := map[string]any{
			"name":    registration.Service.Name(),
			"enabled": registration.Enabled,
		}
		if state != nil {
			entry["running"] = state.running
			if strings.TrimSpace(state.startedAt) != "" {
				entry["started_at"] = state.startedAt
			}
			if strings.TrimSpace(state.lastError) != "" {
				entry["error"] = state.lastError
			}
		}
		if registration.Enabled {
			if err := registration.Service.Ready(ctx); err != nil {
				ready = false
				entry["ready"] = false
				entry["ready_error"] = err.Error()
			} else {
				entry["ready"] = true
			}
		} else {
			entry["ready"] = false
		}
		if details := registration.Service.Snapshot(ctx); len(details) > 0 {
			entry["details"] = details
		}
		services = append(services, entry)
	}
	return map[string]any{
		"ok":       ready,
		"services": services,
	}
}

func (h *Host) markStarted(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.ensureStateLocked(name)
	state.running = true
	state.lastError = ""
	state.startedAt = time.Now().UTC().Format(time.RFC3339Nano)
}

func (h *Host) markStopped(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	state := h.ensureStateLocked(name)
	state.running = false
	if err != nil {
		state.lastError = err.Error()
	}
}

func (h *Host) getState(name string) *runtimeState {
	h.mu.RLock()
	defer h.mu.RUnlock()
	state, ok := h.states[name]
	if !ok {
		return nil
	}
	copied := *state
	return &copied
}

func (h *Host) ensureStateLocked(name string) *runtimeState {
	state, ok := h.states[name]
	if !ok {
		state = &runtimeState{}
		h.states[name] = state
	}
	return state
}
