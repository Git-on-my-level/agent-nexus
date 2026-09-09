package observation

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const seatbeltSandboxPath = "/usr/bin/sandbox-exec"
const seatbeltShellPath = "/bin/sh"

// seatbeltTempRoot is a host directory that is not covered by the profile's
// /Users and /Volumes content-read denials. This host's TMPDIR is
// /Volumes/scratch/tmp, so os.MkdirTemp("") would place the artifact and
// scratch on a denied volume and flake under load when path aliasing differs.
func seatbeltTempRoot() string {
	for _, dir := range []string{"/private/tmp", "/tmp"} {
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil || !filepath.IsAbs(resolved) {
			continue
		}
		if strings.HasPrefix(resolved, "/Volumes") || strings.HasPrefix(resolved, "/Users") {
			continue
		}
		if st, err := os.Stat(resolved); err == nil && st.IsDir() {
			return resolved
		}
	}
	return "/private/tmp"
}

func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err = os.WriteFile(dst, data, 0500); err != nil {
		return err
	}
	return os.Chmod(dst, 0500)
}

// SeatbeltRunner is the Darwin production executable backend. Generated code
// runs under sandbox-exec with a deny-default profile: process-exec of the
// reader artifact only, scratch writes, no network, no process-fork.
//
// Seatbelt cannot enforce memory, CPU, nproc, file size, wall timeout, or
// output bytes. Darwin rejects RLIMIT_AS/DATA/RSS (Invalid argument) and
// RLIMIT_NPROC is user-global, so this runner must not set ulimit -u.
// Enforced here: Seatbelt path/network/fork policy; ulimit CPU, file size,
// open files, core; Go wall timeout and output-byte cap.
type SeatbeltRunner struct {
	sandbox string
	sh      string
	once    sync.Once
	avail   error
}

func NewSeatbeltRunner() *SeatbeltRunner {
	find := func(path string) string {
		s, err := os.Stat(path)
		if err == nil && s.Mode().IsRegular() && s.Mode().Perm()&0111 != 0 {
			return path
		}
		return ""
	}
	return &SeatbeltRunner{sandbox: find(seatbeltSandboxPath), sh: find(seatbeltShellPath)}
}

func (r *SeatbeltRunner) Available() error {
	r.once.Do(func() { r.avail = r.probe() })
	return r.avail
}

func (r *SeatbeltRunner) probe() error {
	if runtime.GOOS != "darwin" || r.sandbox == "" || r.sh == "" {
		return failure(ErrIsolation, "generated executable readers require macOS sandbox-exec; no host-execution fallback")
	}
	if os.Geteuid() == 0 {
		return failure(ErrIsolation, "generated readers require a dedicated unprivileged runner account")
	}
	truePath, err := filepath.EvalSymlinks("/usr/bin/true")
	if err != nil || !filepath.IsAbs(truePath) {
		return failure(ErrIsolation, "sandbox-exec availability probe cannot resolve /usr/bin/true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	deny := exec.CommandContext(ctx, r.sandbox, "-p", "(version 1)(deny default)", truePath)
	deny.Env = []string{"PATH=/", "LANG=C"}
	if err := deny.Run(); err == nil {
		return failure(ErrIsolation, "sandbox-exec deny-default probe executed; isolation is not enforced")
	}
	allow := exec.CommandContext(ctx, r.sandbox, "-p", seatbeltProfile(truePath, ""), truePath)
	allow.Env = []string{"PATH=/", "LANG=C"}
	if err := allow.Run(); err != nil || ctx.Err() != nil {
		return failure(ErrIsolation, "sandbox-exec cannot exec a deny-default probe")
	}
	return nil
}

func sbLiteral(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\"\\\n\x00") {
		return "", failure(ErrPolicy, "sandbox path is not a safe absolute literal")
	}
	return path, nil
}

// seatbeltProfile is the exact production profile. file-read* is required for
// dyld shared-cache mapping on this macOS; content reads of operator homes,
// /Volumes (except the artifact and scratch literals), credentials and
// keychains are denied. Writes are scratch-only. Network and fork are denied.
func seatbeltProfile(artifact, scratch string) string {
	artifact, err := sbLiteral(artifact)
	if err != nil {
		return "(version 1)(deny default)"
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process-exec* (literal \"" + artifact + "\"))\n")
	b.WriteString("(allow signal)\n")
	b.WriteString("(allow file-read*)\n")
	b.WriteString("(allow file-map-executable)\n")
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n")
	b.WriteString("(allow mach-priv-host-port)\n")
	b.WriteString("(allow ipc-posix-shm)\n")
	b.WriteString("(allow file-ioctl (literal \"/dev/null\") (literal \"/dev/urandom\") (literal \"/dev/dtracehelper\"))\n")
	if scratch != "" {
		if scratch, err = sbLiteral(scratch); err != nil {
			return "(version 1)(deny default)"
		}
		b.WriteString("(allow file-write* (subpath \"" + scratch + "\"))\n")
	}
	b.WriteString("(deny file-read-data (subpath \"/Users\") (subpath \"/Volumes\") (subpath \"/Applications\") (subpath \"/opt\") (subpath \"/private/etc\") (subpath \"/private/var/root\") (subpath \"/Library/Keychains\"))\n")
	b.WriteString("(allow file-read-data (literal \"" + artifact + "\"))\n")
	if scratch != "" {
		b.WriteString("(allow file-read-data (subpath \"" + scratch + "\"))\n")
	}
	b.WriteString("(deny network*)\n")
	b.WriteString("(deny process-fork)\n")
	return b.String()
}

func (r *SeatbeltRunner) Run(ctx context.Context, artifact string, input []byte, l IsolationLimits) ([]byte, error) {
	if err := r.Available(); err != nil {
		return nil, err
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(artifact) || int64(len(input)) > l.InputBytes {
		return nil, failure(ErrLimit, "invalid artifact path or input budget exceeded")
	}
	resolved, err := filepath.EvalSymlinks(artifact)
	if err != nil {
		return nil, failure(ErrPolicy, "generated executable unavailable or replaced")
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil, failure(ErrPolicy, "generated executable unavailable or replaced")
	}
	root, err := os.MkdirTemp(seatbeltTempRoot(), "anx-jit-seatbelt-")
	if err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	defer os.RemoveAll(root)
	if err = os.Chmod(root, 0700); err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	copied := filepath.Join(root, "reader")
	if err = copyExecutable(resolved, copied); err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	resolved, err = filepath.EvalSymlinks(copied)
	if err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	scratch := filepath.Join(root, "scratch")
	if err = os.Mkdir(scratch, 0700); err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	scratch, err = filepath.EvalSymlinks(scratch)
	if err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	profile := seatbeltProfile(resolved, scratch)
	profilePath := filepath.Join(root, "profile.sb")
	if err = os.WriteFile(profilePath, []byte(profile), 0400); err != nil {
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	blocks := l.FileBytes / 512
	if l.FileBytes%512 != 0 {
		blocks++
	}
	if blocks < 1 {
		blocks = 1
	}
	// Trusted wrapper (like Linux prlimit): apply per-process rlimits that
	// Darwin accepts, then exec sandbox-exec. The generated reader is the
	// sandboxed command, never this shell.
	script := `ulimit -t "$3" && ulimit -f "$4" && ulimit -n 32 && ulimit -c 0 && exec "$5" -f "$1" "$2"`
	ctx, cancel := context.WithTimeout(ctx, l.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.sh, "-c", script, "seatbelt", profilePath, resolved, strconv.Itoa(l.CPUSeconds), strconv.FormatInt(blocks, 10), r.sandbox)
	cmd.Env = []string{"PATH=/", "LANG=C", "HOME=" + scratch, "TMPDIR=" + scratch, "TMP=" + scratch, "TEMP=" + scratch}
	cmd.Dir = scratch
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
		return nil, failure(ErrIsolation, "isolated reader or sandbox setup failed")
	}
	return stdout.Bytes(), nil
}
