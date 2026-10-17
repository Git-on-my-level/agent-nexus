package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const MaxPending = 100
const MaxLedger = 1000
const MaxAge = 30 * 24 * time.Hour
const MaxDrain = 4

var ErrIdentityChanged = errors.New("identity_changed")

// RejectedDelivery is a definitive card-create refusal, not an uncertain send.
// Keep the text for inspection but exclude it from automatic replay.
type RejectedDelivery struct{ Status int }

func (e RejectedDelivery) Error() string { return fmt.Sprintf("terminal_http_%d", e.Status) }

// DeferredDelivery retains a frozen request for replay after access or its
// target is repaired, with a short reason suitable for local status output.
type DeferredDelivery struct{ Reason string }

func (e DeferredDelivery) Error() string { return e.Reason }

type Delivery struct {
	PrincipalID string         `json:"principal_id,omitempty"`
	Key         string         `json:"key"`
	HostID      string         `json:"host_id,omitempty"`
	CommandID   string         `json:"command_id"`
	BaseURL     string         `json:"base_url"`
	Agent       string         `json:"agent"`
	Body        map[string]any `json:"body"`
}
type Pending struct {
	Event    Event     `json:"event"`
	Delivery *Delivery `json:"delivery,omitempty"`
	Reason   string    `json:"reason"`
}
type Receipt struct {
	Key    string    `json:"key"`
	TaskID string    `json:"task_id,omitempty"`
	At     time.Time `json:"at"`
}
type Queue struct {
	Pending        []Pending `json:"pending"`
	DeadLetters    []Pending `json:"dead_letters"`
	Ledger         []Receipt `json:"ledger"`
	Dropped        int       `json:"dropped"`
	LastDropReason string    `json:"last_drop_reason,omitempty"`
}

func QueuePath(dir, name, id string) string {
	return filepath.Join(dir, "integrations", name, id+".spool.json")
}
func LoadQueue(path string) (Queue, error) {
	q := Queue{Pending: []Pending{}, DeadLetters: []Pending{}, Ledger: []Receipt{}}
	b, err := Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return q, nil
	}
	if err != nil {
		return q, err
	}
	err = json.Unmarshal(b, &q)
	return q, err
}
func updateQueue(ctx context.Context, path string, fn func(*Queue) error) error {
	unlock, err := Lock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer unlock()
	q, err := LoadQueue(path)
	if err != nil {
		return err
	}
	if err = fn(&q); err != nil {
		return err
	}
	return AtomicWrite(path, JSON(q))
}
func (q *Queue) prune(now time.Time) {
	pruneAge := func(entries []Pending) []Pending {
		kept := entries[:0]
		for _, p := range entries {
			if now.Sub(p.Event.Timestamp) > MaxAge {
				q.Dropped++
				q.LastDropReason = "age"
			} else {
				kept = append(kept, p)
			}
		}
		return kept
	}
	q.Pending = pruneAge(q.Pending)
	q.DeadLetters = pruneAge(q.DeadLetters)
	for len(q.Pending)+len(q.DeadLetters) > MaxPending {
		if len(q.DeadLetters) > 0 && (len(q.Pending) == 0 || q.DeadLetters[0].Event.Timestamp.Before(q.Pending[0].Event.Timestamp)) {
			q.DeadLetters = q.DeadLetters[1:]
		} else {
			q.Pending = q.Pending[1:]
		}
		q.Dropped++
		q.LastDropReason = "capacity"
	}
	if len(q.Ledger) > MaxLedger {
		q.Ledger = q.Ledger[len(q.Ledger)-MaxLedger:]
	}
}
func Enqueue(ctx context.Context, path string, e Event, reason ...string) error {
	return updateQueue(ctx, path, func(q *Queue) error {
		q.prune(e.Timestamp)
		for i, p := range q.Pending {
			if p.Event.Key == e.Key {
				if len(reason) > 0 && reason[0] != "pending" {
					q.Pending[i].Reason = reason[0]
				}
				return nil
			}
		}
		for _, p := range q.DeadLetters {
			if p.Event.Key == e.Key {
				return nil
			}
		}
		for _, r := range q.Ledger {
			if r.Key == e.Key {
				return nil
			}
		}
		initial := "pending"
		if len(reason) > 0 {
			initial = reason[0]
		}
		q.Pending = append(q.Pending, Pending{Event: e, Reason: initial})
		q.prune(e.Timestamp)
		return nil
	})
}

// Drain serializes senders while queue writers only hold a short local lock.
// Delivery is saved before sending and never recomputed after an uncertain write.
func Drain(ctx context.Context, path string, prepare func(Event) (Delivery, error), send func(context.Context, Delivery) error, priority ...string) error {
	return drainSelected(ctx, path, prepare, send, nil, priority...)
}

// DrainSelected isolates a host job's admitted workspace/agent entries without
// rotating or rewriting entries belonging to another registered job.
func DrainSelected(ctx context.Context, path string, prepare func(Event) (Delivery, error), send func(context.Context, Delivery) error, admit func(Pending) bool) error {
	return drainSelected(ctx, path, prepare, send, admit)
}
func drainSelected(ctx context.Context, path string, prepare func(Event) (Delivery, error), send func(context.Context, Delivery) error, admit func(Pending) bool, priority ...string) error {
	lockCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	unlock, err := Lock(lockCtx, path+".drain.lock")
	if err != nil {
		return nil
	}
	defer unlock()
	if admit == nil {
		if err := updateQueue(ctx, path, func(q *Queue) error { q.prune(time.Now()); return nil }); err != nil {
			return err
		}
	}
	q, err := LoadQueue(path)
	if err != nil {
		return err
	}
	if len(priority) > 0 {
		for i, p := range q.Pending {
			if p.Event.Key == priority[0] {
				q.Pending = append([]Pending{p}, append(q.Pending[:i], q.Pending[i+1:]...)...)
				break
			}
		}
	}
	count := 0
	ledgerCount := len(q.Ledger)
	var last error
	for _, candidate := range q.Pending {
		if admit != nil && !admit(candidate) {
			continue
		}
		if count >= MaxDrain || ctx.Err() != nil {
			break
		}
		// Background draining cannot prune another namespace or evict its receipts.
		// Old unsent work stays inspectable beyond the conservative replay cutoff.
		if admit != nil && (ledgerCount >= MaxLedger || time.Since(candidate.Event.Timestamp) > 25*24*time.Hour) {
			reason := "replay_window_expired"
			if ledgerCount >= MaxLedger {
				reason = "receipt_capacity"
			}
			if err := updateQueue(ctx, path, func(current *Queue) error {
				for i := range current.Pending {
					if current.Pending[i].Event.Key == candidate.Event.Key {
						current.Pending[i].Reason = reason
					}
				}
				return nil
			}); err != nil {
				return err
			}
			last = DeferredDelivery{Reason: reason}
			continue
		}
		count++
		p := candidate
		err = updateQueue(ctx, path, func(current *Queue) error {
			found := false
			for _, entry := range current.Pending {
				if entry.Event.Key == p.Event.Key {
					p = entry
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("event_expired")
			}
			if p.Delivery == nil {
				d, prepErr := prepare(p.Event)
				if prepErr != nil {
					p.Reason = prepErr.Error()
					last = prepErr
				} else {
					p.Delivery = &d
					p.Reason = "pending"
				}
			}
			if p.Delivery != nil {
				p.Delivery.Key = p.Event.Key
			}
			for i := range current.Pending {
				if current.Pending[i].Event.Key == p.Event.Key {
					current.Pending[i] = p
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if p.Delivery == nil {
			if err := updateQueue(ctx, path, func(q *Queue) error {
				for i, entry := range q.Pending {
					if entry.Event.Key == p.Event.Key {
						q.Pending = append(append(q.Pending[:i], q.Pending[i+1:]...), entry)
						break
					}
				}
				return nil
			}); err != nil {
				return err
			}
			continue
		}
		sendErr := send(ctx, *p.Delivery)
		if sendErr == nil {
			ledgerCount++
		}
		err = updateQueue(ctx, path, func(current *Queue) error {
			for i := range current.Pending {
				if current.Pending[i].Event.Key != p.Event.Key {
					continue
				}
				if sendErr != nil {
					current.Pending[i].Reason = "delivery_failed"
					var deferred DeferredDelivery
					if errors.As(sendErr, &deferred) {
						current.Pending[i].Reason = deferred.Reason
					}
					if errors.Is(sendErr, ErrIdentityChanged) {
						current.Pending[i].Reason = "identity_changed"
					}
					last = sendErr
					failed := current.Pending[i]
					current.Pending = append(current.Pending[:i], current.Pending[i+1:]...)
					var rejected RejectedDelivery
					if errors.As(sendErr, &rejected) {
						failed.Reason = rejected.Error()
						current.DeadLetters = append(current.DeadLetters, failed)
					} else {
						current.Pending = append(current.Pending, failed)
					}
				} else {
					current.Pending = append(current.Pending[:i], current.Pending[i+1:]...)
					current.Ledger = append(current.Ledger, Receipt{Key: p.Event.Key, TaskID: p.Event.TaskID, At: time.Now().UTC()})
					if admit == nil {
						current.prune(time.Now())
					}
				}
				break
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return last
}

// BindHost durably fixes the authentication namespace before the first mutation.
// An offline event may have a prepared body before any host is enrolled.
func BindHost(ctx context.Context, path, key, hostID string, principal ...string) error {
	return updateQueue(ctx, path, func(q *Queue) error {
		for i := range q.Pending {
			p := &q.Pending[i]
			if p.Event.Key != key {
				continue
			}
			if p.Delivery == nil {
				return fmt.Errorf("delivery_not_prepared")
			}
			if p.Delivery.HostID != "" && p.Delivery.HostID != hostID {
				return ErrIdentityChanged
			}
			if len(principal) > 0 {
				if p.Delivery.PrincipalID != "" && p.Delivery.PrincipalID != principal[0] {
					return ErrIdentityChanged
				}
				p.Delivery.PrincipalID = principal[0]
			}
			p.Delivery.HostID = hostID
			return nil
		}
		return fmt.Errorf("delivery_not_pending")
	})
}
