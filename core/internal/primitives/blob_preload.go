package primitives

import (
	"context"
	"log"

	"golang.org/x/sync/errgroup"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/resourceaccess"
)

// Creators store blob content in two steps so that slow remote uploads stay
// outside the workspace write lock while the reference commit stays safe
// against collectUnreferencedBlobs:
//
//  1. Before the write transaction, preload the content. A remote backend
//     uploads it and keeps the staged copy; a local backend does nothing,
//     because its Promote is a local stat and rename.
//  2. Inside the write transaction, before commit, Promote each staged write.
//     After a preload this is one existence check. If a collector removed the
//     object after the preload, Promote restores it from the staged copy.
//
// Step 2 and the commit run under the same write lock as the collector's
// recount and delete, so the object a committed row references exists when it
// commits, and a collector that runs later counts that row.
//
// If the create then fails, discardUnlessCommitted removes the objects the
// preload created, through the same locked recount, so a failed request does
// not leave untracked blobs behind. A create nested in an outer
// WriteTransaction (idempotent replay) keeps that decision until the outer
// transaction finishes, because its rows are durable only then.
//
// A replay-wrapped create opens its outer transaction before the create runs,
// so a preload there would hold the lock. PreloadDocumentContentForReplay lets the
// caller preload known content before the outer transaction instead; the
// nested create then skips those hashes.

const blobTransferConcurrency = 8

type stagedBlob struct {
	hash  string
	write blob.StagedWrite
}

type blobContent struct {
	hash string
	data []byte
}

// blobPreload records which objects a create's preload created.
type blobPreload struct {
	store     *Store
	ctx       context.Context
	created   []string
	committed bool
}

func (s *Store) preloadStagedBlobs(ctx context.Context, staged ...stagedBlob) (*blobPreload, error) {
	hashes := make([]string, len(staged))
	for i, item := range staged {
		hashes[i] = item.hash
	}
	return s.preloadBlobs(ctx, hashes, func(i int) (bool, error) {
		if staged[i].write == nil {
			return false, nil
		}
		return blob.Preload(staged[i].write)
	})
}

func (s *Store) preloadBlobContent(ctx context.Context, contents ...blobContent) (*blobPreload, error) {
	hashes := make([]string, len(contents))
	for i, item := range contents {
		hashes[i] = item.hash
	}
	return s.preloadBlobs(ctx, hashes, func(i int) (bool, error) {
		return blob.PreloadContent(ctx, s.blob, contents[i].hash, contents[i].data)
	})
}

func (s *Store) preloadBlobs(ctx context.Context, hashes []string, preload func(int) (bool, error)) (*blobPreload, error) {
	p := &blobPreload{store: s, ctx: ctx}
	created := make([]bool, len(hashes))
	already, _ := ctx.Value(replayPreloadKey{}).(map[string]struct{})
	g := new(errgroup.Group)
	g.SetLimit(blobTransferConcurrency)
	for i := range hashes {
		if _, ok := already[hashes[i]]; ok {
			continue
		}
		g.Go(func() error {
			ok, err := preload(i)
			created[i] = ok
			return err
		})
	}
	err := g.Wait()
	for i, ok := range created {
		if ok {
			p.created = append(p.created, hashes[i])
		}
	}
	if err != nil {
		p.discardUnlessCommitted()
		return nil, err
	}
	if s.blobPreloadedHook != nil {
		s.blobPreloadedHook(uniqueSortedStrings(hashes))
	}
	return p, nil
}

// markCommitted records that the create's write transaction committed its
// references. Inside an outer WriteTransaction that was only a savepoint, so
// the preloaded objects are discarded if the outer transaction rolls back.
func (p *blobPreload) markCommitted() {
	if p == nil {
		return
	}
	p.committed = true
	if len(p.created) == 0 {
		return
	}
	created, store := p.created, p.store
	resourceaccess.AfterWriteTransaction(p.ctx, func(ctx context.Context, committed bool) {
		if !committed {
			store.discardBlobs(ctx, created)
		}
	})
}

// discardUnlessCommitted is deferred by creators. When the create did not
// commit, it collects the objects its preload created. The collector only
// deletes an object no committed row references, and a concurrent creator of
// the same bytes restores it under the write lock, so this is always safe.
func (p *blobPreload) discardUnlessCommitted() {
	if p == nil || p.committed || len(p.created) == 0 {
		return
	}
	p.store.discardBlobs(p.ctx, p.created)
	p.created = nil
}

func (s *Store) discardBlobs(ctx context.Context, hashes []string) {
	if err := s.collectUnreferencedBlobs(context.WithoutCancel(ctx), hashes); err != nil {
		log.Printf("discard preloaded blobs after failed create: %v", err)
	}
}

type replayPreloadKey struct{}

// PreloadDocumentContentForReplay preloads the revision content a
// replay-wrapped document create or revise will store, before the replay opens
// its outer write transaction. Run the replay with the returned context, then
// call finish: it collects the preloaded object if no committed row references
// it, so a failed replay (or content the mutation did not store) leaves nothing
// behind. Content that cannot be encoded, or a failed preload, is left to the
// mutation itself.
func (s *Store) PreloadDocumentContentForReplay(ctx context.Context, content any, contentType string) (context.Context, func()) {
	noop := func() {}
	if s == nil || s.blob == nil {
		return ctx, noop
	}
	if _, ok := s.blob.(blob.ContentPreloader); !ok {
		return ctx, noop
	}
	contentType, err := normalizeDocumentContentType(contentType)
	if err != nil {
		return ctx, noop
	}
	data, err := encodeContent(content, contentType)
	if err != nil {
		return ctx, noop
	}
	hash := sha256Hex(data)
	p, err := s.preloadBlobContent(ctx, blobContent{hash: hash, data: data})
	if err != nil {
		return ctx, noop
	}
	already, _ := ctx.Value(replayPreloadKey{}).(map[string]struct{})
	next := make(map[string]struct{}, len(already)+1)
	for h := range already {
		next[h] = struct{}{}
	}
	next[hash] = struct{}{}
	return context.WithValue(ctx, replayPreloadKey{}, next), p.discardUnlessCommitted
}

// promoteStagedBlobs promotes a batch's staged writes inside its write
// transaction, a few at a time, so a large batch on a remote backend holds the
// write lock for a handful of round trips rather than one per member.
func promoteStagedBlobs(writes []blob.StagedWrite) error {
	g := new(errgroup.Group)
	g.SetLimit(blobTransferConcurrency)
	for _, write := range writes {
		if write == nil {
			continue
		}
		g.Go(write.Promote)
	}
	return g.Wait()
}
