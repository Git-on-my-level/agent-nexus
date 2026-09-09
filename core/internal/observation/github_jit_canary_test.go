package observation

import (
	"context"
	"os"
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
