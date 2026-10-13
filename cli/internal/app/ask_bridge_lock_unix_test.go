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
	if askBridgeLockHeld(path) {
		t.Fatal("empty lock reported a running bridge")
	}
	unlock, err := lockAskBridge(path)
	if err != nil {
		t.Fatalf("probe leaked an exclusive lock: %v", err)
	}
	if !askBridgeLockHeld(path) {
		t.Fatal("held exclusive lock not observed")
	}
	unlock()
	if askBridgeLockHeld(path) {
		t.Fatal("released lock still reported running")
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

	if askBridgeLockHeld(path) {
		t.Fatal("a concurrent shared probe was treated as a live exclusive bridge")
	}
}
