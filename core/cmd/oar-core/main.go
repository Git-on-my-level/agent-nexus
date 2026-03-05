package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/server"
	"organization-autorunner-core/internal/storage"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8000
	defaultSchemaPath    = "../contracts/oar-schema.yaml"
	defaultWorkspaceRoot = ".oar-workspace"
	defaultAPIVersion    = "v0"
	defaultMinCLIVersion = "0.1.0"
	defaultInstanceID    = "core-local"
)

func main() {
	var (
		host                  = envString("OAR_HOST", defaultHost)
		port                  = envInt("OAR_PORT", defaultPort)
		listenAddress         = envString("OAR_LISTEN_ADDR", "")
		schemaPath            = envString("OAR_SCHEMA_PATH", defaultSchemaPath)
		workspaceRoot         = envString("OAR_WORKSPACE_ROOT", defaultWorkspaceRoot)
		coreVersion           = envString("OAR_CORE_VERSION", "")
		apiVersion            = envString("OAR_API_VERSION", defaultAPIVersion)
		minCLIVersion         = envString("OAR_MIN_CLI_VERSION", defaultMinCLIVersion)
		recommendedCLIVersion = envString("OAR_RECOMMENDED_CLI_VERSION", defaultMinCLIVersion)
		cliDownloadURL        = envString("OAR_CLI_DOWNLOAD_URL", "")
		coreInstanceID        = envString("OAR_CORE_INSTANCE_ID", defaultInstanceID)
		metaCommandsPath      = envString("OAR_META_COMMANDS_PATH", "")
		streamPollInterval    = envDuration("OAR_STREAM_POLL_INTERVAL", time.Second)
	)

	flag.StringVar(&host, "host", host, "host interface to bind")
	flag.IntVar(&port, "port", port, "port to listen on")
	flag.StringVar(&listenAddress, "listen-addr", listenAddress, "full listen address host:port; overrides --host/--port")
	flag.StringVar(&schemaPath, "schema-path", schemaPath, "path to ../contracts/oar-schema.yaml")
	flag.StringVar(&workspaceRoot, "workspace-root", workspaceRoot, "root directory for sqlite/filesystem workspace")
	flag.StringVar(&coreVersion, "core-version", coreVersion, "core version reported in handshake/version headers (defaults to schema version)")
	flag.StringVar(&apiVersion, "api-version", apiVersion, "api version reported in handshake/version headers")
	flag.StringVar(&minCLIVersion, "min-cli-version", minCLIVersion, "minimum compatible CLI version")
	flag.StringVar(&recommendedCLIVersion, "recommended-cli-version", recommendedCLIVersion, "recommended CLI version")
	flag.StringVar(&cliDownloadURL, "cli-download-url", cliDownloadURL, "CLI download URL included in compatibility metadata")
	flag.StringVar(&coreInstanceID, "core-instance-id", coreInstanceID, "stable core instance identifier for handshake metadata")
	flag.StringVar(&metaCommandsPath, "meta-commands-path", metaCommandsPath, "path to generated commands metadata JSON")
	flag.DurationVar(&streamPollInterval, "stream-poll-interval", streamPollInterval, "poll interval used by SSE stream endpoints")
	flag.Parse()

	workspace, err := storage.InitializeWorkspace(context.Background(), workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize workspace storage: %v\n", err)
		os.Exit(1)
	}
	defer workspace.Close()

	contract, err := schema.Load(schemaPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load schema: %v\n", err)
		os.Exit(1)
	}
	if strings.TrimSpace(coreVersion) == "" {
		coreVersion = contract.Version
	}
	if strings.TrimSpace(apiVersion) == "" {
		apiVersion = defaultAPIVersion
	}
	if strings.TrimSpace(minCLIVersion) == "" {
		minCLIVersion = defaultMinCLIVersion
	}
	if strings.TrimSpace(recommendedCLIVersion) == "" {
		recommendedCLIVersion = minCLIVersion
	}
	if strings.TrimSpace(coreInstanceID) == "" {
		coreInstanceID = defaultInstanceID
	}
	if streamPollInterval <= 0 {
		streamPollInterval = time.Second
	}

	addr := listenAddress
	if addr == "" {
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}

	actorRegistry := actors.NewStore(workspace.DB())
	if _, err := actorRegistry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed system actor: %v\n", err)
		os.Exit(1)
	}
	authStore := auth.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)
	handler := server.NewHandler(
		contract.Version,
		server.WithHealthCheck(workspace.Ping),
		server.WithActorRegistry(actorRegistry),
		server.WithAuthStore(authStore),
		server.WithPrimitiveStore(primitiveStore),
		server.WithSchemaContract(contract),
		server.WithCoreVersion(coreVersion),
		server.WithAPIVersion(apiVersion),
		server.WithMinCLIVersion(minCLIVersion),
		server.WithRecommendedCLIVersion(recommendedCLIVersion),
		server.WithCLIDownloadURL(cliDownloadURL),
		server.WithCoreInstanceID(coreInstanceID),
		server.WithMetaCommandsPath(metaCommandsPath),
		server.WithStreamPollInterval(streamPollInterval),
	)
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

func envString(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid integer value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid duration value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}
