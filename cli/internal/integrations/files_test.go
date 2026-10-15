package integrations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateSettingsRetriesExternalWrites(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint("replace=", replace), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			original := []byte(`{"editor":"before"}`)
			external := []byte(`{"editor":"edited"}`) // same size; restore mtime for byte comparison
			if err := AtomicWrite(path, original); err != nil {
				t.Fatal(err)
			}
			unlock, err := Lock(context.Background(), path+".anx-lock")
			if err != nil {
				t.Fatal(err)
			}
			defer unlock()
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			next, changed, err := UpdateSettings(path, func(raw []byte) ([]byte, bool, error) {
				calls++
				merged, changed, err := (ClaudeCode{}).Merge(raw, recipe(t), "owned", "", true)
				if calls == 1 {
					done := make(chan error, 1)
					go func() {
						var err error
						if replace {
							err = AtomicWrite(path, external)
						} else {
							err = os.WriteFile(path, external, 0600)
						}
						if err == nil {
							err = os.Chtimes(path, info.ModTime(), info.ModTime())
						}
						done <- err
					}()
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				return merged, changed, err
			})
			if err != nil || !changed || calls != 2 || !bytes.Contains(next, []byte(`"edited"`)) {
				t.Fatal(string(next), changed, calls, err)
			}
			saved, err := Read(path)
			if err != nil || !bytes.Equal(saved, next) {
				t.Fatal(string(saved), err)
			}
			// Further external churn refuses without replacing the last external edit.
			calls = 0
			_, _, err = UpdateSettings(path, func(raw []byte) ([]byte, bool, error) {
				calls++
				if err := AtomicWrite(path, JSON(map[string]int{"external": calls})); err != nil {
					t.Fatal(err)
				}
				return (ClaudeCode{}).Merge(raw, recipe(t), "other", "owned", true)
			})
			if !errors.Is(err, ErrSettingsChanged) || calls != 3 {
				t.Fatal(calls, err)
			}
			saved, err = Read(path)
			if err != nil || !bytes.Equal(saved, JSON(map[string]int{"external": 3})) {
				t.Fatal(string(saved), err)
			}
			temps, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".anx-write-*"))
			if len(temps) != 0 {
				t.Fatal("left temporary files", temps)
			}
		})
	}
}
