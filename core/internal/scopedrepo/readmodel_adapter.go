package scopedrepo

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"

	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopes"
)

// Disabled integration foundation. Import guards must remain in force until
// complete route/privacy/derivation/parity and full-request cost approval.
type ReadModelReader interface {
	readmodel.Reader
	readmodel.CounterReader
}

func AdaptReadModel(reader FeedReader, directory scopes.DirectoryPage, cursor string) ReadModelReader {
	if directory.Bindings != nil {
		directory.Bindings = append([]scopes.Binding{}, directory.Bindings...)
	}
	return &readModelAdapter{reader: reader, directory: directory, directoryCursor: cursor}
}

// Trusted transaction adapter; no factory or SQL handle is exposed.
type readModelAdapter struct {
	mu              sync.Mutex
	frozen          *readmodel.Snapshot
	failure         error
	reader          FeedReader
	directory       scopes.DirectoryPage
	directoryCursor string
}

func (a *readModelAdapter) Snapshot(ctx context.Context) (readmodel.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return readmodel.Snapshot{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure != nil {
		return readmodel.Snapshot{}, a.failure
	}
	if a.reader == nil {
		a.failure = readmodel.ErrProjection
		return readmodel.Snapshot{}, a.failure
	}
	raw, err := a.reader.Snapshot()
	if err != nil {
		return readmodel.Snapshot{}, err
	}
	s := readmodel.Snapshot{Binding: raw.Binding, Streams: append([]readmodel.Stream(nil), raw.Streams...), MoreScopes: raw.MoreScopes, DirectoryContinuation: raw.DirectoryContinuation, AsOf: raw.AsOf}
	for _, r := range raw.Scopes {
		s.Scopes = append(s.Scopes, readmodel.Scope{ID: r.ID, Generation: r.Generation, Availability: readmodel.Availability(r.Availability), Ready: r.Ready})
	}
	// Explicit selection does not assert directory completeness. Overlay only a
	// matching authorized directory page; directory cursors are already encrypted.
	if a.directory.Bindings != nil {
		if len(a.directory.Bindings) != len(s.Scopes) || a.directory.MoreScopes && a.directoryCursor == "" {
			a.failure = readmodel.ErrProjection
			return readmodel.Snapshot{}, a.failure
		}
		for i, b := range a.directory.Bindings {
			if b.ID != s.Scopes[i].ID || b.Available != (s.Scopes[i].Availability == readmodel.Available) {
				a.failure = readmodel.ErrProjection
				return readmodel.Snapshot{}, a.failure
			}
		}
		s.MoreScopes = a.directory.MoreScopes
		s.DirectoryContinuation = a.directoryCursor
	}
	if a.frozen != nil && !reflect.DeepEqual(*a.frozen, s) {
		a.failure = readmodel.ErrProjection
		return readmodel.Snapshot{}, a.failure
	}
	frozen := s
	frozen.Scopes = append([]readmodel.Scope(nil), s.Scopes...)
	frozen.Streams = append([]readmodel.Stream(nil), s.Streams...)
	a.frozen = &frozen
	return s, nil
}

// Data operations require a validated immutable transaction snapshot first.
func (a *readModelAdapter) dataReady(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure != nil {
		return a.failure
	}
	if err := ctx.Err(); err != nil {
		a.failure = err
		return err
	}
	if a.frozen == nil || a.reader == nil {
		a.failure = readmodel.ErrProjection
		return a.failure
	}
	return nil
}
func (a *readModelAdapter) Candidates(ctx context.Context, stream int, after *readmodel.Key, limit int) ([]readmodel.Candidate, error) {
	if err := a.dataReady(ctx); err != nil {
		return nil, err
	}
	var key *FeedKey
	if after != nil {
		key = &FeedKey{Sort: after.Sort, RID: after.RID}
	}
	rows, err := a.reader.Candidates(stream, key, limit)
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.Candidate, len(rows))
	for i, r := range rows {
		out[i] = readmodel.Candidate{Key: readmodel.Key{Sort: r.Key.Sort, RID: r.Key.RID}, Version: r.Version}
	}
	return out, nil
}
func (a *readModelAdapter) Hydrate(ctx context.Context, refs []readmodel.Reference) ([]readmodel.Item, error) {
	if err := a.dataReady(ctx); err != nil {
		return nil, err
	}
	batch := make([]FeedReference, len(refs))
	for i, r := range refs {
		batch[i] = FeedReference{Stream: r.Stream, Candidate: FeedCandidate{Key: FeedKey{Sort: r.Candidate.Key.Sort, RID: r.Candidate.Key.RID}, Version: r.Candidate.Version}}
	}
	rows, err := a.reader.Hydrate(batch)
	if err != nil {
		return nil, err
	}
	out := make([]readmodel.Item, len(rows))
	for i, r := range rows {
		out[i] = readmodel.Item{Ref: r.Ref, Data: append(json.RawMessage(nil), r.Data...)}
	}
	return out, nil
}
func (a *readModelAdapter) Buckets(ctx context.Context, buckets []string) (map[string]int64, error) {
	if err := a.dataReady(ctx); err != nil {
		return nil, err
	}
	return a.reader.Buckets(append([]string(nil), buckets...))
}
