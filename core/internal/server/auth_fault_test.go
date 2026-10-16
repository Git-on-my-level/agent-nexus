package server

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasskeyDevRegisterLogsSanitizedStoreFailure(t *testing.T) {
	requireIntegrationTest(t) // Real auth/storage HTTP stack; serial process-wide log capture.
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	if _, err := env.workspace.DB().Exec(`CREATE TRIGGER register_failure BEFORE INSERT ON agents BEGIN SELECT RAISE(ABORT,'register probe token=credential-secret'); END`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(old) })
	response := postJSONExpectStatusWithHeaders(t, env.server.URL+"/auth/passkey/dev/register", map[string]any{"display_name": "unlogged-display", "bootstrap_token": testBootstrapToken}, nil, http.StatusInternalServerError)
	defer response.Body.Close()
	body := new(bytes.Buffer)
	body.ReadFrom(response.Body)
	if !strings.Contains(body.String(), "failed to register passkey agent") || strings.Contains(body.String(), "probe") || strings.Contains(body.String(), "secret") {
		t.Fatal(body.String())
	}
	line := output.String()
	if strings.Count(line, "anx-core: server error") != 1 || !strings.Contains(line, "route=/auth/passkey/dev/register") || !strings.Contains(line, "register probe") || strings.Contains(line, "error=-") {
		t.Fatal(line)
	}
	for _, secret := range []string{"credential-secret", testBootstrapToken, "unlogged-display"} {
		if strings.Contains(line, secret) {
			t.Fatalf("log leaked %q: %s", secret, line)
		}
	}
	if !strings.Contains(line, "[redacted]") {
		t.Fatal(line)
	}
}

// New auth handlers cannot silently fall back to error=- for an internal 500.
// This checks the call boundary, while the integration test exercises the chain.
func TestAuthInternalErrorsRetainCause(t *testing.T) {
	files, err := filepath.Glob("auth*handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, raw, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != "writeError" || len(call.Args) < 2 {
				return true
			}
			status := call.Args[1]
			if value, ok := status.(*ast.BasicLit); ok && value.Value == "500" {
				t.Errorf("%s drops internal-error cause", name)
			}
			if selector, ok := status.(*ast.SelectorExpr); ok && selector.Sel.Name == "StatusInternalServerError" {
				t.Errorf("%s drops internal-error cause", name)
			}
			return true
		})
	}
}
