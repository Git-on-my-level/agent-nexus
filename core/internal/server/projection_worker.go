package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"organization-autorunner-core/internal/primitives"
)

const defaultProjectionWorkerPollInterval = 30 * time.Second

type ProjectionWorker struct {
	opts handlerOptions
	now  func() time.Time
}

func NewProjectionWorker(options ...HandlerOption) *ProjectionWorker {
	opts := handlerOptions{
		inboxRiskHorizon: defaultInboxRiskHorizon,
	}
	for _, option := range options {
		option(&opts)
	}
	if opts.inboxRiskHorizon <= 0 {
		opts.inboxRiskHorizon = defaultInboxRiskHorizon
	}
	return &ProjectionWorker{
		opts: opts,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (w *ProjectionWorker) RunUntilIdle(ctx context.Context) error {
	return w.runUntilIdle(ctx, "oar-core", true)
}

func (w *ProjectionWorker) RunFullRebuild(ctx context.Context, actorID string) error {
	if w == nil || w.opts.primitiveStore == nil {
		return nil
	}

	now := w.now().UTC()
	emittedThreadIDs, err := emitStaleThreadExceptions(ctx, w.opts, now, actorID)
	if err != nil {
		return err
	}
	if len(emittedThreadIDs) > 0 {
		if err := w.opts.primitiveStore.MarkThreadProjectionsDirty(ctx, emittedThreadIDs, now); err != nil {
			return err
		}
	}

	threads, _, err := w.opts.primitiveStore.ListThreads(ctx, primitives.ThreadListFilter{})
	if err != nil {
		return err
	}
	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadID := anyString(thread["id"])
		if threadID != "" {
			threadIDs = append(threadIDs, threadID)
		}
	}
	if err := w.opts.primitiveStore.MarkThreadProjectionsDirty(ctx, uniqueServerStrings(threadIDs), now); err != nil {
		return err
	}

	return w.runUntilIdle(ctx, actorID, false)
}

func (w *ProjectionWorker) runUntilIdle(ctx context.Context, actorID string, scanExpired bool) error {
	if w == nil || w.opts.primitiveStore == nil {
		return nil
	}
	now := w.now().UTC()
	if scanExpired {
		if err := markExpiredThreadProjectionsDirty(ctx, w.opts, now); err != nil {
			return err
		}
	}

	var firstErr error
	for {
		startedAt := w.now().UTC()
		job, ok, err := w.opts.primitiveStore.ClaimNextDirtyThreadProjection(ctx, startedAt)
		if err != nil {
			return err
		}
		if !ok {
			return firstErr
		}

		if err := refreshDerivedThreadProjection(ctx, w.opts, job.ThreadID, startedAt, actorID); err != nil {
			_ = w.opts.primitiveStore.MarkThreadProjectionRefreshFailed(ctx, job.ThreadID, w.now().UTC(), err.Error())
			if firstErr == nil {
				firstErr = fmt.Errorf("refresh derived projection for thread %s: %w", job.ThreadID, err)
			}
			continue
		}
		if err := w.opts.primitiveStore.MarkThreadProjectionRefreshSucceeded(ctx, job.ThreadID, w.now().UTC()); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("mark thread %s projection refresh succeeded: %w", job.ThreadID, err)
			}
		}
	}
}

type syncProjectionMaintenance struct {
	worker *ProjectionWorker
}

func NewSyncProjectionMaintenance(worker *ProjectionWorker) ProjectionMaintenance {
	return &syncProjectionMaintenance{worker: worker}
}

func (m *syncProjectionMaintenance) Start() {}

func (m *syncProjectionMaintenance) Notify(ctx context.Context) error {
	if m == nil || m.worker == nil {
		return nil
	}
	return m.worker.RunUntilIdle(ctx)
}

func (m *syncProjectionMaintenance) Stop(context.Context) error {
	return nil
}

type backgroundProjectionMaintenance struct {
	worker    *ProjectionWorker
	interval  time.Duration
	wakeCh    chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewBackgroundProjectionMaintenance(worker *ProjectionWorker, interval time.Duration) ProjectionMaintenance {
	if interval <= 0 {
		interval = defaultProjectionWorkerPollInterval
	}
	return &backgroundProjectionMaintenance{
		worker:   worker,
		interval: interval,
		wakeCh:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

func (m *backgroundProjectionMaintenance) Start() {
	if m == nil || m.worker == nil {
		return
	}
	m.startOnce.Do(func() {
		go m.loop()
	})
}

func (m *backgroundProjectionMaintenance) Notify(context.Context) error {
	if m == nil || m.worker == nil {
		return nil
	}
	select {
	case m.wakeCh <- struct{}{}:
	default:
	}
	return nil
}

func (m *backgroundProjectionMaintenance) Stop(ctx context.Context) error {
	if m == nil || m.worker == nil {
		return nil
	}
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
	select {
	case <-m.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *backgroundProjectionMaintenance) loop() {
	defer close(m.doneCh)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		_ = m.worker.RunUntilIdle(context.Background())
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
		case <-m.wakeCh:
		}
	}
}
