package readmodel

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The kernel is not approved for serving. Remove this gate only when the
// transaction-bound repository/hooks, old-policy uniform parity and actual HTTP
// cost/route/storage tests land together; a default-false handler flag alone is
// not adequate proof that a new call site cannot accidentally select the bridge.
func TestReadModelNotServing(t *testing.T) {
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
