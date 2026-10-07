package readmodel

import (
	"fmt"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The kernel is not approved for serving. Remove this gate only when the
// transaction-bound repository/hooks, old-policy uniform parity and actual HTTP
// cost/route/storage tests land together; a default-false handler flag alone is
// not adequate proof that a new call site cannot accidentally select the bridge.
func TestReadModelNotServing(t *testing.T) {
	if err := checkKernelDependencyClosure("..", "agent-nexus-core/internal/readmodel"); err != nil {
		t.Error(err)
	}
	if err := checkKernelCallableSurface("..", "testdata/kernel_callable_surface.txt"); err != nil {
		t.Error(err)
	}
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, data, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				if name == "agent-nexus-core/internal/readmodel" && !trustedRepositoryAdapter(path) {
					t.Errorf("read-model bridge not approved for production consumption: %s", path)
				}
				if inKernelTree(path) && !kernelImportAllowed(name) {
					t.Errorf("read-model kernel must not acquire raw storage/authority: %s", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Walk every allowed application dependency, including files excluded by the
// current platform/build tags. An allowed package is not a trusted factory.
// Standard-library operations remain a closed, reviewed leaf allowlist.
func checkKernelDependencyClosure(internalRoot, rootPackage string) error {
	_, err := kernelDependencyPackages(internalRoot, rootPackage)
	return err
}

func kernelDependencyPackages(internalRoot, rootPackage string) ([]string, error) {
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		visited[name] = true
		const prefix = "agent-nexus-core/internal/"
		if !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("invalid application dependency: %s", name)
		}
		dir := filepath.Join(internalRoot, strings.TrimPrefix(name, prefix))
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				dependency, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				if !kernelImportAllowed(dependency) {
					return fmt.Errorf("kernel dependency %s acquires forbidden import %s in %s", name, dependency, path)
				}
				if strings.HasPrefix(dependency, prefix) {
					if err := visit(dependency); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	if err := visit(rootPackage); err != nil {
		return nil, err
	}
	var packages []string
	for name := range visited {
		if name != rootPackage {
			packages = append(packages, name)
		}
	}
	sort.Strings(packages)
	return packages, nil
}

// This is the reviewer's exact indirect SQL ingress: the kernel imports only
// scopes, which exports sql.Open; its caller can read a private document title.
// The previous direct-import guard admitted both production files.
func TestKernelRejectsSQLFactoryInAllowedScopeDependency(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"scopes/types.go":   "package scopes\nimport \"context\"\nvar _ context.Context\n",
		"readmodel/page.go": "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\n",
	}
	write := func(path, source string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for path, source := range files {
		write(path, source)
	}
	if err := checkKernelDependencyClosure(root, "agent-nexus-core/internal/readmodel"); err != nil {
		t.Fatal(err)
	}
	write("scopes/zz_review_bypass.go", `package scopes
import "database/sql"
var ReviewRawFactory = sql.Open
`)
	write("readmodel/review_bypass.go", `package readmodel
import "agent-nexus-core/internal/scopes"
func reviewIndirectBypass(path string) (string,error) {
 db,e:=scopes.ReviewRawFactory("sqlite",path)
 if e!=nil {return "",e};defer db.Close()
 var secret string
 e=db.QueryRow("SELECT title FROM documents LIMIT 1").Scan(&secret)
 return secret,e
}
`)
	if err := checkKernelDependencyClosure(root, "agent-nexus-core/internal/readmodel"); err == nil || !strings.Contains(err.Error(), "database/sql") || !strings.Contains(err.Error(), "scopes") {
		t.Fatalf("indirect SQL factory escaped: %v", err)
	}
}

// Storage supplies an implementation through a scopes interface; the kernel's
// imports remain unchanged. The clean baseline passes, the valid wired fixture
// fails API review, and removing that fixture restores the same golden.
func TestKernelRejectsStorageInterfaceInAllowedScopeDependency(t *testing.T) {
	root := t.TempDir()
	writeKernelFixture(t, root, "scopes/types.go", "package scopes\ntype ID string\n")
	writeKernelFixture(t, root, "readmodel/page.go", `package readmodel
import "agent-nexus-core/internal/scopes"
func ScopeID() scopes.ID { return "" }
`)
	baseline, err := kernelCallableSurface(root)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(root, "reviewed.txt")
	writeKernelFixture(t, root, "reviewed.txt", baseline)
	if err := checkKernelCallableSurface(root, golden); err != nil {
		t.Fatal(err)
	}
	writeKernelFixture(t, root, "scopes/review_bypass.go", `package scopes
import "context"
type ReviewStorageReader interface { Read(context.Context) (string, error) }
`)
	writeKernelFixture(t, root, "readmodel/review_bypass.go", `package readmodel
import (
 "context"
 "agent-nexus-core/internal/scopes"
)
func ReviewKernelRead(ctx context.Context, reader scopes.ReviewStorageReader) (string, error) {
 return reader.Read(ctx)
}
`)
	writeKernelFixture(t, root, "scopedrepo/review_bypass.go", `package scopedrepo
import (
 "context"
 "database/sql"
 "agent-nexus-core/internal/readmodel"
 "agent-nexus-core/internal/scopes"
)
type storageReader struct { db *sql.DB }
func (r storageReader) Read(ctx context.Context) (string, error) {
 var title string
 err := r.db.QueryRowContext(ctx, "SELECT title FROM documents LIMIT 1").Scan(&title)
 return title, err
}
var _ scopes.ReviewStorageReader = storageReader{}
func ReviewWiredRead(ctx context.Context, db *sql.DB) (string, error) {
 return readmodel.ReviewKernelRead(ctx, storageReader{db})
}
`)
	// Type-check the external implementation and actual call wiring too.
	fset := token.NewFileSet()
	loader := &kernelTypeImporter{
		root: root, fset: fset, packages: map[string]*types.Package{},
		standard: importer.ForCompiler(fset, "gc", nil),
	}
	if _, err := loader.Import("agent-nexus-core/internal/scopedrepo"); err != nil {
		t.Fatal(err)
	}
	if err := checkKernelDependencyClosure(root, kernelPackage); err != nil {
		t.Fatalf("fixture must pass the import-only guard: %v", err)
	}
	if err := checkKernelCallableSurface(root, golden); err == nil || !strings.Contains(err.Error(), "ReviewStorageReader") {
		t.Fatalf("storage-backed interface escaped API review: %v", err)
	}
	for _, path := range []string{"scopes/review_bypass.go", "readmodel/review_bypass.go", "scopedrepo/review_bypass.go"} {
		if err := os.Remove(filepath.Join(root, path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkKernelCallableSurface(root, golden); err != nil {
		t.Fatalf("removing the fixture must restore the reviewed surface: %v", err)
	}
}

func writeKernelFixture(t *testing.T, root, path, source string) {
	t.Helper()
	path = filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}

// A deny-list of raw handles misses helper packages that acquire them on the
// kernel's behalf. The closed computation tree admits only its immutable scope
// types and these pure/cryptographic standard-library operations. New helper
// packages cannot add a transitive factory or filesystem/network ingress.
func kernelImportAllowed(name string) bool {
	switch name {
	case "agent-nexus-core/internal/scopes", "bytes", "container/heap", "context",
		"crypto/aes", "crypto/cipher", "crypto/rand", "crypto/sha256",
		"encoding/base64", "encoding/json", "errors", "fmt", "math", "sort",
		"strings", "time", "unicode/utf8":
		return true
	default:
		return false
	}
}

func TestKernelCannotAcquireIndirectFactories(t *testing.T) {
	for _, name := range []string{"database/sql", "agent-nexus-core/internal/primitives", "agent-nexus-core/internal/storage", "agent-nexus-core/internal/scopedrepo", "agent-nexus-core/internal/pm", "os", "net/http", "unsafe", "C", "agent-nexus-core/internal/readmodel/nested"} {
		if kernelImportAllowed(name) {
			t.Fatal("indirect or direct authority ingress admitted", name)
		}
	}
}

// These exact adapters and fixed inbox computation remain unreachable from serving: scopedrepo's independent
// TestFoundationNotServing rejects every production import of that package.
// This admits the reviewed dependency edge, not a handler or constructor.
func trustedRepositoryAdapter(path string) bool {
	switch filepath.ToSlash(filepath.Clean(path)) {
	case "../scopedrepo/readmodel_adapter.go", "../scopedrepo/readmodel_hook.go", "../scopedrepo/feed_ordered_adapter.go", "../scopedrepo/inbox_dispatch.go":
		return true
	default:
		return false
	}
}

func TestTrustedAdapterBoundaryIsExact(t *testing.T) {
	for path, want := range map[string]bool{
		"../scopedrepo/readmodel_adapter.go":     true,
		"../scopedrepo/readmodel_hook.go":        true,
		"../scopedrepo/feed_ordered_adapter.go":  true,
		"../scopedrepo/inbox_dispatch.go":        true,
		"../scopedrepo/other.go":                 false,
		"../server/readmodel_adapter.go":         false,
		"../scopedrepo/nested/readmodel_hook.go": false,
		"../../cmd/readmodel_hook.go":            false,
	} {
		if got := trustedRepositoryAdapter(filepath.FromSlash(path)); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}

func inKernelTree(path string) bool {
	dir := filepath.Clean(filepath.Dir(path))
	root := filepath.Join("..", "readmodel")
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

func TestKernelBoundaryIncludesNestedPackages(t *testing.T) {
	for path, want := range map[string]bool{
		"../readmodel/page.go":         true,
		"../readmodel/nested/store.go": true,
		"../readmodels/store.go":       false,
		"../storage/store.go":          false,
		"../../cmd/main.go":            false,
	} {
		if got := inKernelTree(filepath.FromSlash(path)); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}
