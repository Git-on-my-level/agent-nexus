package readmodel

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type kernelInspection struct {
	loader       *kernelTypeImporter
	dependencies []string
	kernels      map[string]bool
}

// Include independent nested kernel packages and production files excluded by
// build tags. Type-checking all of them fails closed on conflicting declarations.
func inspectKernel(internalRoot string) (*kernelInspection, error) {
	fset := token.NewFileSet()
	a := &kernelInspection{
		loader:  &kernelTypeImporter{root: internalRoot, fset: fset, packages: map[string]*types.Package{}, standard: importer.ForCompiler(fset, "gc", nil)},
		kernels: map[string]bool{kernelPackage: true},
	}
	err := filepath.WalkDir(filepath.Join(internalRoot, "readmodel"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "vendor" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			rel, err := filepath.Rel(internalRoot, filepath.Dir(path))
			if err != nil {
				return err
			}
			a.kernels[applicationPrefix+filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dependencies := map[string]bool{}
	for _, name := range sortedKeys(a.kernels) {
		deps, err := kernelDependencyPackages(internalRoot, name)
		if err != nil {
			return nil, err
		}
		for _, dep := range deps {
			dependencies[dep] = true
		}
		if _, err := a.loader.Import(name); err != nil {
			return nil, err
		}
	}
	a.dependencies = sortedKeys(dependencies)
	return a, nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Uses resolves aliases, dot imports, type arguments and method expressions.
// Selections resolves fields, promoted methods and interface/method dispatch.
// Count uses per declaration, without line numbers: adding another reference in
// an existing function requires review too, while moving lines does not.
func (a *kernelInspection) symbolUses() string {
	counts := map[string]int{}
	for _, name := range sortedKeys(a.kernels) {
		info := a.loader.infos[name]
		context := func(pos token.Pos) string {
			for _, file := range a.loader.files[name] {
				if pos < file.Pos() || pos > file.End() {
					continue
				}
				path, _ := filepath.Rel(a.loader.root, a.loader.fset.Position(pos).Filename)
				for _, decl := range file.Decls {
					if pos < decl.Pos() || pos > decl.End() {
						continue
					}
					switch decl := decl.(type) {
					case *ast.FuncDecl:
						function := decl.Name.Name
						if object, ok := info.Defs[decl.Name].(*types.Func); ok {
							if receiver := object.Type().(*types.Signature).Recv(); receiver != nil {
								function = types.TypeString(receiver.Type(), packageQualifier) + "." + function
							}
						}
						return filepath.ToSlash(path) + ":func " + function
					case *ast.GenDecl:
						for _, spec := range decl.Specs {
							if pos < spec.Pos() || pos > spec.End() {
								continue
							}
							switch spec := spec.(type) {
							case *ast.TypeSpec:
								return filepath.ToSlash(path) + ":type " + spec.Name.Name
							case *ast.ValueSpec:
								var symbols []string
								for _, ident := range spec.Names {
									symbols = append(symbols, ident.Name)
								}
								return filepath.ToSlash(path) + ":value " + strings.Join(symbols, ",")
							}
						}
					}
				}
				return filepath.ToSlash(path)
			}
			panic("typed kernel use without source context")
		}
		for ident, object := range info.Uses {
			if object.Pkg() == nil || a.kernels[object.Pkg().Path()] || object.Parent() != object.Pkg().Scope() {
				continue
			}
			counts[context(ident.Pos())+" use "+object.Pkg().Path()+"."+object.Name()]++
		}
		for expression, selection := range info.Selections {
			object := selection.Obj()
			_, method := object.(*types.Func)
			// Local data fields carry no new callable authority; local methods
			// still matter, especially existing adapter interface methods.
			if !method && (object.Pkg() == nil || a.kernels[object.Pkg().Path()]) {
				continue
			}
			origin := "builtin"
			if object.Pkg() != nil {
				origin = object.Pkg().Path()
			}
			counts[context(expression.Pos())+" select "+types.TypeString(selection.Recv(), packageQualifier)+"."+object.Name()+" from "+origin]++
		}
	}
	var lines []string
	for entry, count := range counts {
		lines = append(lines, fmt.Sprintf("%d %s", count, entry))
	}
	sort.Strings(lines)
	return "# Reviewed actual kernel symbol uses and selections, counted per declaration.\n" + strings.Join(lines, "\n") + "\n"
}

type reviewedCallableVariable struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// API and use goldens cannot detect an adapter rebinding an unchanged variable.
// Require a separate, explicit exception for every interface/function global:
// all application globals (including private ones), and exported stdlib globals.
func (a *kernelInspection) checkCallableVariables(reviewed map[string]reviewedCallableVariable) error {
	seen := map[string]bool{}
	var problems []string
	for _, pkgName := range sortedPackageNames(a.loader.packages) {
		pkg := a.loader.packages[pkgName]
		for _, name := range pkg.Scope().Names() {
			variable, ok := pkg.Scope().Lookup(name).(*types.Var)
			if !ok || name == "_" || (!strings.HasPrefix(pkgName, applicationPrefix) && !variable.Exported()) {
				continue
			}
			if containsCallableState(variable.Type(), map[types.Type]bool{}) {
				key := pkgName + "." + name
				entry, approved := reviewed[key]
				if !approved || entry.Type != types.TypeString(variable.Type(), packageQualifier) || strings.TrimSpace(entry.Reason) == "" {
					problems = append(problems, fmt.Sprintf("unreviewed callable package variable: %s (%s)", key, types.TypeString(variable.Type(), packageQualifier)))
				}
				seen[key] = true
			}
		}
	}
	for key := range reviewed {
		if !seen[key] {
			problems = append(problems, "stale callable package variable exception: "+key)
		}
	}
	if len(problems) != 0 {
		sort.Strings(problems)
		return fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return nil
}

// Callback globals remain injectable through pointers and aggregate fields.
// Apply the same explicit exception policy to those containers as direct vars.
func containsCallableState(t types.Type, visited map[types.Type]bool) bool {
	if visited[t] {
		return false
	}
	visited[t] = true
	switch t := t.Underlying().(type) {
	case *types.Interface, *types.Signature:
		return true
	case *types.Pointer:
		return containsCallableState(t.Elem(), visited)
	case *types.Slice:
		return containsCallableState(t.Elem(), visited)
	case *types.Array:
		return containsCallableState(t.Elem(), visited)
	case *types.Chan:
		return containsCallableState(t.Elem(), visited)
	case *types.Map:
		return containsCallableState(t.Key(), visited) || containsCallableState(t.Elem(), visited)
	case *types.Struct:
		for j := 0; j < t.NumFields(); j++ {
			if containsCallableState(t.Field(j).Type(), visited) {
				return true
			}
		}
	}
	return false
}

func sortedPackageNames(packages map[string]*types.Package) []string {
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func checkKernelReview(internalRoot, goldenDir string) error {
	a, err := inspectKernel(internalRoot)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(goldenDir, "kernel_callable_variables.json"))
	if err != nil {
		return err
	}
	var reviewed map[string]reviewedCallableVariable
	if err := json.Unmarshal(data, &reviewed); err != nil {
		return err
	}
	if err := a.checkCallableVariables(reviewed); err != nil {
		return err
	}
	if err := compareKernelGolden(a.apiSurface(), filepath.Join(goldenDir, "kernel_callable_surface.txt"), "callable surface"); err != nil {
		return err
	}
	return compareKernelGolden(a.symbolUses(), filepath.Join(goldenDir, "kernel_symbol_uses.txt"), "symbol uses")
}
