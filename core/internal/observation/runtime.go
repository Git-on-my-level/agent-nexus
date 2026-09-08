package observation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RuntimeCallbacks bind the package to the canonical workspace store, avoiding
// a second work database. Core supplies authenticated, service-scoped callbacks.
// Request/Claim/Finish MUST be durable and lease-fenced across core processes.
type RuntimeCallbacks struct {
	GetWork func(context.Context, string) (map[string]any, error)
	Request func(context.Context, string, string) (map[string]any, error)
	Claim   func(context.Context, string, string, time.Duration) (map[string]any, error)
	Submit  func(context.Context, string, string, map[string]any) (map[string]any, error)
	Finish  func(context.Context, string, string, map[string]any) (map[string]any, error)
}

// Registration is approved operator configuration. Neither the public refresh
// body nor source report can choose a reader, credential, executable or path.
type Registration struct {
	CardRef string
	Target  Target
	Reader  Reader
	Policy  RefreshPolicy
}
type Runtime struct {
	callbacks     RuntimeCallbacks
	registrations []Registration
	actorID       string
	workerID      string
	concurrency   int
	tickMu        sync.Mutex
}
type RuntimeResult struct {
	CardRef string `json:"card_ref"`
	Skipped bool   `json:"skipped"`
	Error   error  `json:"-"`
}

func NewRuntime(c RuntimeCallbacks, registrations []Registration, actorID, workerID string, concurrency int) (*Runtime, error) {
	if c.GetWork == nil || c.Request == nil || c.Claim == nil || c.Submit == nil || c.Finish == nil || actorID == "" || workerID == "" || concurrency < 1 || concurrency > 32 || len(registrations) == 0 || len(registrations) > 1000 {
		return nil, failure(ErrConfiguration, "runtime needs canonical store callbacks, service identity and 1..1000 approved targets")
	}
	seen := map[string]bool{}
	for _, entry := range registrations {
		if entry.CardRef == "" || entry.Target.Validate() != nil || entry.Reader == nil || entry.Policy.Validate() != nil || seen[entry.CardRef] {
			return nil, failure(ErrConfiguration, "invalid or duplicate runtime registration")
		}
		seen[entry.CardRef] = true
	}
	return &Runtime{callbacks: c, registrations: append([]Registration(nil), registrations...), actorID: actorID, workerID: workerID, concurrency: concurrency}, nil
}
func (t Target) CanonicalNativeID() string {
	if t.Source == "github" {
		return t.Repository + "#" + t.NativeID
	}
	return t.NativeID
}
func (t Target) Authority() string {
	if t.Source == "ssh_git" {
		return "git"
	}
	return t.Source
}

// Tick consolidates simultaneous polling calls and uses a bounded worker pool.
// It is safe to run in more than one core process only with durable Claim/Finish.
func (r *Runtime) Tick(ctx context.Context) []RuntimeResult {
	if !r.tickMu.TryLock() {
		return []RuntimeResult{{Skipped: true, Error: failure(ErrLimit, "runtime tick already running")}}
	}
	defer r.tickMu.Unlock()
	results := make([]RuntimeResult, len(r.registrations))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < r.concurrency; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				results[i] = r.collect(ctx, r.registrations[i])
			}
		}()
	}
	for i := range r.registrations {
		select {
		case jobs <- i:
		case <-ctx.Done():
			results[i] = RuntimeResult{CardRef: r.registrations[i].CardRef, Skipped: true, Error: ctx.Err()}
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

// Run polls manual/scheduled work. It does not launch source mutations or agents.
// Caller owns the goroutine and shutdown context; each Tick completes before next.
func (r *Runtime) Run(ctx context.Context, pollInterval time.Duration, onResults func([]RuntimeResult)) error {
	if pollInterval < time.Second || pollInterval > time.Minute {
		return failure(ErrConfiguration, "runtime poll interval must be 1s..1m")
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		results := r.Tick(ctx)
		if onResults != nil {
			onResults(results)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (r *Runtime) collect(ctx context.Context, entry Registration) RuntimeResult {
	out := RuntimeResult{CardRef: entry.CardRef}
	ctx, cancel := context.WithTimeout(ctx, entry.Policy.Timeout+10*time.Second)
	defer cancel()
	work, err := r.callbacks.GetWork(ctx, entry.CardRef)
	if err != nil {
		out.Error = err
		return out
	}
	source, _ := work["source"].(map[string]any)
	if str(source, "authority") != entry.Target.Authority() || str(source, "connection_id") != entry.Target.ConnectionID || str(source, "native_id") != entry.Target.CanonicalNativeID() {
		out.Error = failure(ErrPermission, "work source differs from approved collector registration")
		return out
	}
	refresh, _ := work["refresh"].(map[string]any)
	state := str(refresh, "state")
	now := time.Now().UTC()
	next := stamp(str(refresh, "next_due_at"))
	// Failure backoff remains binding even when a manual request was queued.
	failures := numberInt(refresh["failures"])
	if next != nil && now.Before(*next) && (state != "queued" || failures > 0) {
		out.Skipped = true
		return out
	}
	if state != "queued" && state != "running" {
		if _, err = r.callbacks.Request(ctx, r.actorID, entry.CardRef); err != nil {
			out.Error = err
			return out
		}
	}
	claim, err := r.callbacks.Claim(ctx, entry.CardRef, r.workerID, entry.Policy.Timeout+10*time.Second)
	if err != nil {
		out.Error = err
		return out
	}
	lease := str(claim, "lease_token")
	if lease == "" {
		out.Error = failure(ErrPolicy, "canonical store did not return a refresh lease")
		return out
	}
	readCtx, readCancel := context.WithTimeout(ctx, entry.Policy.Timeout)
	report, readErr := readSafely(readCtx, entry.Reader, entry.Target)
	if readErr == nil && readCtx.Err() != nil {
		readErr = failure(ErrLimit, "reader exceeded refresh deadline")
	}
	readCancel()
	if readErr == nil && (report.Target != entry.Target || report.Validate() != nil) {
		readErr = failure(ErrPolicy, "reader returned an invalid source binding")
	}
	if readErr == nil {
		report, readErr = finishReport(report)
	}
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	finish := map[string]any{"state": "succeeded", "failures": 0, "next_due_at": time.Now().UTC().Add(entry.Policy.Interval + jitter(entry.Policy.Interval/10)).Format(time.RFC3339Nano)}
	if readErr == nil {
		_, readErr = r.callbacks.Submit(finishCtx, r.actorID, entry.CardRef, report.Observation(entry.Policy.StaleAfter))
	}
	if readErr != nil {
		typed := &ReadError{Kind: ErrUnavailable, Message: "collector read or persistence failed"}
		var re *ReadError
		if errors.As(readErr, &re) {
			copy := *re
			typed = &copy
		}
		failures++
		if failures > 30 {
			failures = 30
		}
		delay := entry.Policy.Interval
		for n := 1; n < failures && delay < entry.Policy.MaxBackoff; n++ {
			delay *= 2
		}
		if delay > entry.Policy.MaxBackoff {
			delay = entry.Policy.MaxBackoff
		}
		if typed.RetryAfter > delay {
			delay = typed.RetryAfter
		}
		finish = map[string]any{"state": "failed", "failures": failures, "last_error": map[string]any{"code": string(typed.Kind), "message": typed.Message}, "next_due_at": time.Now().UTC().Add(delay + jitter(delay/10)).Format(time.RFC3339Nano)}
		attempted := time.Now().UTC()
		errorReport := map[string]any{"idempotency_key": digest([]byte(entry.CardRef + lease)), "reader_id": "collector:" + entry.Target.ConnectionID, "reader_revision": ReaderRevision, "observed_at": attempted.Format(time.RFC3339Nano), "status": "error", "facts": map[string]any{}, "evidence": []any{}, "error": map[string]any{"code": string(typed.Kind), "message": typed.Message}, "stale_after_seconds": int64(entry.Policy.StaleAfter / time.Second)}
		_, _ = r.callbacks.Submit(finishCtx, r.actorID, entry.CardRef, errorReport)
		out.Error = typed
	}
	if _, err = r.callbacks.Finish(finishCtx, entry.CardRef, lease, finish); err != nil {
		out.Error = fmt.Errorf("finish canonical refresh lease: %w", err)
	}
	return out
}
func numberInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
