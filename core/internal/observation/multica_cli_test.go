package observation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMulticaCLIReusesProfileWithoutReadingTokens(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture-multica")
	script := `#!/bin/sh
case "$*" in
 *"issue get fixture-id"*) printf '%s' '{"id":"fixture-id","workspace_id":"source-w","identifier":"SCA-1","title":"Fixture","status":"in_progress","updated_at":"2026-09-07T00:00:00Z"}' ;;
 *"issue runs fixture-id"*) printf '%s' '[{"id":"run-1","issue_id":"fixture-id","status":"completed"}]' ;;
 *"issue pull-requests fixture-id"*) printf '%s' '[]' ;;
 *) exit 2 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	r, err := NewMulticaCLIReader(MulticaCLIConfig{Binary: binary, Profile: "fixture", BaseURL: "https://multica.example.test", WorkspaceID: "w", ConnectionID: "c", SourceWorkspaceID: "source-w", Timeout: time.Second, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	target := Target{WorkspaceID: "w", ConnectionID: "c", Source: "multica", Kind: "issue", NativeID: "fixture-id"}
	out, err := r.Read(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if out.NativeStatus != "in_progress" || out.Facts["phase"] != "in_progress" || out.ReaderID != "builtin:multica-cli" {
		t.Fatalf("wrong CLI report: %+v", out)
	}
	target.NativeID = "--help"
	if _, err := r.Read(context.Background(), target); err == nil {
		t.Fatal("CLI flag injection accepted")
	}
}
