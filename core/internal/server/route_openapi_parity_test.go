package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type commandMeta struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

type commandsFile struct {
	Commands []commandMeta `json:"commands"`
}

type nonOpenapiFile struct {
	Endpoints []struct {
		Method      string `yaml:"method"`
		PathPattern string `yaml:"path_pattern"`
	} `yaml:"endpoints"`
}

func repoRootFromServerTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// .../core/internal/server -> repo root is three levels up from `core`.
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestExactRegisterRoutesCoveredByOpenAPOrExceptions(t *testing.T) {
	root := repoRootFromServerTest(t)
	commandsPath := filepath.Join(root, "contracts", "gen", "meta", "commands.json")
	commandsRaw, err := os.ReadFile(commandsPath)
	if err != nil {
		t.Fatalf("read commands.json: %v", err)
	}
	var cmdFile commandsFile
	if err := json.Unmarshal(commandsRaw, &cmdFile); err != nil {
		t.Fatalf("decode commands.json: %v", err)
	}
	commands := cmdFile.Commands

	exceptionsPath := filepath.Join(root, "contracts", "non-openapi-endpoints.yaml")
	exceptionsRaw, err := os.ReadFile(exceptionsPath)
	if err != nil {
		t.Fatalf("read non-openapi-endpoints.yaml: %v", err)
	}
	var excFile nonOpenapiFile
	if err := yaml.Unmarshal(exceptionsRaw, &excFile); err != nil {
		t.Fatalf("decode non-openapi-endpoints.yaml: %v", err)
	}

	handlerPath := filepath.Join(root, "core", "internal", "server", "handler.go")
	handlerSrc, err := os.ReadFile(handlerPath)
	if err != nil {
		t.Fatalf("read handler.go: %v", err)
	}

	registerExact := regexp.MustCompile(`registerRoute\("(/[^"]*)",\s*exactRouteAccess\(([^)]*)\)`)
	methodLiteral := regexp.MustCompile(`http\.Method([A-Za-z]+)`)

	for _, m := range registerExact.FindAllSubmatch(handlerSrc, -1) {
		pattern := string(m[1])
		if pattern == "/" {
			continue
		}
		inner := string(m[2])
		var methods []string
		for _, mm := range methodLiteral.FindAllStringSubmatch(inner, -1) {
			if len(mm) < 2 {
				continue
			}
			switch mm[1] {
			case "Get":
				methods = append(methods, "GET")
			case "Post":
				methods = append(methods, "POST")
			case "Patch":
				methods = append(methods, "PATCH")
			case "Put":
				methods = append(methods, "PUT")
			case "Delete":
				methods = append(methods, "DELETE")
			case "Head":
				methods = append(methods, "HEAD")
			default:
				t.Fatalf("unsupported http method constant Method%s in exactRouteAccess for %q", mm[1], pattern)
			}
		}
		if len(methods) == 0 {
			t.Fatalf("exactRouteAccess for %q has no http.Method* literals (expand route_openapi_parity_test.go)", pattern)
		}

		for _, method := range methods {
			if bestOpenAPICommandMatch(method, pattern, commands) != nil {
				continue
			}
			if exceptionMatches(method, pattern, excFile.Endpoints) {
				continue
			}
			t.Fatalf("handler exact route %s %s not found in contracts/gen/meta/commands.json and not listed in contracts/non-openapi-endpoints.yaml", method, pattern)
		}
	}
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func pathMatchesTemplate(templatePath, concretePath string) bool {
	tp := splitPath(templatePath)
	cp := splitPath(concretePath)
	if len(tp) != len(cp) {
		return false
	}
	for i := range tp {
		if strings.HasPrefix(tp[i], "{") {
			continue
		}
		if tp[i] != cp[i] {
			return false
		}
	}
	return true
}

func bestOpenAPICommandMatch(method, concrete string, commands []commandMeta) *commandMeta {
	var best *commandMeta
	bestScore := -1
	for i := range commands {
		c := &commands[i]
		if !strings.EqualFold(c.Method, method) {
			continue
		}
		if !pathMatchesTemplate(c.Path, concrete) {
			continue
		}
		score := strings.Count(c.Path, "{")
		if best == nil || score < bestScore {
			best = c
			bestScore = score
		}
	}
	return best
}

func exceptionMatches(method, concrete string, endpoints []struct {
	Method      string `yaml:"method"`
	PathPattern string `yaml:"path_pattern"`
}) bool {
	for _, e := range endpoints {
		if !strings.EqualFold(strings.TrimSpace(e.Method), method) {
			continue
		}
		if pathMatchesTemplate(strings.TrimSpace(e.PathPattern), concrete) {
			return true
		}
	}
	return false
}
