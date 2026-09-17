package observation

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	// No mounts of host root, home, credentials, sockets or runtime libraries.
	// Static ELF only is enforced again by the manager before every invocation.
	args := []string{"--as=" + strconv.FormatInt(l.MemoryBytes, 10), "--cpu=" + strconv.Itoa(l.CPUSeconds), "--nproc=" + strconv.Itoa(l.Processes), "--fsize=" + strconv.FormatInt(l.FileBytes, 10), "--nofile=32", "--core=0", "--", r.bwrap, "--unshare-all", "--unshare-user", "--disable-userns", "--assert-userns-disabled", "--die-with-parent", "--new-session", "--clearenv", "--cap-drop", "ALL", "--size", "1048576", "--tmpfs", "/", "--ro-bind", artifact, "/reader", "--dir", "/dev", "--ro-bind", "/dev/null", "/dev/null", "--ro-bind", "/dev/urandom", "/dev/urandom", "--dir", "/tmp", "--remount-ro", "/", "--chdir", "/tmp", "--setenv", "PATH", "/", "--setenv", "LANG", "C", "--", "/reader"}
	ctx, cancel := context.WithTimeout(ctx, l.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.prlimit, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: l.OutputBytes}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if stdout.exceeded || stderr.exceeded {
			return nil, failure(ErrLimit, "isolated reader output limit exceeded")
		}
		if ctx.Err() != nil {
			return nil, failure(ErrLimit, "isolated reader exceeded deadline")
		}
		// Failed sandbox setup and reader errors both deny validation/activation. Do not
		// leak raw stderr; source snapshots can contain confidential text.
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	return stdout.Bytes(), nil
}
