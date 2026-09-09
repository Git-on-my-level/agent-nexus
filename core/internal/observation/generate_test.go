package observation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAndCompileGeneratedC(t *testing.T) {
	src, err := ExtractGeneratedC([]byte("here is the program:\n```c\n#include <stdio.h>\nint main(void){ puts(\"{\\\"facts\\\":{\\\"reader\\\":\\\"generated-c-transform\\\",\\\"has_snapshot\\\":true},\\\"uncertainty\\\":[],\\\"evidence\\\":[]}\"); return 0; }\n```\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "int main") {
		t.Fatalf("extracted source missing main: %s", src)
	}
	dir := t.TempDir()
	path, err := CompileGeneratedC(dir, src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateArtifact(raw); err != nil {
		t.Fatal(err)
	}
}

func TestParseHarnessRejectsSubstitutedModel(t *testing.T) {
	provider, model := ParseHarnessModel([]byte(`{"provider":"cursor","model":"claude-4.6-opus-high"}`))
	if provider != "cursor" || model != "claude-4.6-opus-high" {
		t.Fatalf("model parse: %s %s", provider, model)
	}
}

func TestHarnessExecutionID(t *testing.T) {
	raw := []byte(`{"ok":true,"result":{"id":"exec-fixture-id","state":"running"}}`)
	if harnessExecutionID(raw) != "exec-fixture-id" {
		t.Fatalf("id=%q", harnessExecutionID(raw))
	}
	if !harnessFailed([]byte(`{"outcome":{"failure":{"code":"native_execution_failed"}}}`)) {
		t.Fatal("failed harness not detected")
	}
}

func TestGenerateWorkspaceWritesManagedRevisionInputs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "generate")
	manifest := Manifest{AdapterID: "github-issue-transform", Target: Target{WorkspaceID: "ws_main", ConnectionID: "github-main", Source: "github", Kind: "issue", NativeID: "208", Repository: "Git-on-my-level/agent-nexus"}, Limits: isolationTestPolicy().Limits}
	if err := GenerateWorkspace(GenerateRequest{Workspace: dir, Manifest: manifest, Snapshot: json.RawMessage(`{"title":"sample"}`)}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "target.json", "snapshot.json", "prompt.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}
