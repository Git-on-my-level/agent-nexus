package foreignvar

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFixtureRejectsStdlibGlobalMutation(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test file unavailable")
	}
	checked := mustCheck(t, filepath.Join(filepath.Dir(file), "testdata", "src"), false, "./reject")
	if len(checked) != 1 {
		t.Fatalf("packages: %+v", checked)
	}
	var got []string
	for _, finding := range checked[0].Findings {
		got = append(got, finding.Message)
	}
	want := []string{
		"assignment to package-level var crypto/rand.Reader",
		"address of package-level var crypto/rand.Reader",
		"assignment to package-level var crypto/rand.Reader",
		"address of package-level var crypto/rand.Reader",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("fixture diagnostics:\n%s", strings.Join(got, "\n"))
	}
}

func TestAllowsMainTestsSamePackageAndFieldStores(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module fixture.foreignvar\n\ngo 1.23\n")
	writeFixture(t, root, "other/other.go", "package other\n\nvar Count int\n")
	writeFixture(t, root, "bad/bad.go", `package bad

import "fixture.foreignvar/other"

func Mutate() {
	other.Count++
	other.Count += 1
	for other.Count = range []int{1, 2} {
	}
}
`)
	writeFixture(t, root, "ok/ok.go", `package ok

import (
	"crypto/rand"
	"io"
	"net/http"
	"time"
)

var local io.Reader

func Use() {
	local = rand.Reader
	_ = &local
	_, _ = rand.Read(make([]byte, 1))
	http.DefaultClient.Timeout = time.Second
}
`)
	writeFixture(t, root, "ok/ok_test.go", `package ok

import (
	"crypto/rand"
	"testing"
)

func TestSeam(t *testing.T) {
	rand.Reader = nil
	_ = &rand.Reader
}
`)
	writeFixture(t, root, "cmd/main.go", `package main

import "crypto/rand"

func main() {
	rand.Reader = nil
	_ = &rand.Reader
}
`)
	checked := mustCheck(t, root, true, "./...")
	var bad, allowed []string
	for _, pkg := range checked {
		var messages []string
		for _, finding := range pkg.Findings {
			messages = append(messages, pkg.ImportPath+": "+finding.Message)
		}
		if strings.HasSuffix(pkg.ImportPath, "/bad") {
			bad = messages
			continue
		}
		allowed = append(allowed, messages...)
	}
	wantBad := []string{
		"fixture.foreignvar/bad: assignment to package-level var fixture.foreignvar/other.Count",
		"fixture.foreignvar/bad: assignment to package-level var fixture.foreignvar/other.Count",
		"fixture.foreignvar/bad: assignment to package-level var fixture.foreignvar/other.Count",
	}
	if strings.Join(bad, "\n") != strings.Join(wantBad, "\n") {
		t.Fatalf("bad package diagnostics:\n%s", strings.Join(bad, "\n"))
	}
	if len(allowed) != 0 {
		t.Fatalf("main, tests, and same-package stores were rejected:\n%s", strings.Join(allowed, "\n"))
	}
}

func TestCorePackagesDoNotMutateForeignPackageVars(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test file unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	checked := mustCheck(t, root, false, "./internal/...")
	if len(checked) < 20 {
		t.Fatalf("checked %d packages", len(checked))
	}
	var diagnostics []string
	for _, pkg := range checked {
		for _, finding := range pkg.Findings {
			diagnostics = append(diagnostics, pkg.ImportPath+": "+finding.Message)
		}
	}
	if len(diagnostics) != 0 {
		t.Fatalf("foreign package-level var mutations:\n%s", strings.Join(diagnostics, "\n"))
	}
}

func mustCheck(t *testing.T, dir string, tests bool, patterns ...string) []checkedPackage {
	t.Helper()
	checked, err := checkPatterns(dir, tests, patterns...)
	if err != nil {
		t.Fatal(err)
	}
	return checked
}

func writeFixture(t *testing.T, root, path, source string) {
	t.Helper()
	path = filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
