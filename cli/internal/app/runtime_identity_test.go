package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func runtimeIdentityFixture() string {
	return `{"ok":true,"schema_version":1,"result":{"schema_version":"agentctl.identity.v1","provider":{"id":"hermes","provenance":"explicit","confidence":"self_reported"},"native_session":{"id":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","id_kind":"provider_session_sha256","provenance":"explicit","confidence":"self_reported"},"execution":{"id":null,"provenance":"unknown","confidence":"unknown"},"capabilities":{"resume":{"status":"unknown","provenance":"unknown","confidence":"unknown"},"history":{"status":"unsupported","provenance":"unknown","confidence":"unknown"},"logs":{"status":"unknown","provenance":"unknown","confidence":"unknown"}},"harnesses":[{"provider_id":"hermes","availability":"available","provenance":"path_lookup"},{"provider_id":"claude-code","availability":"available","provenance":"path_lookup"},{"provider_id":"hermes","availability":"available","provenance":"path_lookup"},{"provider_id":"bad/name","availability":"available","provenance":"path_lookup"},{"provider_id":"codex","availability":"unavailable","provenance":"path_lookup"}],"raw_transcript":"must-not-leak"}}`
}

func TestRuntimeIdentityVersionedProjection(t *testing.T) {
	report, err := decodeRuntimeIdentity([]byte(runtimeIdentityFixture()))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(availableRuntimeAdapters(report), ","); got != "claude,hermes" {
		t.Fatalf("discovery must support arbitrary valid names and deduplicate: %s", got)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "must-not-leak") || strings.Contains(string(encoded), "raw_transcript") {
		t.Fatalf("unknown provider data leaked: %s", encoded)
	}
	for name, raw := range map[string]string{
		"old contract":                  strings.Replace(runtimeIdentityFixture(), "agentctl.identity.v1", "agentctl.identity.v0", 1),
		"bad envelope":                  strings.Replace(runtimeIdentityFixture(), `"schema_version":1`, `"schema_version":2`, 1),
		"untrusted raw locator":         strings.Replace(runtimeIdentityFixture(), "provider_session_sha256", "raw_native_id", 1),
		"raw locator disguised as hash": strings.Replace(runtimeIdentityFixture(), "sha256:"+strings.Repeat("a", 64), "/private/session/location", 1),
		"unknown capability":            strings.Replace(runtimeIdentityFixture(), `"status":"unsupported"`, `"status":"probably"`, 1),
		"invalid confidence":            strings.Replace(runtimeIdentityFixture(), `"confidence":"self_reported"`, `"confidence":"certain"`, 1),
		"oversized":                     strings.Repeat("x", runtimeIdentityOutputLimit+1),
		"trailing JSON":                 runtimeIdentityFixture() + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRuntimeIdentity([]byte(raw)); err == nil {
				t.Fatal("invalid runtime evidence accepted")
			}
		})
	}
}

func TestRuntimeIdentityDoesNotOverrideExplicitPrincipal(t *testing.T) {
	a := newTestApp(t)
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
		t.Fatal("explicit identity must not probe runtime")
		return nil, nil
	}
	name, source, err := a.identityName(config.Resolved{As: "reviewer", IdentitySource: "flag:--as"})
	if err != nil || name != "reviewer" || source != "flag:--as" {
		t.Fatalf("%q %q %v", name, source, err)
	}
}

func TestRuntimeIdentityIsOptionalAndUsesProviderEvidence(t *testing.T) {
	a := newTestApp(t)
	a.Getenv = func(key string) string {
		if key == "CODEX_THREAD_ID" {
			return "parent-thread"
		}
		return ""
	}
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) { return decodeRuntimeIdentity([]byte(runtimeIdentityFixture())) }
	name, source, err := a.identityName(config.Resolved{})
	if err != nil || name != "hermes" || source != "provider:agentctl" {
		t.Fatalf("provider evidence lost: %q %q %v", name, source, err)
	}
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) { return nil, errors.New("old provider") }
	name, source, err = a.identityName(config.Resolved{})
	if err != nil || name != "codex" || source != "harness:codex" {
		t.Fatalf("direct fallback lost: %q %q %v", name, source, err)
	}
}

func TestUnresolvedIdentitySuggestsTheOnlyDetectedAdapter(t *testing.T) {
	a := newTestApp(t)
	a.Getenv = func(string) string { return "" }
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
		return &runtimeIdentityReport{Harnesses: []runtimeHarness{{ProviderID: "hermes", Availability: "available"}}}, nil
	}
	_, _, err := a.identityName(config.Resolved{})
	if err == nil {
		t.Fatal("expected active identity to remain unresolved")
	}
	message := err.Error()
	if !strings.Contains(message, "anx --as hermes doctor") || !strings.Contains(message, "ANX_AS=hermes") {
		t.Fatalf("missing concrete identity repair: %s", message)
	}
	normalized := errnorm.Normalize(err)
	if got := strings.Join(stringList(asMap(normalized.Details)["next_argv"]), " "); got != "anx --as hermes auth whoami" {
		t.Fatalf("next action=%q", got)
	}
}

func TestHostDiscoverIsLocalAndNeverRegisters(t *testing.T) {
	for _, available := range []bool{true, false} {
		a := newTestApp(t)
		a.Getenv = func(string) string { return "" }
		a.UserHomeDir = func() (string, error) { return "", errors.New("no home") }
		a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
			if available {
				return decodeRuntimeIdentity([]byte(runtimeIdentityFixture()))
			}
			return nil, errors.New("missing")
		}
		var stdout bytes.Buffer
		a.Stdout = &stdout
		if code := a.Run([]string{"--json", "--base-url", "http://127.0.0.1:1", "host", "discover"}); code != 0 {
			t.Fatalf("discovery needs no server/home/identity: %d %s", code, stdout.String())
		}
		result := asMap(assertEnvelopeOK(t, stdout.String())["result"])
		if result["available"] != available || result["registration_requires_provider"] != false {
			t.Fatalf("incorrect discovery result: %#v", result)
		}
	}
	if got := commandSideEffectClass("host discover"); got != "read_only" {
		t.Fatal(got)
	}
}

func TestManagedIdentityNeverFallsBackToInheritedParent(t *testing.T) {
	for _, key := range []string{"AGENTCTL_EXECUTION_ID", "AGENTCTL_ADAPTER", "AGENTCTL_AUTHORITY"} {
		a := newTestApp(t)
		a.runtimeIdentity = func() (*runtimeIdentityReport, error) { return nil, errors.New("old provider") }
		a.Getenv = func(k string) string {
			if k == key {
				return "unknown-managed"
			}
			if k == "CODEX_THREAD_ID" {
				return "parent-thread"
			}
			return ""
		}
		if name, _, err := a.identityName(config.Resolved{}); err == nil || name != "" {
			t.Fatalf("%s inherited parent identity: %q %v", key, name, err)
		}
	}
	a := newTestApp(t)
	a.Getenv = func(k string) string {
		if k == "CODEX_THREAD_ID" {
			return "parent-thread"
		}
		return ""
	}
	a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
		r, _ := decodeRuntimeIdentity([]byte(runtimeIdentityFixture()))
		r.Provider.ID = nil
		execution := "managed-execution"
		r.Execution.ID = &execution
		return r, nil
	}
	if name, _, err := a.identityName(config.Resolved{}); err == nil || name != "" {
		t.Fatalf("provider-managed parent fallback: %q %v", name, err)
	}
}

func TestAmbiguousNativeMarkersRequireExplicitIdentity(t *testing.T) {
	for _, providerAvailable := range []bool{true, false} {
		a := newTestApp(t)
		a.Getenv = func(k string) string {
			if k == "CLAUDECODE" {
				return "1"
			}
			if k == "CODEX_THREAD_ID" {
				return "parent-thread"
			}
			return ""
		}
		a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
			if !providerAvailable {
				return nil, errors.New("missing")
			}
			r, _ := decodeRuntimeIdentity([]byte(runtimeIdentityFixture()))
			codex := "codex"
			r.Provider.ID = &codex
			r.Provider.Provenance = "native_environment"
			return r, nil
		}
		if name, _, err := a.identityName(config.Resolved{}); err == nil || name != "" {
			t.Fatalf("ambiguous markers chose %q: %v", name, err)
		}
		if name, _, err := a.identityName(config.Resolved{As: "reviewer", IdentitySource: "flag:--as"}); err != nil || name != "reviewer" {
			t.Fatalf("explicit identity blocked: %q %v", name, err)
		}
	}
}

func TestProviderSuppressionDoesNotResurrectManagedContext(t *testing.T) {
	for _, available := range []bool{true, false} {
		a := newTestApp(t)
		a.Getenv = func(k string) string {
			switch k {
			case "AGENTCTL_EXECUTION_ID":
				return "parent-execution"
			case "AGENTCTL_ADAPTER":
				return "codex"
			case "CLAUDECODE":
				return "1"
			}
			return ""
		}
		a.runtimeIdentity = func() (*runtimeIdentityReport, error) {
			if !available {
				return nil, errors.New("unavailable")
			}
			r, _ := decodeRuntimeIdentity([]byte(runtimeIdentityFixture()))
			r.Provider.ID, r.NativeSession.ID, r.Execution.ID = nil, nil, nil
			r.Provider.Confidence = "unknown"
			return r, nil
		}
		if name, _, err := a.identityName(config.Resolved{}); err == nil || name != "" {
			t.Fatalf("resurrected inherited managed identity: %q %v", name, err)
		}
	}
}

func TestParticipantGuideUsesExplicitTaskTargets(t *testing.T) {
	guide := agentGuideText()
	for _, unsafe := range []string{`anx work note "What changed"` + "`", `anx work block "Why" --ask`, "anx work done --evidence", `anx ask "Question" --recommend`} {
		if strings.Contains(guide, unsafe) {
			t.Fatalf("unscoped participant instruction: %s", unsafe)
		}
	}
	for _, argv := range [][]string{
		{"cards", "message", "card:example", "--body", "Blocked: reason and next step"},
		{"ask", "Question", "--subject-ref", "card:example", "--recommend", "Preferred answer"},
		{"work", "block", "Why", "card:example", "--ask", "--recommend", "Preferred answer"},
		{"work", "done", "card:example", "--evidence", "https://example.test/receipt"},
	} {
		if _, err := preflightConfigIndependentUsage(argv); err != nil {
			t.Fatalf("guide command rejected: %v: %v", argv, err)
		}
	}
}
