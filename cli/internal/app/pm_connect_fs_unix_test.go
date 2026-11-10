//go:build !windows

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPMConnectDiagnosticRejectsSymlinksAndForeignDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "diagnostic")
	const sentinel = `{"code":"forbidden","hint":"secret"}`
	if err := os.WriteFile(target, []byte(sentinel), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "last-connect.json")); err != nil {
		t.Fatal(err)
	}
	if failure, err := readPMConnectFailure(dir); err != nil || failure != nil {
		t.Fatalf("followed diagnostic symlink: %+v %v", failure, err)
	}
	linkedDir := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{linkedDir, linkedDir + "/", linkedDir + "/."} {
		if err := recordPMConnect(path, nil); err == nil {
			t.Fatalf("wrote through directory symlink %s", path)
		}
	}
	// Exercise the same open/fstat ownership check with a different current UID,
	// without requiring root or changing ownership of real files.
	if opened, err := openPMConnectDirectory(dir, os.Getuid()+1); err == nil {
		opened.Close()
		t.Fatal("accepted foreign diagnostic directory")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != sentinel {
		t.Fatalf("changed symlink target: %q %v", raw, err)
	}
}
