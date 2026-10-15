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

type Delivery struct {
	CommandID string         `json:"command_id"`
	BaseURL   string         `json:"base_url"`
	Agent     string         `json:"agent"`
	Body      map[string]any `json:"body"`
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
	Ledger         []Receipt `json:"ledger"`
	Dropped        int       `json:"dropped"`
	LastDropReason string    `json:"last_drop_reason,omitempty"`
}

func QueuePath(dir, name, id string) string {
	return filepath.Join(dir, "integrations", name, id+".spool.json")
}
func LoadQueue(path string) (Queue, error) {
	q := Queue{Pending: []Pending{}, Ledger: []Receipt{}}
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
	kept := q.Pending[:0]
	for _, p := range q.Pending {
		if now.Sub(p.Event.Timestamp) > MaxAge {
			q.Dropped++
			q.LastDropReason = "age"
		} else {
			kept = append(kept, p)
		}
	}
	q.Pending = kept
	for len(q.Pending) > MaxPending {
		q.Pending = q.Pending[1:]
		q.Dropped++
		q.LastDropReason = "capacity"
	}
	if len(q.Ledger) > MaxLedger {
		q.Ledger = q.Ledger[len(q.Ledger)-MaxLedger:]
	}
}
func Enqueue(ctx context.Context, path string, e Event) error {
	return updateQueue(ctx, path, func(q *Queue) error {
		q.prune(e.Timestamp)
		for _, p := range q.Pending {
			if p.Event.Key == e.Key {
				return nil
			}
		}
		for _, r := range q.Ledger {
			if r.Key == e.Key {
				return nil
			}
		}
		q.Pending = append(q.Pending, Pending{Event: e, Reason: "pending"})
		q.prune(e.Timestamp)
		return nil
	})
}

// Drain serializes senders while queue writers only hold a short local lock.
// Delivery is saved before sending and never recomputed after an uncertain write.
func Drain(ctx context.Context, path string, prepare func(Event) (Delivery, error), send func(context.Context, Delivery) error, priority ...string) error {
	lockCtx, cancel := context.WithTimeout(ctx, time.Millisecond)
	defer cancel()
	unlock, err := Lock(lockCtx, path+".drain.lock")
	if err != nil {
		return nil
	}
	defer unlock()
	if err := updateQueue(ctx, path, func(q *Queue) error { q.prune(time.Now()); return nil }); err != nil {
		return err
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
	var last error
	for _, candidate := range q.Pending {
		if count >= MaxDrain || ctx.Err() != nil {
			break
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
		err = updateQueue(ctx, path, func(current *Queue) error {
			for i := range current.Pending {
				if current.Pending[i].Event.Key != p.Event.Key {
					continue
				}
				if sendErr != nil {
					current.Pending[i].Reason = "delivery_failed"
					last = sendErr
					failed := current.Pending[i]
					current.Pending = append(append(current.Pending[:i], current.Pending[i+1:]...), failed)
				} else {
					current.Pending = append(current.Pending[:i], current.Pending[i+1:]...)
					current.Ledger = append(current.Ledger, Receipt{Key: p.Event.Key, TaskID: p.Event.TaskID, At: time.Now().UTC()})
					current.prune(time.Now())
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
