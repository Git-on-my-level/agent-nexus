//go:build !windows

package filelock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenNoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	f, err := OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenNoFollow(link, os.O_CREATE|os.O_RDWR, 0600); err == nil {
		f.Close()
		t.Fatal("followed final symlink")
	}
}

func TestLockBlocksAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	ready := filepath.Join(dir, "ready")
	marker := filepath.Join(dir, "acquired")
	f, err := OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := Lock(f); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), "ANX_FILELOCK_HELPER=1", "ANX_FILELOCK_PATH="+path, "ANX_FILELOCK_READY="+ready, "ANX_FILELOCK_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	result := make(chan error, 1)
	go func() { result <- cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("child exited before trying lock: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not reach lock attempt")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-result:
		t.Fatalf("child exited before unlock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child acquired lock before unlock: stat error %v", err)
	}
	if err := Unlock(f); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not acquire lock after unlock")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child did not record lock acquisition: %v", err)
	}
}

func TestLockHelper(t *testing.T) {
	if os.Getenv("ANX_FILELOCK_HELPER") != "1" {
		return
	}
	f, err := OpenNoFollow(os.Getenv("ANX_FILELOCK_PATH"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.WriteFile(os.Getenv("ANX_FILELOCK_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Lock(f); err != nil {
		t.Fatal(err)
	}
	defer Unlock(f)
	if err := os.WriteFile(os.Getenv("ANX_FILELOCK_MARKER"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
}
