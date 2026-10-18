package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hostbridge "agent-nexus-cli/internal/bridge"
)

func inspectBridgeResumeCheck(dir string, w hostbridge.Workspace, agent hostbridge.Agent) map[string]any {
	// Validate before probing the lock so an unsafe namespace is never followed.
	info, statErr := os.Lstat(dir)
	live := statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 &&
		askBridgeLockState(filepath.Join(dir, "consumer.lock")) == answerBridgeRunning
	inProgress, err := inspectResumeNamespace(dir, w, agent, live)
	if err == nil && agent.Jobs["wake"] {
		var runtimeProgress bool
		runtimeProgress, err = inspectRuntimeReceipts(dir, live)
		inProgress = inProgress || runtimeProgress
	}
	// If the owner exited during inspection, executing records need reconciliation.
	if err == nil && inProgress && askBridgeLockState(filepath.Join(dir, "consumer.lock")) != answerBridgeRunning {
		err = fmt.Errorf("resume execution has no live namespace owner; reconcile retained receipts")
	}
	check := map[string]any{"workspace_id": w.ID, "actor_id": agent.ActorID, "check": "resume_namespace", "ok": err == nil}
	if err != nil {
		check["error"] = safeBridgeInspectionError(err)
		check["repair"] = "reconcile retained command/runtime receipts against their external outcome"
	} else if inProgress {
		check["status"] = "in_progress"
	} else {
		check["status"] = "ready"
	}
	return check
}

// JSON errors can quote values from a retained command, and filesystem errors
// can echo arbitrary filenames. Keep the diagnostic kind/offset/OS cause only.
func safeBridgeInspectionError(err error) string {
	var syntax *json.SyntaxError
	var valueType *json.UnmarshalTypeError
	var path *os.PathError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("invalid receipt JSON at byte %d", syntax.Offset)
	case errors.As(err, &valueType):
		return "invalid receipt JSON field type"
	case errors.As(err, &path):
		return "cannot access retained receipt: " + safeBridgeInspectionError(path.Err)
	default:
		if strings.HasPrefix(err.Error(), "refusing non-regular file ") {
			return "retained receipt must be a regular file"
		}
		return err.Error()
	}
}
