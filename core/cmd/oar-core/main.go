package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/buildinfo"
	"organization-autorunner-core/internal/controlplaneauth"
	"organization-autorunner-core/internal/controlplaneauth/heartbeat"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/router"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/server"
	"organization-autorunner-core/internal/sidecar"
	"organization-autorunner-core/internal/storage"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8000
	defaultSchemaPath    = "../contracts/oar-schema.yaml"
	defaultWorkspaceRoot = ".oar-workspace"
	defaultAPIVersion    = "v0"
	defaultInstanceID    = "core-local"

	defaultWorkspaceMaxBlobBytes         int64 = 1 << 30
	defaultWorkspaceMaxArtifacts         int64 = 100000
	defaultWorkspaceMaxDocuments         int64 = 50000
	defaultWorkspaceMaxDocumentRevisions int64 = 250000
	defaultWorkspaceMaxUploadBytes       int64 = 8 << 20
	defaultRequestBodyLimit              int64 = 1 << 20
	defaultAuthRequestBodyLimit          int64 = 256 << 10
	defaultContentRequestBodyLimit       int64 = 8 << 20
	defaultAuthRouteRateLimitPerMinute         = 600
	defaultAuthRouteRateBurst                  = 100
	defaultWriteRouteRateLimitPerMinute        = 1200
	defaultWriteRouteRateBurst                 = 200
)

func main() {
	var (
		host                       = envString("OAR_HOST", defaultHost)
		port                       = envInt("OAR_PORT", defaultPort)
		listenAddress              = envString("OAR_LISTEN_ADDR", "")
		schemaPath                 = envString("OAR_SCHEMA_PATH", defaultSchemaPath)
		workspaceRoot              = envString("OAR_WORKSPACE_ROOT", defaultWorkspaceRoot)
		blobBackend                = envString("OAR_BLOB_BACKEND", "filesystem")
		blobRoot                   = envString("OAR_BLOB_ROOT", "")
		blobS3Bucket               = envString("OAR_BLOB_S3_BUCKET", "")
		blobS3Prefix               = envString("OAR_BLOB_S3_PREFIX", "")
		blobS3Region               = envString("OAR_BLOB_S3_REGION", "")
		blobS3Endpoint             = envString("OAR_BLOB_S3_ENDPOINT", "")
		blobS3AccessKeyID          = envString("OAR_BLOB_S3_ACCESS_KEY_ID", "")
		blobS3SecretAccessKey      = envString("OAR_BLOB_S3_SECRET_ACCESS_KEY", "")
		blobS3SessionToken         = envString("OAR_BLOB_S3_SESSION_TOKEN", "")
		blobS3ForcePathStyle       = envBool("OAR_BLOB_S3_FORCE_PATH_STYLE", false)
		coreVersion                = envString("OAR_CORE_VERSION", buildinfo.Current)
		coreBaseURL                = envString("OAR_CORE_BASE_URL", "")
		apiVersion                 = envString("OAR_API_VERSION", defaultAPIVersion)
		minCLIVersion              = envString("OAR_MIN_CLI_VERSION", buildinfo.Current)
		recommendedCLIVersion      = envString("OAR_RECOMMENDED_CLI_VERSION", buildinfo.Current)
		cliDownloadURL             = envString("OAR_CLI_DOWNLOAD_URL", "")
		coreInstanceID             = envString("OAR_CORE_INSTANCE_ID", defaultInstanceID)
		metaCommandsPath           = envString("OAR_META_COMMANDS_PATH", "")
		streamPollInterval         = envDuration("OAR_STREAM_POLL_INTERVAL", time.Second)
		projectionMode             = envString("OAR_PROJECTION_MODE", server.ProjectionModeBackground)
		projectionPollInterval     = envDuration("OAR_PROJECTION_MAINTENANCE_INTERVAL", 5*time.Second)
		staleScanInterval          = envDuration("OAR_PROJECTION_STALE_SCAN_INTERVAL", 30*time.Second)
		projectionBatchSize        = envInt("OAR_PROJECTION_MAINTENANCE_BATCH_SIZE", 50)
		enableDevActorMode         = envBool("OAR_ENABLE_DEV_ACTOR_MODE", false)
		allowUnauthenticatedWrites = envBool("OAR_ALLOW_UNAUTHENTICATED_WRITES", false)
		allowLoopbackVerifyReads   = envBool("OAR_ALLOW_LOOPBACK_VERIFICATION_READS", false)
		bootstrapToken             = envString("OAR_BOOTSTRAP_TOKEN", "")
		webAuthnRPID               = envString("OAR_WEBAUTHN_RPID", "")
		webAuthnOrigin             = envString("OAR_WEBAUTHN_ORIGIN", "")
		webAuthnAllowedOrigins     = envCSV("OAR_WEBAUTHN_ALLOWED_ORIGINS")
		webAuthnDisplayName        = envString("OAR_WEBAUTHN_RP_DISPLAY_NAME", "OAR")
		humanAuthMode              = envString("OAR_HUMAN_AUTH_MODE", controlplaneauth.HumanAuthModeWorkspaceLocal)
		controlPlaneBaseURL        = envString("OAR_CONTROL_PLANE_BASE_URL", "")
		controlPlaneHeartbeatIntvl = envDuration("OAR_CONTROL_PLANE_HEARTBEAT_INTERVAL", heartbeat.DefaultInterval)
		controlPlaneTokenIssuer    = envString("OAR_CONTROL_PLANE_TOKEN_ISSUER", "")
		controlPlaneTokenAudience  = envString("OAR_CONTROL_PLANE_TOKEN_AUDIENCE", "")
		controlPlaneWorkspaceID    = envString("OAR_CONTROL_PLANE_WORKSPACE_ID", "")
		workspaceID                = envString("OAR_WORKSPACE_ID", "")
		workspaceName              = envString("OAR_WORKSPACE_NAME", "Main")
		controlPlaneTokenPublicKey = envString("OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY", "")
		workspaceServiceID         = envString("OAR_WORKSPACE_SERVICE_ID", "")
		workspaceServicePrivateKey = envString("OAR_WORKSPACE_SERVICE_PRIVATE_KEY", "")
		corsAllowedOrigins         = envString("OAR_CORS_ALLOWED_ORIGINS", "")
		sidecarRouterEnabled       = envBool("OAR_SIDECAR_ROUTER_ENABLED", true)
		sidecarRouterStatePath     = envString("OAR_SIDECAR_ROUTER_STATE_PATH", "")
		sidecarRouterPollInterval  = envDuration("OAR_SIDECAR_ROUTER_POLL_INTERVAL", time.Second)
		sidecarRouterCacheTTL      = envDuration("OAR_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL", time.Minute)
		shutdownTimeout            = envDuration("OAR_SHUTDOWN_TIMEOUT", 15*time.Second)
		workspaceQuota             = primitives.WorkspaceQuota{
			MaxBlobBytes:         envInt64("OAR_WORKSPACE_MAX_BLOB_BYTES", defaultWorkspaceMaxBlobBytes),
			MaxArtifacts:         envInt64("OAR_WORKSPACE_MAX_ARTIFACTS", defaultWorkspaceMaxArtifacts),
			MaxDocuments:         envInt64("OAR_WORKSPACE_MAX_DOCUMENTS", defaultWorkspaceMaxDocuments),
			MaxDocumentRevisions: envInt64("OAR_WORKSPACE_MAX_DOCUMENT_REVISIONS", defaultWorkspaceMaxDocumentRevisions),
			MaxUploadBytes:       envInt64("OAR_WORKSPACE_MAX_UPLOAD_BYTES", defaultWorkspaceMaxUploadBytes),
		}
		requestBodyLimits = server.RequestBodyLimits{
			Default: envInt64("OAR_REQUEST_BODY_LIMIT_BYTES", defaultRequestBodyLimit),
			Auth:    envInt64("OAR_AUTH_REQUEST_BODY_LIMIT_BYTES", defaultAuthRequestBodyLimit),
			Content: envInt64("OAR_CONTENT_REQUEST_BODY_LIMIT_BYTES", defaultContentRequestBodyLimit),
		}
		routeRateLimits = server.RouteRateLimits{
			AuthRequestsPerMinute:  envInt("OAR_AUTH_ROUTE_RATE_LIMIT_PER_MINUTE", defaultAuthRouteRateLimitPerMinute),
			AuthBurst:              envInt("OAR_AUTH_ROUTE_RATE_BURST", defaultAuthRouteRateBurst),
			WriteRequestsPerMinute: envInt("OAR_WRITE_ROUTE_RATE_LIMIT_PER_MINUTE", defaultWriteRouteRateLimitPerMinute),
			WriteBurst:             envInt("OAR_WRITE_ROUTE_RATE_BURST", defaultWriteRouteRateBurst),
		}
	)

	flag.StringVar(&host, "host", host, "host interface to bind")
	flag.IntVar(&port, "port", port, "port to listen on")
	flag.StringVar(&listenAddress, "listen-addr", listenAddress, "full listen address host:port; overrides --host/--port")
	flag.StringVar(&schemaPath, "schema-path", schemaPath, "path to ../contracts/oar-schema.yaml")
	flag.StringVar(&workspaceRoot, "workspace-root", workspaceRoot, "root directory for sqlite/filesystem workspace")
	flag.StringVar(&blobBackend, "blob-backend", blobBackend, "blob storage backend (filesystem|object|s3)")
	flag.StringVar(&blobRoot, "blob-root", blobRoot, "root directory for filesystem/object blob storage (defaults to workspace artifacts/content)")
	flag.StringVar(&coreVersion, "core-version", coreVersion, "core version reported in handshake/version headers (defaults to repo VERSION)")
	flag.StringVar(&apiVersion, "api-version", apiVersion, "api version reported in handshake/version headers")
	flag.StringVar(&minCLIVersion, "min-cli-version", minCLIVersion, "minimum compatible CLI version")
	flag.StringVar(&recommendedCLIVersion, "recommended-cli-version", recommendedCLIVersion, "recommended CLI version")
	flag.StringVar(&cliDownloadURL, "cli-download-url", cliDownloadURL, "CLI download URL included in compatibility metadata")
	flag.StringVar(&coreInstanceID, "core-instance-id", coreInstanceID, "stable core instance identifier for handshake metadata")
	flag.StringVar(&metaCommandsPath, "meta-commands-path", metaCommandsPath, "path to generated commands metadata JSON")
	flag.DurationVar(&streamPollInterval, "stream-poll-interval", streamPollInterval, "poll interval used by SSE stream endpoints")
	flag.StringVar(&projectionMode, "projection-mode", projectionMode, "projection maintenance mode (background|manual)")
	flag.DurationVar(&projectionPollInterval, "projection-maintenance-interval", projectionPollInterval, "poll interval used by background projection maintenance")
	flag.DurationVar(&staleScanInterval, "projection-stale-scan-interval", staleScanInterval, "interval used by background stale-thread scanning")
	flag.IntVar(&projectionBatchSize, "projection-maintenance-batch-size", projectionBatchSize, "max dirty thread projections refreshed per maintenance pass")
	flag.Parse()

	parsedProjectionMode, err := server.ParseProjectionMode(projectionMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	projectionMode = parsedProjectionMode

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
	if strings.TrimSpace(apiVersion) == "" {
		apiVersion = defaultAPIVersion
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
	if strings.TrimSpace(coreBaseURL) == "" {
		coreBaseURL = defaultCoreBaseURL(addr)
	}
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = strings.TrimSpace(controlPlaneWorkspaceID)
	}
	if strings.TrimSpace(workspaceID) == "" {
		workspaceID = "ws_main"
	}
	if strings.TrimSpace(workspaceName) == "" {
		workspaceName = "Main"
	}
	if strings.TrimSpace(sidecarRouterStatePath) == "" {
		sidecarRouterStatePath = filepath.Join(workspace.Layout().RootDir, "router", "router-state.json")
	}

	blobBackendImpl, effectiveBlobRoot, err := buildBlobBackend(context.Background(), workspace.Layout(), blobBackendConfig{
		Backend: blobBackend,
		Root:    blobRoot,
		S3: blob.S3BackendConfig{
			Bucket:          blobS3Bucket,
			Prefix:          blobS3Prefix,
			Region:          blobS3Region,
			Endpoint:        blobS3Endpoint,
			AccessKeyID:     blobS3AccessKeyID,
			SecretAccessKey: blobS3SecretAccessKey,
			SessionToken:    blobS3SessionToken,
			ForcePathStyle:  blobS3ForcePathStyle,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid blob backend configuration: %v\n", err)
		os.Exit(1)
	}

	actorRegistry := actors.NewStore(workspace.DB())
	if _, err := actorRegistry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		fmt.Fprintf(os.Stderr, "failed to seed system actor: %v\n", err)
		os.Exit(1)
	}
	authStore := auth.NewStore(workspace.DB(), auth.WithBootstrapToken(bootstrapToken))
	passkeySessionStore := auth.NewPasskeySessionStore(auth.DefaultPasskeySessionTTL)
	defer passkeySessionStore.Close()
	var (
		controlPlaneVerifier *controlplaneauth.WorkspaceHumanVerifier
		serviceIdentity      *controlplaneauth.WorkspaceServiceIdentity
	)
	needsWorkspaceServiceIdentity := strings.TrimSpace(humanAuthMode) == controlplaneauth.HumanAuthModeControlPlane || strings.TrimSpace(controlPlaneBaseURL) != ""
	switch strings.TrimSpace(humanAuthMode) {
	case controlplaneauth.HumanAuthModeWorkspaceLocal:
	case controlplaneauth.HumanAuthModeControlPlane:
		publicKey, err := controlplaneauth.ParseEd25519PublicKeyBase64(controlPlaneTokenPublicKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid OAR_CONTROL_PLANE_TOKEN_PUBLIC_KEY: %v\n", err)
			os.Exit(1)
		}
		controlPlaneVerifier, err = controlplaneauth.NewWorkspaceHumanVerifier(controlplaneauth.WorkspaceHumanVerifierConfig{
			Issuer:      controlPlaneTokenIssuer,
			Audience:    controlPlaneTokenAudience,
			WorkspaceID: controlPlaneWorkspaceID,
			PublicKey:   publicKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid control-plane human auth configuration: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "invalid OAR_HUMAN_AUTH_MODE %q (supported: %s, %s)\n", humanAuthMode, controlplaneauth.HumanAuthModeWorkspaceLocal, controlplaneauth.HumanAuthModeControlPlane)
		os.Exit(1)
	}
	if needsWorkspaceServiceIdentity {
		servicePrivateKey, err := controlplaneauth.ParseEd25519PrivateKeyBase64(workspaceServicePrivateKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid OAR_WORKSPACE_SERVICE_PRIVATE_KEY: %v\n", err)
			os.Exit(1)
		}
		serviceIdentity, err = controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
			ID:         workspaceServiceID,
			PrivateKey: servicePrivateKey,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid workspace service identity configuration: %v\n", err)
			os.Exit(1)
		}
	}
	primitiveStore := primitives.NewStore(workspace.DB(), blobBackendImpl, effectiveBlobRoot, primitives.WithWorkspaceQuota(workspaceQuota))
	projectionMaintainer := server.NewProjectionMaintainer(server.ProjectionMaintainerConfig{
		PrimitiveStore:    primitiveStore,
		Contract:          contract,
		Mode:              projectionMode,
		PollInterval:      projectionPollInterval,
		StaleScanInterval: staleScanInterval,
		DirtyBatchSize:    projectionBatchSize,
		SystemActorID:     "oar-core",
	})
	routerState, err := router.NewStateStore(sidecarRouterStatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize router state: %v\n", err)
		os.Exit(1)
	}
	routerService := router.NewService(router.Config{
		BaseURL:           coreBaseURL,
		WorkspaceID:       workspaceID,
		WorkspaceName:     workspaceName,
		StatePath:         sidecarRouterStatePath,
		PrincipalCacheTTL: sidecarRouterCacheTTL,
		PollInterval:      sidecarRouterPollInterval,
		ActorID:           "oar-core",
	}, router.Dependencies{
		ListPrincipals: func(ctx context.Context, limit int) ([]auth.AuthPrincipalSummary, error) {
			filter := auth.AuthPrincipalListFilter{}
			if limit > 0 {
				filter.Limit = &limit
			}
			principals, _, err := authStore.ListPrincipals(ctx, filter)
			return principals, err
		},
		ListMessagePostedAfter: func(ctx context.Context, cursor primitives.EventCursor, limit int) ([]map[string]any, error) {
			return primitiveStore.ListEventsAfter(ctx, primitives.EventListFilter{Types: []string{router.MessagePostedEvent}}, cursor, limit)
		},
		GetRegistrationContent: func(ctx context.Context, documentID string) (map[string]any, error) {
			_, revision, err := primitiveStore.GetDocument(ctx, documentID)
			if err != nil {
				return nil, err
			}
			content, ok := revision["content"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("document %s content is not structured", documentID)
			}
			return content, nil
		},
		GetEvent:  primitiveStore.GetEvent,
		GetThread: primitiveStore.GetThread,
		CreateArtifact: func(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) error {
			_, err := primitiveStore.CreateArtifact(ctx, actorID, artifact, content, contentType)
			return err
		},
		AppendEvent: func(ctx context.Context, actorID string, event map[string]any) error {
			_, err := primitiveStore.AppendEvent(ctx, actorID, event)
			return err
		},
		MarkThreadDirty: func(ctx context.Context, threadID string, queuedAt time.Time) error {
			return primitiveStore.MarkThreadProjectionsDirty(ctx, []string{threadID}, queuedAt)
		},
	}, routerState)
	sidecarHost := sidecar.NewHost(sidecar.Registration{
		Service: routerService,
		Enabled: sidecarRouterEnabled,
	})
	var heartbeatReporter *heartbeat.Reporter
	if strings.TrimSpace(controlPlaneBaseURL) != "" {
		heartbeatReporter, err = heartbeat.NewReporter(heartbeat.ReporterConfig{
			BaseURL:     controlPlaneBaseURL,
			WorkspaceID: controlPlaneWorkspaceID,
			Interval:    controlPlaneHeartbeatIntvl,
			Version:     coreVersion,
			Build:       detectBuildString(coreInstanceID, coreVersion),
			Identity:    serviceIdentity,
			ReadinessSummary: func(ctx context.Context) map[string]any {
				if err := workspace.Ping(ctx); err != nil {
					return map[string]any{
						"ok": false,
						"error": map[string]any{
							"code":    "storage_unavailable",
							"message": "storage health check failed",
						},
					}
				}
				summary := map[string]any{"ok": true}
				if err := sidecarHost.Ready(ctx); err != nil {
					return map[string]any{
						"ok": false,
						"error": map[string]any{
							"code":    "sidecar_unavailable",
							"message": "sidecar readiness check failed",
						},
						"sidecars": sidecarHost.Snapshot(ctx),
					}
				}
				summary["sidecars"] = sidecarHost.Snapshot(ctx)
				return summary
			},
			ProjectionMaintenanceSummary: func(ctx context.Context, now time.Time) map[string]any {
				return toStringAnyMap(projectionMaintainer.Snapshot(ctx, now))
			},
			UsageSummary: func(ctx context.Context) (map[string]any, error) {
				summary, err := primitiveStore.GetWorkspaceUsageSummary(ctx)
				if err != nil {
					return nil, err
				}
				return toStringAnyMap(summary), nil
			},
			LastSuccessfulBackupAt: func(ctx context.Context) (*string, error) {
				_ = ctx
				return heartbeat.DiscoverLastSuccessfulBackupAt(workspace.Layout().RootDir)
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid heartbeat reporter configuration: %v\n", err)
			os.Exit(1)
		}
	}
	handler := server.NewHandler(
		contract.Version,
		server.WithHealthCheck(workspace.Ping),
		server.WithReadinessCheck("sidecars", "sidecar_unavailable", "sidecar readiness check failed", sidecarHost.Ready),
		server.WithActorRegistry(actorRegistry),
		server.WithAuthStore(authStore),
		server.WithPasskeySessionStore(passkeySessionStore),
		server.WithPrimitiveStore(primitiveStore),
		server.WithSchemaContract(contract),
		server.WithWebAuthnConfig(server.WebAuthnConfig{
			RPDisplayName:  webAuthnDisplayName,
			RPID:           webAuthnRPID,
			RPOrigin:       webAuthnOrigin,
			AllowedOrigins: webAuthnAllowedOrigins,
		}),
		server.WithHumanAuthMode(humanAuthMode),
		server.WithControlPlaneHumanVerifier(controlPlaneVerifier),
		server.WithWorkspaceServiceIdentity(serviceIdentity),
		server.WithEnableDevActorMode(enableDevActorMode),
		server.WithAllowUnauthenticatedWrites(allowUnauthenticatedWrites),
		server.WithAllowLoopbackVerificationReads(allowLoopbackVerifyReads),
		server.WithCoreVersion(coreVersion),
		server.WithAPIVersion(apiVersion),
		server.WithMinCLIVersion(minCLIVersion),
		server.WithRecommendedCLIVersion(recommendedCLIVersion),
		server.WithCLIDownloadURL(cliDownloadURL),
		server.WithCoreInstanceID(coreInstanceID),
		server.WithMetaCommandsPath(metaCommandsPath),
		server.WithStreamPollInterval(streamPollInterval),
		server.WithCORSAllowedOrigins(corsAllowedOrigins),
		server.WithProjectionMaintainer(projectionMaintainer),
		server.WithOpsHealthSection("sidecars", sidecarHost.Snapshot),
		server.WithRequestBodyLimits(requestBodyLimits),
		server.WithRouteRateLimits(routeRateLimits),
	)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	maintenanceCtx, maintenanceCancel := context.WithCancel(context.Background())
	defer maintenanceCancel()
	if projectionMode == server.ProjectionModeBackground {
		go projectionMaintainer.Run(maintenanceCtx)
	}
	sidecarHost.Run(maintenanceCtx)
	if heartbeatReporter != nil {
		go heartbeatReporter.Run(maintenanceCtx)
	}
	go func() {
		fmt.Printf("oar-core listening on http://%s\n", addr)
		fmt.Printf("  projection mode: %s\n", projectionMode)
		fmt.Printf("  sidecars: router=%t (workspace_id=%s, workspace_name=%s)\n", sidecarRouterEnabled, workspaceID, workspaceName)
		if heartbeatReporter != nil {
			fmt.Printf("  control-plane heartbeat: %s (workspace_id=%s, interval=%s)\n", controlPlaneBaseURL, controlPlaneWorkspaceID, controlPlaneHeartbeatIntvl)
		}
		if enableDevActorMode {
			fmt.Println("  WARNING: dev actor mode enabled (anonymous workspace reads and legacy actor flows)")
		}
		if strings.TrimSpace(humanAuthMode) == controlplaneauth.HumanAuthModeControlPlane {
			fmt.Printf("  human auth mode: %s (workspace_id=%s, service_identity=%s)\n", humanAuthMode, controlPlaneWorkspaceID, serviceIdentity.ID())
		}
		if allowUnauthenticatedWrites {
			fmt.Println("  WARNING: unauthenticated writes enabled (dev mode)")
		}
		if allowLoopbackVerifyReads {
			fmt.Println("  WARNING: loopback verification reads enabled (read-only loopback bypass)")
		}
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case err := <-serverErr:
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	case sig := <-shutdown:
		fmt.Printf("\nreceived %s, shutting down gracefully...\n", sig)
		maintenanceCancel()
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "graceful shutdown failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("server stopped")
	}
}

func envString(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envCSV(name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
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

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
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

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid boolean value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func toStringAnyMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func detectBuildString(coreInstanceID string, coreVersion string) string {
	parts := make([]string, 0, 3)
	if info, ok := debug.ReadBuildInfo(); ok {
		if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
			parts = append(parts, version)
		}
		revision := ""
		modified := false
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				modified = strings.TrimSpace(setting.Value) == "true"
			}
		}
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if revision != "" {
			parts = append(parts, revision)
		}
		if modified {
			parts = append(parts, "dirty")
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "+")
	}
	if strings.TrimSpace(coreInstanceID) != "" {
		return strings.TrimSpace(coreInstanceID)
	}
	if strings.TrimSpace(coreVersion) != "" {
		return strings.TrimSpace(coreVersion)
	}
	return "oar-core"
}

func defaultCoreBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "http://127.0.0.1:8000"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
