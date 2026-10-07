package readmodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKernelReviewGoldens(t *testing.T, root string, a *kernelInspection, variables map[string]reviewedCallableVariable) string {
	t.Helper()
	data, err := json.MarshalIndent(variables, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"kernel_callable_surface.txt":    a.apiSurface(),
		"kernel_symbol_uses.txt":         a.symbolUses(),
		"kernel_callable_variables.json": string(data),
	} {
		writeKernelFixture(t, root, filepath.Join("goldens", name), source)
	}
	return filepath.Join(root, "goldens")
}

func TestKernelRejectsNewUsesWithoutDependencyAPIChanges(t *testing.T) {
	const scopesSource = `package scopes
type ID string
type ExistingReader interface { Read() string; Other() string }
type Page[T any] struct { Item T }
var ExistingError error
var ExistingFunction func() string
`
	const kernelSource = `package readmodel
import "agent-nexus-core/internal/scopes"
func ScopeID() scopes.ID { return "" }
func Existing(r scopes.ExistingReader) string { return r.Read() }
`
	for _, fixture := range []struct{ name, path, source, symbol string }{
		{"existing_interface", "readmodel/review.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc Review(r scopes.ExistingReader) string { return r.Other() }", "ExistingReader.Other"},
		{"existing_global", "readmodel/review.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc Review() string { return scopes.ExistingError.Error() }", "ExistingError"},
		{"existing_function_global", "readmodel/review.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc Review() string { return scopes.ExistingFunction() }", "ExistingFunction"},
		{"dot_import", "readmodel/review.go", "package readmodel\nimport . \"agent-nexus-core/internal/scopes\"\nfunc Review(r ExistingReader) string { return r.Other() }", "ExistingReader.Other"},
		{"aliased_generic", "readmodel/review.go", "package readmodel\nimport alias \"agent-nexus-core/internal/scopes\"\ntype Review = alias.Page[alias.ExistingReader]", "scopes.Page"},
		{"method_expression", "readmodel/review.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nvar Review = scopes.ExistingReader.Other", "ExistingReader.Other"},
		{"nested_package", "readmodel/nested/review.go", "package nested\nimport \"agent-nexus-core/internal/scopes\"\nfunc Review() scopes.ID { return \"\" }", "nested/review.go"},
		{"excluded_tag", "readmodel/review.go", "//go:build review_other_platform\n\npackage readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc Review() error { return scopes.ExistingError }", "ExistingError"},
		{"repeated_use", "readmodel/page.go", strings.Replace(kernelSource, "return r.Read()", "return r.Read()+r.Read()", 1), "2 readmodel/page.go:func Existing"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			writeKernelFixture(t, root, "scopes/types.go", scopesSource)
			writeKernelFixture(t, root, "readmodel/page.go", kernelSource)
			a, err := inspectKernel(root)
			if err != nil {
				t.Fatal(err)
			}
			// Explicit fixture exceptions isolate the use gate from the separate
			// global policy; production approves no application callback globals.
			variables := map[string]reviewedCallableVariable{
				applicationPrefix + "scopes.ExistingError":    {Type: "error", Reason: "Fixture-only existing error capability."},
				applicationPrefix + "scopes.ExistingFunction": {Type: "func() string", Reason: "Fixture-only existing callback capability."},
			}
			goldens := writeKernelReviewGoldens(t, root, a, variables)
			if err := checkKernelReview(root, goldens); err != nil {
				t.Fatalf("reviewed baseline rejected: %v", err)
			}
			writeKernelFixture(t, root, fixture.path, fixture.source)
			a, err = inspectKernel(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := compareKernelGolden(a.apiSurface(), filepath.Join(goldens, "kernel_callable_surface.txt"), "callable surface"); err != nil {
				t.Fatalf("fixture must preserve the dependency API: %v", err)
			}
			// The method-expression fixture also introduces a callable kernel
			// global. Check usage directly to verify its independent rejection.
			err = compareKernelGolden(a.symbolUses(), filepath.Join(goldens, "kernel_symbol_uses.txt"), "symbol uses")
			if err == nil || !strings.Contains(err.Error(), "kernel symbol uses changed") || !strings.Contains(err.Error(), fixture.symbol) {
				t.Fatalf("new use escaped review: %v", err)
			}
			if fixture.name == "method_expression" {
				if err := checkKernelReview(root, goldens); err == nil || !strings.Contains(err.Error(), "unreviewed callable package variable: "+kernelPackage+".Review") {
					t.Fatalf("new callable kernel global escaped: %v", err)
				}
				variable := a.loader.packages[kernelPackage].Scope().Lookup("Review")
				variables[kernelPackage+".Review"] = reviewedCallableVariable{Type: types.TypeString(variable.Type(), packageQualifier), Reason: "Fixture-only method expression."}
				data, err := json.Marshal(variables)
				if err != nil {
					t.Fatal(err)
				}
				writeKernelFixture(t, root, "goldens/kernel_callable_variables.json", string(data))
			}
			if err := checkKernelReview(root, goldens); err == nil || !strings.Contains(err.Error(), "kernel symbol uses changed") {
				t.Fatalf("production guard missed new use: %v", err)
			}
			writeKernelReviewGoldens(t, root, a, variables)
			if err := checkKernelReview(root, goldens); err != nil {
				t.Fatalf("explicitly reviewed use must pass: %v", err)
			}
		})
	}
}

func TestKernelRejectsReassignedExportedErrorWithoutAPIOrUseChanges(t *testing.T) {
	root := t.TempDir()
	writeKernelFixture(t, root, "scopes/types.go", "package scopes\nimport \"errors\"\nvar ErrDenied = errors.New(\"scope unavailable\")\n")
	writeKernelFixture(t, root, "readmodel/page.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc ReviewError() string { return scopes.ErrDenied.Error() }\n")
	a, err := inspectKernel(root)
	if err != nil {
		t.Fatal(err)
	}
	api, uses := a.apiSurface(), a.symbolUses()
	goldens := writeKernelReviewGoldens(t, root, a, nil)
	writeKernelFixture(t, root, "scopedrepo/review.go", `package scopedrepo
import (
 "database/sql"
 "agent-nexus-core/internal/scopes"
 "agent-nexus-core/internal/readmodel"
)
type storageError struct { db *sql.DB }
func (e storageError) Error() string {
 var title string
 _ = e.db.QueryRow("SELECT title FROM documents LIMIT 1").Scan(&title)
 return title
}
func Poison(db *sql.DB) string {
 scopes.ErrDenied = storageError{db}
 return readmodel.ReviewError()
}
`)
	if _, err := a.loader.Import(applicationPrefix + "scopedrepo"); err != nil {
		t.Fatalf("storage-backed reassignment must be valid Go: %v", err)
	}
	if a.apiSurface() != api || a.symbolUses() != uses {
		t.Fatal("reassignment fixture must leave dependency API and kernel uses unchanged")
	}
	if err := checkKernelCallableSurface(root, filepath.Join(goldens, "kernel_callable_surface.txt")); err != nil {
		t.Fatalf("the former API guard must admit this fixture: %v", err)
	}
	if err := checkKernelReview(root, goldens); err == nil || !strings.Contains(err.Error(), "unreviewed callable package variable: "+applicationPrefix+"scopes.ErrDenied") {
		t.Fatalf("reassignable error escaped variable policy: %v", err)
	}
	// Copy the real immutable scope implementation and require the same SQL
	// adapter's assignment to fail compilation against production constants.
	copyKernelProductionPackage(t, root, "scopes")
	a, err = inspectKernel(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ErrDenied", "ErrUpdating", "ErrBudget", "ErrDerivation", "ErrClosed"} {
		if _, ok := a.loader.packages[applicationPrefix+"scopes"].Scope().Lookup(name).(*types.Const); !ok {
			t.Fatalf("scope sentinel %s is reassignable", name)
		}
	}
	if _, err := a.loader.Import(applicationPrefix + "scopedrepo"); err == nil || !strings.Contains(err.Error(), "scopedrepo/review.go") {
		t.Fatalf("SQL adapter reassigned the production sentinel: %v", err)
	}
}

func copyKernelProductionPackage(t *testing.T, root, pkg string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", pkg))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", pkg, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		writeKernelFixture(t, root, filepath.Join(pkg, entry.Name()), string(data))
	}
}

func TestImmutableKernelSentinelCompatibility(t *testing.T) {
	a, err := inspectKernel("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ErrBudget", "ErrProjection", "ErrCursor"} {
		if _, ok := a.loader.packages[kernelPackage].Scope().Lookup(name).(*types.Const); !ok {
			t.Fatalf("kernel sentinel %s is reassignable", name)
		}
	}
	for err, message := range map[error]string{
		ErrBudget: "readmodel budget exceeded", ErrProjection: "invalid readmodel projection",
		ErrCursor: "readmodel continuation invalid; restart required",
	} {
		if err.Error() != message || !errors.Is(fmt.Errorf("wrapped: %w", err), err) {
			t.Fatalf("sentinel contract changed: %v", err)
		}
	}
}

func TestKernelCallableGlobalExceptionsAreSeparateFromGoldens(t *testing.T) {
	for _, source := range []string{
		"var callback func() string",
		"type callbackType func() string; var callback callbackType",
		"var callback *error",
		"var callback []func() string",
		"var callback struct { Read func() string }",
	} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			writeKernelFixture(t, root, "scopes/types.go", "package scopes\ntype ID string\n"+source)
			writeKernelFixture(t, root, "readmodel/page.go", "package readmodel\nimport \"agent-nexus-core/internal/scopes\"\nfunc ScopeID() scopes.ID { return \"\" }\n")
			a, err := inspectKernel(root)
			if err != nil {
				t.Fatal(err)
			}
			goldens := writeKernelReviewGoldens(t, root, a, nil)
			if err := checkKernelReview(root, goldens); err == nil || !strings.Contains(err.Error(), "unreviewed callable package variable: "+applicationPrefix+"scopes.callback") {
				t.Fatalf("regenerating API/use goldens approved a callback global: %v", err)
			}
			variable := a.loader.packages[applicationPrefix+"scopes"].Scope().Lookup("callback")
			exception := map[string]reviewedCallableVariable{
				applicationPrefix + "scopes.callback": {Type: types.TypeString(variable.Type(), packageQualifier), Reason: "Fixture-only reviewed callback."},
			}
			writeKernelReviewGoldens(t, root, a, exception)
			if err := checkKernelReview(root, goldens); err != nil {
				t.Fatalf("explicitly reviewed fixture exception rejected: %v", err)
			}
		})
	}
}
