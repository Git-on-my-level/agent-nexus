package foreignvar

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type listedPackage struct {
	ImportPath      string
	Name            string
	Dir             string
	GoFiles         []string
	CompiledGoFiles []string
	TestGoFiles     []string
	Export          string
	DepOnly         bool
	Error           *struct {
		Err string
	}
}

type checkedPackage struct {
	ImportPath string
	Findings   []Finding
}

func checkPatterns(dir string, tests bool, patterns ...string) ([]checkedPackage, error) {
	listed, err := goList(dir, tests, patterns...)
	if err != nil {
		return nil, err
	}
	exports := map[string]string{}
	for _, pkg := range listed {
		if pkg.Export != "" && !strings.Contains(pkg.ImportPath, " ") {
			exports[pkg.ImportPath] = pkg.Export
		}
	}
	chosen := map[string]listedPackage{}
	for _, pkg := range listed {
		if pkg.DepOnly || strings.HasSuffix(pkg.ImportPath, ".test") {
			continue
		}
		// go list -test emits the library and a second copy that also
		// contains its _test.go files. Keep the copy with more sources.
		key := pkg.Dir + "\x00" + pkg.Name
		prev, ok := chosen[key]
		if !ok || len(pkg.CompiledGoFiles) > len(prev.CompiledGoFiles) {
			chosen[key] = pkg
		}
	}
	var checked []checkedPackage
	for _, pkg := range chosen {
		if pkg.Error != nil {
			return nil, fmt.Errorf("%s: %s", pkg.ImportPath, pkg.Error.Err)
		}
		if i := strings.Index(pkg.ImportPath, " ["); i >= 0 {
			pkg.ImportPath = pkg.ImportPath[:i]
		}
		findings, err := checkListed(pkg, exports, tests)
		if err != nil {
			return nil, err
		}
		checked = append(checked, checkedPackage{ImportPath: pkg.ImportPath, Findings: findings})
	}
	if len(checked) == 0 {
		return nil, fmt.Errorf("no packages matched %s in %s", patterns, dir)
	}
	return checked, nil
}

func goList(dir string, tests bool, patterns ...string) ([]listedPackage, error) {
	args := []string{"list", "-e", "-json", "-deps", "-export", "-compiled", fmt.Sprintf("-test=%t", tests)}
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(stdout)
	var listed []listedPackage
	for {
		var pkg listedPackage
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			_ = cmd.Wait()
			return nil, fmt.Errorf("go list json: %w", err)
		}
		listed = append(listed, pkg)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, stderr.String())
	}
	return listed, nil
}

func checkListed(pkg listedPackage, exports map[string]string, tests bool) ([]Finding, error) {
	fset := token.NewFileSet()
	var files []*ast.File
	names := append([]string{}, pkg.CompiledGoFiles...)
	if len(names) == 0 {
		names = append(names, pkg.GoFiles...)
	}
	if tests {
		have := map[string]bool{}
		for _, name := range names {
			have[filepath.Base(name)] = true
		}
		for _, name := range pkg.TestGoFiles {
			if !have[filepath.Base(name)] {
				names = append(names, name)
			}
		}
	}
	for _, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(pkg.Dir, name)
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, nil
	}
	lookup := func(path string) (io.ReadCloser, error) {
		export, ok := exports[path]
		if !ok || export == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(export)
	}
	info := &types.Info{
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	config := types.Config{Importer: importer.ForCompiler(fset, "gc", lookup)}
	checked, err := config.Check(pkg.ImportPath, fset, files, info)
	if err != nil {
		return nil, fmt.Errorf("typecheck %s: %w", pkg.ImportPath, err)
	}
	return Findings(fset, checked, files, info), nil
}
