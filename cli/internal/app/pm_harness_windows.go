//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

func attachHarnessProcessGroup(cmd *exec.Cmd) {}

func signalHarnessProcessGroup(cmd *exec.Cmd, _ syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
