package observation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Envelope is deliberately narrow in v1. Network, credentials, dependencies and
// host paths are broker-only. Generated code receives a bounded JSON snapshot.
type Envelope struct {
	ReadPaths         []string `json:"read_paths,omitempty"`
	ScratchPaths      []string `json:"scratch_paths,omitempty"`
	NetworkHosts      []string `json:"network_hosts,omitempty"`
	CredentialHandles []string `json:"credential_handles,omitempty"`
	Dependencies      []string `json:"dependencies,omitempty"`
}
type IsolationLimits struct {
	Timeout     time.Duration `json:"timeout"`
	MemoryBytes int64         `json:"memory_bytes"`
	OutputBytes int64         `json:"output_bytes"`
	InputBytes  int64         `json:"input_bytes"`
	CPUSeconds  int           `json:"cpu_seconds"`
	Processes   int           `json:"processes"`
	FileBytes   int64         `json:"file_bytes"`
}

func (l IsolationLimits) Validate() error {
	if l.Timeout <= 0 || l.Timeout > time.Minute || l.MemoryBytes < 16<<20 || l.MemoryBytes > 1<<30 || l.OutputBytes <= 0 || l.OutputBytes > 2<<20 || l.InputBytes <= 0 || l.InputBytes > 2<<20 || l.CPUSeconds < 1 || l.CPUSeconds > 60 || l.Processes < 1 || l.Processes > 64 || l.FileBytes <= 0 || l.FileBytes > 16<<20 {
		return failure(ErrConfiguration, "generated reader requires bounded time, memory, I/O, CPU, processes and files")
	}
	return nil
}

type isolatedExecutor interface {
	Available() error
	Run(context.Context, string, []byte, IsolationLimits) ([]byte, error)
}

// NewIsolatedRunner selects the enforced host sandbox. Linux uses bubblewrap;
// Darwin uses Seatbelt. Any other OS fails closed. A directory is not a sandbox.
func NewIsolatedRunner() isolatedExecutor {
	if runtime.GOOS == "darwin" {
		return NewSeatbeltRunner()
	}
	return NewBubblewrapRunner()
}

// BubblewrapRunner is the Linux production executable backend. No shell fallback.
// A rootless Linux host with user namespaces, bubblewrap and prlimit is required.
type BubblewrapRunner struct {
	bwrap   string
	prlimit string
}

func NewBubblewrapRunner() *BubblewrapRunner {
	// Resolve only standard operator-installed paths, never an agent-writable PATH.
	find := func(name string) string {
		for _, dir := range []string{"/usr/bin", "/bin"} {
			p := filepath.Join(dir, name)
			if s, err := os.Stat(p); err == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0 {
				return p
			}
		}
		return ""
	}
	return &BubblewrapRunner{bwrap: find("bwrap"), prlimit: find("prlimit")}
}
func (r *BubblewrapRunner) Available() error {
	if runtime.GOOS != "linux" || r.bwrap == "" || r.prlimit == "" {
		return failure(ErrIsolation, "generated executable readers require Linux bubblewrap and prlimit; no host-execution fallback")
	}
	if os.Geteuid() == 0 {
		return failure(ErrIsolation, "generated readers require a dedicated unprivileged runner account")
	}
	return nil
}
func (r *BubblewrapRunner) Run(ctx context.Context, artifact string, input []byte, l IsolationLimits) ([]byte, error) {
	if err := r.Available(); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(artifact) || int64(len(input)) > l.InputBytes {
		return nil, failure(ErrLimit, "invalid artifact path or input budget exceeded")
	}
	// RLIMIT_NPROC is counted per real UID, across the whole host user namespace,
	// so it cannot be set on bwrap: the runner account's own threads (this Go
	// process alone has more than any sane reader budget) make bwrap's namespace
	// clone fail with EAGAIN. bwrap reports the sandbox pid on --info-fd and then
	// blocks on --block-fd after all namespace setup, before it forks the reader.
	// We set the process limit on that blocked pid only. Inside the sandbox's own
	// user namespace the count starts at zero, so the budget is per sandbox.
	infoR, infoW, err := os.Pipe()
	if err != nil {
		return nil, failure(ErrIsolation, "sandbox setup failed: info pipe")
	}
	defer infoR.Close()
	blockR, blockW, err := os.Pipe()
	if err != nil {
		infoW.Close()
		return nil, failure(ErrIsolation, "sandbox setup failed: block pipe")
	}
	defer blockW.Close()
	// No mounts of host root, home, credentials, sockets or runtime libraries.
	// Static ELF only is enforced again by the manager before every invocation.
	// ExtraFiles start at fd 3: fd 3 is the info pipe, fd 4 the block pipe.
	args := []string{"--as=" + strconv.FormatInt(l.MemoryBytes, 10), "--cpu=" + strconv.Itoa(l.CPUSeconds), "--fsize=" + strconv.FormatInt(l.FileBytes, 10), "--nofile=32", "--core=0", "--", r.bwrap, "--unshare-all", "--unshare-user", "--disable-userns", "--assert-userns-disabled", "--die-with-parent", "--new-session", "--clearenv", "--cap-drop", "ALL", "--info-fd", "3", "--block-fd", "4", "--size", "1048576", "--tmpfs", "/", "--ro-bind", artifact, "/reader", "--dir", "/dev", "--ro-bind", "/dev/null", "/dev/null", "--ro-bind", "/dev/urandom", "/dev/urandom", "--dir", "/tmp", "--remount-ro", "/", "--chdir", "/tmp", "--setenv", "PATH", "/", "--setenv", "LANG", "C", "--", "/reader"}
	ctx, cancel := context.WithTimeout(ctx, l.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.prlimit, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(input)
	cmd.ExtraFiles = []*os.File{infoW, blockR}
	stdout := &boundedBuffer{limit: l.OutputBytes}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Start()
	infoW.Close()
	blockR.Close()
	if err != nil {
		return nil, failure(ErrIsolation, "sandbox setup failed: cannot start bubblewrap")
	}
	if err := r.limitSandboxProcesses(ctx, infoR, l.Processes); err != nil {
		// Kill before blockW closes: bwrap ignores the read result, so EOF on the
		// block pipe would release the reader without its process limit.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, sandboxSetupFailure(err, stderr)
	}
	if _, err := blockW.Write([]byte{0}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, sandboxSetupFailure(err, stderr)
	}
	blockW.Close()
	if err := cmd.Wait(); err != nil {
		if stdout.exceeded || stderr.exceeded {
			return nil, failure(ErrLimit, "isolated reader output limit exceeded")
		}
		if ctx.Err() != nil {
			return nil, failure(ErrLimit, "isolated reader exceeded deadline")
		}
		// The reader has run, so stderr may carry its output. Do not leak it;
		// source snapshots can contain confidential text.
		return nil, failure(ErrIsolation, "isolated reader failed")
	}
	return stdout.Bytes(), nil
}

// limitSandboxProcesses waits for bwrap's sandbox pid and caps its tasks. The
// budget is the reader's processes and threads plus bwrap's pid-1 reaper.
func (r *BubblewrapRunner) limitSandboxProcesses(ctx context.Context, info io.Reader, processes int) error {
	raw, err := io.ReadAll(io.LimitReader(info, 4096))
	if err != nil {
		return errors.New("read sandbox info")
	}
	var parsed struct {
		ChildPID int `json:"child-pid"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed.ChildPID <= 1 {
		return errors.New("bubblewrap exited before reporting the sandbox pid")
	}
	limit := strconv.Itoa(processes + 1)
	cmd := exec.CommandContext(ctx, r.prlimit, "--pid", strconv.Itoa(parsed.ChildPID), "--nproc="+limit+":"+limit)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set sandbox process limit: %v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// sandboxSetupFailure reports a failure that happened before the reader was
// released. Only bubblewrap and prlimit have written to stderr at that point,
// so their diagnostics are safe to surface and are what operators need.
func sandboxSetupFailure(cause error, stderr *boundedBuffer) error {
	msg := "sandbox setup failed: " + cause.Error()
	if detail := strings.TrimSpace(string(stderr.Bytes())); detail != "" {
		msg += ": " + detail
	}
	return failure(ErrIsolation, msg)
}
