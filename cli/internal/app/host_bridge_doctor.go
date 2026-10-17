package app

import (
	hostbridge "agent-nexus-cli/internal/bridge"
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Read-only inventory shares install's ownership proof. No service manager
// mutations, signals, wrapper execution, or environment-file reads occur here.
func (a *App) inspectBridgeInventory(ctx context.Context, root string) []map[string]any {
	fail := func(e error) []map[string]any {
		return []map[string]any{{"check": "service_inventory", "ok": false, "detail": e.Error()}}
	}
	home, e := a.bridgeHome()
	if e != nil {
		return fail(e)
	}
	units, e := hostbridge.ReadUnits(home, bridgePlatform)
	if e != nil {
		return fail(e)
	}
	units, e = loadedBridgeUnits(ctx, units)
	if e != nil {
		return fail(e)
	}
	processes, e := bridgeProcesses(ctx)
	if e != nil {
		return fail(e)
	}
	exe, e := os.Executable()
	if e != nil {
		return fail(e)
	}
	plan := hostbridge.PlanMigration(home, exe, os.Getuid(), units, processes)
	supervisors := 0
	for _, p := range processes {
		if p.UID == os.Getuid() && len(p.Argv) == 3 && p.Argv[1] == "bridge" && p.Argv[2] == "run" {
			supervisors++
		}
	}
	checks := []map[string]any{{"check": "service_inventory", "ok": len(plan.Units) == 0 && len(plan.Processes) == 0 && len(plan.Blockers) == 0 && supervisors <= 1, "legacy_units": len(plan.Units), "legacy_consumers": len(plan.Processes), "host_supervisors": supervisors, "blockers": plan.Blockers, "repair": "preview anx bridge install --dry-run; never retire an unowned process"}}
	lock := askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning
	checks = append(checks, map[string]any{"check": "singleton_lock", "ok": lock == (supervisors == 1), "detail": fmt.Sprintf("lock=%t supervisors=%d", lock, supervisors)})
	return checks
}
