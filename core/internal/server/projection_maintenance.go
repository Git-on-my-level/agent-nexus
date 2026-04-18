package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

const (
	defaultProjectionMaintenancePollInterval = 5 * time.Second
	defaultProjectionStaleScanInterval       = 30 * time.Second
	defaultProjectionMaintenanceBatchSize    = 50
	ProjectionModeBackground                 = "background"
	ProjectionModeManual                     = "manual"
)

type ProjectionMaintainerConfig struct {
	PrimitiveStore    PrimitiveStore
	Contract          *schema.Contract
	InboxRiskHorizon  time.Duration
	PollInterval      time.Duration
	StaleScanInterval time.Duration
	DirtyBatchSize    int
	SystemActorID     string
	Mode              string
}

type ProjectionMaintenanceErrorSnapshot struct {
	At        string `json:"at"`
	Message   string `json:"message"`
	Operation string `json:"operation"`
}

type ProjectionMaintenanceSnapshot struct {
	Mode                      string                              `json:"mode"`
	PendingDirtyCount         int                                 `json:"pending_dirty_count"`
	OldestDirtyAt             string                              `json:"oldest_dirty_at,omitempty"`
	OldestDirtyLagSeconds     int64                               `json:"oldest_dirty_lag_seconds,omitempty"`
	LastSuccessfulStaleScanAt string                              `json:"last_successful_stale_scan_at,omitempty"`
	LastError                 *ProjectionMaintenanceErrorSnapshot `json:"last_error,omitempty"`
}

type ProjectionMaintainer struct {
	opts              handlerOptions
	mode              string
	pollInterval      time.Duration
	staleScanInterval time.Duration
	dirtyBatchSize    int
	systemActorID     string
	notifyCh          chan struct{}

	stepMu  sync.Mutex
	stateMu sync.RWMutex
	state   projectionMaintenanceState
}

type projectionMaintenanceState struct {
	lastSuccessfulStaleScanAt time.Time
	lastStaleScanAttemptAt    time.Time
	lastError                 *ProjectionMaintenanceErrorSnapshot
}

func ParseProjectionMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ProjectionModeBackground:
		return ProjectionModeBackground, nil
	case ProjectionModeManual:
		return ProjectionModeManual, nil
	default:
		return "", fmt.Errorf("invalid projection mode %q (supported: %s, %s)", raw, ProjectionModeBackground, ProjectionModeManual)
	}
}

func NewProjectionMaintainer(config ProjectionMaintainerConfig) *ProjectionMaintainer {
	if config.PrimitiveStore == nil {
		return nil
	}
	mode, err := ParseProjectionMode(config.Mode)
	if err != nil {
		mode = ProjectionModeBackground
	}

	pollInterval := config.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultProjectionMaintenancePollInterval
	}
	staleScanInterval := config.StaleScanInterval
	if staleScanInterval <= 0 {
		staleScanInterval = defaultProjectionStaleScanInterval
	}
	dirtyBatchSize := config.DirtyBatchSize
	if dirtyBatchSize <= 0 {
		dirtyBatchSize = defaultProjectionMaintenanceBatchSize
	}

	return &ProjectionMaintainer{
		opts: handlerOptions{
			primitiveStore:   config.PrimitiveStore,
			contract:         config.Contract,
			inboxRiskHorizon: config.InboxRiskHorizon,
		},
		mode:              mode,
		pollInterval:      pollInterval,
		staleScanInterval: staleScanInterval,
		dirtyBatchSize:    dirtyBatchSize,
		systemActorID:     firstNonEmptyString(strings.TrimSpace(config.SystemActorID), "anx-core"),
		notifyCh:          make(chan struct{}, 1),
	}
}

func (m *ProjectionMaintainer) Run(ctx context.Context) {
	if m == nil || m.mode != ProjectionModeBackground {
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
		case <-m.notifyCh:
		case <-ticker.C:
		}
	}
}

func (m *ProjectionMaintainer) Notify() {
	if m == nil || m.notifyCh == nil {
		return
	}
	select {
	case m.notifyCh <- struct{}{}:
	default:
	}
}

func (m *ProjectionMaintainer) Step(ctx context.Context, now time.Time) error {
	if m == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	m.stepMu.Lock()
	defer m.stepMu.Unlock()

	var firstErr error
	ranStaleScan := false
	if m.shouldRunStaleScan(now) {
		ranStaleScan = true
		if err := m.runStaleScan(ctx, now); err != nil {
			firstErr = err
		}
	}
	processed, err := m.processDirtyQueue(ctx, now)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	if firstErr == nil && (ranStaleScan || processed > 0) {
		m.clearError()
	}
	return firstErr
}

func (m *ProjectionMaintainer) RunFullRebuild(ctx context.Context, now time.Time, actorID string) error {
	if m == nil || m.opts.primitiveStore == nil {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	m.stepMu.Lock()
	defer m.stepMu.Unlock()

	actorID = firstNonEmptyString(actorID, m.systemActorID)
	emittedThreadIDs, err := emitStaleThreadExceptions(ctx, m.opts, now, actorID)
	if err != nil {
		m.recordError("stale_scan", now, err)
		return fmt.Errorf("run stale scan: %w", err)
	}

	allThreadIDs, err := m.loadAllThreadIDs(ctx)
	if err != nil {
		m.recordError("list_threads", now, err)
		return fmt.Errorf("list threads for full rebuild: %w", err)
	}
	if err := markTopicProjectionsDirty(ctx, m.opts, now, append(allThreadIDs, emittedThreadIDs...)...); err != nil {
		m.recordError("mark_dirty", now, err)
		return fmt.Errorf("mark projections dirty for full rebuild: %w", err)
	}

	m.stateMu.Lock()
	m.state.lastStaleScanAttemptAt = now
	m.state.lastSuccessfulStaleScanAt = now
	m.state.lastError = nil
	m.stateMu.Unlock()

	for {
		processed, err := m.processDirtyQueue(ctx, now)
		if err != nil {
			return err
		}
		if processed == 0 {
			break
		}
	}

	m.clearError()
	return nil
}

func (m *ProjectionMaintainer) Snapshot(ctx context.Context, now time.Time) ProjectionMaintenanceSnapshot {
	if m == nil || m.opts.primitiveStore == nil {
		return ProjectionMaintenanceSnapshot{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	stats, err := m.opts.primitiveStore.GetDerivedTopicProjectionQueueStats(ctx)
	snapshot := ProjectionMaintenanceSnapshot{Mode: m.mode}
	if err == nil {
		snapshot.PendingDirtyCount = stats.PendingCount
		snapshot.OldestDirtyAt = strings.TrimSpace(stats.OldestDirtyAt)
		if oldestAt, parseErr := time.Parse(time.RFC3339Nano, snapshot.OldestDirtyAt); parseErr == nil && !oldestAt.IsZero() {
			lag := now.Sub(oldestAt)
			if lag > 0 {
				snapshot.OldestDirtyLagSeconds = int64(lag / time.Second)
			}
		}
	}

	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	snapshot.LastSuccessfulStaleScanAt = formatOptionalTime(m.state.lastSuccessfulStaleScanAt)
	if m.state.lastError != nil {
		copy := *m.state.lastError
		snapshot.LastError = &copy
	}
	return snapshot
}

func (m *ProjectionMaintainer) shouldRunStaleScan(now time.Time) bool {
	m.stateMu.RLock()
	lastAttempt := m.state.lastStaleScanAttemptAt
	m.stateMu.RUnlock()
	return lastAttempt.IsZero() || now.Sub(lastAttempt) >= m.staleScanInterval
}

func (m *ProjectionMaintainer) runStaleScan(ctx context.Context, now time.Time) error {
	m.stateMu.Lock()
	m.state.lastStaleScanAttemptAt = now
	m.stateMu.Unlock()

	threadIDs, err := emitStaleThreadExceptions(ctx, m.opts, now, m.systemActorID)
	if err != nil {
		m.recordError("stale_scan", now, err)
		return fmt.Errorf("run stale scan: %w", err)
	}
	if err := markTopicProjectionsDirty(ctx, m.opts, now, threadIDs...); err != nil {
		m.recordError("mark_dirty", now, err)
		return fmt.Errorf("mark stale threads dirty: %w", err)
	}

	m.stateMu.Lock()
	m.state.lastSuccessfulStaleScanAt = now
	m.state.lastError = nil
	m.stateMu.Unlock()
	return nil
}

func (m *ProjectionMaintainer) processDirtyQueue(ctx context.Context, now time.Time) (int, error) {
	entries, err := m.opts.primitiveStore.ListDerivedTopicProjectionDirtyEntries(ctx, m.dirtyBatchSize)
	if err != nil {
		m.recordError("load_dirty_queue", now, err)
		return 0, fmt.Errorf("load dirty projection queue: %w", err)
	}
	processed := 0
	for _, entry := range entries {
		startedAt := now
		if startedAt.IsZero() {
			startedAt = time.Now().UTC()
		}
		startedGeneration, err := m.opts.primitiveStore.MarkTopicProjectionRefreshStarted(ctx, entry.ThreadID, startedAt)
		if err != nil {
			m.recordError("start_dirty_projection", startedAt, err)
			return processed, fmt.Errorf("mark dirty projection %s started: %w", entry.ThreadID, err)
		}
		if err := m.opts.primitiveStore.ClearDerivedTopicProjectionDirty(ctx, entry.ThreadID); err != nil {
			m.recordError("clear_dirty_projection", startedAt, err)
			return processed, fmt.Errorf("clear dirty projection %s: %w", entry.ThreadID, err)
		}
		if startedGeneration == 0 {
			processed++
			continue
		}
		if err := refreshDerivedTopicProjection(ctx, m.opts, entry.ThreadID, startedAt, m.systemActorID); err != nil {
			failureMessage := fmt.Sprintf("refresh dirty projection %s: %v", entry.ThreadID, err)
			queuedAt, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(entry.DirtyAt))
			if parseErr != nil || queuedAt.IsZero() {
				queuedAt = startedAt
			}
			if queueErr := m.opts.primitiveStore.RequeueTopicProjectionRefresh(ctx, entry.ThreadID, queuedAt); queueErr != nil {
				m.recordError("requeue_failed_projection", startedAt, queueErr)
				return processed, fmt.Errorf("%s: %w", failureMessage, queueErr)
			}
			if markErr := m.opts.primitiveStore.MarkTopicProjectionRefreshFailed(ctx, entry.ThreadID, startedGeneration, startedAt, failureMessage); markErr != nil {
				m.recordError("mark_failed_projection", startedAt, markErr)
				return processed, fmt.Errorf("%s: %w", failureMessage, markErr)
			}
			m.recordError("refresh_dirty_projection", startedAt, err)
			return processed, fmt.Errorf("refresh dirty projection %s: %w", entry.ThreadID, err)
		}
		completedAt := time.Now().UTC()
		if err := m.opts.primitiveStore.MarkTopicProjectionRefreshSucceeded(ctx, entry.ThreadID, startedGeneration, completedAt); err != nil {
			m.recordError("mark_succeeded_projection", completedAt, err)
			return processed, fmt.Errorf("mark dirty projection %s succeeded: %w", entry.ThreadID, err)
		}
		processed++
	}
	return processed, nil
}

func (m *ProjectionMaintainer) loadAllThreadIDs(ctx context.Context) ([]string, error) {
	threads, _, err := m.opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
	if err != nil {
		return nil, err
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadID := strings.TrimSpace(anyString(thread["id"]))
		if threadID != "" {
			threadIDs = append(threadIDs, threadID)
		}
	}
	return uniqueServerStrings(threadIDs), nil
}

func (m *ProjectionMaintainer) recordError(operation string, now time.Time, err error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	message := strings.TrimSpace(strings.ReplaceAll(operation, "_", " "))
	if err != nil {
		detail := strings.TrimSpace(err.Error())
		if detail != "" {
			message = fmt.Sprintf("%s: %s", message, detail)
		}
	}
	m.state.lastError = &ProjectionMaintenanceErrorSnapshot{
		At:        now.UTC().Format(time.RFC3339Nano),
		Message:   message,
		Operation: strings.TrimSpace(operation),
	}
}

func (m *ProjectionMaintainer) clearError() {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.state.lastError = nil
}
