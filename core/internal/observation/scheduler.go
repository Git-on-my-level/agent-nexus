package observation

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

type RefreshPolicy struct {
	Interval   time.Duration `json:"interval"`
	StaleAfter time.Duration `json:"stale_after"`
	Timeout    time.Duration `json:"timeout"`
	MaxBackoff time.Duration `json:"max_backoff"`
}

func (p RefreshPolicy) Validate() error {
	if p.Interval < time.Second || p.Interval > 7*24*time.Hour || p.StaleAfter < p.Interval || p.StaleAfter > 30*24*time.Hour || p.Timeout <= 0 || p.Timeout > 5*time.Minute || p.MaxBackoff < p.Interval || p.MaxBackoff > 7*24*time.Hour {
		return failure(ErrConfiguration, "invalid refresh interval, freshness, timeout or backoff bounds")
	}
	return nil
}

type RefreshHealth struct {
	Target      Target     `json:"target"`
	State       string     `json:"state"`
	LastAttempt time.Time  `json:"last_attempt_at"`
	LastSuccess time.Time  `json:"last_success_at"`
	NextDue     time.Time  `json:"next_due_at"`
	LastError   *ReadError `json:"last_error,omitempty"`
	Failures    int        `json:"failures"`
	LastGood    *Report    `json:"last_good,omitempty"`
}

func (h RefreshHealth) Freshness(now time.Time, staleAfter time.Duration) string {
	if h.LastError != nil {
		return "error"
	}
	if h.LastGood == nil {
		return "unknown"
	}
	if !now.Before(h.LastGood.ObservedAt.Add(staleAfter)) {
		return "stale"
	}
	return "fresh"
}

type RefreshResult struct {
	Health    RefreshHealth `json:"health"`
	Skipped   bool          `json:"skipped"`
	Coalesced bool          `json:"coalesced"`
}
type flight struct {
	done   chan struct{}
	result RefreshResult
	err    error
}

// Scheduler owns bounded execution and coalescing, not durable truth. Persist
// Snapshot after each transition, or have the canonical store implement its own
// queue claim/finish around Refresh. Restore recovers interrupted reads as due.
type Scheduler struct {
	mu      sync.Mutex
	states  map[string]RefreshHealth
	flights map[string]*flight
	slots   chan struct{}
	now     func() time.Time
}

func NewScheduler(maxConcurrent int) (*Scheduler, error) {
	if maxConcurrent < 1 || maxConcurrent > 32 {
		return nil, failure(ErrConfiguration, "refresh concurrency must be 1..32")
	}
	return &Scheduler{states: map[string]RefreshHealth{}, flights: map[string]*flight{}, slots: make(chan struct{}, maxConcurrent), now: time.Now}, nil
}
func (s *Scheduler) Refresh(ctx context.Context, key string, p RefreshPolicy, reader Reader, target Target, manual bool) (RefreshResult, error) {
	if err := p.Validate(); err != nil {
		return RefreshResult{}, err
	}
	if err := target.Validate(); err != nil {
		return RefreshResult{}, err
	}
	if key == "" || reader == nil {
		return RefreshResult{}, failure(ErrConfiguration, "refresh key and reader required")
	}
	s.mu.Lock()
	if h, ok := s.states[key]; ok && h.Target != target {
		s.mu.Unlock()
		return RefreshResult{}, failure(ErrPermission, "refresh key is already bound to another target")
	}
	if f, ok := s.flights[key]; ok {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return RefreshResult{}, ctx.Err()
		case <-f.done:
			out := cloneResult(f.result)
			out.Coalesced = true
			return out, f.err
		}
	}
	now := s.now().UTC()
	h := s.states[key]
	h.Target = target
	// Manual requests bypass a successful interval, never a failure/rate-limit embargo.
	if now.Before(h.NextDue) && (!manual || h.Failures > 0) {
		s.mu.Unlock()
		return RefreshResult{Health: cloneHealth(h), Skipped: true}, nil
	}
	f := &flight{done: make(chan struct{})}
	s.flights[key] = f
	h.State = "running"
	h.LastAttempt = now
	s.states[key] = h
	s.mu.Unlock()
	runCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	var report Report
	var err error
	select {
	case s.slots <- struct{}{}:
		report, err = readSafely(runCtx, reader, target)
		<-s.slots
	case <-runCtx.Done():
		err = failure(ErrLimit, "refresh concurrency wait exceeded deadline")
	}
	if err == nil && runCtx.Err() != nil {
		err = failure(ErrLimit, "reader exceeded refresh deadline")
	}
	if err == nil && report.Target != target {
		err = failure(ErrPolicy, "reader returned a different target")
	}
	if err == nil {
		report, err = finishReport(report)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h = s.states[key]
	now = s.now().UTC()
	if err != nil {
		re := &ReadError{Kind: ErrUnavailable, Message: "reader failed"}
		var typed *ReadError
		if errors.As(err, &typed) {
			v := *typed
			re = &v
		}
		h.State = "failed"
		h.Failures++
		if h.Failures > 30 {
			h.Failures = 30
		}
		h.LastError = re
		delay := p.Interval
		for i := 1; i < h.Failures && delay < p.MaxBackoff; i++ {
			delay *= 2
		}
		if delay > p.MaxBackoff {
			delay = p.MaxBackoff
		}
		if re.RetryAfter > delay {
			delay = re.RetryAfter
		}
		h.NextDue = now.Add(delay + jitter(delay/10))
		err = re
	} else {
		h.State = "succeeded"
		h.Failures = 0
		h.LastError = nil
		h.LastSuccess = now
		h.LastGood = &report
		h.NextDue = now.Add(p.Interval + jitter(p.Interval/10))
	}
	s.states[key] = h
	f.result = RefreshResult{Health: cloneHealth(h)}
	f.err = err
	delete(s.flights, key)
	close(f.done)
	return cloneResult(f.result), err
}
func readSafely(ctx context.Context, reader Reader, t Target) (r Report, err error) {
	defer func() {
		if recover() != nil {
			r = Report{}
			err = failure(ErrUnavailable, "reader panicked")
		}
	}()
	return reader.Read(ctx, t)
}
func jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return time.Duration(binary.LittleEndian.Uint64(b[:]) % uint64(max+1))
}
func cloneHealth(h RefreshHealth) RefreshHealth {
	b, _ := json.Marshal(h)
	var out RefreshHealth
	_ = json.Unmarshal(b, &out)
	return out
}
func cloneResult(r RefreshResult) RefreshResult { r.Health = cloneHealth(r.Health); return r }
func (s *Scheduler) Snapshot() map[string]RefreshHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]RefreshHealth{}
	for k, v := range s.states {
		out[k] = cloneHealth(v)
	}
	return out
}
func (s *Scheduler) Restore(states map[string]RefreshHealth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.flights) > 0 {
		return failure(ErrConfiguration, "cannot restore while refreshes run")
	}
	if len(states) > 10000 {
		return failure(ErrLimit, "scheduler snapshot too large")
	}
	out := map[string]RefreshHealth{}
	for k, h := range states {
		if k == "" || h.Target.Validate() != nil || h.Failures < 0 || h.Failures > 30 {
			return failure(ErrInvalidOutput, "invalid refresh snapshot")
		}
		if h.LastGood != nil && (h.LastGood.Validate() != nil || h.LastGood.Target != h.Target) {
			return failure(ErrInvalidOutput, "invalid last good observation")
		}
		if h.State == "running" || h.State == "queued" {
			h.State = "failed"
			h.NextDue = s.now().UTC()
			h.LastError = &ReadError{Kind: ErrUnavailable, Message: "refresh interrupted by restart"}
		}
		out[k] = cloneHealth(h)
	}
	s.states = out
	return nil
}

// Observation maps to the canonical core Observation envelope. The server sets
// actor, received_at and verification; source claims cannot promote themselves.
func (r Report) Observation(staleAfter time.Duration) map[string]any {
	facts := map[string]any{}
	for k, v := range r.Facts {
		facts[k] = v
	}
	if r.Title != "" {
		facts["title"] = r.Title
	}
	if r.NativeStatus != "" {
		facts["native_status"] = r.NativeStatus
	}
	evidence := make([]map[string]any, 0, len(r.Evidence))
	for _, e := range r.Evidence {
		evidence = append(evidence, map[string]any{"url": e.Reference, "kind": e.Kind, "revision": e.Revision, "summary": e.Summary, "knowledge": e.Knowledge})
	}
	out := map[string]any{"idempotency_key": r.IdempotencyKey, "reader_id": r.ReaderID, "reader_revision": r.ReaderRevision, "observed_at": r.ObservedAt.Format(time.RFC3339Nano), "source_revision": r.SourceRevision, "status": r.Knowledge, "facts": facts, "evidence": evidence, "coverage": r.Coverage, "uncertainty": r.Coverage.Limitations, "stale_after_seconds": int64(staleAfter / time.Second)}
	if r.SourceSequence != nil {
		out["source_sequence"] = *r.SourceSequence
	}
	if r.SourceActivityAt != nil {
		out["source_activity_at"] = r.SourceActivityAt.Format(time.RFC3339Nano)
	}
	return out
}
