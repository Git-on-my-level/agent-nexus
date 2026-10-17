//go:build darwin

package bridge

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func Processes(ctx context.Context) ([]Process, error) {
	out, e := exec.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,uid=,lstart=").Output()
	if e != nil {
		return nil, e
	}
	all := []Process{}
	for _, line := range strings.Split(string(out), "\n") {
		v := strings.Fields(line)
		if len(v) < 8 {
			continue
		}
		pid, _ := strconv.Atoi(v[0])
		ppid, _ := strconv.Atoi(v[1])
		uid, _ := strconv.Atoi(v[2])
		raw, e := unix.SysctlRaw("kern.procargs2", pid)
		if e != nil {
			continue
		}
		if len(raw) < 4 {
			continue
		}
		argc := int(binary.LittleEndian.Uint32(raw[:4]))
		if argc < 1 || argc > 4096 {
			continue
		}
		raw = raw[4:]
		end := bytes.IndexByte(raw, 0)
		if end < 0 {
			continue
		}
		exe := string(raw[:end])
		raw = bytes.TrimLeft(raw[end:], "\x00")
		argv := []string{}
		for len(argv) < argc {
			end = bytes.IndexByte(raw, 0)
			if end < 0 {
				break
			}
			argv = append(argv, string(raw[:end]))
			raw = raw[end+1:]
		}
		if len(argv) != argc {
			continue
		}
		p := Process{PID: pid, PPID: ppid, UID: uid, Started: strings.Join(v[3:], " "), Executable: exe, Argv: argv}
		if IsConsumer(argv) {
			identity, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
			if err != nil {
				return nil, fmt.Errorf("cannot verify bridge PID %d start time", pid)
			}
			p.Started = fmt.Sprintf("%d.%06d", identity.Proc.P_starttime.Sec, identity.Proc.P_starttime.Usec)
			p.PPID = int(identity.Eproc.Ppid)

			cwd, e := exec.CommandContext(ctx, "/usr/sbin/lsof", "-a", "-p", fmt.Sprint(pid), "-d", "cwd", "-Fn").Output()
			if e != nil {
				return nil, fmt.Errorf("cannot verify bridge PID %d cwd", pid)
			}
			for _, line := range strings.Split(string(cwd), "\n") {
				if strings.HasPrefix(line, "n") {
					p.CWD = line[1:]
				}
			}
		}
		all = append(all, p)
	}
	return all, nil
}

func unitOwned(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
