package server

import "context"

// runInStoreBatch runs fn in one store write transaction when the store
// offers it (writes inside join as savepoints and commit once), and plainly
// otherwise.
func runInStoreBatch(ctx context.Context, opts handlerOptions, fn func(context.Context) error) error {
	if store, ok := opts.primitiveStore.(interface {
		RunBatch(context.Context, func(context.Context) error) error
	}); ok {
		return store.RunBatch(ctx, fn)
	}
	return fn(ctx)
}
