//go:build windows

package app

import (
	"os/exec"
	"syscall"
)

func attachDetachedSkillsProcess(cmd *exec.Cmd) {
	if cmd != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
	}
}
