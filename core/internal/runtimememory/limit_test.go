package runtimememory

import (
	"bytes"
	"os"
	"runtime/debug"
	"testing"
)

func TestCgroupLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  int64
	}{
		{"v2", map[string]string{"/sys/fs/cgroup/memory.max": "1073741824\n"}, 1 << 30},
		{"v1", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "1073741824"}, 1 << 30},
		{"v1-flat", map[string]string{"/sys/fs/cgroup/memory.limit_in_bytes": "536870912"}, 1 << 29},
		{"unlimited-v2", map[string]string{"/sys/fs/cgroup/memory.max": "max"}, 0},
		{"unlimited-v1", map[string]string{"/sys/fs/cgroup/memory/memory.limit_in_bytes": "9223372036854771712"}, 0},
		{"missing", nil, 0},
		{"invalid", map[string]string{"/sys/fs/cgroup/memory.max": "-1"}, 0},
		{"overflow", map[string]string{"/sys/fs/cgroup/memory.max": "99999999999999999999999999999"}, 0},
		{"systemd-ancestor", map[string]string{"/sys/fs/cgroup/memory.max": "max", "/proc/self/cgroup": "0::/system.slice/anx.service", "/sys/fs/cgroup/system.slice/memory.max": "536870912", "/sys/fs/cgroup/system.slice/anx.service/memory.max": "1073741824"}, 1 << 29},
		{"v1-subgroup", map[string]string{"/proc/self/cgroup": "5:cpu,memory:/anx", "/sys/fs/cgroup/memory/anx/memory.limit_in_bytes": "536870912"}, 1 << 29},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(path string) ([]byte, error) {
				if value, ok := tc.files[path]; ok {
					return []byte(value), nil
				}
				return nil, os.ErrNotExist
			}
			if got := cgroupLimit(read); got != tc.want {
				t.Fatalf("got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestExplicitLimitWins(t *testing.T) {
	old := debug.SetMemoryLimit(96 << 20)
	defer debug.SetMemoryLimit(old)
	for _, value := range []string{"96MiB", "off", ""} {
		t.Setenv("GOMEMLIMIT", value)
		var log bytes.Buffer
		if got := Configure(&log); got != 0 || debug.SetMemoryLimit(-1) != 96<<20 {
			t.Fatal("explicit setting overridden")
		}
	}
}
