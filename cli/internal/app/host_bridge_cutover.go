package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/integrations"
)

func saveBridgeMigration(root string, j *bridgeMigrationJournal) error {
	return integrations.PrivateWrite(filepath.Join(root, "migration.json"), integrations.JSON(j))
}

// A busy namespace is rejected before quiescing any unit. If a command starts
// during that narrow race, rollback retains its unknown outcome outside the
// legacy *.json intake; older executors do not understand State=uncertain.
func checkBridgeResumeIdle(w hostbridge.Workspace) error {
	for _, a := range w.Agents {
		for _, dir := range a.RegistryDirs {
			files, e := os.ReadDir(dir)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			for _, f := range files {
				if !isResumeRegistrationFile(f.Name()) {
					continue
				}
				entry, e := readAskRegistration(filepath.Join(dir, f.Name()))
				if e != nil {
					return e
				}
				if !entry.Acknowledged && (entry.State == "executing" || entry.State == "uncertain" || entry.State == "pending" && entry.Attempts > 0 || filepath.Ext(f.Name()) == ".paused") {
					return fmt.Errorf("resume namespace busy or uncertain; retained record %s must finish or be reconciled before retry", filepath.Join(dir, f.Name()))
				}
			}
		}
	}
	return nil
}

// Stop the new generation first, restore every original file and loaded state,
// and keep a retryable journal even when one namespace needs reconciliation.
// Recovery must also run after the invoking command's context was cancelled.
func (a *App) rollbackHostBridge(parent context.Context, root string, j *bridgeMigrationJournal) error {
	ctx, done := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer done()
	j.Phase = "rolling_back"
	if e := saveBridgeMigration(root, j); e != nil {
		return e
	}
	var failures []error
	if j.NewActivated {
		if e := serviceCall(ctx, j.NewUnit.Kind, "stop", j.NewUnit); e != nil {
			return fmt.Errorf("cannot stop new generation before restore: %w", e)
		}
		if e := serviceCall(ctx, j.NewUnit.Kind, "disable", j.NewUnit); e != nil {
			return fmt.Errorf("cannot disable new generation before restore: %w", e)
		}
		for askBridgeLockState(filepath.Join(root, "service.lock")) == answerBridgeRunning {
			select {
			case <-ctx.Done():
				return fmt.Errorf("new generation did not release singleton before restore: %w", ctx.Err())
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	// Journal destinations before moving any uncertain record. Keep commands,
	// attempts, subscription and claim IDs; healthy namespaces are untouched.
	for _, w := range j.After.Workspaces {
		for _, agent := range w.Agents {
			for _, dir := range agent.RegistryDirs {
				files, e := os.ReadDir(dir)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					failures = append(failures, e)
					continue
				}
				for _, f := range files {
					if !isResumeRegistrationFile(f.Name()) {
						continue
					}
					path := filepath.Join(dir, f.Name())
					entry, e := readAskRegistration(path)
					if e != nil {
						failures = append(failures, e)
						continue
					}
					if entry.Acknowledged || !(entry.State == "executing" || entry.State == "uncertain" || entry.State == "pending" && entry.Attempts > 0) {
						continue
					}
					if filepath.Ext(path) == ".paused" {
						continue
					}
					paused := path + ".paused"
					j.Paused = append(j.Paused, paused)
					if e = saveBridgeMigration(root, j); e != nil {
						failures = append(failures, e)
						continue
					}
					entry.State = "uncertain"
					if e = writeAskRegistration(path, entry); e == nil {
						e = os.Rename(path, paused)
					}
					if e != nil {
						failures = append(failures, e)
					}
				}
			}
		}
	}
	// Restore files for ALL sources, including the unit whose stop call failed
	// and units not reached by the quiesce loop. Nothing is pruned on failure.
	for _, u := range j.Plan.Units {
		if e := integrations.AtomicWrite(u.Path, u.Content); e != nil {
			failures = append(failures, e)
			continue
		}
		mode := os.FileMode(u.Mode)
		if mode == 0 {
			mode = 0600
		}
		if e := os.Chmod(u.Path, mode); e != nil {
			failures = append(failures, e)
		}
	}
	if j.RegistryExisted {
		if e := integrations.PrivateWrite(filepath.Join(root, "registry.json"), j.RegistryBefore); e != nil {
			failures = append(failures, e)
		}
	} else if e := os.Remove(filepath.Join(root, "registry.json")); e != nil && !os.IsNotExist(e) {
		failures = append(failures, e)
	}
	if j.UnitExisted {
		if e := integrations.PrivateWrite(j.NewUnit.Path, j.UnitBefore); e != nil {
			failures = append(failures, e)
		}
	} else if e := os.Remove(j.NewUnit.Path); e != nil && !os.IsNotExist(e) {
		failures = append(failures, e)
	}
	if j.NewUnit.Kind == "systemd-user" {
		if _, _, e := bridgeCommandRun(ctx, "systemctl", "--user", "daemon-reload"); e != nil {
			failures = append(failures, e)
		}
	}
	for _, u := range j.Plan.Units {
		unit := hostbridge.UnitPlan{Path: u.Path, Label: u.Label, Kind: u.Kind}
		action := "disable"
		if u.Enabled {
			action = "enable"
		}
		if e := serviceCall(ctx, u.Kind, action, unit); e != nil {
			failures = append(failures, e)
		}
		current, e := loadedBridgeUnits(ctx, []hostbridge.LegacyUnit{u})
		if e != nil {
			failures = append(failures, e)
			continue
		}
		wantLoaded := u.Loaded || u.PID > 0
		if wantLoaded && !current[0].Loaded {
			e = serviceCall(ctx, u.Kind, "load", unit)
		}
		if !wantLoaded && current[0].Loaded {
			e = serviceCall(ctx, u.Kind, "stop", unit)
		}
		if e != nil {
			failures = append(failures, e)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	j.Phase = "rolled_back"
	return saveBridgeMigration(root, j)
}
