package server

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Exemptions are reviewed service boundaries, never free-form rationale alone.
// Host roster reads and derived rebuilds deliberately are not in these families.
func independentPrivacyRoute(p privacyRoutePolicy) bool {
	if strings.HasPrefix(p.Path, "/auth/access") {
		return false
	}
	root := strings.Split(strings.TrimPrefix(p.Path, "/"), "/")[0]
	switch root {
	case "auth", "meta", "series", "secrets", "adapters", "actors", "sessions":
		return true
	}
	switch p.Method + " " + p.Path {
	case "POST /pm/disconnect", "POST /pm/connect", "GET /pm/presence", "GET /health", "GET /livez", "GET /readyz", "GET /version", "GET /work/capabilities", "POST /pm/bindings", "POST /pm/ingress/discord", "POST /pm/ingress/telegram", "POST /hosts/{host_id}/bridge/check-in":
		return true
	}
	return false
}

func validatePrivacyMounts(policies []privacyRoutePolicy, mounted map[string]routeAccessClassifier) error {
	mux := http.NewServeMux()
	for pattern := range mounted {
		mux.HandleFunc(pattern, func(http.ResponseWriter, *http.Request) {})
	}
	covered := map[string]bool{"/": true}
	param := regexp.MustCompile(`\{[^/{}]+\}`)
	for _, p := range policies {
		if p.Policy == "independent" && !independentPrivacyRoute(p) {
			return fmt.Errorf("resource route cannot be exempt: %s %s", p.Method, p.Path)
		}
		r, _ := http.NewRequest(p.Method, param.ReplaceAllString(p.Path, "fixture"), nil)
		_, pattern := mux.Handler(r)
		classify, ok := mounted[pattern]
		if !ok || pattern == "/" || !classify(r).supported {
			return fmt.Errorf("missing authentication classifier: %s %s (%s)", p.Method, p.Path, pattern)
		}
		covered[pattern] = true
	}
	for pattern := range mounted {
		if !covered[pattern] {
			return fmt.Errorf("undocumented mux registration: %s", pattern)
		}
	}
	return nil
}

func privacyMountedRoutes() map[string]routeAccessClassifier {
	mounted := map[string]routeAccessClassifier{}
	NewHandler("", func(o *handlerOptions) { o.routeObserver = func(p string, c routeAccessClassifier) { mounted[p] = c } })
	return mounted
}

func TestResourceAccessMountedInventory(t *testing.T) {
	policies := loadPrivacyRouteMatrix(t)
	mounted := privacyMountedRoutes()
	if err := validatePrivacyMounts(policies, mounted); err != nil {
		t.Fatal(err)
	}
	// Reproduce both inventory bypasses from the adversarial review.
	bad := append([]privacyRoutePolicy(nil), policies...)
	for i := range bad {
		if bad[i].Path == "/events" && bad[i].Method == "GET" {
			bad[i].Policy = "independent"
			bad[i].Reason = "arbitrary rationale"
		}
	}
	if validatePrivacyMounts(bad, mounted) == nil {
		t.Fatal("incorrect exemption accepted")
	}
	mounted["/undocumented-events"] = exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet)
	if validatePrivacyMounts(policies, mounted) == nil {
		t.Fatal("undocumented registration accepted")
	}
}

func TestResourceAccessRegistrationCannotBypassObserver(t *testing.T) {
	checkResourceAccessRegistration(t)
}

func checkResourceAccessRegistration(t *testing.T) {
	t.Helper()
	// All real mux registrations must pass the observed registration functions.
	// This also catches direct mux.Handle/HandleFunc additions omitted from them.
	f, err := parser.ParseFile(token.NewFileSet(), "handler.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Name != "mux" || (sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc") {
			return true
		}
		count++
		allowed := false
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == "pattern" {
			allowed = true
		}
		if s, ok := call.Args[0].(*ast.SelectorExpr); ok {
			if x, ok := s.X.(*ast.Ident); ok && x.Name == "stream" && s.Sel.Name == "Prefix" {
				allowed = true
			}
		}
		if !allowed {
			t.Error("direct mux registration bypasses privacy inventory; use registerRoute")
		}
		return true
	})
	if count != 2 {
		t.Fatalf("unexpected direct registrations: %d", count)
	}
}
