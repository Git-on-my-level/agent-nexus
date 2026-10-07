// Package foreignvar forbids mutating another package's package-level variables.
// Callable-surface allowlists may name a standard-library global such as
// crypto/rand.Reader; that review does not permit rebinding it.
package foreignvar

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Finding is one forbidden mutation.
type Finding struct {
	Message string
}

// Findings reports assignments to, and addresses taken of, package-level
// variables declared in another package. The standard library is included.
// main packages and _test.go files are exempt: both are allowed to install
// process-wide test seams.
func Findings(fset *token.FileSet, pkg *types.Package, files []*ast.File, info *types.Info) []Finding {
	if pkg == nil || pkg.Name() == "main" || info == nil {
		return nil
	}
	var found []Finding
	for _, file := range files {
		position := fset.File(file.Pos())
		if position == nil || strings.HasSuffix(position.Name(), "_test.go") {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					report(&found, pkg, info, lhs, "assignment to package-level var %s")
				}
			case *ast.IncDecStmt:
				report(&found, pkg, info, n.X, "assignment to package-level var %s")
			case *ast.RangeStmt:
				if n.Tok == token.ASSIGN {
					report(&found, pkg, info, n.Key, "assignment to package-level var %s")
					report(&found, pkg, info, n.Value, "assignment to package-level var %s")
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					report(&found, pkg, info, n.X, "address of package-level var %s")
				}
			}
			return true
		})
	}
	return found
}

func report(found *[]Finding, pkg *types.Package, info *types.Info, expr ast.Expr, format string) {
	variable := foreignPackageVar(pkg, info, expr)
	if variable == nil {
		return
	}
	*found = append(*found, Finding{Message: fmt.Sprintf(format, variable.Pkg().Path()+"."+variable.Name())})
}

// foreignPackageVar resolves a direct reference to a package-level variable
// declared by a package other than the one being checked. Field and method
// selections are not that variable, so mutating a field of a global pointer
// is outside this check.
func foreignPackageVar(pkg *types.Package, info *types.Info, expr ast.Expr) *types.Var {
	if expr == nil {
		return nil
	}
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			break
		}
		expr = paren.X
	}
	var object types.Object
	switch expr := expr.(type) {
	case *ast.Ident:
		object = info.ObjectOf(expr)
	case *ast.SelectorExpr:
		if _, selected := info.Selections[expr]; selected {
			return nil
		}
		object = info.ObjectOf(expr.Sel)
	default:
		return nil
	}
	variable, ok := object.(*types.Var)
	if !ok || variable.Pkg() == nil || variable.Pkg() == pkg || variable.Parent() != variable.Pkg().Scope() {
		return nil
	}
	return variable
}
