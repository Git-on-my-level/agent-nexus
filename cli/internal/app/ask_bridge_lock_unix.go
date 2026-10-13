//go:build !windows

package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func lockAskBridge(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("answer bridge already running")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// askBridgeLockHeld probes whether an exclusive answer-bridge lock is held
// without taking one. A shared non-blocking lock succeeds alongside other
// probes and fails only when `anx bridge run` holds exclusive; holding
// exclusive here would starve a starting bridge and look like a live one
// to a concurrent ask/orient.
func askBridgeLockHeld(path string) bool {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return true
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// lockAskBridgeReady takes the exclusive answer-bridge lock, retrying briefly
// so a concurrent shared liveness probe cannot abort `anx bridge run`.
func lockAskBridgeReady(path string) (func(), error) {
	unlock, err := lockAskBridge(path)
	deadline := time.Now().Add(100 * time.Millisecond)
	for err != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		unlock, err = lockAskBridge(path)
	}
	return unlock, err
}

// Kill the process group before Wait returns so timeout retries cannot overlap
// a child runner that outlived its shell.
func configureAnswerCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
