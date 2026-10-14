package server

import (
	"context"
	"net/http"
	"runtime"
	"strings"
	"sync"
)

// Limit competing finite projections on small CPUs. An admitted answer prevents
// new read work until its handler finishes, without waiting for or cancelling a
// reader already using a WAL snapshot. SQLite still owns mutation serialization.
// Streams admit bounded ticks in their own readers and never hold this lane.
type readAdmission struct {
	mu                      sync.Mutex
	changed                 chan struct{}
	readers, answers, limit int
}

func newReadAdmission() *readAdmission {
	return &readAdmission{changed: make(chan struct{}), limit: max(1, min(4, runtime.GOMAXPROCS(0)/2))}
}
func (a *readAdmission) signal() { close(a.changed); a.changed = make(chan struct{}) }
func (a *readAdmission) answer() func() {
	a.mu.Lock()
	a.answers++
	a.signal()
	a.mu.Unlock()
	return func() { a.mu.Lock(); a.answers--; a.signal(); a.mu.Unlock() }
}
func (a *readAdmission) read(ctx context.Context) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.answers == 0 && a.readers < a.limit {
			a.readers++
			a.mu.Unlock()
			return func() { a.mu.Lock(); a.readers--; a.signal(); a.mu.Unlock() }, nil
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func hotProjectionRead(r *http.Request) bool {
	path := r.URL.Path
	if r.Method == http.MethodPost {
		return false
	}
	if r.Method != http.MethodGet {
		return false
	}
	switch path {
	case "/inbox", "/inbox/summary", "/home/unread", "/events", "/work", "/pm/actions", "/pm/decisions":
		return true
	}
	return strings.HasPrefix(path, "/topics/") && strings.HasSuffix(path, "/workspace")
}
