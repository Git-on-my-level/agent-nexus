package scopemigrate

import (
	"context"
	"sync"
	"time"
)

// Worker is a shutdown handle, not a readiness dependency. StartWorker's zero
// configuration performs no DB work and starts no goroutine. A must explicitly
// authorize schema/capture/census coverage before enabling it. Close joins the
// worker before its DB is closed; it is safe to call concurrently or repeatedly.
type Worker struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	err    error
}

func StartWorker(ctx context.Context, enabled bool, runner *Runner, interval time.Duration, report func(string)) (*Worker, error) {
	w := &Worker{done: make(chan struct{})}
	if !enabled {
		close(w.done)
		return w, nil
	}
	if err := runner.validate(); err != nil {
		return nil, err
	}
	if interval < time.Millisecond {
		return nil, ErrBudget
	}
	// Freeze the runner configuration. Its Source/Rebuilder implementations must
	// also remain immutable; all durable writes belong to Runner's transaction.
	r := *runner
	ctx, w.cancel = context.WithCancel(ctx)
	go func() {
		defer close(w.done)
		w.err = r.Run(ctx, interval, report)
	}()
	return w, nil
}

func (w *Worker) Close() error {
	if w == nil {
		return nil
	}
	w.once.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
	})
	<-w.done
	return w.err
}
