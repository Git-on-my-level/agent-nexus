package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEveryStdinReadUsesSharedAccessor(t *testing.T) {
	t.Parallel()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "vendor" || strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !callArgReadsStdin(call) {
				return true
			}
			accessor := enclosingFuncName(file, call)
			if accessor != "readStdinBytes" && accessor != "readStdinScanner" {
				t.Errorf("%s reads stdin outside the shared accessors", fset.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func callArgReadsStdin(call *ast.CallExpr) bool {
	for _, arg := range call.Args {
		if exprReadsStdin(arg) {
			return true
		}
	}
	return false
}

func exprReadsStdin(expr ast.Expr) bool {
	switch node := expr.(type) {
	case *ast.SelectorExpr:
		return node.Sel != nil && node.Sel.Name == "Stdin"
	case *ast.CallExpr:
		for _, arg := range node.Args {
			if exprReadsStdin(arg) {
				return true
			}
		}
	case *ast.ParenExpr:
		return exprReadsStdin(node.X)
	case *ast.StarExpr:
		return exprReadsStdin(node.X)
	case *ast.UnaryExpr:
		return exprReadsStdin(node.X)
	}
	return false
}

func enclosingFuncName(file *ast.File, target ast.Node) string {
	name := ""
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Body == nil || target.Pos() < fn.Body.Pos() || target.End() > fn.Body.End() {
			return true
		}
		if fn.Name != nil {
			name = fn.Name.Name
		}
		return false
	})
	return name
}
