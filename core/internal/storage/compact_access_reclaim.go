package storage

import "context"

// ReclaimCompactAccessOffline prepares a verified compact file, never overwrites
// the source. Stop the core first and keep it stopped until the operator replaces
// the file: this function's exclusive SQLite lease ends when it returns.
// The serving OS lease excludes live cores; SQLite excludes external writers.
//
// This is the compact-access reclaim mode: it additionally requires certified
// stable raw edge identities. Use CompactSQLiteOffline for generic compaction.
func ReclaimCompactAccessOffline(ctx context.Context, source, destination string) error {
	return ReclaimCompactAccessOfflineWithReserve(ctx, source, destination, 0)
}

// ReclaimCompactAccessOfflineWithReserve is ReclaimCompactAccessOffline with an
// explicit destination free-space reserve. Zero selects the default 2 GiB.
func ReclaimCompactAccessOfflineWithReserve(ctx context.Context, source, destination string, reserveBytes int64) error {
	_, err := CompactSQLiteOffline(ctx, source, destination, CompactOptions{
		RequireStableAccess: true,
		ReserveBytes:        reserveBytes,
	})
	return err
}
