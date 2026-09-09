package observation

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const handwrittenGitHubTransform = `#include <stdio.h>
#include <string.h>
int main(void){
 char input[1048576];
 size_t n=fread(input,1,sizeof(input)-1,stdin); input[n]=0;
 if (strstr(input,"\"reject\":true")) { puts("{\"error\":\"fixture rejected\"}"); return 0; }
 if (!strstr(input,"Git-on-my-level/agent-nexus") && !strstr(input,"title")) {
  puts("{\"error\":\"snapshot missing repository identity\"}");
  return 0;
 }
 puts("{\"facts\":{\"reader\":\"handwritten-github-transform\",\"has_snapshot\":true},\"uncertainty\":[\"hand-written artifact; not model-generated\"],\"evidence\":[]}");
 return 0;
}
`

func TestLiveGitHubJITCanary(t *testing.T) {
	if os.Getenv("ANX_OBSERVATION_GITHUB_CANARY") != "1" {
		t.Skip("set ANX_OBSERVATION_GITHUB_CANARY=1 to run the public GitHub canary")
	}
	_ = isolationRunnerOrSkip(t)
	root := os.Getenv("ANX_JIT_STATE_ROOT")
	if root == "" {
		root = filepath.Join(t.TempDir(), "managed")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(resolved, 0700); err != nil {
		t.Fatal(err)
	}
	policy := JITPolicy{MaxArtifactBytes: 16 << 20, Limits: IsolationLimits{Timeout: 30 * time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 1 << 20, CPUSeconds: 5, Processes: 8, FileBytes: 1 << 20}, FailureThreshold: 2}
	manager, err := NewJITManager(resolved, policy)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	binary := compileIsolatedFixture(t, dir, "github-transform", handwrittenGitHubTransform)
	artifact, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{WorkspaceID: "ws_main", ConnectionID: "github-main", Source: "github", Kind: "issue", NativeID: "208", Repository: "Git-on-my-level/agent-nexus"}
	version, err := manager.Stage(Manifest{AdapterID: "github-issue-transform", Target: target, Limits: policy.Limits}, artifact)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewGitHubReader(HTTPConfig{BaseURL: "https://api.github.com", WorkspaceID: target.WorkspaceID, ConnectionID: target.ConnectionID, Timeout: 30 * time.Second, MaxPages: 5, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	switch version.State {
	case "staged":
		cases := []ValidationCase{{Name: "happy", Input: []byte(`{"title":"Git-on-my-level/agent-nexus"}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
		if err := manager.Validate(context.Background(), "github-issue-transform", version.Revision, cases); err != nil {
			t.Fatal(err)
		}
		fallthrough
	case "validated":
		if _, err := manager.Canary(context.Background(), "github-issue-transform", version.Revision, reader); err != nil {
			t.Fatal(err)
		}
		fallthrough
	case "canaried":
		if err := manager.Activate("github-issue-transform", version.Revision); err != nil {
			t.Fatal(err)
		}
	case "active":
	default:
		t.Fatalf("unexpected adapter state %q", version.State)
	}
	read, err := manager.Read(context.Background(), "github-issue-transform", reader)
	if err != nil {
		t.Fatal(err)
	}
	findings, _ := read.Facts["generated_findings"].(map[string]any)
	if findings["reader"] != "handwritten-github-transform" || read.Title == "" || read.ReaderID != "jit:github-issue-transform" {
		t.Fatalf("live canary missing generated findings: title=%q facts=%v", read.Title, read.Facts)
	}
	t.Logf("canary title=%q native=%s revision=%s findings=%v", read.Title, read.NativeStatus, version.Revision, findings)
}

func generatedTransformSource(t *testing.T) []byte {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "dev", "github-transform.c"))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func stageGenerated(t *testing.T, manager *JITManager, manifest Manifest, source []byte) Version {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "transform.c"), source, 0600); err != nil {
		t.Fatal(err)
	}
	compiled, err := CompileGeneratedC(dir, source)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(compiled)
	if err != nil {
		t.Fatal(err)
	}
	version, err := manager.Stage(manifest, artifact)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func lifecycleGenerated(t *testing.T, manager *JITManager, adapter string, version Version, source Reader) Report {
	t.Helper()
	cases := []ValidationCase{{Name: "happy", Input: []byte(`{"title":"Git-on-my-level/agent-nexus"}`), WantValid: true}, {Name: "rejected", Input: []byte(`{"reject":true}`), WantValid: false}}
	switch version.State {
	case "staged":
		if err := manager.Validate(context.Background(), adapter, version.Revision, cases); err != nil {
			t.Fatal(err)
		}
		fallthrough
	case "validated":
		if _, err := manager.Canary(context.Background(), adapter, version.Revision, source); err != nil {
			t.Fatal(err)
		}
		fallthrough
	case "canaried":
		if err := manager.Activate(adapter, version.Revision); err != nil {
			t.Fatal(err)
		}
	case "active":
	default:
		t.Fatalf("unexpected adapter state %q", version.State)
	}
	read, err := manager.Read(context.Background(), adapter, source)
	if err != nil {
		t.Fatal(err)
	}
	findings, _ := read.Facts["generated_findings"].(map[string]any)
	if findings["reader"] != "generated-c-transform" || read.Title == "" {
		t.Fatalf("generated canary missing findings: title=%q facts=%v", read.Title, read.Facts)
	}
	return read
}

func TestLiveGeneratedGitHubCanary(t *testing.T) {
	if os.Getenv("ANX_OBSERVATION_GITHUB_CANARY") != "1" {
		t.Skip("set ANX_OBSERVATION_GITHUB_CANARY=1 to run the public GitHub generated canary")
	}
	_ = isolationRunnerOrSkip(t)
	root := filepath.Join(t.TempDir(), "managed")
	policy := JITPolicy{MaxArtifactBytes: 16 << 20, Limits: IsolationLimits{Timeout: 30 * time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 1 << 20, CPUSeconds: 5, Processes: 8, FileBytes: 1 << 20}, FailureThreshold: 2}
	manager, err := NewJITManager(root, policy)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{WorkspaceID: "ws_main", ConnectionID: "github-main", Source: "github", Kind: "issue", NativeID: "208", Repository: "Git-on-my-level/agent-nexus"}
	version := stageGenerated(t, manager, Manifest{AdapterID: "github-issue-transform", Target: target, Limits: policy.Limits}, generatedTransformSource(t))
	reader, err := NewGitHubReader(HTTPConfig{BaseURL: "https://api.github.com", WorkspaceID: target.WorkspaceID, ConnectionID: target.ConnectionID, Timeout: 30 * time.Second, MaxPages: 5, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	read := lifecycleGenerated(t, manager, "github-issue-transform", version, reader)
	t.Logf("generated github canary title=%q native=%s revision=%s language=%s", read.Title, read.NativeStatus, version.Revision, GeneratedLanguage)
}

func TestLiveGeneratedMulticaCanary(t *testing.T) {
	if os.Getenv("ANX_OBSERVATION_MULTICA_CANARY") != "1" {
		t.Skip("set ANX_OBSERVATION_MULTICA_CANARY=1 with ANX_MULTICA_PROFILE, ANX_MULTICA_WORKSPACE_ID, ANX_MULTICA_ISSUE_ID (read-only)")
	}
	_ = isolationRunnerOrSkip(t)
	bin := os.Getenv("ANX_MULTICA_BIN")
	if bin == "" {
		resolved, err := exec.LookPath("multica")
		if err != nil {
			t.Fatal("multica CLI is not on PATH")
		}
		bin, err = filepath.Abs(resolved)
		if err != nil {
			t.Fatal(err)
		}
	}
	profile := os.Getenv("ANX_MULTICA_PROFILE")
	workspace := os.Getenv("ANX_MULTICA_WORKSPACE_ID")
	issue := os.Getenv("ANX_MULTICA_ISSUE_ID")
	base := os.Getenv("ANX_MULTICA_BASE_URL")
	if profile == "" || workspace == "" || issue == "" || base == "" {
		t.Fatal("ANX_MULTICA_PROFILE, ANX_MULTICA_WORKSPACE_ID, ANX_MULTICA_ISSUE_ID and ANX_MULTICA_BASE_URL are required")
	}
	root := filepath.Join(t.TempDir(), "managed")
	policy := JITPolicy{MaxArtifactBytes: 16 << 20, Limits: IsolationLimits{Timeout: 30 * time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 1 << 20, CPUSeconds: 5, Processes: 8, FileBytes: 1 << 20}, FailureThreshold: 2}
	manager, err := NewJITManager(root, policy)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{WorkspaceID: "ws_main", ConnectionID: "multica-main", Source: "multica", Kind: "issue", NativeID: issue}
	version := stageGenerated(t, manager, Manifest{AdapterID: "multica-issue-transform", Target: target, Limits: policy.Limits}, generatedTransformSource(t))
	reader, err := NewMulticaCLIReader(MulticaCLIConfig{Binary: bin, Profile: profile, BaseURL: base, WorkspaceID: target.WorkspaceID, ConnectionID: target.ConnectionID, SourceWorkspaceID: workspace, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	read := lifecycleGenerated(t, manager, "multica-issue-transform", version, reader)
	t.Logf("generated multica canary title=%q native=%s revision=%s", read.Title, read.NativeStatus, version.Revision)
}

func TestLiveGenerateHarnessGitHub(t *testing.T) {
	if os.Getenv("ANX_OBSERVATION_GENERATE_CANARY") != "1" {
		t.Skip("set ANX_OBSERVATION_GENERATE_CANARY=1 to generate a transform with agentctl/omp glm-5.3")
	}
	_ = isolationRunnerOrSkip(t)
	root := filepath.Join(t.TempDir(), "managed")
	policy := JITPolicy{MaxArtifactBytes: 16 << 20, Limits: IsolationLimits{Timeout: 30 * time.Second, MemoryBytes: 128 << 20, OutputBytes: 65536, InputBytes: 1 << 20, CPUSeconds: 5, Processes: 8, FileBytes: 1 << 20}, FailureThreshold: 2}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(root, "github-issue-transform", ".generate")
	target := Target{WorkspaceID: "ws_main", ConnectionID: "github-main", Source: "github", Kind: "issue", NativeID: "208", Repository: "Git-on-my-level/agent-nexus"}
	manifest := Manifest{AdapterID: "github-issue-transform", Target: target, Limits: policy.Limits}
	reader, err := NewGitHubReader(HTTPConfig{BaseURL: "https://api.github.com", WorkspaceID: target.WorkspaceID, ConnectionID: target.ConnectionID, Timeout: 30 * time.Second, MaxPages: 5, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	sample, err := reader.Read(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	generated, err := RunGenerateHarness(ctx, GenerateRequest{Workspace: workspace, Manifest: manifest, Snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	if generated.Language != GeneratedLanguage {
		t.Fatalf("language %q", generated.Language)
	}
	t.Logf("harness provider=%s model=%s source_bytes=%d artifact_bytes=%d", generated.Provider, generated.Model, len(generated.Source), len(generated.Binary))
	manager, err := NewJITManager(root, policy)
	if err != nil {
		t.Fatal(err)
	}
	version, err := manager.Stage(manifest, generated.Binary)
	if err != nil {
		t.Fatal(err)
	}
	read := lifecycleGenerated(t, manager, "github-issue-transform", version, reader)
	t.Logf("model-generated github canary title=%q revision=%s", read.Title, version.Revision)
}
