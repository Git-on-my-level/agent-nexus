package observation

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSHConfig is an approved exact host/repository binding. Keys stay in their
// existing operator-owned location; no SSH config, agent forwarding or proxying.
type SSHConfig struct {
	WorkspaceID    string
	ConnectionID   string
	Host           string
	Repository     string
	KnownHostsFile string
	IdentityFile   string
	Port           int
	Timeout        time.Duration
	MaxBytes       int64
}
type SSHGitReader struct{ config SSHConfig }

var sshHost = regexp.MustCompile(`^([A-Za-z0-9_][A-Za-z0-9_.-]*@)?[A-Za-z0-9][A-Za-z0-9.-]*$`)

func NewSSHGitReader(c SSHConfig) (*SSHGitReader, error) {
	if c.Timeout == 0 {
		c.Timeout = 20 * time.Second
	}
	if c.MaxBytes == 0 {
		c.MaxBytes = 65536
	}
	if c.Port == 0 {
		c.Port = 22
	}
	if c.WorkspaceID == "" || c.ConnectionID == "" || !sshHost.MatchString(c.Host) || !approvedPath(c.Repository) || !approvedPath(c.KnownHostsFile) || (c.IdentityFile != "" && !approvedPath(c.IdentityFile)) || c.Port < 1 || c.Port > 65535 || c.Timeout <= 0 || c.Timeout > time.Minute || c.MaxBytes < 1 || c.MaxBytes > 2<<20 {
		return nil, failure(ErrConfiguration, "SSH needs exact approved host, absolute repository and known_hosts paths, and bounded resources")
	}
	return &SSHGitReader{config: c}, nil
}
func approvedPath(p string) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && p != "/" && !strings.ContainsAny(p, "\x00\r\n")
}
func (*SSHGitReader) Capabilities() Capabilities {
	return Capabilities{Source: "ssh_git", ReadOne: true, TrustedBuiltin: true, EvidenceKinds: []string{"git_revision", "working_tree"}, Limitations: []string{"Git state is not deployment or acceptance proof; requires pre-provisioned known_hosts and a scoped account; tracked-file status only"}}
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
func (r *SSHGitReader) arguments(t Target) ([]string, error) {
	c := r.config
	if t.Validate() != nil || t.WorkspaceID != c.WorkspaceID || t.ConnectionID != c.ConnectionID || t.Source != "ssh_git" || t.Kind != "repository" || t.Host != c.Host || t.Path != c.Repository {
		return nil, failure(ErrPermission, "SSH target differs from approved host and repository")
	}
	identity := c.IdentityFile
	if identity == "" {
		identity = "none"
	}
	args := []string{"-F", "/dev/null", "-T", "-p", strconv.Itoa(c.Port), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + c.KnownHostsFile, "-o", "GlobalKnownHostsFile=/dev/null", "-o", "UpdateHostKeys=no", "-o", "ClearAllForwardings=yes", "-o", "ForwardAgent=no", "-o", "ForwardX11=no", "-o", "IdentityAgent=none", "-o", "IdentitiesOnly=yes", "-o", "IdentityFile=" + identity, "-o", "PasswordAuthentication=no", "-o", "KbdInteractiveAuthentication=no", "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=1", c.Host}
	git := "env -i PATH=/usr/bin:/bin GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_OPTIONAL_LOCKS=0 git --no-pager --no-optional-locks -c core.fsmonitor=false -c log.showSignature=false -C " + shellQuote(c.Repository)
	command := git + " log -1 --no-show-signature --format='%H%n%cI' HEAD && " + git + " status --porcelain=v1 --untracked-files=no --ignore-submodules=all"
	return append(args, command), nil
}
func (r *SSHGitReader) Read(ctx context.Context, t Target) (Report, error) {
	args, err := r.arguments(t)
	if err != nil {
		return Report{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.config.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/ssh", args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.WaitDelay = time.Second
	stdout := &boundedBuffer{limit: r.config.MaxBytes}
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if stdout.exceeded || stderr.exceeded {
			return Report{}, failure(ErrLimit, "SSH output exceeded limit")
		}
		return Report{}, failure(ErrUnavailable, "approved SSH repository read failed or timed out")
	}
	return parseGitOutput(t, stdout.Bytes())
}
func parseGitOutput(t Target, raw []byte) (Report, error) {
	parts := strings.SplitN(string(raw), "\n", 3)
	if len(parts) < 3 || !validSHA(parts[0]) || stamp(parts[1]) == nil {
		return Report{}, failure(ErrInvalidOutput, "SSH reader returned invalid git evidence")
	}
	out := newReport(t, "builtin:ssh-git")
	out.SourceRevision = parts[0]
	out.SourceActivityAt = stamp(parts[1])
	out.SourceUpdatedAt = out.SourceActivityAt
	// The transport principal is provenance, not a credential in an evidence URL.
	host := t.Host
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	u := url.URL{Scheme: "ssh", Host: host, Path: t.Path}
	out.URL = u.String()
	out.Facts = map[string]any{"commit": parts[0], "working_tree_dirty": strings.TrimSpace(parts[2]) != "", "host": t.Host, "repository": t.Path}
	out.Evidence = []Evidence{{Kind: "git_revision", Reference: out.URL, Revision: parts[0], Knowledge: "reported", Summary: "Repository HEAD observed; deployment not verified"}}
	out.Coverage = Coverage{Complete: true, Pages: 1, Limitations: []string{"Tracked files only; commit time is not deployment time or acceptance evidence"}}
	return finishReport(out)
}

// boundedBuffer stops subprocess pipe consumption after a fixed output budget.
// Returning an error closes the pipe; CommandContext supplies the wall deadline.
type boundedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int64
	exceeded bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.buf.Len())
	if int64(len(p)) > remaining {
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.exceeded = true
		return 0, io.ErrShortWrite
	}
	return b.buf.Write(p)
}
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}
