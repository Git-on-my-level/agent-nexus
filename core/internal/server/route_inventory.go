package server

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
)

// RouteOperation names an operation from the HTTP contract or its exception registry.
type RouteOperation struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	AccessClass string `json:"access_class"`
}

// RouteInventory derives access classes from the same classifiers mounted by
// NewHandler. Classes describe the core's outer authentication gate; handlers
// can enforce additional permissions. They are not a downstream proxy policy.
func RouteInventory(operations []RouteOperation) ([]RouteOperation, error) {
	classifiers := http.NewServeMux()
	NewHandler("", func(opts *handlerOptions) {
		opts.routeObserver = func(pattern string, classify routeAccessClassifier) {
			classifiers.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
				if pattern == "/" && r.URL.Path != "/" {
					return
				}
				requirement := classify(r)
				w.Header().Set("Access-Class", "handler_defined")
				if requirement.supported {
					w.Header().Set("Access-Class", string(requirement.bucket))
				}
			})
		}
	})
	parameters := regexp.MustCompile(`\{[^/{}]+\}`)
	result := append([]RouteOperation(nil), operations...)
	seen := map[string]bool{}
	for i := range result {
		route := &result[i]
		key := route.Method + " " + route.Path
		if seen[key] {
			return nil, fmt.Errorf("duplicate route: %s", key)
		}
		seen[key] = true
		request, err := http.NewRequest(route.Method, parameters.ReplaceAllString(route.Path, "inventory-param"), nil)
		if err != nil {
			return nil, err
		}
		response := &inventoryResponse{header: http.Header{}}
		classifiers.ServeHTTP(response, request)
		route.AccessClass = response.header.Get("Access-Class")
		if route.AccessClass == "" {
			return nil, fmt.Errorf("no supported router access class for %s", key)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		return result[i].Method < result[j].Method
	})
	return result, nil
}

type inventoryResponse struct{ header http.Header }

func (r *inventoryResponse) Header() http.Header       { return r.header }
func (*inventoryResponse) Write(b []byte) (int, error) { return len(b), nil }
func (*inventoryResponse) WriteHeader(int)             {}
