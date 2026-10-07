package readmodel

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const kernelPackage = "agent-nexus-core/internal/readmodel"
const applicationPrefix = "agent-nexus-core/internal/"

// Import closure alone cannot seal callbacks: a forbidden package can implement
// an allowed interface without adding an import to the kernel. Snapshot the
// entire exported API of allowed application dependencies (even unused exports),
// including signatures, variables, fields and reachable private named types.
// Standard-library packages remain the reviewed leaf allowlist in boundary_test.
// This is an API review gate, not proof that an approved implementation is pure.
func kernelCallableSurface(internalRoot string) (string, error) {
	dependencies, err := kernelDependencyPackages(internalRoot, kernelPackage)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	loader := &kernelTypeImporter{
		root: internalRoot, fset: fset, packages: map[string]*types.Package{},
		standard: importer.ForCompiler(fset, "gc", nil),
	}
	// Check production sources as well as the dependencies: fixture wiring must
	// be valid Go, rather than an inert, syntactically plausible import edge.
	if _, err := loader.Import(kernelPackage); err != nil {
		return "", err
	}
	surface := &callableSurface{entries: map[string]bool{}, visited: map[types.Type]bool{}}
	for _, name := range dependencies {
		pkg := loader.packages[name]
		for _, symbol := range pkg.Scope().Names() {
			object := pkg.Scope().Lookup(symbol)
			if !object.Exported() {
				continue
			}
			switch object.(type) {
			case *types.Func, *types.Var:
				surface.add(types.ObjectString(object, packageQualifier))
			case *types.TypeName:
				// Preserve alias declarations as well as their reachable target.
				surface.add(types.ObjectString(object, packageQualifier))
			default:
				continue // Constants cannot carry implementations/callbacks.
			}
			surface.walk(object.Type())
		}
	}
	lines := make([]string, 0, len(surface.entries))
	for entry := range surface.entries {
		lines = append(lines, entry)
	}
	sort.Strings(lines)
	return "# Reviewed kernel-reachable application API; changes require layering review.\n" + strings.Join(lines, "\n") + "\n", nil
}

// Parse every production file, matching the import guard's treatment of build
// tags/platform files. Conflicting declarations fail closed and require review;
// they must never silently remove an API from the inventory on one CI platform.
type kernelTypeImporter struct {
	root     string
	fset     *token.FileSet
	standard types.Importer
	packages map[string]*types.Package
	loading  map[string]bool
}

func (i *kernelTypeImporter) Import(name string) (*types.Package, error) {
	if !strings.HasPrefix(name, applicationPrefix) {
		return i.standard.Import(name)
	}
	if pkg := i.packages[name]; pkg != nil {
		return pkg, nil
	}
	if i.loading == nil {
		i.loading = map[string]bool{}
	}
	if i.loading[name] {
		return nil, fmt.Errorf("kernel API import cycle: %s", name)
	}
	i.loading[name] = true
	defer delete(i.loading, name)
	dir := filepath.Join(i.root, strings.TrimPrefix(name, applicationPrefix))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(i.fset, filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	config := types.Config{Importer: i}
	pkg, err := config.Check(name, i.fset, files, nil)
	if err != nil {
		return nil, fmt.Errorf("kernel API type check: %w", err)
	}
	i.packages[name] = pkg
	return pkg, nil
}

func packageQualifier(pkg *types.Package) string { return pkg.Path() }

type callableSurface struct {
	entries map[string]bool
	visited map[types.Type]bool
}

func (s *callableSurface) add(entry string) { s.entries[entry] = true }

func (s *callableSurface) walk(t types.Type) {
	if t == nil || s.visited[t] {
		return
	}
	s.visited[t] = true
	switch t := t.(type) {
	case *types.Alias:
		s.walk(types.Unalias(t))
	case *types.Named:
		// Do not fingerprint standard-library internals or toolchain-specific
		// method sets. Their qualified types stay in the application signatures.
		if t.Obj().Pkg() == nil || !strings.HasPrefix(t.Obj().Pkg().Path(), applicationPrefix) {
			return
		}
		name := types.TypeString(t, packageQualifier)
		s.add("underlying " + name + " " + types.TypeString(t.Underlying(), packageQualifier))
		for j := 0; j < t.TypeParams().Len(); j++ {
			param := t.TypeParams().At(j)
			s.add("constraint " + name + "." + param.Obj().Name() + " " + types.TypeString(param.Constraint(), packageQualifier))
			s.walk(param.Constraint())
		}
		for j := 0; j < t.TypeArgs().Len(); j++ {
			s.walk(t.TypeArgs().At(j))
		}
		// Include promoted and pointer methods; private return/embedded types
		// can expose callable methods even without an exported type declaration.
		for _, receiver := range []types.Type{t, types.NewPointer(t)} {
			methods := types.NewMethodSet(receiver)
			for j := 0; j < methods.Len(); j++ {
				method := methods.At(j).Obj()
				if method.Exported() {
					s.add("method " + types.TypeString(receiver, packageQualifier) + "." + method.Name() + " " + types.TypeString(method.Type(), packageQualifier))
					s.walk(method.Type())
				}
			}
		}
		s.walk(t.Underlying())
	case *types.Pointer:
		s.walk(t.Elem())
	case *types.Array:
		s.walk(t.Elem())
	case *types.Slice:
		s.walk(t.Elem())
	case *types.Map:
		s.walk(t.Key())
		s.walk(t.Elem())
	case *types.Chan:
		s.walk(t.Elem())
	case *types.Struct:
		for j := 0; j < t.NumFields(); j++ {
			s.walk(t.Field(j).Type())
		}
	case *types.Interface:
		t.Complete()
		for j := 0; j < t.NumMethods(); j++ {
			s.walk(t.Method(j).Type())
		}
		for j := 0; j < t.NumEmbeddeds(); j++ {
			s.walk(t.EmbeddedType(j))
		}
	case *types.Signature:
		for j := 0; j < t.TypeParams().Len(); j++ {
			s.walk(t.TypeParams().At(j).Constraint())
		}
		s.walk(t.Params())
		s.walk(t.Results())
	case *types.Tuple:
		for j := 0; j < t.Len(); j++ {
			s.walk(t.At(j).Type())
		}
	case *types.TypeParam:
		s.walk(t.Constraint())
	case *types.Union:
		for j := 0; j < t.Len(); j++ {
			s.walk(t.Term(j).Type())
		}
	}
}

func checkKernelCallableSurface(internalRoot, goldenPath string) error {
	actual, err := kernelCallableSurface(internalRoot)
	if err != nil {
		return err
	}
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		return err
	}
	if actual == string(expected) {
		return nil
	}
	want, got := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(string(expected), "\n") {
		want[line] = true
	}
	for _, line := range strings.Split(actual, "\n") {
		got[line] = true
	}
	var changes []string
	for line := range got {
		if !want[line] {
			changes = append(changes, "+ "+line)
		}
	}
	for line := range want {
		if !got[line] {
			changes = append(changes, "- "+line)
		}
	}
	sort.Strings(changes)
	return fmt.Errorf("kernel callable surface changed; review implementations and update %s explicitly:\n%s", goldenPath, strings.Join(changes, "\n"))
}

// Regeneration is explicit and separate from enforcement. Editing this golden
// is a request for API/security review, never approval to enable a reader.
func TestKernelCallableSurfaceGolden(t *testing.T) {
	if os.Getenv("ANX_UPDATE_KERNEL_API") != "1" {
		return
	}
	surface, err := kernelCallableSurface("..")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("testdata/kernel_callable_surface.txt", []byte(surface), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestKernelCallableSurfaceRejectsCallbackChanges(t *testing.T) {
	for _, fixture := range []struct {
		name, source, symbol string
	}{
		{"inferred_variable", "var ReviewCallback = func() string { return \"\" }", "ReviewCallback"},
		{"function_field", "type ReviewCallbacks struct { Read func() string }", "ReviewCallbacks"},
		{"function_parameter", "func ReviewRun(read func() string) string { return read() }", "ReviewRun"},
		{"private_result_type", "type privateReader struct { Read func() string }; func ReviewReader() *privateReader { return nil }", "privateReader"},
		{"nested_alias", "type privateCallback func() string; type ReviewAlias = []map[string]privateCallback", "privateCallback"},
		{"embedded_interface", "type privateReader interface { Read() string }; type ReviewReader interface { privateReader }", "ReviewReader"},
		{"pointer_method", "func (*ID) ReviewRead() string { return \"\" }", "ReviewRead"},
		{"excluded_build_tag", "//go:build kernel_review_fixture\n\npackage scopes\nvar ReviewCallback func() string", "ReviewCallback"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeKernelFixture(t, root, "scopes/types.go", "package scopes\ntype ID string\n")
			writeKernelFixture(t, root, "readmodel/page.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc ScopeID() scopes.ID { return \"\" }\n")
			baseline, err := kernelCallableSurface(root)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join(root, "reviewed.txt")
			writeKernelFixture(t, root, "reviewed.txt", baseline)
			source := fixture.source
			if !strings.HasPrefix(source, "//go:build") {
				source = "package scopes\n" + source
			}
			writeKernelFixture(t, root, "scopes/review_bypass.go", source)
			if err := checkKernelDependencyClosure(root, kernelPackage); err != nil {
				t.Fatalf("callback fixture must pass import-only guard: %v", err)
			}
			if err := checkKernelCallableSurface(root, golden); err == nil || !strings.Contains(err.Error(), "kernel callable surface changed") || !strings.Contains(err.Error(), fixture.symbol) {
				t.Fatalf("callback change escaped API review: %v", err)
			}
		})
	}
}

func TestKernelCallableSurfaceTracksPrivateResultChanges(t *testing.T) {
	root := t.TempDir()
	source := `package scopes
type ID string
type privateReader struct { Read func() string }
func ReviewReader() *privateReader { return nil }
`
	writeKernelFixture(t, root, "scopes/types.go", source)
	writeKernelFixture(t, root, "readmodel/page.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc ScopeID() scopes.ID { return \"\" }\n")
	baseline, err := kernelCallableSurface(root)
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join(root, "reviewed.txt")
	writeKernelFixture(t, root, "reviewed.txt", baseline)
	// Keep the exported function signature and all import edges unchanged.
	writeKernelFixture(t, root, "scopes/types.go", strings.Replace(source, "Read func() string", "Read func() ([]byte, error)", 1))
	if err := checkKernelCallableSurface(root, golden); err == nil || !strings.Contains(err.Error(), "kernel callable surface changed") || !strings.Contains(err.Error(), "underlying "+applicationPrefix+"scopes.privateReader") {
		t.Fatalf("changed callback behind a stable private return type escaped: %v", err)
	}
}
