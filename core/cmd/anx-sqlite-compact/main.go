package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	sqlite3 "modernc.org/sqlite/lib"

	"agent-nexus-core/internal/storage"
)

func main() {
	source := flag.String("source", "", "stopped workspace state.sqlite")
	destination := flag.String("destination", "", "new verified database file (must not exist)")
	reserve := flag.Int64("reserve-bytes", storage.DefaultCompactReserveBytes, "free space to leave on the destination filesystem after live pages")
	requireStableAccess := flag.Bool("require-stable-access", false, "require certified stable compact-access raw edge identities")
	flag.Parse()
	if *source == "" || *destination == "" {
		fmt.Fprintln(os.Stderr, "--source and --destination are required; stop the core first")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	tempLease, err := storage.PrepareSQLiteTempDirectory(*source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer tempLease.Close()
	sqlite3.Xsqlite3_temp_directory = tempLease.NativePath
	result, err := storage.CompactSQLiteOffline(ctx, *source, *destination, storage.CompactOptions{
		RequireStableAccess: *requireStableAccess,
		ReserveBytes:        *reserve,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf(
		"Verified copy: before=%d after=%d saved=%d bytes auto_vacuum=%d page_size=%d live_pages=%d wal=%d free_before=%d reserve=%d. Keep the core stopped until replacement; source was retained.\n",
		result.SourceBytes, result.DestinationBytes, result.SavedBytes, result.AutoVacuum,
		result.PageSize, result.LivePageBytes, result.WALBytes, result.FreeBytes, result.ReserveBytes,
	)
}
