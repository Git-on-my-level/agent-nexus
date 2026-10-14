package plans

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestStalenessKnobs pins the end-state rule "one clock: promise versus
// evidence": no workspace-tuned staleness thresholds feed human-visible state.
// It scans the Go sources of core/ and cli/ (tests and generated files
// excluded) and fails when an env knob or a duration constant for staleness,
// stalling or darkness appears. The only duration fallback is
// plans.NoPromiseFallback, which orders no_promise rows and never makes a
// state. Adding a knob means changing this test, on purpose.
func TestStalenessKnobs(t *testing.T) {
	envKnob := regexp.MustCompile(`ANX_[A-Z0-9_]*(STALE|STALL|DARK|AFTER)[A-Z0-9_]*`)
	durationName := regexp.MustCompile(`(?i)(stale|stall|dark|fallback)`)
	durationValue := regexp.MustCompile(`\btime\.(Hour|Minute|Second|Duration)\b`)
	// SignalStaleness measures agent process liveness, not initiative health.
	exempt := map[string]bool{"commandcenter.SignalStaleness": true}
	root := filepath.Join("..", "..", "..")
	found := map[string]bool{}
	flags := []string{}
	for _, module := range []string{"core", "cli"} {
		err := filepath.WalkDir(filepath.Join(root, module), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "node_modules", "testdata", ".git", "vendor":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			src := string(raw)
			if strings.Contains(src, "Code generated") && strings.Contains(src, "DO NOT EDIT") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			for _, name := range envKnob.FindAllString(src, -1) {
				found["env "+name+" ("+rel+")"] = true
			}
			if module == "cli" && strings.Contains(src, "stale-hours") {
				flags = append(flags, rel)
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					text := ""
					if value.Type != nil {
						text += string(raw[value.Type.Pos()-1 : value.Type.End()-1])
					}
					for _, v := range value.Values {
						text += " " + string(raw[v.Pos()-1:v.End()-1])
					}
					if !durationValue.MatchString(text) {
						continue
					}
					for _, name := range value.Names {
						if durationName.MatchString(name.Name) {
							found[file.Name.Name+"."+name.Name] = true
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	got := make([]string, 0, len(found))
	for name := range found {
		if !exempt[name] {
			got = append(got, name)
		}
	}
	sort.Strings(got)
	if len(got) != 1 || got[0] != "plans.NoPromiseFallback" {
		t.Fatalf("staleness knobs must be exactly [plans.NoPromiseFallback]; found %v", got)
	}
	if len(flags) > 0 {
		t.Fatalf("--stale-hours must not exist in cli; found in %v", flags)
	}
}
