package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"agent-nexus-core/internal/auth"
)

const (
	// defaultPersonaArchiveAfterDays archives a host persona after this many
	// days without authenticated use. ANX_PERSONA_ARCHIVE_AFTER_DAYS=0 disables.
	defaultPersonaArchiveAfterDays = 30
	personaSweepInterval           = 6 * time.Hour
	personaSweepTimeout            = 2 * time.Minute
)

// personaArchiveAfter reads ANX_PERSONA_ARCHIVE_AFTER_DAYS. Zero or a negative
// value turns the sweep off.
func personaArchiveAfter() time.Duration {
	days := envInt("ANX_PERSONA_ARCHIVE_AFTER_DAYS", defaultPersonaArchiveAfterDays)
	if days <= 0 {
		return 0
	}
	return time.Duration(days) * 24 * time.Hour
}

// startPersonaArchiveSweep archives idle derived personas every few hours.
// Each run reads a bounded keyset page budget and remembers where it stopped,
// so a large roster is covered across runs instead of in one long scan.
func startPersonaArchiveSweep(ctx context.Context, store *auth.Store) {
	after := personaArchiveAfter()
	if after <= 0 || store == nil {
		fmt.Println("persona archive sweep: disabled")
		return
	}
	go func() {
		var cursor auth.PersonaSweepCursor
		runEvery(ctx, personaSweepInterval, personaSweepTimeout, func(jobCtx context.Context) {
			result, err := store.ArchiveInactivePersonas(jobCtx, auth.PersonaSweepOptions{InactiveFor: after, After: cursor})
			if err != nil {
				fmt.Fprintf(os.Stderr, "persona archive sweep: %v\n", err)
				return
			}
			cursor = result.Next
			if result.Archived > 0 {
				fmt.Printf("persona archive sweep: archived %d of %d personas unused for %s\n", result.Archived, result.Scanned, after)
			}
		})
	}()
}
