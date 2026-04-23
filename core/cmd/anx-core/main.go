package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/buildinfo"
	"agent-nexus-core/internal/heartbeat"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/server"
	"agent-nexus-core/internal/server/stream"
	"agent-nexus-core/internal/sidecar"
	"agent-nexus-core/internal/storage"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8000
	defaultSchemaPath    = "../contracts/anx-schema.yaml"
	defaultWorkspaceRoot = ".anx-workspace"
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
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheckCLI())
	}

	var (
		host                        = envString("ANX_HOST", defaultHost)
		port                        = envInt("ANX_PORT", defaultPort)
		listenAddress               = envString("ANX_LISTEN_ADDR", "")
		schemaPath                  = envString("ANX_SCHEMA_PATH", defaultSchemaPath)
		workspaceRoot               = envString("ANX_WORKSPACE_ROOT", defaultWorkspaceRoot)
		blobBackend                 = envString("ANX_BLOB_BACKEND", "filesystem")
		blobRoot                    = envString("ANX_BLOB_ROOT", "")
		blobS3Bucket                = envString("ANX_BLOB_S3_BUCKET", "")
		blobS3Prefix                = envString("ANX_BLOB_S3_PREFIX", "")
		blobS3Region                = envString("ANX_BLOB_S3_REGION", "")
		blobS3Endpoint              = envString("ANX_BLOB_S3_ENDPOINT", "")
		blobS3AccessKeyID           = envString("ANX_BLOB_S3_ACCESS_KEY_ID", "")
		blobS3SecretAccessKey       = envString("ANX_BLOB_S3_SECRET_ACCESS_KEY", "")
		blobS3SessionToken          = envString("ANX_BLOB_S3_SESSION_TOKEN", "")
		blobS3ForcePathStyle        = envBool("ANX_BLOB_S3_FORCE_PATH_STYLE", false)
		coreVersion                 = envString("ANX_CORE_VERSION", buildinfo.Current)
		coreBaseURL                 = envString("ANX_CORE_BASE_URL", "")
		apiVersion                  = envString("ANX_API_VERSION", defaultAPIVersion)
		minCLIVersion               = envString("ANX_MIN_CLI_VERSION", buildinfo.Current)
		recommendedCLIVersion       = envString("ANX_RECOMMENDED_CLI_VERSION", buildinfo.Current)
		cliDownloadURL              = envString("ANX_CLI_DOWNLOAD_URL", "")
		coreInstanceID              = envString("ANX_CORE_INSTANCE_ID", defaultInstanceID)
		metaCommandsPath            = envString("ANX_META_COMMANDS_PATH", "")
		streamPollInterval          = envDuration("ANX_STREAM_POLL_INTERVAL", time.Second)
		projectionMode              = envString("ANX_PROJECTION_MODE", server.ProjectionModeBackground)
		projectionPollInterval      = envDuration("ANX_PROJECTION_MAINTENANCE_INTERVAL", 5*time.Second)
		staleScanInterval           = envDuration("ANX_PROJECTION_STALE_SCAN_INTERVAL", 30*time.Second)
		projectionBatchSize         = envInt("ANX_PROJECTION_MAINTENANCE_BATCH_SIZE", 50)
		devRegisterLinkedActors     = envBool("ANX_DEV_REGISTER_LINKED_ACTORS", false)
		enableDevActorMode          = envBool("ANX_ENABLE_DEV_ACTOR_MODE", false)
		allowUnauthenticatedWrites  = envBool("ANX_ALLOW_UNAUTHENTICATED_WRITES", false)
		allowLoopbackVerifyReads    = envBool("ANX_ALLOW_LOOPBACK_VERIFICATION_READS", false)
		bootstrapToken              = envString("ANX_BOOTSTRAP_TOKEN", "")
		webAuthnRPID                = envString("ANX_WEBAUTHN_RPID", "")
		webAuthnOrigin              = envString("ANX_WEBAUTHN_ORIGIN", "")
		webAuthnAllowedOrigins      = envCSV("ANX_WEBAUTHN_ALLOWED_ORIGINS")
		webAuthnDisplayName         = envString("ANX_WEBAUTHN_RP_DISPLAY_NAME", "OAR")
		workspaceID                 = envString("ANX_WORKSPACE_ID", "")
		workspaceHumanGrantIssuer   = envString("ANX_WORKSPACE_HUMAN_GRANT_ISSUER", "")
		workspaceHumanGrantAudience = envString("ANX_WORKSPACE_HUMAN_GRANT_AUDIENCE", "")
		workspaceHumanGrantLeeway   = envDuration("ANX_WORKSPACE_HUMAN_GRANT_LEEWAY", auth.DefaultWorkspaceHumanGrantLeeway)
		secretsKey                  = envString("ANX_SECRETS_KEY", "")
		workspaceName               = envString("ANX_WORKSPACE_NAME", "Main")
		corsAllowedOrigins          = envString("ANX_CORS_ALLOWED_ORIGINS", "")
		sidecarRouterEnabled        = envBool("ANX_SIDECAR_ROUTER_ENABLED", true)
		sidecarRouterStatePath      = envString("ANX_SIDECAR_ROUTER_STATE_PATH", "")
		sidecarRouterPollInterval   = envDuration("ANX_SIDECAR_ROUTER_POLL_INTERVAL", time.Second)
		sidecarRouterCacheTTL       = envDuration("ANX_SIDECAR_ROUTER_PRINCIPAL_CACHE_TTL", time.Minute)
		shutdownTimeout             = envDuration("ANX_SHUTDOWN_TIMEOUT", 15*time.Second)
		enforceLocalQuotas          = envBool("ANX_ENFORCE_LOCAL_QUOTAS", true)
		workspaceQuota              = primitives.WorkspaceQuota{
			MaxBlobBytes:         envInt64("ANX_WORKSPACE_MAX_BLOB_BYTES", defaultWorkspaceMaxBlobBytes),
			MaxArtifacts:         envInt64("ANX_WORKSPACE_MAX_ARTIFACTS", defaultWorkspaceMaxArtifacts),
			MaxDocuments:         envInt64("ANX_WORKSPACE_MAX_DOCUMENTS", defaultWorkspaceMaxDocuments),
			MaxDocumentRevisions: envInt64("ANX_WORKSPACE_MAX_DOCUMENT_REVISIONS", defaultWorkspaceMaxDocumentRevisions),
			MaxUploadBytes:       envInt64("ANX_WORKSPACE_MAX_UPLOAD_BYTES", defaultWorkspaceMaxUploadBytes),
		}
		requestBodyLimits = server.RequestBodyLimits{
			Default: envInt64("ANX_REQUEST_BODY_LIMIT_BYTES", defaultRequestBodyLimit),
			Auth:    envInt64("ANX_AUTH_REQUEST_BODY_LIMIT_BYTES", defaultAuthRequestBodyLimit),
			Content: envInt64("ANX_CONTENT_REQUEST_BODY_LIMIT_BYTES", defaultContentRequestBodyLimit),
		}
		routeRateLimits = server.RouteRateLimits{
			AuthRequestsPerMinute:  envInt("ANX_AUTH_ROUTE_RATE_LIMIT_PER_MINUTE", defaultAuthRouteRateLimitPerMinute),
			AuthBurst:              envInt("ANX_AUTH_ROUTE_RATE_BURST", defaultAuthRouteRateBurst),
			WriteRequestsPerMinute: envInt("ANX_WRITE_ROUTE_RATE_LIMIT_PER_MINUTE", defaultWriteRouteRateLimitPerMinute),
			WriteBurst:             envInt("ANX_WRITE_ROUTE_RATE_BURST", defaultWriteRouteRateBurst),
		}
	)

	flag.StringVar(&host, "host", host, "host interface to bind")
	flag.IntVar(&port, "port", port, "port to listen on")
	flag.StringVar(&listenAddress, "listen-addr", listenAddress, "full listen address host:port; overrides --host/--port")
	flag.StringVar(&schemaPath, "schema-path", schemaPath, "path to ../contracts/anx-schema.yaml")
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
	flag.BoolVar(&enforceLocalQuotas, "enforce-local-quotas", enforceLocalQuotas, "enforce workspace-local write quotas")
	flag.Parse()

	if !enforceLocalQuotas {
		workspaceQuota = primitives.WorkspaceQuota{}
	}

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

	var secretsEncryptor *secrets.Encryptor
	if strings.TrimSpace(secretsKey) != "" {
		enc, err := secrets.NewEncryptor(secretsKey, "v1")
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid ANX_SECRETS_KEY: %v\n", err)
			os.Exit(1)
		}
		secretsEncryptor = enc
	}
	secretsStore := secrets.NewStore(workspace.DB(), secretsEncryptor)

	if secretsEncryptor == nil {
		hasSecrets, err := secretsStore.HasSecrets(context.Background())
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to check secrets table: %v\n", err)
			os.Exit(1)
		}
		if hasSecrets {
			fmt.Fprintf(os.Stderr, "error: secrets exist in the database but ANX_SECRETS_KEY is not set.\nSet ANX_SECRETS_KEY to the encryption key used when the secrets were created.\nRefusing to start — decryption would be impossible.\n")
			os.Exit(1)
		}
	}

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
		workspaceID = "ws_main"
	}
	if strings.TrimSpace(workspaceName) == "" {
		workspaceName = "Main"
	}
	if workspaceHumanGrantLeeway <= 0 {
		workspaceHumanGrantLeeway = auth.DefaultWorkspaceHumanGrantLeeway
	}
	workspaceHumanGrantReplayRetention := auth.DefaultWorkspaceHumanGrantTTL + workspaceHumanGrantLeeway
	var workspaceHumanGrantVerifier auth.WorkspaceHumanGrantIdentityVerifier
	if strings.TrimSpace(workspaceHumanGrantIssuer) != "" || strings.TrimSpace(workspaceHumanGrantAudience) != "" {
		if strings.TrimSpace(workspaceHumanGrantIssuer) == "" || strings.TrimSpace(workspaceHumanGrantAudience) == "" {
			fmt.Fprintln(os.Stderr, "both ANX_WORKSPACE_HUMAN_GRANT_ISSUER and ANX_WORKSPACE_HUMAN_GRANT_AUDIENCE are required to enable external workspace grants")
			os.Exit(1)
		}
		jwksURL, err := auth.WorkspaceHumanGrantJWKSURL(workspaceHumanGrantIssuer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid ANX_WORKSPACE_HUMAN_GRANT_ISSUER: %v\n", err)
			os.Exit(1)
		}
		jwksResolver, err := auth.NewWorkspaceHumanGrantJWKResolver(auth.WorkspaceHumanGrantJWKResolverConfig{
			JWKSURL: jwksURL,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize workspace grant jwks resolver: %v\n", err)
			os.Exit(1)
		}
		workspaceHumanGrantVerifier, err = auth.NewWorkspaceHumanGrantVerifier(auth.WorkspaceHumanGrantVerifierConfig{
			Issuer:      workspaceHumanGrantIssuer,
			Audience:    workspaceHumanGrantAudience,
			WorkspaceID: workspaceID,
			Leeway:      workspaceHumanGrantLeeway,
			Resolver:    jwksResolver,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize workspace human grant verifier: %v\n", err)
			os.Exit(1)
		}
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

	var accountStatusChecker auth.AccountStatusChecker
	accountStatusBase := strings.TrimSpace(os.Getenv("ANX_ACCOUNT_STATUS_URL"))
	legacyAccountStatusBase := strings.TrimSpace(os.Getenv("ANX_CONTROL_PLANE_URL"))
	if accountStatusBase == "" && legacyAccountStatusBase != "" {
		accountStatusBase = legacyAccountStatusBase
		log.Printf("ANX_CONTROL_PLANE_URL is deprecated, use ANX_ACCOUNT_STATUS_URL")
	}
	if accountStatusBase != "" {
		serviceIdentityID := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_ID"))
		serviceIdentityPrivateKeyB64 := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_PRIVATE_KEY"))
		if serviceIdentityID == "" || serviceIdentityPrivateKeyB64 == "" {
			fmt.Fprintln(os.Stderr, "ANX_ACCOUNT_STATUS_URL (or deprecated ANX_CONTROL_PLANE_URL) is set but ANX_WORKSPACE_SERVICE_ID and ANX_WORKSPACE_SERVICE_PRIVATE_KEY are required for account status checks")
			os.Exit(1)
		}
		raw, err := base64.StdEncoding.DecodeString(serviceIdentityPrivateKeyB64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "account status checker: invalid ANX_WORKSPACE_SERVICE_PRIVATE_KEY: %v\n", err)
			os.Exit(1)
		}
		if len(raw) != ed25519.PrivateKeySize {
			fmt.Fprintf(os.Stderr, "account status checker: invalid ANX_WORKSPACE_SERVICE_PRIVATE_KEY length %d (expected %d)\n", len(raw), ed25519.PrivateKeySize)
			os.Exit(1)
		}
		accountStatusAudience := envString("ANX_ACCOUNT_STATUS_AUDIENCE", auth.DefaultAccountStatusAudience)
		accountStatusPath := envString("ANX_ACCOUNT_STATUS_PATH", "v1/internal/accounts/status")
		signer := auth.NewEd25519WorkspaceServiceAssertionSigner(
			serviceIdentityID,
			ed25519.PrivateKey(raw),
			accountStatusAudience,
			workspaceID,
		)
		checker, err := auth.NewHTTPAccountStatusChecker(auth.HTTPAccountStatusCheckerConfig{
			BaseURL:      accountStatusBase,
			EndpointPath: accountStatusPath,
			WorkspaceID:  workspaceID,
			Signer:       signer,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "account status checker: %v\n", err)
			os.Exit(1)
		}
		accountStatusChecker = checker
	}

	authStoreOpts := []auth.Option{
		auth.WithBootstrapToken(bootstrapToken),
		auth.WithAllowDevRegisterLinkedActor(devRegisterLinkedActors),
	}
	if accountStatusChecker != nil {
		authStoreOpts = append(authStoreOpts, auth.WithAccountStatusChecker(accountStatusChecker))
	}
	authStore := auth.NewStore(workspace.DB(), authStoreOpts...)
	passkeySessionStore := auth.NewPasskeySessionStore(auth.DefaultPasskeySessionTTL)
	defer passkeySessionStore.Close()
	primitiveStore := primitives.NewStore(workspace.DB(), blobBackendImpl, effectiveBlobRoot, primitives.WithWorkspaceQuota(workspaceQuota))
	projectionMaintainer := server.NewProjectionMaintainer(server.ProjectionMaintainerConfig{
		PrimitiveStore:    primitiveStore,
		Contract:          contract,
		Mode:              projectionMode,
		PollInterval:      projectionPollInterval,
		StaleScanInterval: staleScanInterval,
		DirtyBatchSize:    projectionBatchSize,
		SystemActorID:     actors.SystemActorID,
	})
	sidecarHost := sidecar.NewHost()
	if sidecarRouterEnabled {
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
			ActorID:           actors.SystemActorID,
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
				return primitiveStore.MarkTopicProjectionsDirty(ctx, []string{threadID}, queuedAt)
			},
		}, routerState)
		sidecarHost = sidecar.NewHost(sidecar.Registration{
			Service: routerService,
			Enabled: true,
		})
	}
	handler := server.NewHandler(
		contract.Version,
		server.WithHealthCheck(workspace.Ping),
		server.WithReadinessCheck("sidecars", "sidecar_unavailable", "sidecar readiness check failed", sidecarHost.Ready),
		server.WithActorRegistry(actorRegistry),
		server.WithAuthStore(authStore),
		server.WithWorkspaceHumanGrantVerifier(workspaceHumanGrantVerifier),
		server.WithPasskeySessionStore(passkeySessionStore),
		server.WithPrimitiveStore(primitiveStore),
		server.WithSchemaContract(contract),
		server.WithWebAuthnConfig(server.WebAuthnConfig{
			RPDisplayName:  webAuthnDisplayName,
			RPID:           webAuthnRPID,
			RPOrigin:       webAuthnOrigin,
			AllowedOrigins: webAuthnAllowedOrigins,
		}),
		server.WithWorkspaceID(workspaceID),
		server.WithSecretsStore(secretsStore),
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
	heartbeatURL := strings.TrimSpace(os.Getenv("ANX_HEARTBEAT_PUBLISHER_URL"))
	if heartbeatURL == "" {
		fmt.Println("heartbeat publisher: disabled (ANX_HEARTBEAT_PUBLISHER_URL unset)")
	} else {
		serviceIdentityID := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_ID"))
		serviceIdentityPrivateKeyB64 := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_PRIVATE_KEY"))
		if serviceIdentityID == "" || serviceIdentityPrivateKeyB64 == "" {
			fmt.Fprintln(os.Stderr, "heartbeat publisher: ANX_HEARTBEAT_PUBLISHER_URL is set but ANX_WORKSPACE_SERVICE_ID and ANX_WORKSPACE_SERVICE_PRIVATE_KEY are required")
			os.Exit(1)
		}
		serviceIdentityPrivateKey, err := base64.StdEncoding.DecodeString(serviceIdentityPrivateKeyB64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "heartbeat publisher: invalid ANX_WORKSPACE_SERVICE_PRIVATE_KEY: %v\n", err)
			os.Exit(1)
		}
		if len(serviceIdentityPrivateKey) != ed25519.PrivateKeySize {
			fmt.Fprintf(os.Stderr, "heartbeat publisher: invalid ANX_WORKSPACE_SERVICE_PRIVATE_KEY length %d (expected %d)\n", len(serviceIdentityPrivateKey), ed25519.PrivateKeySize)
			os.Exit(1)
		}

		heartbeatInterval := envDuration("ANX_HEARTBEAT_INTERVAL", heartbeat.DefaultInterval)
		if heartbeatInterval <= 0 {
			heartbeatInterval = heartbeat.DefaultInterval
		}
		heartbeatAudience := envString("ANX_HEARTBEAT_AUDIENCE", heartbeat.DefaultAudience)

		publisher := &heartbeat.Publisher{
			URL:         heartbeatURL,
			Audience:    heartbeatAudience,
			WorkspaceID: workspaceID,
			Interval:    heartbeatInterval,
			Identity: heartbeat.Identity{
				ID:         serviceIdentityID,
				PrivateKey: ed25519.PrivateKey(serviceIdentityPrivateKey),
			},
			Snapshot: func(ctx context.Context) heartbeat.Snapshot {
				healthSummary := map[string]any{"ok": true}
				if err := workspace.Ping(ctx); err != nil {
					healthSummary["ok"] = false
					healthSummary["storage_error"] = err.Error()
				}
				if err := sidecarHost.Ready(ctx); err != nil {
					healthSummary["ok"] = false
					healthSummary["sidecars_error"] = err.Error()
				}

				projectionSummary := map[string]any{}
				if projectionMaintainer != nil {
					projectionSummary = asMapAny(projectionMaintainer.Snapshot(ctx, time.Now().UTC()))
				}

				usageSummary := map[string]any{}
				if usage, err := primitiveStore.GetWorkspaceUsageSummary(ctx); err != nil {
					usageSummary["error"] = err.Error()
				} else {
					usageSummary = asMapAny(usage)
				}

				return heartbeat.Snapshot{
					Version:                      coreVersion,
					Build:                        coreVersion,
					HealthSummary:                healthSummary,
					ProjectionMaintenanceSummary: projectionSummary,
					UsageSummary:                 usageSummary,
					ActiveStreamCount:            stream.Snapshot(),
				}
			},
		}
		go publisher.Run(maintenanceCtx)
		fmt.Printf("heartbeat publisher: enabled (url=%s, workspace_id=%s, identity=%s, interval=%s)\n", heartbeatURL, workspaceID, serviceIdentityID, heartbeatInterval)
	}
	go runDailyAtUTC(maintenanceCtx, 3, 0, func(jobCtx context.Context) {
		deleted, err := authStore.PurgeConsumedGrantJTIs(jobCtx, workspaceHumanGrantReplayRetention)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to purge consumed workspace grant JTIs: %v\n", err)
			return
		}
		if deleted > 0 {
			fmt.Printf("purged %d consumed workspace grant JTIs\n", deleted)
		}
	})
	go func() {
		humanAuthMode := auth.HumanAuthModeWorkspaceLocal
		if workspaceHumanGrantVerifier != nil {
			humanAuthMode = auth.HumanAuthModeExternalGrant
		}
		fmt.Printf("anx-core listening on http://%s\n", addr)
		fmt.Printf("  projection mode: %s\n", projectionMode)
		fmt.Printf("  sidecars: router=%t (workspace_id=%s, workspace_name=%s)\n", sidecarRouterEnabled, workspaceID, workspaceName)
		fmt.Printf("  human auth mode: %s\n", humanAuthMode)
		if enableDevActorMode {
			fmt.Println("  WARNING: dev actor mode enabled (unauthenticated reads; legacy POST /actors)")
		}
		if allowUnauthenticatedWrites {
			fmt.Println("  WARNING: unauthenticated writes enabled (actor_id in body; local dev only)")
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

func runDailyAtUTC(ctx context.Context, hour int, minute int, fn func(context.Context)) {
	if fn == nil {
		return
	}
	timer := time.NewTimer(durationUntilNextUTCRun(time.Now().UTC(), hour, minute))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			runCtx, cancel := context.WithTimeout(ctx, time.Minute)
			fn(runCtx)
			cancel()
			timer.Reset(durationUntilNextUTCRun(time.Now().UTC(), hour, minute))
		}
	}
}

func durationUntilNextUTCRun(now time.Time, hour int, minute int) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}

// runHealthcheckCLI performs an HTTP GET to /livez for the configured listen address.
// Used by container HEALTHCHECK (distroless images have no curl).
func runHealthcheckCLI() int {
	addr := strings.TrimSpace(os.Getenv("ANX_LISTEN_ADDR"))
	if addr == "" {
		host := strings.TrimSpace(os.Getenv("ANX_HOST"))
		if host == "" {
			host = defaultHost
		}
		port := defaultPort
		if portStr := strings.TrimSpace(os.Getenv("ANX_PORT")); portStr != "" {
			parsed, err := strconv.Atoi(portStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "healthcheck: invalid ANX_PORT %q: %v\n", portStr, err)
				return 1
			}
			port = parsed
		}
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if strings.Contains(addr, "://") {
		fmt.Fprintln(os.Stderr, "healthcheck: ANX_LISTEN_ADDR must be host:port, not a URL")
		return 1
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: parse listen address %q: %v\n", addr, err)
		return 1
	}
	// When the server binds 0.0.0.0 or ::, dialing that address from the client is
	// unreliable inside containers; use loopback for the probe instead.
	dialHost := host
	switch host {
	case "0.0.0.0", "":
		dialHost = "127.0.0.1"
	case "::":
		dialHost = "::1"
	}
	url := "http://" + net.JoinHostPort(dialHost, portStr) + "/livez"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: build request: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: %v\n", url, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: status %d\n", url, resp.StatusCode)
		return 1
	}
	return 0
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

func asMapAny(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	if decoded == nil {
		return map[string]any{}
	}
	return decoded
}
