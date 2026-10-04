//go:build !windows

package app

import (
	"os/exec"
	"syscall"
)

func attachDetachedSkillsProcess(cmd *exec.Cmd) {
	if cmd != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}
