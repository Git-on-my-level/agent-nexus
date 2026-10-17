package app

import (
	hostbridge "agent-nexus-cli/internal/bridge"
	"context"
	"fmt"
	"os"
	"slices"
	"time"
)

func retireBridgeProcess(ctx context.Context, original hostbridge.Process) error {
	// The restart source has already been disabled. Recheck kernel identity on
	// every signal and wait; a reused PID is never signalled as the old bridge.
	deadline := time.Now().Add(10 * time.Second)
	signalled := false
	for time.Now().Before(deadline) {
		all, e := bridgeProcesses(ctx)
		if e != nil {
			return e
		}
		found := false
		for _, p := range all {
			if p.PID != original.PID {
				continue
			}
			if p.Started != original.Started || p.Executable != original.Executable || p.UID != original.UID {
				return nil
			}
			if !hostbridge.IsConsumer(p.Argv) || !slices.Equal(p.Argv, original.Argv) {
				return fmt.Errorf("PID identity changed before quiescence")
			}
			found = true
			if !signalled {
				process, e := os.FindProcess(p.PID)
				if e != nil {
					return e
				}
				if e = process.Signal(os.Interrupt); e != nil && !os.IsNotExist(e) {
					return e
				}
				signalled = true
			}
		}
		if !found {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("legacy bridge PID %d did not stop; no new consumer activated", original.PID)
}
