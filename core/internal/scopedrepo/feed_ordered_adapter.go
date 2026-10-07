package scopedrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"sync"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopes"
)

type OrderedReadModelReader interface {
	readmodel.OrderedReader
	readmodel.CounterReader
}

// AdaptOrderedReadModel preserves the kernel's per-stream interface while
// issuing only ONE global P+1 candidate query. The directory is trusted copied
// dispatcher input, not a completeness certificate. No handler selects this.
func AdaptOrderedReadModel(reader OrderedBatchFeedReader, directory scopes.DirectoryPage, cursor string) OrderedReadModelReader {
	if directory.Bindings != nil {
		directory.Bindings = append([]scopes.Binding{}, directory.Bindings...)
	}
	return &orderedReadModelAdapter{reader: reader, directory: directory, directoryCursor: cursor}
}

type orderedReadModelAdapter struct {
	mu              sync.Mutex
	reader          OrderedBatchFeedReader
	directory       scopes.DirectoryPage
	directoryCursor string
	frozen          *readmodel.Snapshot
	failure         error
	queried         []bool
	loaded          bool
	limit           int
	after           *OrderedFeedKey
	rows            [][]OrderedFeedCandidate
}

func (a *orderedReadModelAdapter) fail(err error) error {
	if a.failure == nil {
		a.failure = err
	}
	if r, ok := a.reader.(interface{ orderedState(error) error }); ok {
		return r.orderedState(a.failure)
	}
	return a.failure
}
func (a *orderedReadModelAdapter) state(ctx context.Context, data bool) error {
	// This private method prevents the cached branches from outliving the real
	// transaction or hiding an ignored repository error without another query.
	// Expiry takes precedence over a failure retained from the callback.
	if r, ok := a.reader.(interface{ orderedState(error) error }); ok {
		if err := r.orderedState(nil); err != nil {
			return a.fail(err)
		}
	} else {
		return a.fail(readmodel.ErrProjection)
	}
	if a.failure != nil {
		return a.failure
	}
	if err := ctx.Err(); err != nil {
		return a.fail(err)
	}
	if data && a.frozen == nil {
		return a.fail(readmodel.ErrProjection)
	}
	return nil
}
func cloneOrderedSnapshot(s readmodel.Snapshot) readmodel.Snapshot {
	s.Scopes = append([]readmodel.Scope(nil), s.Scopes...)
	s.Streams = append([]readmodel.Stream(nil), s.Streams...)
	return s
}
func (a *orderedReadModelAdapter) Snapshot(ctx context.Context) (readmodel.Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.state(ctx, false); err != nil {
		return readmodel.Snapshot{}, err
	}
	raw, err := a.reader.Snapshot()
	if err != nil {
		return readmodel.Snapshot{}, a.fail(err)
	}
	s := readmodel.Snapshot{Binding: raw.Binding, Streams: append([]readmodel.Stream(nil), raw.Streams...), MoreScopes: raw.MoreScopes, DirectoryContinuation: raw.DirectoryContinuation, AsOf: raw.AsOf}
	for _, r := range raw.Scopes {
		s.Scopes = append(s.Scopes, readmodel.Scope{ID: r.ID, Generation: r.Generation, Availability: readmodel.Availability(r.Availability), Ready: r.Ready})
	}
	if a.directory.Bindings != nil {
		if len(a.directory.Bindings) != len(s.Scopes) || a.directory.MoreScopes && a.directoryCursor == "" {
			return readmodel.Snapshot{}, a.fail(readmodel.ErrProjection)
		}
		for i, b := range a.directory.Bindings {
			if b.ID != s.Scopes[i].ID || b.Available != (s.Scopes[i].Availability == readmodel.Available) {
				return readmodel.Snapshot{}, a.fail(readmodel.ErrProjection)
			}
		}
		s.MoreScopes = a.directory.MoreScopes
		s.DirectoryContinuation = a.directoryCursor
	}
	if a.frozen != nil && !reflect.DeepEqual(*a.frozen, s) {
		return readmodel.Snapshot{}, a.fail(readmodel.ErrProjection)
	}
	frozen := cloneOrderedSnapshot(s)
	a.frozen = &frozen
	if a.queried == nil {
		a.queried = make([]bool, len(s.Streams))
	}
	return s, nil
}

func (a *orderedReadModelAdapter) OrderedCandidates(ctx context.Context, stream int, after *readmodel.OrderedKey, limit int) ([]readmodel.OrderedCandidate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.state(ctx, true); err != nil {
		return nil, err
	}
	if stream < 0 || stream >= len(a.queried) || a.queried[stream] || limit < 2 || limit > scopes.MaxPage+1 {
		return nil, a.fail(scopes.ErrBudget)
	}
	var key *OrderedFeedKey
	if after != nil {
		k := OrderedFeedKey{append([]byte(nil), after.Order...), after.RID}
		if !validOrderedFeedKey(k) {
			return nil, a.fail(ErrFeedProjection)
		}
		key = &k
	}
	if a.loaded {
		if limit != a.limit || !reflect.DeepEqual(key, a.after) {
			return nil, a.fail(ErrFeedProjection)
		}
	} else {
		refs, err := a.reader.Candidates(key, limit-1)
		if err != nil {
			return nil, a.fail(err)
		}
		a.rows = make([][]OrderedFeedCandidate, len(a.queried))
		a.limit = limit
		a.after = key
		a.loaded = true
		for _, ref := range refs {
			if ref.Stream < 0 || ref.Stream >= len(a.rows) {
				return nil, a.fail(ErrFeedProjection)
			}
			ref.Candidate.Key = copyOrderedFeedKey(ref.Candidate.Key)
			a.rows[ref.Stream] = append(a.rows[ref.Stream], ref.Candidate)
		}
	}
	a.queried[stream] = true
	out := make([]readmodel.OrderedCandidate, len(a.rows[stream]))
	for i, r := range a.rows[stream] {
		out[i] = readmodel.OrderedCandidate{Key: readmodel.OrderedKey{Order: append([]byte(nil), r.Key.Order...), RID: r.Key.RID}, Version: r.Version}
	}
	return out, nil
}

// Validate the private envelope against the joined registry, then detach only
// its item. HTTP shaping, notification targets and freshness are separate gates.
func detachOrderedInboxItem(row OrderedFeedItem) (json.RawMessage, error) {
	if _, err := primitives.DecodeScopeInbox(row.Identity, row.Data); err != nil {
		return nil, ErrFeedProjection
	}
	var envelope struct {
		Format   int `json:"format"`
		Identity struct {
			Scope    scopes.ID `json:"scope"`
			Resource string    `json:"resource"`
			RID      int64     `json:"rid"`
			Version  int64     `json:"version"`
		} `json:"identity"`
		Item json.RawMessage `json:"item"`
	}
	d := json.NewDecoder(bytes.NewReader(row.Data))
	d.DisallowUnknownFields()
	if d.Decode(&envelope) != nil || envelope.Format != 1 || envelope.Identity.Scope != row.Identity.ScopeID || envelope.Identity.Resource != row.Identity.ResourceID || envelope.Identity.RID != row.Identity.RID || envelope.Identity.Version != row.Identity.CanonicalVersion {
		return nil, ErrFeedProjection
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrFeedProjection
	}
	var item struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(envelope.Item, &item) != nil || item.ID != row.Identity.CanonicalID {
		return nil, ErrFeedProjection
	}
	return append(json.RawMessage(nil), envelope.Item...), nil
}

func (a *orderedReadModelAdapter) HydrateOrdered(ctx context.Context, refs []readmodel.OrderedReference) ([]readmodel.Item, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.state(ctx, true); err != nil {
		return nil, err
	}
	batch := make([]OrderedFeedReference, len(refs))
	for i, r := range refs {
		batch[i] = OrderedFeedReference{Stream: r.Stream, Candidate: OrderedFeedCandidate{Key: OrderedFeedKey{Order: append([]byte(nil), r.Candidate.Key.Order...), RID: r.Candidate.Key.RID}, Version: r.Candidate.Version}}
	}
	rows, err := a.reader.Hydrate(batch)
	if err != nil {
		return nil, a.fail(err)
	}
	out := make([]readmodel.Item, len(rows))
	for i, row := range rows {
		data, err := detachOrderedInboxItem(row)
		if err != nil {
			return nil, a.fail(err)
		}
		out[i] = readmodel.Item{Ref: "inbox:" + row.Identity.ResourceID, Data: data}
	}
	return out, nil
}
func (a *orderedReadModelAdapter) Buckets(ctx context.Context, buckets []string) (map[string]int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.state(ctx, true); err != nil {
		return nil, err
	}
	rows, err := a.reader.Buckets(append([]string(nil), buckets...))
	if err != nil {
		return nil, a.fail(err)
	}
	out := make(map[string]int64, len(rows))
	for k, v := range rows {
		out[k] = v
	}
	return out, nil
}
