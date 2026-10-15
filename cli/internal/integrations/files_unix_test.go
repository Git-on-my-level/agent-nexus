//go:build !windows

package integrations

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCreatedParentsHaveExactPrivateMode(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(existing, 0755); err != nil {
		t.Fatal(err)
	}
	old := syscall.Umask(0777)
	defer syscall.Umask(old)
	path := filepath.Join(existing, "new", "nested", "settings.json")
	if err := AtomicWrite(path, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	unlock, err := Lock(context.Background(), filepath.Join(existing, "locks", "nested", "test.lock"))
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	for _, relative := range []string{"new", "new/nested", "locks", "locks/nested"} {
		st, err := os.Stat(filepath.Join(existing, relative))
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatal(relative, st, err)
		}
	}
	st, err := os.Stat(existing)
	if err != nil || st.Mode().Perm() != 0755 {
		t.Fatal("changed existing directory", st, err)
	}
}
