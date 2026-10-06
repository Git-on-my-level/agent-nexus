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
				if name == "agent-nexus-core/internal/readmodel" {
					t.Errorf("read-model bridge not approved for production consumption: %s", path)
				}
				if inKernelTree(path) && (name == "database/sql" || name == "agent-nexus-core/internal/storage" || name == "agent-nexus-core/internal/scopedrepo") {
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
