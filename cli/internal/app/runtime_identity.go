package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

const runtimeIdentitySchema = "agentctl.identity.v1"
const runtimeIdentityOutputLimit = 128 * 1024

var runtimeSessionHash = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// This allowlisted projection deliberately excludes unrecognized fields and raw
// runtime environment/history. These are local discovery hints, never auth claims.
type runtimeIdentityEvidence struct {
	ID         *string `json:"id"`
	IDKind     string  `json:"id_kind,omitempty"`
	Provenance string  `json:"provenance"`
	Confidence string  `json:"confidence"`
}

type runtimeCapability struct {
	Status     string `json:"status"`
	Provenance string `json:"provenance"`
	Confidence string `json:"confidence"`
}

type runtimeHarness struct {
	ProviderID   string `json:"provider_id"`
	Availability string `json:"availability"`
	Provenance   string `json:"provenance"`
}

type runtimeIdentityReport struct {
	SchemaVersion string                  `json:"schema_version"`
	Provider      runtimeIdentityEvidence `json:"provider"`
	NativeSession runtimeIdentityEvidence `json:"native_session"`
	Execution     runtimeIdentityEvidence `json:"execution"`
	Capabilities  struct {
		Resume  runtimeCapability `json:"resume"`
		History runtimeCapability `json:"history"`
		Logs    runtimeCapability `json:"logs"`
	} `json:"capabilities"`
	Harnesses []runtimeHarness `json:"harnesses"`
}

func decodeRuntimeIdentity(raw []byte) (*runtimeIdentityReport, error) {
	if len(raw) > runtimeIdentityOutputLimit {
		return nil, errors.New("runtime identity output exceeds limit")
	}
	var envelope struct {
		OK            bool                  `json:"ok"`
		SchemaVersion int                   `json:"schema_version"`
		Result        runtimeIdentityReport `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, errors.New("runtime identity response is invalid JSON")
	}
	if !envelope.OK || envelope.SchemaVersion != 1 || envelope.Result.SchemaVersion != runtimeIdentitySchema {
		return nil, errors.New("runtime identity contract is unsupported")
	}
	report := &envelope.Result
	for _, evidence := range []*runtimeIdentityEvidence{&report.Provider, &report.NativeSession, &report.Execution} {
		if evidence.Confidence != "observed" && evidence.Confidence != "self_reported" && evidence.Confidence != "unknown" {
			return nil, errors.New("runtime identity confidence is invalid")
		}
		if evidence.ID != nil && (strings.TrimSpace(*evidence.ID) == "" || len(*evidence.ID) > 256 || strings.ContainsAny(*evidence.ID, "\r\n\x00")) {
			return nil, errors.New("runtime identity identifier is invalid")
		}
	}
	// ANX never needs the native opaque locator itself. Preserve only the safe,
	// provider-scoped correlation hash; unknown schemes require explicit registration.
	if report.NativeSession.ID != nil && (report.NativeSession.IDKind != "provider_session_sha256" || !runtimeSessionHash.MatchString(*report.NativeSession.ID)) {
		return nil, errors.New("runtime native session identifier kind is unsupported")
	}
	for _, capability := range []*runtimeCapability{&report.Capabilities.Resume, &report.Capabilities.History, &report.Capabilities.Logs} {
		if capability.Status != "supported" && capability.Status != "unsupported" && capability.Status != "unknown" {
			return nil, errors.New("runtime capability status is invalid")
		}
	}
	return report, nil
}

type boundedRuntimeOutput struct{ bytes.Buffer }

func (b *boundedRuntimeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > runtimeIdentityOutputLimit {
		return 0, errors.New("runtime identity output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func loadRuntimeIdentity(getenv func(string) string) (*runtimeIdentityReport, error) {
	path, err := exec.LookPath("agentctl")
	if err != nil {
		return nil, errors.New("agentctl identity is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "identity", "--json")
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = os.Environ()
	// Honor the calling App's environment view, including tests. In particular,
	// never substitute an inherited parent conversation for a managed execution.
	for _, key := range []string{"HOME", "AGENTCTL_EXECUTION_ID", "AGENTCTL_ADAPTER", "AGENTCTL_HOST_ID", "AGENTCTL_AUTHORITY", "CODEX_THREAD_ID", "CLAUDECODE", "CURSOR_AGENT_COMPLETED_PATH"} {
		prefix := key + "="
		filtered := cmd.Env[:0]
		for _, entry := range cmd.Env {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		cmd.Env = append(filtered, prefix+getenv(key))
	}
	var stdout boundedRuntimeOutput
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, errors.New("agentctl identity is unavailable or failed")
	}
	return decodeRuntimeIdentity(stdout.Bytes())
}

func availableRuntimeAdapters(report *runtimeIdentityReport) []string {
	seen := map[string]bool{}
	if report != nil {
		for _, harness := range report.Harnesses {
			name := agentctlName(harness.ProviderID)
			if harness.Availability == "available" && agentNamePattern.MatchString(name) {
				seen[name] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (a *App) discoverRuntime() *commandResult {
	data := map[string]any{"provider": "agentctl", "available": false, "registration_requires_provider": false}
	var report *runtimeIdentityReport
	var err error
	if a.runtimeIdentity != nil {
		report, err = a.runtimeIdentity()
	}
	if err != nil || report == nil {
		data["reason"] = "agentctl identity v1 is unavailable; explicit session registration remains available"
		return &commandResult{Data: data, Text: "runtime provider=agentctl available=false generic_registration=true"}
	}
	data["available"] = true
	data["identity"] = report
	data["adapters"] = availableRuntimeAdapters(report)
	return &commandResult{Data: data, Text: fmt.Sprintf("runtime provider=agentctl available=true schema=%s adapters=%s generic_registration=true", report.SchemaVersion, strings.Join(availableRuntimeAdapters(report), ","))}
}

func init() {
	localHelperTopics = append(localHelperTopics, localHelperTopic{
		Path: "host discover", Summary: "Inspect optional local runtime identity and installed harness evidence without registration or network requests.",
		JSONShape:   "`provider`, `available`, `registration_requires_provider`, optional `identity` and `adapters`",
		Composition: "Read-only local agentctl identity v1 evidence. No principal grant, task assignment, transcript read, or automatic upload. Installed availability is not a live-session capability.",
		Examples:    []string{"anx host discover", "anx host discover --json"},
	})
}
