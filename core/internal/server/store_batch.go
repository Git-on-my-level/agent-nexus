package server

import "context"

type storeBatchKey struct{}

// runInStoreBatch runs fn in one store write transaction when the store
// offers it (writes inside join as savepoints and commit once), and plainly
// otherwise. Work inside must not take server locks that are ever held while
// waiting for the store's quota lock; see inStoreBatch.
func runInStoreBatch(ctx context.Context, opts handlerOptions, fn func(context.Context) error) error {
	store, ok := opts.primitiveStore.(interface {
		RunBatch(context.Context, func(context.Context) error) error
	})
	if !ok {
		return fn(ctx)
	}
	return store.RunBatch(ctx, func(ctx context.Context) error {
		return fn(context.WithValue(ctx, storeBatchKey{}, true))
	})
}

// inStoreBatch reports whether ctx runs inside runInStoreBatch.
func inStoreBatch(ctx context.Context) bool {
	in, _ := ctx.Value(storeBatchKey{}).(bool)
	return in
}
