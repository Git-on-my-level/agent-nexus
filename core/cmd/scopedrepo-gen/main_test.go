package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedTemplatesCurrent(t *testing.T) {
	root := filepath.Join("..", "..", "internal", "scopedrepo")
	got, e := generate(root)
	if e != nil {
		t.Fatal(e)
	}
	want, e := os.ReadFile(filepath.Join(root, "queries_gen.go"))
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("run go generate ./internal/scopedrepo")
	}
}
func TestManifestRejectsUnownedTemplate(t *testing.T) {
	root := t.TempDir()
	if e := os.Mkdir(filepath.Join(root, "queries"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"read":"scope"}`), 0644); e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"read", "unowned"} {
		if e := os.WriteFile(filepath.Join(root, "queries", n+".sql"), []byte("SELECT 1"), 0644); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := generate(root); e == nil {
		t.Fatal("unowned template accepted")
	}
	if e := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"read":"invented-policy","unowned":"scope"}`), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := generate(root); e == nil {
		t.Fatal("manifest became a policy language")
	}
}
