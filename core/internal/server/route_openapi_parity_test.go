package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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

type nonOpenapiEndpoint struct {
	Method          string `yaml:"method"`
	PathPattern     string `yaml:"path_pattern"`
	Owner           string `yaml:"owner"`
	Reason          string `yaml:"reason"`
	ExpectedClients string `yaml:"expected_clients"`
}

type nonOpenapiFile struct {
	Endpoints []nonOpenapiEndpoint `yaml:"endpoints"`
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
	validateNonOpenAPIRegistry(t, excFile, commands)

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

func validateNonOpenAPIRegistry(t *testing.T, excFile nonOpenapiFile, commands []commandMeta) {
	t.Helper()

	allowedExpectedClients := map[string]bool{
		"none":         true,
		"server-only":  true,
		"operator-cli": true,
	}
	seen := make(map[string]struct{})

	for i, e := range excFile.Endpoints {
		method := strings.TrimSpace(e.Method)
		pathPattern := strings.TrimSpace(e.PathPattern)
		owner := strings.TrimSpace(e.Owner)
		reason := strings.TrimSpace(e.Reason)
		expectedClients := strings.TrimSpace(e.ExpectedClients)
		label := "contracts/non-openapi-endpoints.yaml"
		if method != "" || pathPattern != "" {
			label += " entry " + method + " " + pathPattern
		} else {
			label += " entry #" + strconv.Itoa(i+1)
		}

		if method == "" {
			t.Errorf("%s missing method", label)
		}
		if pathPattern == "" {
			t.Errorf("%s missing path_pattern", label)
		}
		if owner == "" {
			t.Errorf("%s missing owner", label)
		}
		if reason == "" {
			t.Errorf("%s missing reason", label)
		}
		if expectedClients == "" {
			t.Errorf("%s missing expected_clients", label)
		} else if !allowedExpectedClients[expectedClients] {
			t.Errorf("%s has invalid expected_clients %q", label, expectedClients)
		}

		key := strings.ToUpper(method) + " " + pathPattern
		if _, ok := seen[key]; ok {
			t.Errorf("%s duplicates %s", label, key)
		}
		seen[key] = struct{}{}

		if method != "" && pathPattern != "" {
			if match := bestOpenAPICommandMatch(method, pathPattern, commands); match != nil {
				t.Errorf("%s is already covered by OpenAPI command metadata as %s %s", label, match.Method, match.Path)
			}
		}
	}
}

func exceptionMatches(method, concrete string, endpoints []nonOpenapiEndpoint) bool {
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
