package workspaceconfig

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// caseInsensitivePath detects filesystem semantics once per rule resolution,
// using an existing ancestor for paths that have not been created yet. Native
// volume metadata is preferred; otherwise a differently cased spelling must
// resolve to the same filesystem object. Unknown semantics stay case-sensitive.
func caseInsensitivePath(path string) bool {
	for {
		if info, err := os.Stat(path); err == nil {
			if insensitive, known := filesystemCaseInsensitive(path); known {
				return insensitive
			}
			name := filepath.Base(path)
			variant := strings.Map(func(r rune) rune {
				if unicode.IsUpper(r) {
					return unicode.ToLower(r)
				}
				return unicode.ToUpper(r)
			}, name)
			if variant != name {
				// A separate symlink with different casing is not evidence that
				// the filesystem itself treats those names as equivalent.
				other, err := os.Lstat(filepath.Join(filepath.Dir(path), variant))
				return err == nil && os.SameFile(info, other)
			}
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}
