//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAskBridgeLockProbeDoesNotAcquireExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.lock")
	if askBridgeLockState(path) != answerBridgeAbsent {
		t.Fatal("empty lock reported something other than absent")
	}
	unlock, err := lockAskBridge(path)
	if err != nil {
		t.Fatalf("probe leaked an exclusive lock: %v", err)
	}
	if askBridgeLockState(path) != answerBridgeRunning {
		t.Fatal("held exclusive lock not observed")
	}
	unlock()
	if askBridgeLockState(path) != answerBridgeAbsent {
		t.Fatal("released lock still reported running")
	}
}

// An uninspectable lock must read as unknown, never as a live bridge: an
// ask must not select resume delivery on a probe it could not run.
func TestAskBridgeLockProbeReportsUnknownWhenUninspectable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "bridge.lock")
	if askBridgeLockState(path) != answerBridgeUnknown {
		t.Fatal("open failure must read as unknown, not running")
	}
}

func TestAskBridgeLockProbesDoNotFalsePositive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge.lock")
	holder, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatalf("hold shared probe lock: %v", err)
	}
	defer syscall.Flock(int(holder.Fd()), syscall.LOCK_UN)

	if askBridgeLockState(path) != answerBridgeAbsent {
		t.Fatal("a concurrent shared probe was treated as a live exclusive bridge")
	}
}
