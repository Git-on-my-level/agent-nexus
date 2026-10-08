package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"agent-nexus-core/internal/primitives"
)

const minimumStreamPollInterval = 100 * time.Millisecond
const maxSharedInboxPages = 128

func streamPollInterval(opts handlerOptions) time.Duration {
	if opts.streamPollInterval < minimumStreamPollInterval {
		return minimumStreamPollInterval
	}
	return opts.streamPollInterval
}

// One observer per handler/workspace, retained only while streams are connected.
// Payload entries are immutable, bounded, and separated by full authorization
// scope, recipient, page and database revision. No client sees the global cursor.
type streamReadHub struct {
	mu      sync.Mutex
	refs    int
	clock   *primitives.StreamRevision
	version int64
	pages   map[inboxStreamPageKey]*sharedInboxPage
}
type inboxStreamPageKey struct {
	scope       primitives.AccessScope
	scoped      bool
	recipient   string
	category    int
	trigger, id string
}
type sharedInboxPage struct {
	done     chan struct{}
	loadedAt time.Time
	records  []inboxStreamRecord
	page     inboxReadPage
	err      error
}

func (h *streamReadHub) acquire(ctx context.Context, store PrimitiveStore) (func(), error) {
	if h == nil {
		return func() {}, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.refs == 0 {
		if source, ok := store.(interface {
			OpenStreamRevision(context.Context) (*primitives.StreamRevision, error)
		}); ok {
			var err error
			h.clock, err = source.OpenStreamRevision(ctx)
			if err != nil {
				return nil, err
			}
		}
		h.pages = make(map[inboxStreamPageKey]*sharedInboxPage)
	}
	h.refs++
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.refs--
		if h.refs == 0 {
			if h.clock != nil {
				_ = h.clock.Close()
			}
			h.clock = nil
			h.pages = nil
		}
	}, nil
}

func (h *streamReadHub) revision(ctx context.Context) (int64, bool, error) {
	if h == nil {
		return 0, false, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clock == nil {
		return 0, false, nil
	}
	v, err := h.clock.Current(ctx)
	if err != nil {
		return 0, true, err
	}
	if v != h.version {
		h.version = v
		h.pages = make(map[inboxStreamPageKey]*sharedInboxPage)
	}
	return v, true, nil
}

func (h *streamReadHub) inboxPage(r *http.Request, opts handlerOptions, filter primitives.DerivedInboxListFilter, revision int64, observed bool) ([]inboxStreamRecord, inboxReadPage, error) {
	load := func() ([]inboxStreamRecord, inboxReadPage, error) {
		items, page, err := loadInboxStreamPage(r, opts, filter)
		return buildInboxStreamRecords(items), page, err
	}
	if h == nil || !observed || opts.readVisibility != nil {
		return load()
	}
	scope, scoped := primitives.StreamReaderScope(r.Context())
	key := inboxStreamPageKey{scope: scope, scoped: scoped, category: filter.BeforeCategory, trigger: filter.BeforeTrigger, id: filter.BeforeID}
	if p, ok := cachedAuthenticatedPrincipal(r); ok {
		key.recipient = p.ActorID
	}
	h.mu.Lock()
	if h.version != revision {
		h.mu.Unlock()
		return load()
	}
	entry := h.pages[key]
	if entry != nil {
		select {
		case <-entry.done:
			// Caught-up streams never request this page while idle. A new
			// subscriber, however, needs current timer-derived ages/health.
			if time.Since(entry.loadedAt) >= streamPollInterval(opts) {
				delete(h.pages, key)
				entry = nil
			}
		default:
		}
	}
	if entry != nil {
		h.mu.Unlock()
		select {
		case <-r.Context().Done():
			return nil, inboxReadPage{}, r.Context().Err()
		case <-entry.done:
			if (errors.Is(entry.err, context.Canceled) || errors.Is(entry.err, context.DeadlineExceeded)) && r.Context().Err() == nil {
				return h.inboxPage(r, opts, filter, revision, observed)
			}
			return entry.records, entry.page, entry.err
		}
	}
	if len(h.pages) >= maxSharedInboxPages {
		h.mu.Unlock()
		return load()
	}
	entry = &sharedInboxPage{done: make(chan struct{})}
	h.pages[key] = entry
	h.mu.Unlock()
	entry.records, entry.page, entry.err = load()
	entry.loadedAt = time.Now()
	h.mu.Lock()
	// Canceled/failed loads must not poison the other readers in this scope.
	if entry.err != nil && h.pages[key] == entry {
		delete(h.pages, key)
	}
	close(entry.done)
	h.mu.Unlock()
	return entry.records, entry.page, entry.err
}
