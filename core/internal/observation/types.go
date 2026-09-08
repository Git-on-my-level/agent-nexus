// Package observation collects source evidence without granting upstream write authority.
// It does not decide task acceptance or own canonical work state.
package observation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"
)

const ReaderRevision = "1"

type Target struct {
	WorkspaceID  string `json:"workspace_id"`
	ConnectionID string `json:"connection_id"`
	Source       string `json:"source"`
	Kind         string `json:"kind"`
	NativeID     string `json:"native_id"`
	Repository   string `json:"repository,omitempty"`
	Host         string `json:"host,omitempty"`
	Path         string `json:"path,omitempty"`
}

func (t Target) Key() string { b, _ := json.Marshal(t); return digest(b) }
func (t Target) Validate() error {
	if t.WorkspaceID == "" || t.ConnectionID == "" || t.Source == "" || t.Kind == "" || t.NativeID == "" {
		return errors.New("workspace, connection, source, kind and native ID are required")
	}
	for _, v := range []string{t.WorkspaceID, t.ConnectionID, t.Source, t.Kind, t.NativeID, t.Repository, t.Host, t.Path} {
		if len(v) > 4096 || strings.ContainsAny(v, "\x00\r\n") {
			return errors.New("invalid target field")
		}
	}
	return nil
}

type Evidence struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Revision  string `json:"revision,omitempty"`
	Knowledge string `json:"knowledge"`
	Summary   string `json:"summary,omitempty"`
}
type Coverage struct {
	Complete    bool     `json:"complete"`
	Pages       int      `json:"pages"`
	NextCursor  string   `json:"next_cursor,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}
type Report struct {
	Target           Target         `json:"target"`
	ReaderID         string         `json:"reader_id"`
	ReaderRevision   string         `json:"reader_revision"`
	SourceRevision   string         `json:"source_revision,omitempty"`
	ObservedAt       time.Time      `json:"observed_at"`
	ReceivedAt       time.Time      `json:"received_at"`
	SourceUpdatedAt  *time.Time     `json:"source_updated_at,omitempty"`
	SourceActivityAt *time.Time     `json:"source_activity_at,omitempty"`
	Knowledge        string         `json:"knowledge"`
	NativeStatus     string         `json:"native_status,omitempty"`
	Title            string         `json:"title,omitempty"`
	URL              string         `json:"url,omitempty"`
	Facts            map[string]any `json:"facts,omitempty"`
	Evidence         []Evidence     `json:"evidence"`
	Coverage         Coverage       `json:"coverage"`
	IdempotencyKey   string         `json:"idempotency_key"`
}

func (r Report) Validate() error {
	if err := r.Target.Validate(); err != nil {
		return err
	}
	if r.ReaderID == "" || r.ReaderRevision == "" || r.ObservedAt.IsZero() {
		return errors.New("reader identity, revision and observed timestamp are required")
	}
	if r.Knowledge != "reported" && r.Knowledge != "verified" && r.Knowledge != "uncertain" {
		return errors.New("invalid knowledge label")
	}
	if r.Coverage.Pages < 0 || len(r.Evidence) > 1000 || len(r.Coverage.Limitations) > 100 {
		return errors.New("report collection bounds exceeded")
	}
	if r.URL != "" && !safeReference(r.URL) {
		return errors.New("invalid source URL")
	}
	for _, e := range r.Evidence {
		if e.Kind == "" || e.Reference == "" || !safeReference(e.Reference) {
			return errors.New("invalid evidence reference")
		}
		if e.Knowledge != "reported" && e.Knowledge != "verified" && e.Knowledge != "uncertain" {
			return errors.New("invalid evidence knowledge")
		}
	}
	b, err := json.Marshal(r)
	if err != nil {
		return errors.New("report is not JSON serializable")
	}
	if len(b) > 2<<20 {
		return errors.New("report exceeds 2 MiB")
	}
	return nil
}
func safeReference(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || strings.ContainsAny(raw, "\r\n\x00") {
		return false
	}
	return (u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "ssh") && u.Host != ""
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func finishReport(r Report) (Report, error) {
	if err := r.Validate(); err != nil {
		return Report{}, err
	}
	// Receipt time and observation clock are not semantic source activity.
	copy := r
	copy.ReceivedAt = time.Time{}
	copy.ObservedAt = time.Time{}
	copy.IdempotencyKey = ""
	b, _ := json.Marshal(copy)
	r.IdempotencyKey = digest(b)
	return r, nil
}

type Capabilities struct {
	Source         string   `json:"source"`
	ReadOne        bool     `json:"read_one"`
	List           bool     `json:"list"`
	Incremental    bool     `json:"incremental"`
	TrustedBuiltin bool     `json:"trusted_builtin"`
	EvidenceKinds  []string `json:"evidence_kinds"`
	Limitations    []string `json:"limitations,omitempty"`
}
type Reader interface {
	Capabilities() Capabilities
	Read(context.Context, Target) (Report, error)
}

type ErrorKind string

const (
	ErrConfiguration ErrorKind = "configuration"
	ErrPermission    ErrorKind = "permission"
	ErrNotFound      ErrorKind = "not_found"
	ErrRateLimit     ErrorKind = "rate_limited"
	ErrUnavailable   ErrorKind = "unavailable"
	ErrInvalidOutput ErrorKind = "invalid_output"
	ErrLimit         ErrorKind = "resource_limit"
	ErrPolicy        ErrorKind = "policy_denied"
	ErrIsolation     ErrorKind = "isolation_unavailable"
)

// ReadError deliberately excludes response bodies, credentials and raw transport errors.
type ReadError struct {
	Kind       ErrorKind     `json:"kind"`
	Message    string        `json:"message"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
	Status     int           `json:"status,omitempty"`
}

func (e *ReadError) Error() string          { return string(e.Kind) + ": " + e.Message }
func failure(k ErrorKind, msg string) error { return &ReadError{Kind: k, Message: msg} }

type Limits struct {
	Timeout        time.Duration `json:"timeout"`
	MaxOutputBytes int64         `json:"max_output_bytes"`
	MaxRequests    int           `json:"max_requests"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
}

func (l Limits) Validate() error {
	if l.Timeout <= 0 || l.Timeout > 5*time.Minute || l.MaxOutputBytes <= 0 || l.MaxOutputBytes > 2<<20 || l.MaxRequests <= 0 || l.MaxRequests > 100 {
		return errors.New("limits must bound timeout (<=5m), output (<=2MiB), requests (<=100)")
	}
	return nil
}

type RemoteBinding struct {
	WorkspaceID  string
	ReaderID     string
	Target       Target
	MaxClockSkew time.Duration
}

func NormalizeRemoteReport(r Report, b RemoteBinding, now time.Time) (Report, error) {
	if b.WorkspaceID == "" || b.ReaderID == "" || b.Target.WorkspaceID != b.WorkspaceID || r.Target != b.Target || r.ReaderID != b.ReaderID {
		return Report{}, failure(ErrPermission, "remote report does not match authenticated target and reader")
	}
	if b.MaxClockSkew < 0 || b.MaxClockSkew > 5*time.Minute {
		return Report{}, failure(ErrConfiguration, "clock skew allowance exceeds five minutes")
	}
	if r.ObservedAt.After(now.Add(b.MaxClockSkew)) {
		return Report{}, failure(ErrInvalidOutput, "observation timestamp exceeds clock skew allowance")
	}
	r.Knowledge = "reported"
	r.ReceivedAt = now.UTC()
	for i := range r.Evidence {
		r.Evidence[i].Knowledge = "reported"
	}
	return finishReport(r)
}

type InvestigationSpec struct {
	Objective                string        `json:"objective"`
	Target                   Target        `json:"target"`
	PresetRef                string        `json:"preset_ref"`
	AllowedSources           []string      `json:"allowed_sources"`
	AllowedPaths             []string      `json:"allowed_paths"`
	EvidenceRequirements     []string      `json:"evidence_requirements"`
	AcceptanceInterpretation string        `json:"acceptance_interpretation,omitempty"`
	Limits                   Limits        `json:"limits"`
	Interval                 time.Duration `json:"interval"`
}

func (s InvestigationSpec) Validate() error {
	if err := s.Target.Validate(); err != nil {
		return err
	}
	if err := s.Limits.Validate(); err != nil {
		return err
	}
	if s.Objective == "" || s.PresetRef == "" || len(s.EvidenceRequirements) == 0 || s.Interval < time.Minute || s.Limits.MaxTokens <= 0 || s.Limits.MaxTokens > 100000 {
		return errors.New("investigation needs objective, existing preset reference, evidence requirements, interval >=1m and token budget <=100000")
	}
	found := false
	for _, v := range s.AllowedSources {
		if v == s.Target.Source {
			found = true
		}
	}
	if !found {
		return failure(ErrPolicy, "target source is not approved")
	}
	for _, p := range s.AllowedPaths {
		if !strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "/" {
			return fmt.Errorf("invalid investigation read path")
		}
	}
	return nil
}
