package workspaceconfig

import "syscall"

func filesystemCaseInsensitive(path string) (bool, bool) {
	// Darwin's <sys/unistd.h> defines _PC_CASE_SENSITIVE as 11. Query the
	// target volume itself, including when cwd is a mounted volume's root.
	const pcCaseSensitive = 11
	value, err := syscall.Pathconf(path, pcCaseSensitive)
	return value == 0, err == nil && value >= 0
}
