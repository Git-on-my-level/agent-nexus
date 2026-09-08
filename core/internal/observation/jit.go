package observation

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type JITPolicy struct {
	MaxArtifactBytes int64           `json:"max_artifact_bytes"`
	Limits           IsolationLimits `json:"limits"`
	FailureThreshold int             `json:"failure_threshold"`
}

func (p JITPolicy) Validate() error {
	if err := p.Limits.Validate(); err != nil {
		return err
	}
	if p.MaxArtifactBytes < 1 || p.MaxArtifactBytes > 32<<20 || p.FailureThreshold < 1 || p.FailureThreshold > 10 {
		return failure(ErrConfiguration, "invalid artifact or suspension bounds")
	}
	return nil
}

type Manifest struct {
	AdapterID string          `json:"adapter_id"`
	Target    Target          `json:"target"`
	Envelope  Envelope        `json:"envelope"`
	Limits    IsolationLimits `json:"limits"`
}

func (m Manifest) Validate(p JITPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if !validComponent(m.AdapterID) || m.Target.Validate() != nil {
		return failure(ErrConfiguration, "invalid generated reader identity or target")
	}
	if err := m.Limits.Validate(); err != nil {
		return err
	}
	e := m.Envelope
	if len(e.ReadPaths) > 0 || len(e.NetworkHosts) > 0 || len(e.CredentialHandles) > 0 || len(e.Dependencies) > 0 {
		return failure(ErrPolicy, "capability expansion requires an approved implementation; v1 generated code has no host files, network, credentials or dependency installation")
	}
	if len(e.ScratchPaths) != 0 {
		return failure(ErrPolicy, "v1 generated transforms use stdin/stdout and memory only; filesystem scratch requires a quota-enforced runner")
	}
	l, max := m.Limits, p.Limits
	if l.Timeout > max.Timeout || l.MemoryBytes > max.MemoryBytes || l.OutputBytes > max.OutputBytes || l.InputBytes > max.InputBytes || l.CPUSeconds > max.CPUSeconds || l.Processes > max.Processes || l.FileBytes > max.FileBytes {
		return failure(ErrPolicy, "generated resource request exceeds approved envelope")
	}
	return nil
}

type TransformOutput struct {
	Facts       map[string]any `json:"facts"`
	Evidence    []Evidence     `json:"evidence,omitempty"`
	Uncertainty []string       `json:"uncertainty"`
}

func parseTransform(raw []byte, limit int64) (TransformOutput, error) {
	var out TransformOutput
	if int64(len(raw)) > limit {
		return out, failure(ErrLimit, "generated output exceeds limit")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&out); err != nil {
		return out, failure(ErrInvalidOutput, "generated output violates transform schema")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return out, failure(ErrInvalidOutput, "generated output contains trailing data")
	}
	if out.Facts == nil || len(out.Evidence) > 100 || len(out.Uncertainty) > 100 {
		return out, failure(ErrInvalidOutput, "generated output requires bounded findings and uncertainty")
	}
	for _, e := range out.Evidence {
		if e.Kind == "" || !safeReference(e.Reference) || e.Knowledge != "reported" {
			return out, failure(ErrPolicy, "generated evidence must be reported source references")
		}
	}
	return out, nil
}

type ValidationCase struct {
	Name      string
	Input     []byte
	WantValid bool
}
type Version struct {
	Revision         string     `json:"revision"`
	ArtifactDigest   string     `json:"artifact_digest"`
	Manifest         Manifest   `json:"manifest"`
	State            string     `json:"state"`
	CreatedAt        time.Time  `json:"created_at"`
	ValidatedAt      *time.Time `json:"validated_at,omitempty"`
	CanaryAt         *time.Time `json:"canary_at,omitempty"`
	CanaryDigest     string     `json:"canary_digest,omitempty"`
	ValidationDigest string     `json:"validation_digest,omitempty"`
	Failures         int        `json:"failures"`
	LastError        *ReadError `json:"last_error,omitempty"`
}
type AdapterState struct {
	Active   string             `json:"active"`
	Previous string             `json:"previous"`
	Versions map[string]Version `json:"versions"`
}

// JITManager persists executable lifecycle metadata, not work truth. Its private
// directory is never mounted into generated readers. All state mutations are
// serialized across processes and atomically replaced with fsync durability.
type JITManager struct {
	root   string
	policy JITPolicy
	runner isolatedExecutor
	mu     sync.Mutex
}

func NewJITManager(root string, p JITPolicy) (*JITManager, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(root) {
		return nil, failure(ErrConfiguration, "JIT state root must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != filepath.Clean(root) {
		return nil, failure(ErrPolicy, "JIT state root must not use symlinks")
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0077 != 0 {
		return nil, failure(ErrPolicy, "JIT state root must have private 0700 permissions")
	}
	return &JITManager{root: root, policy: p, runner: NewBubblewrapRunner()}, nil
}
func (m *JITManager) IsolationAvailable() error { return m.runner.Available() }
func (m *JITManager) locked(fn func() error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(m.root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}
func (m *JITManager) load(id string) (AdapterState, error) {
	empty := AdapterState{Versions: map[string]Version{}}
	if !validComponent(id) {
		return empty, failure(ErrConfiguration, "invalid adapter ID")
	}
	raw, err := os.ReadFile(filepath.Join(m.root, id, "state.json"))
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	if len(raw) > 4<<20 {
		return empty, failure(ErrLimit, "JIT state exceeds limit")
	}
	var state AdapterState
	if err = json.Unmarshal(raw, &state); err != nil || state.Versions == nil {
		return empty, failure(ErrInvalidOutput, "invalid durable adapter state")
	}
	return state, nil
}
func atomicFile(p string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(p), ".replace-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, p); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(p))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (m *JITManager) save(id string, s AdapterState) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicFile(filepath.Join(m.root, id, "state.json"), raw, 0600)
}
func (m *JITManager) Status(id string) (state AdapterState, err error) {
	err = m.locked(func() error { var e error; state, e = m.load(id); return e })
	return
}
func staticELF(data []byte) error {
	f, err := elf.NewFile(bytes.NewReader(data))
	if err != nil {
		return failure(ErrInvalidOutput, "generated artifact must be a static Linux ELF executable")
	}
	defer f.Close()
	if f.Class != elf.ELFCLASS64 || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) || (f.Machine != elf.EM_X86_64 && f.Machine != elf.EM_AARCH64) {
		return failure(ErrPolicy, "unsupported generated executable format")
	}
	for _, prog := range f.Progs {
		if prog.Type == elf.PT_INTERP {
			return failure(ErrPolicy, "dynamic executable interpreters are not permitted")
		}
	}
	libs, err := f.ImportedLibraries()
	if err != nil || len(libs) > 0 {
		return failure(ErrPolicy, "generated executable dependencies are not permitted")
	}
	return nil
}
func versionDigest(manifest Manifest, artifactDigest string) string {
	b, _ := json.Marshal(manifest)
	return digest(append(b, []byte(artifactDigest)...))
}
func (m *JITManager) Stage(manifest Manifest, artifact []byte) (v Version, err error) {
	if err = manifest.Validate(m.policy); err != nil {
		return
	}
	if len(artifact) == 0 || int64(len(artifact)) > m.policy.MaxArtifactBytes {
		return v, failure(ErrLimit, "generated artifact exceeds byte limit")
	}
	if err = staticELF(artifact); err != nil {
		return
	}
	hash := digest(artifact)
	rev := versionDigest(manifest, hash)
	err = m.locked(func() error {
		state, e := m.load(manifest.AdapterID)
		if e != nil {
			return e
		}
		if existing, ok := state.Versions[rev]; ok {
			v = existing
			return nil
		}
		if len(state.Versions) >= 100 {
			return failure(ErrLimit, "adapter version retention limit reached")
		}
		for _, existing := range state.Versions {
			if existing.Manifest.Target != manifest.Target {
				return failure(ErrPermission, "adapter ID is already bound to another target")
			}
		}
		parent := filepath.Join(m.root, manifest.AdapterID)
		if e = os.MkdirAll(parent, 0700); e != nil {
			return e
		}
		dir, e := os.MkdirTemp(parent, ".stage-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(dir)
		if e = atomicFile(filepath.Join(dir, "reader"), artifact, 0500); e != nil {
			return e
		}
		raw, _ := json.Marshal(manifest)
		if e = atomicFile(filepath.Join(dir, "manifest.json"), raw, 0400); e != nil {
			return e
		}
		if e = os.Rename(dir, filepath.Join(parent, rev)); e != nil {
			return e
		}
		v = Version{Revision: rev, ArtifactDigest: hash, Manifest: manifest, State: "staged", CreatedAt: time.Now().UTC()}
		state.Versions[rev] = v
		return m.save(manifest.AdapterID, state)
	})
	return
}
func (m *JITManager) artifact(v Version) (string, error) {
	if err := v.Manifest.Validate(m.policy); err != nil {
		return "", err
	}
	if !validSHA(v.Revision) || versionDigest(v.Manifest, v.ArtifactDigest) != v.Revision {
		return "", failure(ErrPolicy, "generated manifest identity changed")
	}
	p := filepath.Join(m.root, v.Manifest.AdapterID, v.Revision, "reader")
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > m.policy.MaxArtifactBytes {
		return "", failure(ErrPolicy, "generated executable unavailable or replaced")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	if digest(b) != v.ArtifactDigest {
		return "", failure(ErrPolicy, "generated executable digest changed")
	}
	if err = staticELF(b); err != nil {
		return "", err
	}
	return p, nil
}
func (m *JITManager) Validate(ctx context.Context, id, rev string, cases []ValidationCase) error {
	if len(cases) < 2 || len(cases) > 20 {
		return failure(ErrConfiguration, "validation requires 2..20 bounded positive and negative fixture cases")
	}
	positive, negative := false, false
	for _, c := range cases {
		if c.Name == "" {
			return failure(ErrConfiguration, "fixture needs a name")
		}
		if c.WantValid {
			positive = true
		} else {
			negative = true
		}
	}
	if !positive || !negative {
		return failure(ErrConfiguration, "validation requires both successful and rejected fixtures")
	}
	return m.locked(func() error {
		s, err := m.load(id)
		if err != nil {
			return err
		}
		v, ok := s.Versions[rev]
		if !ok {
			return failure(ErrNotFound, "unknown adapter version")
		}
		if v.State != "staged" {
			return failure(ErrPolicy, "only staged versions can be validated")
		}
		p, err := m.artifact(v)
		if err != nil {
			return err
		}
		if err = m.runner.Available(); err != nil {
			return err
		}
		for _, c := range cases {
			if int64(len(c.Input)) > v.Manifest.Limits.InputBytes {
				return failure(ErrLimit, "fixture input exceeds budget")
			}
			raw, runErr := m.runner.Run(ctx, p, c.Input, v.Manifest.Limits)
			if runErr == nil {
				_, runErr = parseTransform(raw, v.Manifest.Limits.OutputBytes)
			}
			// A sandbox setup failure cannot count as a successful negative test.
			var re *ReadError
			if errors.As(runErr, &re) && (re.Kind == ErrIsolation || re.Kind == ErrPolicy) {
				return runErr
			}
			if (runErr == nil) != c.WantValid {
				return failure(ErrInvalidOutput, "generated reader fixture expectation failed")
			}
		}
		now := time.Now().UTC()
		v.ValidatedAt = &now
		v.State = "validated"
		raw, _ := json.Marshal(cases)
		v.ValidationDigest = digest(raw)
		s.Versions[rev] = v
		return m.save(id, s)
	})
}
func (m *JITManager) transform(ctx context.Context, v Version, source Reader) (Report, error) {
	p, err := m.artifact(v)
	if err != nil {
		return Report{}, err
	}
	if err = m.runner.Available(); err != nil {
		return Report{}, err
	}
	if source == nil || !source.Capabilities().TrustedBuiltin {
		return Report{}, failure(ErrPolicy, "JIT source snapshot must come from a trusted built-in reader")
	}
	ctx, cancel := context.WithTimeout(ctx, v.Manifest.Limits.Timeout)
	defer cancel()
	report, err := source.Read(ctx, v.Manifest.Target)
	if err != nil {
		return Report{}, err
	}
	if report.Target != v.Manifest.Target || report.Validate() != nil {
		return Report{}, failure(ErrPolicy, "broker returned an invalid or unapproved source snapshot")
	}
	input, err := json.Marshal(report)
	if err != nil {
		return Report{}, failure(ErrInvalidOutput, "source snapshot cannot be encoded")
	}
	raw, err := m.runner.Run(ctx, p, input, v.Manifest.Limits)
	if err != nil {
		return Report{}, err
	}
	out, err := parseTransform(raw, v.Manifest.Limits.OutputBytes)
	if err != nil {
		return Report{}, err
	}
	allowed := map[string]bool{}
	for _, e := range report.Evidence {
		allowed[e.Reference] = true
	}
	for _, e := range out.Evidence {
		if !allowed[e.Reference] {
			return Report{}, failure(ErrPolicy, "generated evidence reference is not in the brokered source snapshot")
		}
	}
	report.ReaderID = "jit:" + v.Manifest.AdapterID
	report.ReaderRevision = v.Revision
	report.Knowledge = "reported"
	if report.Facts == nil {
		report.Facts = map[string]any{}
	}
	// Generated findings never overwrite title, source status, owner, phase, or timestamps.
	report.Facts["generated_findings"] = out.Facts
	report.Coverage.Limitations = append(report.Coverage.Limitations, out.Uncertainty...)
	report.Evidence = append(report.Evidence, out.Evidence...)
	return finishReport(report)
}
func (m *JITManager) Canary(ctx context.Context, id, rev string, source Reader) (report Report, err error) {
	err = m.locked(func() error {
		s, e := m.load(id)
		if e != nil {
			return e
		}
		v, ok := s.Versions[rev]
		if !ok {
			return failure(ErrNotFound, "unknown adapter version")
		}
		if v.State != "validated" {
			return failure(ErrPolicy, "canary requires validated fixtures")
		}
		report, e = m.transform(ctx, v, source)
		if e != nil {
			return e
		}
		if !report.Coverage.Complete {
			return failure(ErrInvalidOutput, "partial source coverage cannot certify a canary")
		}
		now := time.Now().UTC()
		v.CanaryAt = &now
		v.CanaryDigest = report.SemanticKey
		v.State = "canaried"
		s.Versions[rev] = v
		return m.save(id, s)
	})
	return
}
func (m *JITManager) Activate(id, rev string) error {
	return m.locked(func() error {
		s, err := m.load(id)
		if err != nil {
			return err
		}
		v, ok := s.Versions[rev]
		if !ok {
			return failure(ErrNotFound, "unknown adapter version")
		}
		if v.State != "canaried" || v.ValidatedAt == nil || v.CanaryAt == nil || time.Since(*v.CanaryAt) > time.Hour {
			return failure(ErrPolicy, "activation requires validated fixtures and a canary within one hour")
		}
		if _, err = m.artifact(v); err != nil {
			return err
		}
		if err = m.runner.Available(); err != nil {
			return err
		}
		if old, ok := s.Versions[s.Active]; ok {
			old.State = "standby"
			s.Versions[s.Active] = old
			s.Previous = s.Active
		}
		v.State = "active"
		s.Active = rev
		s.Versions[rev] = v
		return m.save(id, s)
	})
}
func (m *JITManager) Suspend(id, reason string) error {
	return m.locked(func() error {
		s, err := m.load(id)
		if err != nil {
			return err
		}
		if s.Active == "" {
			return nil
		}
		v := s.Versions[s.Active]
		v.State = "suspended"
		v.LastError = &ReadError{Kind: ErrPolicy, Message: "operator suspended reader"}
		_ = reason
		s.Versions[s.Active] = v
		s.Active = ""
		return m.save(id, s)
	})
}
func (m *JITManager) Rollback(id string) error {
	return m.locked(func() error {
		s, err := m.load(id)
		if err != nil {
			return err
		}
		v, ok := s.Versions[s.Previous]
		if !ok || v.State != "standby" || v.ValidatedAt == nil || v.CanaryAt == nil {
			return failure(ErrPolicy, "no previously validated standby version")
		}
		if _, err = m.artifact(v); err != nil {
			return err
		}
		if err = m.runner.Available(); err != nil {
			return err
		}
		if old, ok := s.Versions[s.Active]; ok {
			old.State = "suspended"
			s.Versions[s.Active] = old
		}
		s.Active = s.Previous
		s.Previous = ""
		v.State = "active"
		v.Failures = 0
		v.LastError = nil
		s.Versions[s.Active] = v
		return m.save(id, s)
	})
}
func (m *JITManager) Read(ctx context.Context, id string, source Reader) (report Report, err error) {
	err = m.locked(func() error {
		s, e := m.load(id)
		if e != nil {
			return e
		}
		v, ok := s.Versions[s.Active]
		if !ok || v.State != "active" {
			return failure(ErrPolicy, "generated reader has no active version")
		}
		report, e = m.transform(ctx, v, source)
		if e != nil {
			re := &ReadError{Kind: ErrUnavailable, Message: "generated reader failed"}
			var typed *ReadError
			if errors.As(e, &typed) {
				copy := *typed
				re = &copy
			}
			v.Failures++
			v.LastError = re
			if re.Kind == ErrPolicy || re.Kind == ErrIsolation || v.Failures >= m.policy.FailureThreshold {
				v.State = "suspended"
				s.Active = ""
			}
			s.Versions[v.Revision] = v
			if saveErr := m.save(id, s); saveErr != nil {
				return fmt.Errorf("persist reader suspension: %w", saveErr)
			}
			return re
		}
		v.Failures = 0
		v.LastError = nil
		s.Versions[v.Revision] = v
		return m.save(id, s)
	})
	return
}

// Namespaced generated findings remain claims. No generated instructions are
// executed by the broker, and source content cannot change this policy.
