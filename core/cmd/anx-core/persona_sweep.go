package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"agent-nexus-core/internal/auth"
)

const (
	// personaArchiveIdleFor is fixed on purpose: workspaces do not tune
	// inactivity thresholds (see plans.TestStalenessKnobs). An auto archive
	// only hides a persona; its next host assertion brings it back.
	personaArchiveIdleFor = 30 * 24 * time.Hour
	personaSweepInterval  = 6 * time.Hour
	personaSweepTimeout   = 2 * time.Minute
)

// personaArchiveSweepEnabled reads ANX_PERSONA_ARCHIVE_SWEEP, an on/off
// switch (default on). It never changes the threshold.
func personaArchiveSweepEnabled() bool {
	return envBool("ANX_PERSONA_ARCHIVE_SWEEP", true)
}

// startPersonaArchiveSweep archives idle derived personas every few hours.
// Each run reads a bounded keyset page budget and remembers where it stopped,
// so a large roster is covered across runs instead of in one long scan.
func startPersonaArchiveSweep(ctx context.Context, store *auth.Store) {
	if !personaArchiveSweepEnabled() || store == nil {
		fmt.Println("persona archive sweep: disabled")
		return
	}
	go func() {
		var cursor auth.PersonaSweepCursor
		runEvery(ctx, personaSweepInterval, personaSweepTimeout, func(jobCtx context.Context) {
			result, err := store.ArchiveInactivePersonas(jobCtx, auth.PersonaSweepOptions{InactiveFor: personaArchiveIdleFor, After: cursor})
			if err != nil {
				fmt.Fprintf(os.Stderr, "persona archive sweep: %v\n", err)
				return
			}
			cursor = result.Next
			if result.Archived > 0 {
				fmt.Printf("persona archive sweep: archived %d of %d personas unused for %s\n", result.Archived, result.Scanned, personaArchiveIdleFor)
			}
		})
	}()
}
