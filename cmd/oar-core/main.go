package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/server"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8000
	defaultSchemaPath    = "contracts/oar-schema.yaml"
	defaultWorkspaceRoot = ".oar-workspace"
)

func main() {
	var host string
	var port int
	var schemaPath string
	var workspaceRoot string

	flag.StringVar(&host, "host", defaultHost, "host interface to bind")
	flag.IntVar(&port, "port", defaultPort, "port to listen on")
	flag.StringVar(&schemaPath, "schema-path", defaultSchemaPath, "path to contracts/oar-schema.yaml")
	flag.StringVar(&workspaceRoot, "workspace-root", defaultWorkspaceRoot, "root directory for sqlite/filesystem workspace")
	flag.Parse()

	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to ensure workspace root: %v\n", err)
		os.Exit(1)
	}

	schemaVersion, err := schema.ReadVersion(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read schema version: %v\n", err)
		os.Exit(1)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	handler := server.NewHandler(schemaVersion)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Printf("oar-core listening on http://%s\n", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
