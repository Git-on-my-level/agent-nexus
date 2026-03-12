package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadVersionSuccess(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	version, err := ReadVersion(path)
	if err != nil {
		t.Fatalf("ReadVersion returned error: %v", err)
	}
	if version != "0.2.3" {
		t.Fatalf("unexpected version: got %q", version)
	}
}

func TestReadVersionMissing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "schema.yaml")
	if err := os.WriteFile(path, []byte("name: example\n"), 0o644); err != nil {
		t.Fatalf("failed to write test schema: %v", err)
	}

	_, err := ReadVersion(path)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
}
