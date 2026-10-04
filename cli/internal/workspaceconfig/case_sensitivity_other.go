//go:build !darwin

package workspaceconfig

func filesystemCaseInsensitive(path string) (bool, bool) {
	return false, false // Use the portable filesystem-identity probe.
}
