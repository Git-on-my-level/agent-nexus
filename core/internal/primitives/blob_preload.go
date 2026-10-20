package primitives

import (
	"context"
	"log"

	"golang.org/x/sync/errgroup"

	"agent-nexus-core/internal/blob"
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
// not leave untracked blobs behind.

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
	g := new(errgroup.Group)
	g.SetLimit(blobTransferConcurrency)
	for i := range hashes {
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

// markCommitted records that the write transaction committed its references.
func (p *blobPreload) markCommitted() {
	if p != nil {
		p.committed = true
	}
}

// discardUnlessCommitted is deferred by creators. When the create did not
// commit, it collects the objects its preload created. The collector only
// deletes an object no committed row references, and a concurrent creator of
// the same bytes restores it under the write lock, so this is always safe.
func (p *blobPreload) discardUnlessCommitted() {
	if p == nil || p.committed || len(p.created) == 0 {
		return
	}
	if err := p.store.collectUnreferencedBlobs(context.WithoutCancel(p.ctx), p.created); err != nil {
		log.Printf("discard preloaded blobs after failed create: %v", err)
	}
	p.created = nil
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
