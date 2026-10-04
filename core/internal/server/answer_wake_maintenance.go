package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
)

const (
	DefaultAnswerWakeQuietWindow  = 60 * time.Second
	defaultAnswerWakePollInterval = time.Second
)

type answerWakeBatchStore interface {
	ListHumanAttentionAnswerWakeBatches(context.Context) ([]primitives.HumanAttentionAnswerWakeBatch, error)
	CountOpenHumanAttentionAsks(context.Context, string) (int, error)
	DeleteHumanAttentionAnswerWakeBatch(context.Context, string, string, string) (bool, error)
	CreateArtifact(context.Context, string, map[string]any, any, string) (map[string]any, error)
	UpsertAgentWakeup(context.Context, primitives.AgentWakeup) (primitives.AgentWakeup, error)
}

type AnswerWakeMaintainerConfig struct {
	PrimitiveStore      PrimitiveStore
	WorkspaceID         string
	WorkspaceName       string
	QuietWindow         time.Duration
	PollInterval        time.Duration
	FlushWhenNoOpenAsks bool
}

type AnswerWakeMaintainer struct {
	store               answerWakeBatchStore
	workspaceID         string
	workspaceName       string
	quietWindow         time.Duration
	pollInterval        time.Duration
	flushWhenNoOpenAsks bool
}

func NewAnswerWakeMaintainer(config AnswerWakeMaintainerConfig) *AnswerWakeMaintainer {
	store, ok := config.PrimitiveStore.(answerWakeBatchStore)
	if !ok {
		return nil
	}
	if config.QuietWindow <= 0 {
		config.QuietWindow = DefaultAnswerWakeQuietWindow
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultAnswerWakePollInterval
	}
	if strings.TrimSpace(config.WorkspaceID) == "" {
		config.WorkspaceID = "ws_main"
	}
	return &AnswerWakeMaintainer{
		store:               store,
		workspaceID:         strings.TrimSpace(config.WorkspaceID),
		workspaceName:       strings.TrimSpace(config.WorkspaceName),
		quietWindow:         config.QuietWindow,
		pollInterval:        config.PollInterval,
		flushWhenNoOpenAsks: config.FlushWhenNoOpenAsks,
	}
}

func (m *AnswerWakeMaintainer) Run(ctx context.Context) {
	if m == nil || m.store == nil {
		return
	}
	ticker := time.NewTicker(m.pollInterval)
	defer ticker.Stop()
	for {
		if err := m.Step(ctx, time.Now().UTC()); err != nil && ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *AnswerWakeMaintainer) Step(ctx context.Context, now time.Time) error {
	if m == nil || m.store == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	batches, err := m.store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		openAsks, err := m.store.CountOpenHumanAttentionAsks(ctx, batch.TargetActorID)
		if err != nil {
			return err
		}
		lastAnswered, err := time.Parse(time.RFC3339Nano, batch.LastAnsweredAt)
		if err != nil {
			return fmt.Errorf("parse answer wake batch timestamp: %w", err)
		}
		if !(m.flushWhenNoOpenAsks && openAsks == 0) && now.Sub(lastAnswered) < m.quietWindow {
			continue
		}
		if err := m.dispatch(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

func (m *AnswerWakeMaintainer) FlushTarget(ctx context.Context, targetActorID string) error {
	if m == nil || m.store == nil {
		return nil
	}
	batches, err := m.store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil {
		return err
	}
	for _, batch := range batches {
		if batch.TargetActorID == strings.TrimSpace(targetActorID) {
			return m.dispatch(ctx, batch)
		}
	}
	return nil
}

func (m *AnswerWakeMaintainer) dispatch(ctx context.Context, batch primitives.HumanAttentionAnswerWakeBatch) error {
	// A new answer may have arrived after Step took its snapshot. In that case
	// keep the newer window intact and let its quiet timer run again.
	current, err := m.store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range current {
		if candidate.TargetActorID == batch.TargetActorID && candidate.TriggerEventID != batch.TriggerEventID {
			return nil
		}
	}

	wakeupID := router.WakeupArtifactID(m.workspaceID, batch.ThreadID, batch.TriggerEventID, batch.TargetActorID)
	refs := append([]string(nil), batch.Refs...)
	refs = append(refs, "artifact:"+wakeupID)
	triggerText := fmt.Sprintf("%d answers to your asks are ready. Run `anx await --answers`.", batch.AnswerCount)
	artifact := map[string]any{
		"id":              wakeupID,
		"kind":            router.WakeArtifactKind,
		"summary":         "Answer batch for @" + batch.TargetHandle,
		"refs":            batch.Refs,
		"target_handle":   batch.TargetHandle,
		"target_actor_id": batch.TargetActorID,
		"workspace_id":    m.workspaceID,
		"thread_id":       batch.ThreadID,
	}
	content := map[string]any{
		"version":            router.WakePacketVersion,
		"wakeup_id":          wakeupID,
		"target_handle":      batch.TargetHandle,
		"target_actor_id":    batch.TargetActorID,
		"workspace_id":       m.workspaceID,
		"thread_id":          batch.ThreadID,
		"trigger_event_id":   batch.TriggerEventID,
		"trigger_created_at": batch.TriggerCreatedAt,
		"trigger_text":       triggerText,
		"answer_event_refs":  eventRefs(batch.AnswerEventIDs),
		"ask_event_refs":     eventRefs(batch.AskEventIDs),
	}
	if _, artifactErr := m.store.CreateArtifact(ctx, actors.SystemActorID, artifact, content, "structured"); artifactErr != nil && !strings.Contains(strings.ToLower(artifactErr.Error()), "conflict") {
		// The wakeup row is the durable signal; its refs and trigger text are
		// enough for clients when artifact storage is unavailable.
	}
	_, err = m.store.UpsertAgentWakeup(ctx, primitives.AgentWakeup{
		WakeupID:         wakeupID,
		Status:           primitives.AgentWakeupStatusRequested,
		TargetHandle:     batch.TargetHandle,
		TargetActorID:    batch.TargetActorID,
		WorkspaceID:      m.workspaceID,
		WorkspaceName:    m.workspaceName,
		ThreadID:         batch.ThreadID,
		TriggerEventID:   batch.TriggerEventID,
		TriggerCreatedAt: batch.TriggerCreatedAt,
		TriggerText:      triggerText,
		Refs:             refs,
	})
	if err != nil {
		return fmt.Errorf("queue answer batch wake: %w", err)
	}
	_, err = m.store.DeleteHumanAttentionAnswerWakeBatch(ctx, batch.TargetActorID, batch.BatchID, batch.TriggerEventID)
	if err != nil {
		return err
	}
	return nil
}

func eventRefs(ids []string) []string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			refs = append(refs, "event:"+id)
		}
	}
	return refs
}
