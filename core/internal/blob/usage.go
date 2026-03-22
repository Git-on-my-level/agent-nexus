package blob

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func usageForRoot(rootDir string, tempPrefixes ...string) (Usage, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return Usage{}, fmt.Errorf("blob root directory is not configured")
	}

	info, err := os.Stat(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Usage{}, nil
		}
		return Usage{}, fmt.Errorf("stat blob root: %w", err)
	}
	if !info.IsDir() {
		return Usage{}, fmt.Errorf("blob root is not a directory")
	}

	skipTemp := func(name string) bool {
		for _, prefix := range tempPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}

	var usage Usage
	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == rootDir {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if skipTemp(name) {
				return filepath.SkipDir
			}
			return nil
		}
		if skipTemp(name) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat blob %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		usage.Bytes += info.Size()
		usage.Objects++
		return nil
	})
	if err != nil {
		return Usage{}, fmt.Errorf("walk blob root: %w", err)
	}

	return usage, nil
}
