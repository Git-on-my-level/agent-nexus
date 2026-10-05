//go:build !windows

package app

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateProcessLockSurvivesAgeAndRecoversOnOwnerDeath(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("Python installer dependency unavailable", err)
	}
	path := filepath.Join(t.TempDir(), "anx")
	// Use the real Python installer protocol against Go's same kernel lock.
	cmd := exec.Command(python, "-c", `import fcntl,os,sys,time
f=open(sys.argv[1]+".anx-update.lock","a+")
fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)
print("locked",flush=True)
time.sleep(60)
`, path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		t.Fatalf("%s %v", line, err)
	}
	lockPath := path + ".anx-update.lock"
	old := time.Unix(1, 0)
	_ = os.Chtimes(lockPath, old, old)
	before, _ := os.Stat(lockPath)
	for i := 0; i < 2; i++ {
		if f, err := lockUpdateInstall(path); err == nil {
			f.Close()
			t.Fatal("live owner lock stolen by age")
		}
	}
	after, _ := os.Stat(lockPath)
	if !os.SameFile(before, after) {
		t.Fatal("contender replaced lock inode")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	reaped = true
	lock, err := lockUpdateInstall(path)
	if err != nil {
		t.Fatal("dead owner still blocks lock", err)
	}
	lock.Close()
	final, _ := os.Stat(lockPath)
	if !os.SameFile(before, final) {
		t.Fatal("recovery unlinked lock inode")
	}
}

func TestUpdateContenderCloseCannotReleaseAnotherOwnersLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anx")
	owner, err := lockUpdateInstall(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	for i := 0; i < 20; i++ {
		if contender, err := lockUpdateInstall(path); err == nil {
			contender.Close()
			t.Fatal("two owners of update lock")
		}
	}
	if contender, err := lockUpdateInstall(path); err == nil {
		contender.Close()
		t.Fatal("contender cleanup released owner's lock")
	}
}
