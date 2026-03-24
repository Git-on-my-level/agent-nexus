package blob

import (
	"path"
	"path/filepath"
	"strings"
)

func contentAddressedRelativePath(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) < 4 {
		return hash
	}
	return path.Join(hash[:2], hash[2:4], hash)
}

func contentAddressedFilesystemPath(rootDir, hash string) string {
	relative := contentAddressedRelativePath(hash)
	if relative == "" {
		return filepath.Clean(rootDir)
	}
	return filepath.Join(rootDir, filepath.FromSlash(relative))
}
