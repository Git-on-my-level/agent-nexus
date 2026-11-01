// anx-core binary entrypoint: flag and environment wiring, server assembly,
// startup, and shutdown. The deployment default constants sit beside main()
// because main is their only reader. Support pieces live in scheduler.go,
// healthcheck_cli.go, env_config.go, dev_auth_bypass.go, and pm_config.go.

package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	sqlite3 "modernc.org/sqlite/lib"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/buildinfo"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/heartbeat"
	"agent-nexus-core/internal/loopstats"
	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
	"agent-nexus-core/internal/runtimememory"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/series"
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

	defaultWorkspaceMaxBlobBytes        int64 = 1 << 30
	defaultWorkspaceMaxArtifacts        int64 = 100000
	defaultWorkspaceMaxDocuments        int64 = 50000
	defaultWorkspaceMaxRevisions        int64 = 250000
	defaultWorkspaceMaxUploadBytes      int64 = 8 << 20
	defaultRequestBodyLimit             int64 = 1 << 20
	defaultAuthRequestBodyLimit         int64 = 256 << 10
	defaultContentRequestBodyLimit      int64 = 8 << 20
	defaultAuthRouteRateLimitPerMinute        = 600
	defaultAuthRouteRateBurst                 = 100
	defaultWriteRouteRateLimitPerMinute       = 1200
	defaultWriteRouteRateBurst                = 200
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheckCLI())
	}

	pprofDiagnostics, pprofErr := runtimememory.StartPprof(envString("ANX_DEBUG_PPROF_ADDR", ""), envBool("ANX_DEBUG_PPROF_ALLOW_NON_LOOPBACK", false))
	if pprofErr != nil {
		fmt.Fprintln(os.Stderr, pprofErr)
		os.Exit(1)
	}
	defer pprofDiagnostics.Close()
	runtimememory.Configure(os.Stderr)

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
		publicWebUIWorkspaceURL     = envString("ANX_PUBLIC_WEB_UI_WORKSPACE_URL", "")
		webBaseURL                  = envString("ANX_WEB_BASE_URL", "")
		apiVersion                  = envString("ANX_API_VERSION", defaultAPIVersion)
		minCLIVersion               = envString("ANX_MIN_CLI_VERSION", buildinfo.MinCompatibleCLI)
		recommendedCLIVersion       = envString("ANX_RECOMMENDED_CLI_VERSION", buildinfo.Current)
		cliDownloadURL              = envString("ANX_CLI_DOWNLOAD_URL", "")
		coreInstanceID              = envString("ANX_CORE_INSTANCE_ID", defaultInstanceID)
		metaCommandsPath            = envString("ANX_META_COMMANDS_PATH", "")
		streamPollInterval          = envDuration("ANX_STREAM_POLL_INTERVAL", time.Second)
		projectionMode              = envString("ANX_PROJECTION_MODE", server.ProjectionModeBackground)
		projectionPollInterval      = envDuration("ANX_PROJECTION_MAINTENANCE_INTERVAL", 5*time.Second)
		compactAccessMigration      = envBool("ANX_COMPACT_ACCESS_MIGRATION", true)
		compactAccessLegacyReads    = envBool("ANX_COMPACT_ACCESS_LEGACY_READS", false)
		scopedInboxReader           = envBool("ANX_SCOPED_INBOX_READER", false)
		projectionBatchSize         = envInt("ANX_PROJECTION_MAINTENANCE_BATCH_SIZE", 50)
		answerWakeQuietWindow       = envDuration("ANX_ANSWER_WAKE_QUIET_WINDOW", server.DefaultAnswerWakeQuietWindow)
		flushAnswerWakeWhenEmpty    = envBool("ANX_ANSWER_WAKE_FLUSH_WHEN_NO_OPEN_ASKS", true)
		devRegisterLinkedActors     = envBool("ANX_DEV_REGISTER_LINKED_ACTORS", false)
		allowPasskeyDevBypass       = envBool("ANX_ALLOW_PASSKEY_DEV_BYPASS", false)
		enableDevActorMode          = envBool("ANX_ENABLE_DEV_ACTOR_MODE", false)
		allowUnauthenticatedWrites  = envBool("ANX_ALLOW_UNAUTHENTICATED_WRITES", false)
		allowLoopbackVerifyReads    = envBool("ANX_ALLOW_LOOPBACK_VERIFICATION_READS", false)
		bootstrapToken              = envString("ANX_BOOTSTRAP_TOKEN", "")
		webAuthnRPID                = envString("ANX_WEBAUTHN_RPID", "")
		webAuthnOrigin              = envString("ANX_WEBAUTHN_ORIGIN", "")
		webAuthnAllowedOrigins      = envCSV("ANX_WEBAUTHN_ALLOWED_ORIGINS")
		webAuthnDisplayName         = envString("ANX_WEBAUTHN_RP_DISPLAY_NAME", "Agent Nexus")
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
		enforceLocalQuotas          = localQuotaEnforcementEnabled()
		workspaceAccessMode         = envString("ANX_WORKSPACE_ACCESS_MODE", "")
		workspaceQuota              = primitives.WorkspaceQuota{
			MaxBlobBytes:   envInt64("ANX_WORKSPACE_MAX_BLOB_BYTES", defaultWorkspaceMaxBlobBytes),
			MaxArtifacts:   envInt64("ANX_WORKSPACE_MAX_ARTIFACTS", defaultWorkspaceMaxArtifacts),
			MaxDocuments:   envInt64("ANX_WORKSPACE_MAX_DOCUMENTS", defaultWorkspaceMaxDocuments),
			MaxRevisions:   envInt64("ANX_WORKSPACE_MAX_REVISIONS", defaultWorkspaceMaxRevisions),
			MaxUploadBytes: envInt64("ANX_WORKSPACE_MAX_UPLOAD_BYTES", defaultWorkspaceMaxUploadBytes),
		}
		requestBodyLimits = server.RequestBodyLimits{
			Default:    envInt64("ANX_REQUEST_BODY_LIMIT_BYTES", defaultRequestBodyLimit),
			Auth:       envInt64("ANX_AUTH_REQUEST_BODY_LIMIT_BYTES", defaultAuthRequestBodyLimit),
			Content:    envInt64("ANX_CONTENT_REQUEST_BODY_LIMIT_BYTES", defaultContentRequestBodyLimit),
			Attachment: envInt64("ANX_ATTACHMENT_MAX_UPLOAD_BYTES", primitives.DefaultAttachmentMaxUploadBytes),
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
	flag.StringVar(&minCLIVersion, "min-cli-version", minCLIVersion, "minimum compatible CLI version (wire-compatibility floor; does not track every release)")
	flag.StringVar(&recommendedCLIVersion, "recommended-cli-version", recommendedCLIVersion, "recommended CLI version (defaults to this core release)")
	flag.StringVar(&cliDownloadURL, "cli-download-url", cliDownloadURL, "CLI download URL included in compatibility metadata")
	flag.StringVar(&publicWebUIWorkspaceURL, "public-web-ui-workspace-url", publicWebUIWorkspaceURL, "public workspace-scoped web UI URL used for host enrollment verification")
	flag.StringVar(&webBaseURL, "web-base-url", webBaseURL, "absolute URL of this workspace's web UI root, advertised as web_url for browser links (defaults to --public-web-ui-workspace-url)")
	flag.StringVar(&coreInstanceID, "core-instance-id", coreInstanceID, "stable core instance identifier for handshake metadata")
	flag.StringVar(&metaCommandsPath, "meta-commands-path", metaCommandsPath, "path to generated commands metadata JSON")
	flag.DurationVar(&streamPollInterval, "stream-poll-interval", streamPollInterval, "poll interval used by SSE stream endpoints")
	flag.StringVar(&projectionMode, "projection-mode", projectionMode, "projection maintenance mode (background|manual)")
	flag.DurationVar(&projectionPollInterval, "projection-maintenance-interval", projectionPollInterval, "poll interval used by background projection maintenance")
	flag.IntVar(&projectionBatchSize, "projection-maintenance-batch-size", projectionBatchSize, "max dirty thread projections refreshed per maintenance pass")
	flag.BoolVar(&compactAccessMigration, "compact-access-migration", compactAccessMigration, "enable online compact access migration and certified reader cutover")
	flag.BoolVar(&compactAccessLegacyReads, "compact-access-legacy-reads", compactAccessLegacyReads, "force legacy authorization reads even after certification (rollback kill switch)")
	flag.BoolVar(&scopedInboxReader, "scoped-inbox-reader", scopedInboxReader, "enable the scoped inbox index with legacy authorization and fallback")
	flag.BoolVar(&enforceLocalQuotas, "enforce-local-quotas", enforceLocalQuotas, "opt in to workspace capacity quotas (technical safety limits remain enabled)")
	flag.StringVar(&workspaceAccessMode, "workspace-access-mode", workspaceAccessMode, "workspace HTTP API mode: read_write or read_only (hosted quota enforcement)")
	flag.Parse()
	var diagnostics *runtimememory.Diagnostics
	if envBool("ANX_MEMORY_DIAGNOSTICS", false) {
		var diagnosticErr error
		diagnostics, diagnosticErr = runtimememory.StartDiagnostics(true, envInt("ANX_MEMORY_DIAGNOSTICS_PORT", 6061), os.Stderr)
		if diagnosticErr != nil {
			fmt.Fprintf(os.Stderr, "memory diagnostics: %v\n", diagnosticErr)
			os.Exit(1)
		}
	}
	defer diagnostics.Close()

	workspaceQuota = effectiveWorkspaceQuota(enforceLocalQuotas, workspaceQuota)

	parsedProjectionMode, err := server.ParseProjectionMode(projectionMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	projectionMode = parsedProjectionMode

	normalizedWorkspaceAccessMode, err := server.NormalizeWorkspaceAccessMode(workspaceAccessMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	workspaceAccessMode = normalizedWorkspaceAccessMode

	addr := listenAddress
	if addr == "" {
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if err := validateDevAuthBypassConfig(addr, enableDevActorMode, allowUnauthenticatedWrites); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if err := auth.ValidateBootstrapTokenForNonDevDeploy(bootstrapToken); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	verificationURL, err := server.HostEnrollmentVerificationURL(publicWebUIWorkspaceURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	workspaceWebURL, err := server.WorkspaceWebURL(webBaseURL, publicWebUIWorkspaceURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	tempLease, err := storage.PrepareSQLiteTempDirectory(storage.NewLayout(workspaceRoot).DatabasePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to configure SQLite temp storage: %v\n", err)
		os.Exit(1)
	}
	defer tempLease.Close()
	sqlite3.Xsqlite3_temp_directory = tempLease.NativePath
	workspace, err := storage.InitializeWorkspace(context.Background(), workspaceRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize workspace storage: %v\n", err)
		os.Exit(1)
	}
	defer workspace.Close()
	if err := workspace.SetCompactAccessLegacyReads(context.Background(), compactAccessLegacyReads); err != nil {
		fmt.Fprintln(os.Stderr, "failed to configure compact authorization fallback")
		os.Exit(1)
	}

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
	var workspaceManagedGrantVerifier auth.WorkspaceManagedAgentGrantIdentityVerifier
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
		workspaceManagedGrantVerifier, err = auth.NewWorkspaceManagedAgentGrantVerifier(auth.WorkspaceManagedAgentGrantVerifierConfig{
			Issuer:      workspaceHumanGrantIssuer,
			Audience:    workspaceHumanGrantAudience,
			WorkspaceID: workspaceID,
			Leeway:      workspaceHumanGrantLeeway,
			Resolver:    jwksResolver,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize workspace managed-agent grant verifier: %v\n", err)
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
	accountStatusBase := auth.AccountStatusBaseURLFromEnv()
	if accountStatusBase != "" {
		serviceIdentityID := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_ID"))
		serviceIdentityPrivateKeyB64 := strings.TrimSpace(os.Getenv("ANX_WORKSPACE_SERVICE_PRIVATE_KEY"))
		if serviceIdentityID == "" || serviceIdentityPrivateKeyB64 == "" {
			fmt.Fprintln(os.Stderr, "ANX_ACCOUNT_STATUS_URL is set but ANX_WORKSPACE_SERVICE_ID and ANX_WORKSPACE_SERVICE_PRIVATE_KEY are required for account status checks")
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

	// Passkey dev bypass (synthetic human onboarding without WebAuthn) is a
	// local-only capability: it requires both the explicit env switch and a
	// marker file inside the workspace root, and it is only honored alongside
	// the loopback dev-mode gate below.
	passkeyDevBypassMarkerPath := filepath.Join(workspace.Layout().RootDir, devPasskeyBypassMarkerName)
	passkeyDevBypassMarkerPresent, err := fileExists(passkeyDevBypassMarkerPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to check passkey dev bypass marker: %v\n", err)
		os.Exit(1)
	}
	passkeyDevBypassEffective := allowPasskeyDevBypass && passkeyDevBypassMarkerPresent && enableDevActorMode
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
	primitiveStore := primitives.NewStore(
		workspace.DB(),
		blobBackendImpl,
		effectiveBlobRoot,
		primitives.WithWorkspaceQuota(workspaceQuota),
		primitives.WithAskWebhookEncryption(secretsEncryptor),
		primitives.WithAskDeliveryPolicy(strings.Split(os.Getenv("ANX_ASK_NEXT_ACTOR_ORDER"), ",")),
		primitives.WithAskWebhookAllowHosts(strings.FieldsFunc(os.Getenv("ANX_WEBHOOK_ALLOW_HOSTS"), func(r rune) bool { return r == ',' })),
		primitives.WithScopedInboxReader(scopedInboxReader),
		primitives.WithDatabasePath(workspace.Layout().DatabasePath),
	)
	projectionMaintainer := server.NewProjectionMaintainer(server.ProjectionMaintainerConfig{
		PrimitiveStore: primitiveStore,
		Contract:       contract,
		Mode:           projectionMode,
		PollInterval:   projectionPollInterval,
		DirtyBatchSize: projectionBatchSize,
		SystemActorID:  actors.SystemActorID,
	})
	answerWakeMaintainer := server.NewAnswerWakeMaintainer(server.AnswerWakeMaintainerConfig{
		PrimitiveStore:      primitiveStore,
		WorkspaceID:         workspaceID,
		WorkspaceName:       workspaceName,
		QuietWindow:         answerWakeQuietWindow,
		FlushWhenNoOpenAsks: flushAnswerWakeWhenEmpty,
		RecipientIsActive: func(ctx context.Context, actorID string) (bool, error) {
			principals, _, err := authStore.ListPrincipals(ctx, auth.AuthPrincipalListFilter{})
			if err != nil {
				return false, err
			}
			for _, principal := range principals {
				if principal.ActorID == actorID && !principal.Revoked && principal.PrincipalKind == string(auth.PrincipalKindAgent) {
					return true, nil
				}
			}
			return false, nil
		},
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
			UpsertAgentWakeup: func(ctx context.Context, wakeup primitives.AgentWakeup) error {
				_, err := primitiveStore.UpsertAgentWakeup(ctx, wakeup)
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
	observationRuntime, err := server.LoadObservationRuntime(envString("ANX_OBSERVATION_CONFIG", ""), workspaceID, primitiveStore)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize observation runtime: %v\n", err)
		os.Exit(1)
	}
	if observationRuntime != nil {
		invStore, invErr := observation.NewInvestigationStore(workspace.DB(), func(_ context.Context, preset string) error {
			preset = strings.TrimSpace(preset)
			allowed := envString("ANX_INVESTIGATION_PRESET", "")
			handle := envString("ANX_PM_AGENT_HANDLE", "")
			if preset != "" && ((allowed != "" && preset == allowed) || (handle != "" && preset == handle)) {
				return nil
			}
			return fmt.Errorf("existing investigation preset is unavailable; no model fallback")
		})
		if invErr != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize investigation runtime: %v\n", invErr)
			os.Exit(1)
		}
		invRuntime, invErr := observation.NewInvestigationRuntime(invStore)
		if invErr != nil {
			fmt.Fprintf(os.Stderr, "failed to initialize investigation runtime: %v\n", invErr)
			os.Exit(1)
		}
		observationRuntime.BindInvestigations(invRuntime)
	}
	pmActorID := configuredPMActor(os.Stderr)
	pmRuntime, err := server.NewPMRuntime(workspace.DB(), primitiveStore, authStore, server.PMRuntimeConfig{
		PM: pm.Config{
			WorkspaceID:    workspaceID,
			WorkspaceName:  workspaceName,
			BaseURL:        envString("ANX_PM_BASE_URL", "http://127.0.0.1:"+strconv.Itoa(port)),
			AgentActorID:   pmActorID,
			AgentHandle:    envString("ANX_PM_AGENT_HANDLE", ""),
			TurnTimeout:    envDuration("ANX_PM_TURN_TIMEOUT", 2*time.Minute),
			LeaseTTL:       envDuration("ANX_PM_LEASE_TTL", time.Minute),
			MaxOutputBytes: envInt("ANX_PM_MAX_OUTPUT_BYTES", 16000),
			MaxConcurrent:  envInt("ANX_PM_MAX_CONCURRENT", 2),
			MaxQueued:      envInt("ANX_PM_MAX_QUEUED", 20),
		},
		BridgeEnabled:           envBool("ANX_PM_BRIDGE_ENABLED", false),
		RuntimeEnvelopeEnforced: envBool("ANX_PM_RUNTIME_ENVELOPE_ENFORCED", false),
		Observation:             observationRuntime,
		TelegramWebhookSecret:   envString("ANX_PM_TELEGRAM_WEBHOOK_SECRET", ""),
		TelegramBotID:           envString("ANX_PM_TELEGRAM_BOT_ID", ""),
		TelegramBotToken:        envString("ANX_PM_TELEGRAM_BOT_TOKEN", ""),
		TelegramAPIBase:         envString("ANX_PM_TELEGRAM_API_BASE", ""),
		DiscordPublicKeyHex:     envString("ANX_PM_DISCORD_PUBLIC_KEY", ""),
		DiscordApplicationID:    envString("ANX_PM_DISCORD_APPLICATION_ID", ""),
		DiscordBotToken:         envString("ANX_PM_DISCORD_BOT_TOKEN", ""),
		DiscordAPIBase:          envString("ANX_PM_DISCORD_API_BASE", ""),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize PM runtime: %v\n", err)
		os.Exit(1)
	}
	handler := server.NewHandler(
		contract.Version,
		server.WithHealthCheck(workspace.Ping),
		server.WithReadinessCheck("sidecars", "sidecar_unavailable", "sidecar readiness check failed", sidecarHost.Ready),
		server.WithActorRegistry(actorRegistry),
		server.WithAuthStore(authStore),
		server.WithSeriesStore(&series.Store{DB: workspace.DB(), Auth: authStore}),
		server.WithDeliveryStore(&delivery.Store{DB: workspace.DB(), AccessContext: server.DeliveryAccessContext, CanRead: server.NewEscalationReadCheck(primitiveStore)}),
		server.WithRunStore(commandcenter.NewStore(workspace.DB(), commandcenter.SQLIdentities{DB: workspace.DB()})),
		server.WithWorkspaceHumanGrantVerifier(workspaceHumanGrantVerifier),
		server.WithWorkspaceManagedAgentGrantVerifier(workspaceManagedGrantVerifier),
		server.WithPasskeySessionStore(passkeySessionStore),
		server.WithAllowPasskeyDevBypass(passkeyDevBypassEffective),
		server.WithPrimitiveStore(primitiveStore),
		server.WithPMRuntime(pmRuntime),
		server.WithObservationRuntime(observationRuntime),
		server.WithSchemaContract(contract),
		server.WithWebAuthnConfig(server.WebAuthnConfig{
			RPDisplayName:  webAuthnDisplayName,
			RPID:           webAuthnRPID,
			RPOrigin:       webAuthnOrigin,
			AllowedOrigins: webAuthnAllowedOrigins,
		}),
		server.WithWorkspaceID(workspaceID),
		server.WithAnswerWakeFlushWhenNoOpenAsks(flushAnswerWakeWhenEmpty),
		server.WithAnswerWakeQuietWindow(answerWakeQuietWindow),
		server.WithWorkspaceAccessMode(workspaceAccessMode),
		server.WithSecretsStore(secretsStore),
		server.WithEnableDevActorMode(enableDevActorMode),
		server.WithAllowUnauthenticatedWrites(allowUnauthenticatedWrites),
		server.WithAllowLoopbackVerificationReads(allowLoopbackVerifyReads),
		server.WithCoreVersion(coreVersion),
		server.WithAPIVersion(apiVersion),
		server.WithMinCLIVersion(minCLIVersion),
		server.WithRecommendedCLIVersion(recommendedCLIVersion),
		server.WithCLIDownloadURL(cliDownloadURL),
		server.WithHostEnrollmentVerificationURL(verificationURL),
		server.WithWebURL(workspaceWebURL),
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
	// Do not allow background writers or HTTP admission to compete with install.
	if err := workspace.InitializeUsageTracking(maintenanceCtx); err != nil {
		fmt.Fprintln(os.Stderr, "failed to initialize usage tracking")
		os.Exit(1)
	}
	var compactWait <-chan struct{}
	if compactAccessMigration {
		compactCtx, compactCancel := context.WithTimeout(maintenanceCtx, 10*time.Minute)
		defer compactCancel()
		report := func(_ storage.CompactAccessVerification, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "compact access migration: %v\n", err)
				return
			}
			fmt.Fprintln(os.Stderr, "compact access migration: certified; certified reader selected unless legacy fallback is set")
		}
		proof, resume, err := workspace.PrepareCompactAccessMigration(compactCtx)
		if !resume {
			report(proof, err)
		} else {
			compactDone := make(chan struct{})
			compactWait = compactDone
			go func() { defer close(compactDone); workspace.RunCompactAccessMigration(compactCtx, report) }()
			defer func() { maintenanceCancel(); <-compactDone }()
		}
	}

	retiredDone := make(chan struct{})
	go func() { defer close(retiredDone); workspace.RunCompactAccessRetiredMaintenance(maintenanceCtx) }()
	defer func() { maintenanceCancel(); <-retiredDone }()
	go primitiveStore.RunAskWebhooks(maintenanceCtx, func(ctx context.Context, actor string) bool {
		p, err := authStore.PrincipalForActor(ctx, actor)
		return err == nil && !p.Revoked && p.PrincipalKind == string(auth.PrincipalKindAgent)
	})
	loopstats.Register("ask_webhooks", "ask_lifecycle", "brief", "ask_subjects", "inbox_lifecycle", "document_history", "curation", "replay_gc", "document_search", "comment_search", "scope_inbox", "projection", "answer_wake", "escalation", "card_lease", "observation", "pm", "heartbeat", "compact_migration", "compact_retired", "router")
	go loopstats.Run(maintenanceCtx, os.Stderr)
	go primitiveStore.RunAskLifecycle(maintenanceCtx, func(err error) { fmt.Fprintln(os.Stderr, "ask lifecycle maintenance: retrying bounded batch") })
	go primitiveStore.RunBriefMaintenance(maintenanceCtx, func(err error) { fmt.Fprintf(os.Stderr, "brief maintenance: %v\n", err) })
	go workspace.RunAskSubjectsMaintenance(maintenanceCtx, func(err error) { fmt.Fprintln(os.Stderr, "ask subject maintenance: retrying bounded batch") })
	go workspace.RunInboxLifecycleMaintenance(maintenanceCtx, func(err error) { fmt.Fprintf(os.Stderr, "inbox lifecycle maintenance: %v\n", err) })
	documentHistoryDone := make(chan struct{})
	go func() {
		defer close(documentHistoryDone)
		// Moving edges advances the authorization epoch, which would restart
		// compact access certification; let a resumed certification finish.
		if compactWait != nil {
			select {
			case <-compactWait:
			case <-maintenanceCtx.Done():
				return
			}
		}
		workspace.RunDocumentAccessHistoryMaintenance(maintenanceCtx, func(err error) {
			fmt.Fprintf(os.Stderr, "document access history maintenance: retrying bounded batch: %v\n", err)
		})
	}()
	defer func() { maintenanceCancel(); <-documentHistoryDone }()
	go server.RunCurationFormulaLoop(maintenanceCtx, primitiveStore, func(err error) { fmt.Fprintf(os.Stderr, "curation formula pass: %v\n", err) })

	replayGCDone := make(chan struct{})
	go func() {
		defer close(replayGCDone)
		storage.RunReplayReceiptMaintenance(maintenanceCtx, workspace.DB(), func(error) { fmt.Fprintln(os.Stderr, "replay receipt maintenance: retrying bounded batch") })
	}()
	defer func() { maintenanceCancel(); <-replayGCDone }()
	commentSearchDone := make(chan struct{})
	documentSearchDone := make(chan struct{})
	go func() {
		defer close(documentSearchDone)
		primitiveStore.RunDocumentSearchMaintenance(maintenanceCtx, func(error) { fmt.Fprintln(os.Stderr, "document search maintenance: retrying bounded batch") })
	}()
	defer func() { maintenanceCancel(); <-documentSearchDone }()
	go func() {
		defer close(commentSearchDone)
		workspace.RunCommentSearchMaintenance(maintenanceCtx, func(error) { fmt.Fprintln(os.Stderr, "comment search backfill: retrying bounded batch") })
	}()
	defer func() { maintenanceCancel(); <-commentSearchDone }()
	if scopedInboxReader {
		buildDone, shadowDone := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(buildDone)
			workspace.RunScopeInboxMaintenance(maintenanceCtx, func(err error) { fmt.Fprintln(os.Stderr, "scoped inbox build: retrying bounded batch") })
		}()
		go func() { defer close(shadowDone); primitiveStore.RunScopeInboxShadow(maintenanceCtx) }()
		defer func() { maintenanceCancel(); <-buildDone; <-shadowDone }()
	}

	if projectionMode == server.ProjectionModeBackground {
		go projectionMaintainer.Run(maintenanceCtx)
	}
	if answerWakeMaintainer != nil {
		go answerWakeMaintainer.Run(maintenanceCtx)
	}
	// The decide_by escalation clock runs in core on self-hosted and hosted
	// cores alike; hosted control planes wake idle cores at the earliest
	// decide_by the heartbeat publishes.
	escalator := &delivery.Escalator{
		Store:         delivery.Store{DB: workspace.DB()},
		WorkspaceName: workspaceName,
		LinkBase:      publicWebUIWorkspaceURL,
		DigestHourUTC: envInt("ANX_DIGEST_HOUR_UTC", delivery.DefaultDigestHourUTC),
		CanRead:       server.NewEscalationReadCheck(primitiveStore),
		AskPM:         server.NewEscalationPMWaker(workspace.DB(), primitiveStore, authStore, pmRuntime.AgentActorID, server.NewEscalationReadCheck(primitiveStore), workspaceID, workspaceName),
	}
	go escalator.Run(maintenanceCtx)
	// The lease clock records each expired work lease once and wakes the
	// card's agent assignee (or the PM). Lease state itself is computed on
	// read, so a sleeping hosted core only delays the audit event and wake.
	cardLeaseClock := &server.CardLeaseClock{Store: primitiveStore, AuthStore: authStore, WorkspaceID: workspaceID, WorkspaceName: workspaceName}
	if pmRuntime != nil {
		cardLeaseClock.PMActorID = pmRuntime.AgentActorID
		primitiveStore.SetTrackingLead(pmRuntime.AgentActorID)
	}
	go cardLeaseClock.Run(maintenanceCtx)
	workTrackingClock := &server.WorkTrackingClock{Store: primitiveStore, AuthStore: authStore, Deliveries: delivery.Store{DB: workspace.DB()}, WorkspaceID: workspaceID, WorkspaceName: workspaceName}
	go workTrackingClock.Run(maintenanceCtx)
	sidecarHost.Run(maintenanceCtx)
	if observationRuntime != nil {
		go observationRuntime.Run(maintenanceCtx)
	}
	if pmRuntime != nil {
		go func() {
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-maintenanceCtx.Done():
					return
				case <-ticker.C:
					if err := pmRuntime.Tick(maintenanceCtx); err != nil {
						fmt.Fprintf(os.Stderr, "pm maintenance: %v\n", err)
					}
				}
			}
		}()
	}
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
				if usage, err := primitiveStore.GetMaintenanceWorkspaceUsageSummary(ctx); err != nil {
					usageSummary["error"] = err.Error()
				} else {
					usageSummary = asMapAny(usage)
				}

				// Counts and times only; never titles or refs.
				attentionSummary, err := (delivery.Store{DB: workspace.DB(), CanRead: server.NewEscalationReadCheck(primitiveStore)}).AttentionSummary(ctx, time.Now().UTC())
				if err != nil {
					attentionSummary = nil
				}
				return heartbeat.Snapshot{
					Version:                      coreVersion,
					Build:                        coreVersion,
					HealthSummary:                healthSummary,
					ProjectionMaintenanceSummary: projectionSummary,
					UsageSummary:                 usageSummary,
					ActiveStreamCount:            stream.Snapshot(),
					AttentionSummary:             attentionSummary,
				}
			},
		}
		go publisher.Run(maintenanceCtx)
		fmt.Printf("heartbeat publisher: enabled (url=%s, workspace_id=%s, identity=%s, interval=%s)\n", heartbeatURL, workspaceID, serviceIdentityID, heartbeatInterval)
	}
	go runDailyAtUTC(maintenanceCtx, 3, 15, func(jobCtx context.Context) {
		if err := (&series.Store{DB: workspace.DB(), Auth: authStore}).Compact(jobCtx, time.Now().UTC()); err != nil {
			fmt.Fprintf(os.Stderr, "series retention: %v\n", err)
		}
	})
	go runDailyAtUTC(maintenanceCtx, 3, 0, func(jobCtx context.Context) {
		deleted, err := authStore.PurgeConsumedGrantJTIs(jobCtx, workspaceHumanGrantReplayRetention)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to purge consumed workspace grant JTIs: %v\n", err)
		} else if deleted > 0 {
			fmt.Printf("purged %d consumed workspace grant JTIs\n", deleted)
		}
	})
	go runEvery(maintenanceCtx, auth.AuthRowPurgeInterval, authRowPurgeTimeout, func(jobCtx context.Context) {
		purged, err := authStore.PurgeExpiredAuthRows(jobCtx, auth.AuthRowPurgeOptions{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to purge expired auth tokens and sessions: %v\n", err)
			return
		}
		if purged.AccessTokens > 0 || purged.RefreshSessions > 0 {
			fmt.Printf("purged %d expired access tokens and %d expired or revoked refresh sessions (truncated=%t)\n", purged.AccessTokens, purged.RefreshSessions, purged.Truncated)
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
			if passkeyDevBypassEffective {
				fmt.Printf("  WARNING: passkey dev bypass enabled (marker=%s)\n", passkeyDevBypassMarkerPath)
			} else if allowPasskeyDevBypass {
				fmt.Printf("  WARNING: passkey dev bypass requested but inactive (missing marker %s)\n", passkeyDevBypassMarkerPath)
			}
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
