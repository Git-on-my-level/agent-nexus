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

// seatbeltTempRoot stages artifacts under a stable, canonical local path.
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
	// Separate budgets: a shared 3s context made the allow probe flake when
	// deny was slow under full-suite load (parallel sandbox-exec).
	denyCtx, denyCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer denyCancel()
	deny := exec.CommandContext(denyCtx, r.sandbox, "-p", "(version 1)(deny default)", truePath)
	deny.Env = []string{"PATH=/", "LANG=C"}
	if err := deny.Run(); err == nil {
		return failure(ErrIsolation, "sandbox-exec deny-default probe executed; isolation is not enforced")
	}
	allowCtx, allowCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer allowCancel()
	allow := exec.CommandContext(allowCtx, r.sandbox, "-p", seatbeltProfile(truePath, ""), truePath)
	allow.Env = []string{"PATH=/", "LANG=C"}
	if err := allow.Run(); err != nil || allowCtx.Err() != nil {
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

// seatbeltProfile allows only the compiled reader, its scratch, and the dyld
// runtime. No Mach services are needed by the reader ABI, so mach-lookup stays
// denied. Adding a runtime capability requires a specific allowlist entry and
// conformance evidence; the availability probe never falls back to host execution.
func seatbeltProfile(artifact, scratch string) string {
	artifact, err := sbLiteral(artifact)
	if err != nil {
		return "(version 1)(deny default)"
	}
	if scratch != "" {
		if scratch, err = sbLiteral(scratch); err != nil {
			return "(version 1)(deny default)"
		}
	}
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process-exec* (literal \"" + artifact + "\"))\n")
	b.WriteString("(allow signal (target self))\n")
	// System libraries and the shared cache, including current Cryptex layouts.
	libraries := []string{"/usr/lib", "/System/Library/dyld",
		"/System/Volumes/Preboot/Cryptexes/OS/usr/lib",
		"/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld",
		"/private/preboot/Cryptexes/OS/usr/lib",
		"/private/preboot/Cryptexes/OS/System/Library/dyld"}
	filters := "(literal \"" + artifact + "\")"
	for _, path := range libraries {
		filters += " (subpath \"" + path + "\")"
	}
	b.WriteString("(allow file-read* " + filters + ")\n")
	b.WriteString("(allow file-map-executable " + filters + ")\n")
	// macOS 26 dyld needs a literal root content read to start a process.
	// This is not a subpath grant; other ancestors need only lookup metadata.
	b.WriteString("(allow file-read* (literal \"/\"))\n")
	seen := map[string]bool{"/": true}
	paths := append(append([]string{}, libraries...), artifact)
	if scratch != "" {
		paths = append(paths, scratch)
	}
	for _, path := range paths {
		for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
			if !seen[parent] {
				b.WriteString("(allow file-read-metadata (literal \"" + parent + "\"))\n")
				seen[parent] = true
			}
			if parent == "/" {
				break
			}
		}
	}
	// Go's Darwin runtime reads CTL_HW/HW_PAGESIZE via numeric MIB, which
	// a sysctl-name allowlist does not satisfy on macOS 26. Permit runtime
	// sysctl reads, but explicitly deny process metadata/argv/environment.
	// Known limit, verified on macOS 26.6.2: Seatbelt's sysctl filters see
	// names, not numeric MIBs, and a same-uid numeric KERN_PROCARGS2 read
	// succeeds under every variant of this rule (named allowlist included).
	// The by-name denials below hold; argv/environment of other same-uid
	// processes is not protected by this profile. Do not keep secrets in the
	// environment of long-lived same-uid processes on a host that runs
	// generated readers.
	b.WriteString("(allow sysctl-read)\n")
	b.WriteString("(deny sysctl-read (sysctl-name \"kern.procargs2\"))\n")
	b.WriteString("(deny sysctl-read (sysctl-name-prefix \"kern.proc\"))\n")
	b.WriteString("(allow file-read* (literal \"/dev/null\") (literal \"/dev/urandom\") (literal \"/dev/random\"))\n")
	b.WriteString("(allow file-ioctl (literal \"/dev/null\"))\n")
	if scratch != "" {
		b.WriteString("(allow file-read* file-write* (subpath \"" + scratch + "\"))\n")
	}
	b.WriteString("(deny network*)\n(deny process-fork)\n")
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
