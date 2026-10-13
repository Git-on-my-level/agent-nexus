//go:build windows

package app

import (
	"fmt"
	"os/exec"
)

func lockAskBridge(path string) (func(), error) {
	return nil, fmt.Errorf("answer bridge requires macOS or Linux")
}

func askBridgeLockHeld(path string) bool { return false }

func lockAskBridgeReady(path string) (func(), error) {
	return lockAskBridge(path)
}

func configureAnswerCommand(cmd *exec.Cmd) {}
