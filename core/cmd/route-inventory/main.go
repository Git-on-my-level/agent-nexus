// route-inventory combines the HTTP contract and intentional exception registry
// with the actual router's access classifiers. No server or storage is started.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"agent-nexus-core/internal/server"
	"gopkg.in/yaml.v3"
)

func main() {
	openapi := flag.String("openapi", "../contracts/anx-openapi.yaml", "HTTP contract")
	exceptions := flag.String("exceptions", "../contracts/non-openapi-endpoints.yaml", "exception registry")
	out := flag.String("out", "../contracts/gen/meta/routes.json", "inventory output")
	flag.Parse()
	if err := generate(*openapi, *exceptions, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(openapi, exceptions, out string) error {
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	var registry struct {
		Endpoints []struct {
			Method string `yaml:"method"`
			Path   string `yaml:"path_pattern"`
		} `yaml:"endpoints"`
	}
	for _, input := range []struct {
		path   string
		target any
	}{{openapi, &doc}, {exceptions, &registry}} {
		data, err := os.ReadFile(input.path)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(data, input.target); err != nil {
			return err
		}
	}
	var operations []server.RouteOperation
	for path, item := range doc.Paths {
		for _, method := range []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"} {
			if _, ok := item[method]; ok {
				operations = append(operations, server.RouteOperation{Method: strings.ToUpper(method), Path: path})
			}
		}
	}
	for _, route := range registry.Endpoints {
		operations = append(operations, server.RouteOperation{Method: route.Method, Path: route.Path})
	}
	routes, err := server.RouteInventory(operations)
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return fmt.Errorf("empty route inventory")
	}
	payload := struct {
		Version    int                     `json:"version"`
		RouteCount int                     `json:"route_count"`
		Routes     []server.RouteOperation `json:"routes"`
	}{1, len(routes), routes}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0644)
}
