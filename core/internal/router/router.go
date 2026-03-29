package router

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
)

const (
	defaultPollInterval = time.Second
	eventBatchSize      = 100
)

type Config struct {
	BaseURL           string
	WorkspaceID       string
	WorkspaceName     string
	StatePath         string
	PrincipalCacheTTL time.Duration
	PollInterval      time.Duration
	ActorID           string
}

type Dependencies struct {
	ListPrincipals         func(ctx context.Context, limit int) ([]auth.AuthPrincipalSummary, error)
	ListMessagePostedAfter func(ctx context.Context, cursor primitives.EventCursor, limit int) ([]map[string]any, error)
	GetRegistrationContent func(ctx context.Context, documentID string) (map[string]any, error)
	GetEvent               func(ctx context.Context, eventID string) (map[string]any, error)
	GetThread              func(ctx context.Context, threadID string) (map[string]any, error)
	CreateArtifact         func(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) error
	AppendEvent            func(ctx context.Context, actorID string, event map[string]any) error
	MarkThreadDirty        func(ctx context.Context, threadID string, queuedAt time.Time) error
}

type principalCache struct {
	loadedAt time.Time
	byHandle map[string]auth.AuthPrincipalSummary
}

type Service struct {
	cfg   Config
	deps  Dependencies
	state *StateStore

	cache principalCache

	mu         sync.RWMutex
	ready      bool
	lastError  string
	lastPolled string
}

func NewService(cfg Config, deps Dependencies, state *StateStore) *Service {
	if cfg.PrincipalCacheTTL <= 0 {
		cfg.PrincipalCacheTTL = time.Minute
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if strings.TrimSpace(cfg.WorkspaceName) == "" {
		cfg.WorkspaceName = "Main"
	}
	if strings.TrimSpace(cfg.ActorID) == "" {
		cfg.ActorID = "oar-core"
	}
	return &Service{
		cfg:   cfg,
		deps:  deps,
		state: state,
		cache: principalCache{byHandle: map[string]auth.AuthPrincipalSummary{}},
	}
}

func (s *Service) Name() string {
	return "router"
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := s.runOnce(ctx); err != nil {
			s.recordFailure(err)
			log.Printf("router sidecar poll failed: %v", err)
		} else {
			s.recordSuccess()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) Ready(context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.TrimSpace(s.lastError) != "" {
		return errors.New(s.lastError)
	}
	if !s.ready {
		return errors.New("initial poll not completed")
	}
	return nil
}

func (s *Service) Snapshot(context.Context) map[string]any {
	s.mu.RLock()
	ready := s.ready
	lastError := s.lastError
	lastPolled := s.lastPolled
	s.mu.RUnlock()

	snapshot := s.state.Snapshot()
	snapshot["ready"] = ready
	if strings.TrimSpace(lastError) != "" {
		snapshot["last_error"] = lastError
	}
	if strings.TrimSpace(lastPolled) != "" {
		snapshot["last_polled_at"] = lastPolled
	}
	return snapshot
}

func (s *Service) runOnce(ctx context.Context) error {
	cursor, err := s.resolveCursor(ctx)
	if err != nil {
		return err
	}

	for {
		events, err := s.deps.ListMessagePostedAfter(ctx, cursor, eventBatchSize)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for _, event := range events {
			if err := s.handleEvent(ctx, event); err != nil {
				return err
			}
			cursor = primitives.EventCursor{
				TS: anyString(event["ts"]),
				ID: anyString(event["id"]),
			}
			if err := s.state.SetLastEventCursor(cursor.TS, cursor.ID); err != nil {
				return err
			}
		}
		if len(events) < eventBatchSize {
			return nil
		}
	}
}

func (s *Service) resolveCursor(ctx context.Context) (primitives.EventCursor, error) {
	cursor := primitives.EventCursor{
		TS: s.state.LastEventTS(),
		ID: s.state.LastEventID(),
	}
	if strings.TrimSpace(cursor.TS) != "" || strings.TrimSpace(cursor.ID) == "" {
		return cursor, nil
	}
	event, err := s.deps.GetEvent(ctx, cursor.ID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return primitives.EventCursor{}, nil
		}
		return primitives.EventCursor{}, err
	}
	cursor.TS = anyString(event["ts"])
	return cursor, nil
}

func (s *Service) handleEvent(ctx context.Context, event map[string]any) error {
	eventID := anyString(event["id"])
	text := extractMessageText(event)
	handles := ExtractMentions(text)
	updates := map[string]any{
		"router_last_message_seen_at":       utcNowISO(),
		"router_last_message_seen_event_id": eventID,
	}
	if len(handles) > 0 {
		updates["router_last_tagged_message_event_id"] = eventID
		updates["router_last_tagged_message_seen_at"] = utcNowISO()
		updates["router_last_tagged_handles"] = handles
		updates["router_last_tagged_message_preview"] = compactText(text, 140)
	}
	if err := s.state.Update(updates); err != nil {
		return err
	}
	if len(handles) == 0 {
		return nil
	}
	if err := s.loadPrincipals(ctx, false); err != nil {
		return err
	}
	routed := make([]string, 0, len(handles))
	for _, handle := range handles {
		ok, err := s.routeMention(ctx, handle, event, text)
		if err != nil {
			log.Printf("router sidecar failed routing @%s from event %s: %v", handle, eventID, err)
			if emitErr := s.emitException(ctx, anyString(event["thread_id"]), eventID, handle, "mention_routing_failed", fmt.Sprintf("Failed routing @%s", handle)); emitErr != nil {
				log.Printf("router sidecar failed to emit exception for @%s: %v", handle, emitErr)
			}
			continue
		}
		if ok {
			routed = append(routed, handle)
		}
	}
	if len(routed) == 0 {
		return nil
	}
	return s.state.Update(map[string]any{
		"router_last_routed_event_id": eventID,
		"router_last_routed_at":       utcNowISO(),
		"router_last_routed_handles":  routed,
	})
}

func (s *Service) loadPrincipals(ctx context.Context, force bool) error {
	if !force && time.Since(s.cache.loadedAt) < s.cfg.PrincipalCacheTTL && len(s.cache.byHandle) > 0 {
		return nil
	}
	principals, err := s.deps.ListPrincipals(ctx, 200)
	if err != nil {
		return err
	}
	byHandle := make(map[string]auth.AuthPrincipalSummary, len(principals))
	for _, principal := range principals {
		if principal.Revoked || strings.TrimSpace(principal.PrincipalKind) != "agent" {
			continue
		}
		username := strings.ToLower(strings.TrimSpace(principal.Username))
		if username == "" {
			continue
		}
		byHandle[username] = principal
	}
	s.cache.loadedAt = time.Now().UTC()
	s.cache.byHandle = byHandle
	return nil
}

func (s *Service) routeMention(ctx context.Context, handle string, event map[string]any, text string) (bool, error) {
	threadID := anyString(event["thread_id"])
	eventID := anyString(event["id"])
	if threadID == "" || eventID == "" {
		return false, nil
	}

	principal, ok := s.cache.byHandle[handle]
	if !ok {
		return false, s.emitException(ctx, threadID, eventID, handle, "unknown_agent_handle", fmt.Sprintf("Unknown tagged agent @%s", handle))
	}

	registration, err := s.loadRegistration(ctx, handle)
	if err != nil {
		return false, err
	}
	if registration == nil {
		return false, s.emitException(ctx, threadID, eventID, handle, "missing_agent_registration", fmt.Sprintf("Tagged agent @%s has no registration document", handle))
	}
	if registration.ActorID != strings.TrimSpace(principal.ActorID) {
		return false, s.emitException(ctx, threadID, eventID, handle, "registration_actor_mismatch", fmt.Sprintf("Tagged agent @%s registration actor does not match principal", handle))
	}
	if !registration.SupportsWorkspace(s.cfg.WorkspaceID) {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_not_bound_to_workspace", fmt.Sprintf("Tagged agent @%s is not enabled for workspace %s", handle, s.cfg.WorkspaceID))
	}
	if registration.Status != "active" {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_not_ready", fmt.Sprintf("Tagged agent @%s is registered but not wakeable until its bridge checks in", handle))
	}
	if strings.TrimSpace(registration.BridgeCheckinEventID) == "" {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_not_checked_in", fmt.Sprintf("Tagged agent @%s has no bridge check-in event yet", handle))
	}

	checkin, err := s.loadBridgeCheckin(ctx, registration.BridgeCheckinEventID)
	if err != nil {
		return false, err
	}
	if checkin == nil {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_not_checked_in", fmt.Sprintf("Tagged agent @%s has no valid bridge check-in event yet", handle))
	}
	if checkin.Handle != "" && checkin.Handle != handle {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_handle_mismatch", fmt.Sprintf("Tagged agent @%s bridge check-in handle does not match registration", handle))
	}
	if strings.TrimSpace(registration.BridgeSigningPublicKeySPKIB64) == "" {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_proof_missing", fmt.Sprintf("Tagged agent @%s registration is missing its bridge proof key", handle))
	}
	if !VerifyBridgeCheckinSignature(registration.BridgeSigningPublicKeySPKIB64, *checkin) {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_proof_invalid", fmt.Sprintf("Tagged agent @%s has an invalid bridge readiness proof", handle))
	}
	if checkin.ActorID != registration.ActorID {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_actor_mismatch", fmt.Sprintf("Tagged agent @%s bridge check-in actor does not match registration actor", handle))
	}
	if !checkin.ReadyForWorkspace(s.cfg.WorkspaceID, nowUTC()) {
		return false, s.emitException(ctx, threadID, eventID, handle, "agent_bridge_checkin_stale", fmt.Sprintf("Tagged agent @%s has a stale bridge check-in and is not wakeable right now", handle))
	}

	thread, err := s.deps.GetThread(ctx, threadID)
	if err != nil {
		return false, err
	}
	wakeupID := WakeupArtifactID(s.cfg.WorkspaceID, threadID, eventID, registration.ActorID)
	sessionKey := fmt.Sprintf("oar:%s:%s:%s", s.cfg.WorkspaceID, threadID, handle)
	packet := WakePacket{
		WakeupID:             wakeupID,
		Handle:               handle,
		ActorID:              registration.ActorID,
		WorkspaceID:          s.cfg.WorkspaceID,
		WorkspaceName:        s.cfg.WorkspaceName,
		ThreadID:             threadID,
		ThreadTitle:          firstNonEmpty(anyString(thread["title"]), threadID),
		TriggerEventID:       eventID,
		TriggerCreatedAt:     anyString(event["ts"]),
		TriggerAuthorActorID: anyString(event["actor_id"]),
		TriggerText:          text,
		CurrentSummary:       anyString(thread["current_summary"]),
		SessionKey:           sessionKey,
		OARBaseURL:           strings.TrimRight(s.cfg.BaseURL, "/"),
		ThreadContextURL:     fmt.Sprintf("%s/threads/%s/context", strings.TrimRight(s.cfg.BaseURL, "/"), threadID),
		ThreadWorkspaceURL:   fmt.Sprintf("%s/threads/%s/workspace", strings.TrimRight(s.cfg.BaseURL, "/"), threadID),
		TriggerEventURL:      fmt.Sprintf("%s/events/%s", strings.TrimRight(s.cfg.BaseURL, "/"), eventID),
		CLIThreadInspect:     fmt.Sprintf("oar threads inspect --thread-id %s --json", threadID),
		CLIThreadWorkspace:   fmt.Sprintf("oar threads workspace --thread-id %s --include-related-event-content --json", threadID),
	}

	artifact := map[string]any{
		"id":              wakeupID,
		"kind":            WakeArtifactKind,
		"summary":         fmt.Sprintf("Wake packet for @%s", handle),
		"refs":            []string{fmt.Sprintf("thread:%s", threadID), fmt.Sprintf("event:%s", eventID)},
		"target_handle":   handle,
		"target_actor_id": registration.ActorID,
		"workspace_id":    s.cfg.WorkspaceID,
		"thread_id":       threadID,
	}
	if err := s.deps.CreateArtifact(ctx, s.cfg.ActorID, artifact, packet.ToContent(), "structured"); err != nil && !errors.Is(err, primitives.ErrConflict) {
		return false, err
	}

	requestKey := WakeupRequestKey(s.cfg.WorkspaceID, threadID, eventID, registration.ActorID)
	eventBody := map[string]any{
		"type":      WakeRequestEvent,
		"thread_id": threadID,
		"summary":   fmt.Sprintf("Wake requested for @%s", handle),
		"refs": []string{
			fmt.Sprintf("thread:%s", threadID),
			fmt.Sprintf("event:%s", eventID),
			fmt.Sprintf("artifact:%s", wakeupID),
		},
		"payload": map[string]any{
			"wakeup_id":        wakeupID,
			"wake_artifact_id": wakeupID,
			"target_handle":    handle,
			"target_actor_id":  registration.ActorID,
			"workspace_id":     s.cfg.WorkspaceID,
			"workspace_name":   s.cfg.WorkspaceName,
			"thread_id":        threadID,
			"trigger_event_id": eventID,
			"session_key":      sessionKey,
		},
		"provenance": map[string]any{
			"sources": []string{fmt.Sprintf("actor_statement:%s", eventID)},
		},
	}
	if err := s.appendThreadEvent(ctx, requestKey, eventBody); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) loadRegistration(ctx context.Context, handle string) (*AgentRegistration, error) {
	content, err := s.deps.GetRegistrationContent(ctx, RegistrationDocumentID(handle))
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	registration, err := decodeIntoMap[AgentRegistration](content)
	if err != nil {
		return nil, err
	}
	return &registration, nil
}

func (s *Service) loadBridgeCheckin(ctx context.Context, eventID string) (*AgentBridgeCheckin, error) {
	event, err := s.deps.GetEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if anyString(event["type"]) != BridgeCheckedInEvent {
		return nil, nil
	}
	payload, ok := event["payload"].(map[string]any)
	if !ok {
		return nil, nil
	}
	checkin, err := decodeIntoMap[AgentBridgeCheckin](payload)
	if err != nil {
		return nil, err
	}
	return &checkin, nil
}

func (s *Service) emitException(ctx context.Context, threadID string, eventID string, handle string, code string, summary string) error {
	requestKey := fmt.Sprintf("exc-%s-%s-%s", code, handle, eventID)
	return s.appendThreadEvent(ctx, requestKey, map[string]any{
		"type":      "exception_raised",
		"thread_id": threadID,
		"summary":   summary,
		"refs":      []string{fmt.Sprintf("thread:%s", threadID), fmt.Sprintf("event:%s", eventID)},
		"payload": map[string]any{
			"subtype": code,
			"code":    code,
			"handle":  handle,
		},
		"provenance": map[string]any{
			"sources": []string{fmt.Sprintf("actor_statement:%s", eventID)},
		},
	})
}

func (s *Service) appendThreadEvent(ctx context.Context, requestKey string, event map[string]any) error {
	if strings.TrimSpace(requestKey) != "" && strings.TrimSpace(anyString(event["id"])) == "" {
		event["id"] = deriveRequestScopedID("events.create", s.cfg.ActorID, requestKey, "event")
	}
	err := s.deps.AppendEvent(ctx, s.cfg.ActorID, event)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) {
			return nil
		}
		return err
	}
	threadID := anyString(event["thread_id"])
	if strings.TrimSpace(threadID) != "" && s.deps.MarkThreadDirty != nil {
		if err := s.deps.MarkThreadDirty(ctx, threadID, nowUTC()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	s.lastError = ""
	s.lastPolled = utcNowISO()
}

func (s *Service) recordFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
	s.lastPolled = utcNowISO()
}

func deriveRequestScopedID(scope string, actorID string, requestKey string, label string) string {
	sum := sha256.Sum256([]byte(scope + "\n" + actorID + "\n" + requestKey + "\n" + label))
	return label + "-" + hex.EncodeToString(sum[:])[:20]
}
