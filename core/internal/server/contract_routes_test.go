package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type generatedCommandRegistry struct {
	Commands []generatedCommand `json:"commands"`
}

type generatedCommand struct {
	CommandID string `json:"command_id"`
	Method    string `json:"method"`
	Path      string `json:"path"`
}

var routeParamPattern = regexp.MustCompile(`\{[^}]+\}`)

func TestDocumentedRoutesAreRegistered(t *testing.T) {
	t.Parallel()

	registry := loadGeneratedCommandRegistryForTest(t)
	handler := NewHandler("0.2.2")

	for _, command := range registry.Commands {
		command := command
		t.Run(command.CommandID, func(t *testing.T) {
			t.Parallel()

			path := routeParamPattern.ReplaceAllString(strings.TrimSpace(command.Path), "example")
			method := strings.ToUpper(strings.TrimSpace(command.Method))
			req := requestForDocumentedRoute(t, method, path)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusNotFound {
				return
			}
			if errorMessageFromResponse(rr.Body.Bytes()) != "endpoint not found" {
				return
			}

			t.Fatalf("documented route %s %s returned endpoint not found", method, path)
		})
	}
}

func requestForDocumentedRoute(t *testing.T, method string, path string) *http.Request {
	t.Helper()

	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		req := httptest.NewRequest(method, path, bytes.NewBufferString(`{}`))
		req.Header.Set("Content-Type", "application/json")
		return req
	default:
		return httptest.NewRequest(method, path, nil)
	}
}

func loadGeneratedCommandRegistryForTest(t *testing.T) generatedCommandRegistry {
	t.Helper()

	candidates := []string{
		filepath.Join("..", "..", "..", "contracts", "gen", "meta", "commands.json"),
		filepath.Join("contracts", "gen", "meta", "commands.json"),
	}

	for _, candidate := range candidates {
		content, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}

		var registry generatedCommandRegistry
		if err := json.Unmarshal(content, &registry); err != nil {
			t.Fatalf("decode %s: %v", candidate, err)
		}
		if len(registry.Commands) == 0 {
			t.Fatalf("generated command registry %s is empty", candidate)
		}
		return registry
	}

	t.Fatalf("failed to locate generated command registry")
	return generatedCommandRegistry{}
}

func errorMessageFromResponse(body []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Error.Message)
}
