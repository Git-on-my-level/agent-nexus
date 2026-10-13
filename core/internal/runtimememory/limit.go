// Package runtimememory owns process memory policy and isolated operator diagnostics.
// It deliberately has no database, authentication, or workspace dependencies.
package runtimememory

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

// Configure sets a startup-only soft Go limit. An explicitly present GOMEMLIMIT
// (including "off") is owned by the operator and must never be overridden.
func Configure(log io.Writer) int64 {
	if _, explicit := os.LookupEnv("GOMEMLIMIT"); explicit {
		fmt.Fprintf(log, "memory limit: explicit GOMEMLIMIT preserved (bytes=%d)\n", debug.SetMemoryLimit(-1))
		return 0
	}
	if runtime.GOOS != "linux" {
		return 0
	}
	limit := cgroupLimit(os.ReadFile)
	if limit == 0 {
		fmt.Fprintln(log, "memory limit: no finite cgroup limit; Go default unchanged")
		return 0
	}
	// Divide before multiplying to avoid overflow. Leave 30% for SQLite's
	// native allocator and other container costs outside Go accounting.
	soft := limit/10*7 + limit%10*7/10
	debug.SetMemoryLimit(soft)
	fmt.Fprintf(log, "memory limit: cgroup_bytes=%d Go_soft_bytes=%d ratio=0.70\n", limit, soft)
	return soft
}

func finiteLimit(raw []byte) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	// v1's unlimited sentinel is close to MaxInt64; v2 uses "max".
	if err != nil || n <= 0 || n >= 1<<60 {
		return 0
	}
	return n
}

func cgroupLimit(read func(string) ([]byte, error)) int64 {
	roots := []struct{ root, file string }{
		{"/sys/fs/cgroup", "memory.max"},
		{"/sys/fs/cgroup/memory", "memory.limit_in_bytes"},
		{"/sys/fs/cgroup", "memory.limit_in_bytes"},
	}
	var limit int64
	check := func(path string) {
		if raw, err := read(path); err == nil {
			if n := finiteLimit(raw); n != 0 && (limit == 0 || n < limit) {
				limit = n
			}
		}
	}
	for _, root := range roots {
		check(filepath.Join(root.root, root.file))
	}
	// Containers normally expose their own cgroup as the mount root. On a
	// self-hosted systemd service also consider its subgroup and ancestors.
	if raw, err := read("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) != 3 {
				continue
			}
			for _, root := range roots {
				v2 := root.file == "memory.max"
				if v2 && parts[1] != "" || !v2 && !strings.Contains(","+parts[1]+",", ",memory,") {
					continue
				}
				path := filepath.Join(root.root, strings.TrimPrefix(filepath.Clean("/"+parts[2]), "/"))
				for path != root.root {
					check(filepath.Join(path, root.file))
					path = filepath.Dir(path)
				}
			}
		}
	}
	return limit
}
