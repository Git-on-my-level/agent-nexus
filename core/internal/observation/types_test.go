package observation

import (
	"testing"
	"time"
)

func TestRemoteReportsAreClaimsBoundToAuthenticatedIdentity(t *testing.T) {
	now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	target := Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "owner/repo", NativeID: "42"}
	report := Report{Target: target, ReaderID: "remote", ReaderRevision: "v1", SourceRevision: "r1", ObservedAt: now.Add(-time.Minute), Knowledge: "verified", Title: "fixture"}
	binding := RemoteBinding{WorkspaceID: "w", ReaderID: "remote", Target: target, MaxClockSkew: time.Minute}
	got, err := NormalizeRemoteReport(report, binding, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Knowledge != "reported" || !got.ReceivedAt.Equal(now) || got.IdempotencyKey == "" {
		t.Fatalf("unsafe normalization: %+v", got)
	}
	again, err := NormalizeRemoteReport(report, binding, now.Add(time.Second))
	if err != nil || again.IdempotencyKey != got.IdempotencyKey {
		t.Fatal("replay identity changed")
	}
	report.Target.WorkspaceID = "other"
	if _, err := NormalizeRemoteReport(report, binding, now); err == nil {
		t.Fatal("cross workspace accepted")
	}
	report.Target = target
	report.ObservedAt = now.Add(time.Hour)
	if _, err := NormalizeRemoteReport(report, binding, now); err == nil {
		t.Fatal("future report accepted")
	}
}

func TestInvestigationRequiresBoundedReadOnlyScope(t *testing.T) {
	spec := InvestigationSpec{Objective: "Inspect rollout", Target: Target{WorkspaceID: "w", ConnectionID: "c", Source: "github", Kind: "issue", Repository: "o/r", NativeID: "1"}, PresetRef: "existing-preset", AllowedSources: []string{"github"}, EvidenceRequirements: []string{"serving revision"}, Limits: Limits{Timeout: time.Minute, MaxOutputBytes: 65536, MaxRequests: 10, MaxTokens: 1000}, Interval: time.Hour}
	if err := spec.Validate(); err != nil {
		t.Fatal(err)
	}
	spec.Limits.MaxTokens = 0
	if err := spec.Validate(); err == nil {
		t.Fatal("unbounded tokens accepted")
	}
}

func TestFreshReadIdentityAdvancesWithoutInventingSourceProgress(t *testing.T) {
	a := newReport(fixtureTarget(), "builtin:fixture")
	a.SourceRevision = "unchanged"
	b := a
	b.ObservedAt = a.ObservedAt.Add(time.Minute)
	b.ReceivedAt = b.ObservedAt
	first, err := finishReport(a)
	if err != nil {
		t.Fatal(err)
	}
	next, err := finishReport(b)
	if err != nil {
		t.Fatal(err)
	}
	if first.IdempotencyKey == next.IdempotencyKey {
		t.Fatal("fresh polling observation collapsed into an old receipt")
	}
	if first.SemanticKey != next.SemanticKey {
		t.Fatal("unchanged source was treated as progress")
	}
	if next.SourceActivityAt != nil {
		t.Fatal("polling invented source activity")
	}
}
