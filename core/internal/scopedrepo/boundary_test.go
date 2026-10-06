package scopedrepo_test

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

func importsRepository(source []byte) (bool, error) {
	f, e := parser.ParseFile(token.NewFileSet(), "consumer.go", source, parser.ImportsOnly)
	if e != nil {
		return false, e
	}
	for _, i := range f.Imports {
		p, e := strconv.Unquote(i.Path.Value)
		if e != nil {
			return false, e
		}
		if p == "agent-nexus-core/internal/scopedrepo" {
			return true, nil
		}
	}
	return false, nil
}

// This release gate is intentionally stronger than a symbol deny-list: no live
// package may consume the new Store before reviewed dispatcher/migration wiring.
// Transitive calls cannot reach a capability factory without some import edge.
// Replacing this gate requires the production computation call-graph analyzer,
// route privacy tests and migration parity tests, not just adding an allow-list.
func TestFoundationNotServing(t *testing.T) {
	must(t, filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		imports, e := importsRepository(data)
		if e != nil {
			return e
		}
		if imports {
			t.Errorf("foundation not yet approved for production consumption: %s", path)
		}
		return nil
	}))
}
func TestBoundaryFindsRenamedAndDotImports(t *testing.T) {
	for _, source := range []string{
		`package bypass; import renamed "agent-nexus-core/internal/scopedrepo"`,
		`package bypass; import . "agent-nexus-core/internal/scopedrepo"`,
		`package bypass; import _ "agent-nexus-core/internal/scopedrepo"`,
	} {
		yes, e := importsRepository([]byte(source))
		must(t, e)
		if !yes {
			t.Fatal("factory import escaped", source)
		}
	}
}
