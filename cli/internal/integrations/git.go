package integrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const CaptureCapacity = 100
const WalkPage = 100
const MaxWalk = 10000

type CaptureBinding struct {
	RepoID    string `json:"repo_uuid"`
	Workspace string `json:"base_url"` // resolved base URL including workspace prefix
	Host      string `json:"writer_host_id"`
	Actor     string `json:"writer_actor_id"`
	Agent     string `json:"writer_agent"`
}
type CaptureCursor struct {
	Anchor    string   `json:"anchor"`
	Snapshot  string   `json:"snapshot,omitempty"`
	Walk      []string `json:"walk"`
	Collected bool     `json:"collected"`
	Backfill  bool     `json:"backfill"`
}
type CaptureCheckpoint struct {
	SHA      string `json:"sha"`
	Sequence int64  `json:"sequence"`
}

// Accept numeric checkpoints written by earlier revisions of this unreleased
// feature. Their order survives, but backfill cannot infer a missing commit SHA.
func (c *CaptureCheckpoint) UnmarshalJSON(raw []byte) error {
	type checkpoint CaptureCheckpoint
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return json.Unmarshal(raw, (*checkpoint)(c))
	}
	return json.Unmarshal(raw, &c.Sequence)
}

type CaptureState struct {
	LegacyCheckpoint bool                     `json:"-"`
	Cursors          map[string]CaptureCursor `json:"worktrees"`

	Version           int                          `json:"schema_version"`
	Binding           CaptureBinding               `json:"binding"`
	Root              string                       `json:"root"`
	GitDir            string                       `json:"git_dir"`
	Enabled           bool                         `json:"enabled"`
	Anchor            string                       `json:"anchor"`
	Snapshot          string                       `json:"snapshot,omitempty"`
	Walk              []string                     `json:"walk"`
	Collected         bool                         `json:"collected"`
	Backfill          bool                         `json:"backfill"`
	Pending           []string                     `json:"pending"`
	Sequence          int64                        `json:"sequence"`
	PromiseCheckpoint map[string]CaptureCheckpoint `json:"promise_checkpoint"`
	CommitURL         string                       `json:"commit_url,omitempty"`
	HookPath          string                       `json:"hook_path,omitempty"`
	HookBody          string                       `json:"hook_body,omitempty"`
	ConfirmedSequence int64                        `json:"confirmed_sequence"`
	Confirmed         int                          `json:"confirmed"`
	Quarantined       int                          `json:"quarantined"`
}
type CommitCapture struct {
	Source       []byte    `json:"source_message,omitempty"` // private original bytes; never transmitted
	SHA          string    `json:"sha"`
	Card         string    `json:"card_ref"`
	CardID       string    `json:"card_id,omitempty"`
	Backfill     bool      `json:"backfill,omitempty"`
	Note         string    `json:"note"`
	Next         string    `json:"next,omitempty"`
	What         string    `json:"what,omitempty"`
	At           time.Time `json:"committed_at"`
	Sequence     int64     `json:"sequence"`
	Historical   bool      `json:"historical"`
	Delivery     *Delivery `json:"delivery,omitempty"`
	State        string    `json:"state"`
	Reason       string    `json:"reason,omitempty"`
	Attempts     int       `json:"attempts"`
	FirstAttempt time.Time `json:"first_attempt,omitempty"`
	RetryAt      time.Time `json:"retry_at,omitempty"`
}

func RandomID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}

// Bounded subprocess output; never uses a shell, remote, textconv or diff driver.
type cappedGitOutput struct {
	bytes.Buffer
	limit int
}

func (b *cappedGitOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("git output limit exceeded")
	}
	return b.Buffer.Write(p)
}

// Git hook subprocesses export repository-local variables. Clear the same
// localization inputs as `git rev-parse --local-env-vars` before using -C;
// otherwise an absolute GIT_DIR/index can silently select the caller's repo.
func GitEnvironment(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, value := range env {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX", "GIT_SHALLOW_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_INTERNAL_SUPER_PREFIX":
			continue
		}
		if strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		out = append(out, value)
	}
	return append(out, "GIT_NO_REPLACE_OBJECTS=1")
}

func Git(ctx context.Context, root string, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-C", root}, args...)...)
	cmd.WaitDelay = 100 * time.Millisecond
	var out = cappedGitOutput{limit: limit}
	cmd.Stdout = &out
	cmd.Stderr = &cappedGitOutput{limit: 4096}
	// No optional writes/interactive prompts; callers never fetch or request content filters.
	cmd.Env = append(GitEnvironment(os.Environ()), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	if e := cmd.Run(); e != nil {
		return nil, fmt.Errorf("git operation failed (%s): %w", args[0], e)
	}
	return out.Bytes(), nil
}
func Repo(ctx context.Context, path string) (root, gitdir string, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return
	}
	b, e := Git(ctx, path, 8192, "rev-parse", "--show-toplevel")
	if e != nil {
		err = e
		return
	}
	root = strings.TrimSpace(string(b))
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return
	}
	b, e = Git(ctx, root, 8192, "rev-parse", "--path-format=absolute", "--git-common-dir")
	gitdir = strings.TrimSpace(string(b))
	if e != nil {
		err = e
		return
	}
	gitdir, err = filepath.EvalSymlinks(gitdir)
	return
}
func CapturePath(dir, gitdir string) string {
	return filepath.Join(dir, "git", Digest(gitdir), "state.json")
}
func CaptureRecordPath(path, sha string) string {
	return filepath.Join(filepath.Dir(path), "commits", sha+".json")
}

// CaptureJSON snapshots the active worktree cursor while retaining the repo-wide
// writer, FIFO queue and replay records. All writers hold the common repo lock.
func CaptureJSON(s CaptureState) []byte {
	if s.Cursors == nil {
		s.Cursors = map[string]CaptureCursor{}
	}
	s.Cursors[s.Root] = CaptureCursor{s.Anchor, s.Snapshot, s.Walk, s.Collected, s.Backfill}
	return JSON(s)
}
func (s *CaptureState) SelectWorktree(root string) bool {
	if s.Root == root {
		return true
	}
	c, ok := s.Cursors[root]
	if !ok {
		return false
	}
	if s.Cursors == nil {
		s.Cursors = map[string]CaptureCursor{}
	}
	s.Cursors[s.Root] = CaptureCursor{s.Anchor, s.Snapshot, s.Walk, s.Collected, s.Backfill}
	s.Root = root
	s.Anchor, s.Snapshot, s.Walk, s.Collected, s.Backfill = c.Anchor, c.Snapshot, c.Walk, c.Collected, c.Backfill
	return true
}
func (s CaptureState) HasScan() bool {
	if s.Snapshot != "" {
		return true
	}
	for root, c := range s.Cursors {
		if root != s.Root && c.Snapshot != "" {
			return true
		}
	}
	return false
}
func LoadCapture(path string) (CaptureState, error) {
	var s CaptureState
	b, e := PrivateRead(path)
	if e != nil {
		return s, e
	}
	e = json.Unmarshal(b, &s)
	if e == nil && (s.Version != 1 || s.Binding.RepoID == "" || len(s.Pending) > CaptureCapacity || len(s.Walk) > MaxWalk || len(s.Cursors) > 100) {
		e = fmt.Errorf("invalid capture state")
	}
	for _, c := range s.Cursors {
		if len(c.Walk) > MaxWalk {
			return s, fmt.Errorf("invalid worktree cursor")
		}
	}
	if s.Cursors == nil {
		s.Cursors = map[string]CaptureCursor{}
	}
	if s.PromiseCheckpoint == nil {
		s.PromiseCheckpoint = map[string]CaptureCheckpoint{}
	}
	for _, checkpoint := range s.PromiseCheckpoint {
		if checkpoint.Sequence > 0 && checkpoint.SHA == "" {
			s.LegacyCheckpoint = true
		}
	}
	return s, e
}
func LoadCommit(path, sha string) (CommitCapture, error) {
	var c CommitCapture
	if !ObjectID(sha) {
		return c, fmt.Errorf("invalid object id")
	}
	b, e := PrivateRead(CaptureRecordPath(path, sha))
	if e != nil {
		return c, e
	}
	e = json.Unmarshal(b, &c)
	return c, e
}
func SaveCommit(path string, c CommitCapture) error {
	if !ObjectID(c.SHA) {
		return fmt.Errorf("invalid object id")
	}
	return PrivateWrite(CaptureRecordPath(path, c.SHA), JSON(c))
}
func ObjectID(s string) bool {
	return (len(s) == 40 || len(s) == 64) && strings.Trim(s, "0123456789abcdef") == ""
}
func ResolveCommit(ctx context.Context, root, ref string) (string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
		return "", fmt.Errorf("invalid revision")
	}
	b, e := Git(ctx, root, 256, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if e != nil {
		return "", e
	}
	sha := strings.TrimSpace(string(b))
	if !ObjectID(sha) {
		return "", fmt.Errorf("invalid object id")
	}
	return sha, nil
}
func ParseCommit(ctx context.Context, root, sha string) (CommitCapture, error) {
	c := CommitCapture{SHA: sha, State: "pending"}
	if !ObjectID(sha) {
		return c, fmt.Errorf("invalid object id")
	}
	raw, e := Git(ctx, root, MaxPayloadBytes+16384, "cat-file", "commit", sha)
	if e != nil {
		return c, e
	}
	head, msg, ok := strings.Cut(string(raw), "\n\n")
	if ok && len(msg) <= MaxPayloadBytes {
		c.Source = []byte(msg)
	}
	if !ok || len(msg) > MaxPayloadBytes || !utf8.ValidString(msg) || strings.ContainsRune(msg, 0) {
		return c, fmt.Errorf("invalid_commit_message")
	}
	for _, line := range strings.Split(head, "\n") {
		if strings.HasPrefix(line, "committer ") {
			f := strings.Fields(line)
			if len(f) < 3 {
				return c, fmt.Errorf("invalid_commit_time")
			}
			at, e := time.Parse("-0700", f[len(f)-1])
			if e != nil {
				return c, fmt.Errorf("invalid_commit_time")
			}
			_ = at
			var sec int64
			if _, e = fmt.Sscan(f[len(f)-2], &sec); e != nil {
				return c, fmt.Errorf("invalid_commit_time")
			}
			c.At = time.Unix(sec, 0).UTC()
		}
	}
	if c.At.IsZero() {
		return c, fmt.Errorf("invalid_commit_time")
	}
	// --parse implies --only-input: configured trailer commands are not executed.
	cmd := exec.CommandContext(ctx, "git", "-C", root, "interpret-trailers", "--parse")
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = GitEnvironment(os.Environ())
	cmd.Stdin = strings.NewReader(msg)
	out := cappedGitOutput{limit: MaxPayloadBytes}
	cmd.Stdout = &out
	cmd.Stderr = &cappedGitOutput{limit: 4096}
	if e = cmd.Run(); e != nil {
		return c, e
	}
	vals := map[string]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		key, val, ok := strings.Cut(line, ":")
		key = strings.ToLower(strings.TrimSpace(key))
		if !ok || !strings.HasPrefix(key, "anx-") {
			continue
		}
		if key != "anx-card" && key != "anx-note" && key != "anx-next" && key != "anx-step" && key != "anx-status" {
			continue
		}
		if _, ok := vals[key]; ok {
			return c, fmt.Errorf("duplicate_trailer")
		}
		val = strings.TrimSpace(val)
		if !utf8.ValidString(val) || strings.IndexFunc(val, unicode.IsControl) >= 0 {
			return c, fmt.Errorf("invalid_trailer")
		}
		vals[key] = val
	}
	if len(vals) == 0 {
		c.State = "skipped"
		c.Source = nil
		return c, nil
	}
	// Detect folding in the original final trailer paragraph before Git unfolds it.
	block := strings.TrimSpace(msg)
	if i := strings.LastIndex(block, "\n\n"); i >= 0 {
		block = block[i+2:]
	}
	prev := ""
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if strings.HasPrefix(prev, "anx-") {
				return c, fmt.Errorf("multiline_trailer")
			}
		} else {
			prev, _, _ = strings.Cut(strings.ToLower(line), ":")
		}
	}
	if _, ok := vals["anx-step"]; ok {
		return c, fmt.Errorf("unsupported_trailer")
	}
	if _, ok := vals["anx-status"]; ok {
		return c, fmt.Errorf("unsupported_trailer")
	}
	c.Card = vals["anx-card"]
	if c.Card == "" {
		c.State = "skipped"
		c.Source = nil
		return c, nil
	}
	if !ValidCard(c.Card) {
		return c, fmt.Errorf("invalid_card")
	}
	c.Note = vals["anx-note"]
	if c.Note == "" {
		c.Note = strings.SplitN(msg, "\n", 2)[0]
	}
	if len(c.Note) > 4096 || strings.IndexFunc(c.Note, unicode.IsControl) >= 0 {
		return c, fmt.Errorf("invalid_note")
	}
	if next, ok := vals["anx-next"]; ok {
		c.Next, c.What, ok = strings.Cut(next, " ")
		c.What = strings.TrimSpace(c.What)
		if !ok || c.Next == "" || c.What == "" {
			return c, fmt.Errorf("invalid_next")
		}
	}
	return c, nil
}

// Quarantine only malformed claims. Missing objects, subprocess failures and
// deadlines pause the frozen scan without advancing its cursor.
func InvalidCaptureClaim(e error) bool {
	if e == nil {
		return false
	}
	switch e.Error() {
	case "invalid_commit_message", "invalid_commit_time", "duplicate_trailer", "invalid_trailer", "multiline_trailer", "unsupported_trailer", "invalid_card", "invalid_note", "invalid_next":
		return true
	}
	return false
}

// Collect captures a frozen ref in bounded pages, newest first. Publishing waits
// until collection finishes, then consumes oldest first. --skip never advances a
// live ref; the snapshot and walk are persisted between pages.
func Collect(ctx context.Context, s *CaptureState) error {
	if s.Snapshot == "" {
		head, e := ResolveCommit(ctx, s.Root, "HEAD")
		if e != nil {
			return e
		}
		if head == s.Anchor {
			return nil
		}
		if _, e = Git(ctx, s.Root, 256, "merge-base", "--is-ancestor", s.Anchor, head); e != nil {
			return fmt.Errorf("divergent_history: reconcile using --since")
		}
		s.Snapshot = head
	}
	if s.Collected {
		return nil
	}
	b, e := Git(ctx, s.Root, WalkPage*66+1, "rev-list", "--topo-order", fmt.Sprintf("--skip=%d", len(s.Walk)), fmt.Sprintf("--max-count=%d", WalkPage), s.Snapshot, "^"+s.Anchor)
	if e != nil {
		return e
	}
	page := strings.Fields(string(b))
	if len(s.Walk)+len(page) > MaxWalk {
		return fmt.Errorf("walk_capacity: choose an explicit backfill boundary")
	}
	for _, sha := range page {
		if !ObjectID(sha) {
			return fmt.Errorf("invalid object id")
		}
	}
	s.Walk = append(s.Walk, page...)
	s.Collected = len(page) < WalkPage
	return nil
}
func HasCommit(path, sha string) (bool, error) {
	_, e := LoadCommit(path, sha)
	if errors.Is(e, os.ErrNotExist) {
		return false, nil
	}
	return e == nil, e
}
