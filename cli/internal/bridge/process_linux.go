//go:build linux

package bridge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func Processes(ctx context.Context) ([]Process, error) {
	dirs, e := os.ReadDir("/proc")
	if e != nil {
		return nil, e
	}
	out := []Process{}
	for _, d := range dirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, e := strconv.Atoi(d.Name())
		if e != nil {
			continue
		}
		root := filepath.Join("/proc", d.Name())
		info, e := os.Stat(root)
		if e != nil {
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(root, "cmdline"))
		if e != nil || len(raw) == 0 {
			continue
		}
		argv := []string{}
		for _, arg := range bytes.Split(bytes.TrimSuffix(raw, []byte{0}), []byte{0}) {
			argv = append(argv, string(arg))
		}
		stat, e := os.ReadFile(filepath.Join(root, "stat"))
		if e != nil {
			continue
		}
		end := strings.LastIndexByte(string(stat), ')')
		if end < 0 {
			continue
		}
		fields := strings.Fields(string(stat[end+1:]))
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		exe, _ := os.Readlink(filepath.Join(root, "exe"))
		cwd, _ := os.Readlink(filepath.Join(root, "cwd"))
		out = append(out, Process{pid, ppid, int(st.Uid), fields[19], exe, argv, cwd})
	}
	return out, nil
}

func unitOwned(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
