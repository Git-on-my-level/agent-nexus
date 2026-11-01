// HandlerOption setters and the handlerOptions struct they configure. Each
// option wires one dependency or deployment mode into NewHandler.

package server

import (
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/secrets"
	"agent-nexus-core/internal/series"
)

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	reportPanels                   *reportPanelCache
	readAdmission                  *readAdmission
	routeObserver                  func(string, routeAccessClassifier)
	readVisibility                 func(string, string) bool // Request-scoped access for composed resource reads.
	summaryFormat                  bool                      // Explicit summary=1 migration for composed card collections.
	observationRuntime             *ObservationRuntime
	pmRuntime                      *PMRuntime
	pmHandler                      http.Handler
	healthCheck                    HealthCheckFunc
	actorRegistry                  ActorRegistry
	seriesStore                    *series.Store
	deliveryStore                  *delivery.Store
	authStore                      *auth.Store
	runStore                       *commandcenter.Store
	agentChanges                   *agentChangeHub
	workspaceHumanGrantVerifier    auth.WorkspaceHumanGrantIdentityVerifier
	workspaceManagedGrantVerifier  auth.WorkspaceManagedAgentGrantIdentityVerifier
	passkeySessionStore            *auth.PasskeySessionStore
	primitiveStore                 PrimitiveStore
	contract                       *schema.Contract
	webAuthnConfig                 WebAuthnConfig
	enableDevActorMode             bool
	allowPasskeyDevBypass          bool
	allowUnauthenticatedWrites     bool
	allowLoopbackVerificationReads bool
	inboxRiskHorizon               time.Duration
	answerWakeFlushWhenNoOpenAsks  bool
	answerWakeQuietWindow          time.Duration
	coreVersion                    string
	apiVersion                     string
	minCLIVersion                  string
	recommendedCLIVersion          string
	cliDownloadURL                 string
	hostEnrollmentVerificationURL  string
	webURL                         string
	coreInstanceID                 string
	metaCommandsPath               string
	streamPollInterval             time.Duration
	streamReads                    *streamReadHub
	corsAllowedOrigins             []string
	projectionMaintainer           *ProjectionMaintainer
	requestBodyLimits              RequestBodyLimits
	routeRateLimits                RouteRateLimits
	rateLimiter                    *routeRateLimiter
	workspaceHumanGrantRateLimiter *routeRateLimiter
	// workspaceGrantSubjectRateLimiter budgets verified grant exchanges per
	// principal and grant type.
	workspaceGrantSubjectRateLimiter *routeRateLimiter
	// beforeGrantConsume, when set by tests, runs after a verified grant is
	// charged and before its jti is consumed.
	beforeGrantConsume  func()
	workspaceID         string
	readinessChecks     []namedReadinessCheck
	opsHealthSections   map[string]OpsHealthSectionFunc
	secretsStore        *secrets.Store
	workspaceAccessMode string
}

func WithHealthCheck(healthCheck HealthCheckFunc) HandlerOption {
	return func(opts *handlerOptions) {
		opts.healthCheck = healthCheck
	}
}

func WithReadinessCheck(name string, code string, message string, check HealthCheckFunc) HandlerOption {
	return func(opts *handlerOptions) {
		opts.readinessChecks = append(opts.readinessChecks, namedReadinessCheck{
			Name:    strings.TrimSpace(name),
			Code:    strings.TrimSpace(code),
			Message: strings.TrimSpace(message),
			Check:   check,
		})
	}
}

func WithOpsHealthSection(name string, fn OpsHealthSectionFunc) HandlerOption {
	return func(opts *handlerOptions) {
		name = strings.TrimSpace(name)
		if name == "" || fn == nil {
			return
		}
		if opts.opsHealthSections == nil {
			opts.opsHealthSections = make(map[string]OpsHealthSectionFunc)
		}
		opts.opsHealthSections[name] = fn
	}
}

func WithActorRegistry(actorRegistry ActorRegistry) HandlerOption {
	return func(opts *handlerOptions) {
		opts.actorRegistry = actorRegistry
	}
}

func WithAuthStore(authStore *auth.Store) HandlerOption {
	return func(opts *handlerOptions) {
		opts.authStore = authStore
	}
}

func WithRunStore(store *commandcenter.Store) HandlerOption {
	return func(opts *handlerOptions) { opts.runStore = store }
}

func WithWorkspaceHumanGrantVerifier(verifier auth.WorkspaceHumanGrantIdentityVerifier) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceHumanGrantVerifier = verifier
	}
}

func WithWorkspaceManagedAgentGrantVerifier(verifier auth.WorkspaceManagedAgentGrantIdentityVerifier) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceManagedGrantVerifier = verifier
	}
}

func WithAnswerWakeFlushWhenNoOpenAsks(enabled bool) HandlerOption {
	return func(opts *handlerOptions) { opts.answerWakeFlushWhenNoOpenAsks = enabled }
}

func WithAnswerWakeQuietWindow(window time.Duration) HandlerOption {
	return func(opts *handlerOptions) { opts.answerWakeQuietWindow = window }
}

func WithPasskeySessionStore(store *auth.PasskeySessionStore) HandlerOption {
	return func(opts *handlerOptions) {
		opts.passkeySessionStore = store
	}
}

// WithPMHandler mounts the PM package behind workspace auth, body limits,
// rate limits, and write-access checks. The PM handler must additionally bind
// its Principal to the same authenticated request and enforce PM permissions.
func WithObservationRuntime(runtime *ObservationRuntime) HandlerOption {
	return func(opts *handlerOptions) { opts.observationRuntime = runtime }
}

func WithPMRuntime(runtime *PMRuntime) HandlerOption {
	return func(opts *handlerOptions) {
		opts.pmRuntime = runtime
		if runtime != nil {
			opts.pmHandler = runtime
		}
	}
}

func WithPMHandler(handler http.Handler) HandlerOption {
	return func(opts *handlerOptions) { opts.pmHandler = handler }
}

func WithPrimitiveStore(primitiveStore PrimitiveStore) HandlerOption {
	return func(opts *handlerOptions) {
		opts.primitiveStore = primitiveStore
	}
}

func WithSchemaContract(contract *schema.Contract) HandlerOption {
	return func(opts *handlerOptions) {
		opts.contract = contract
	}
}

func WithWebAuthnConfig(config WebAuthnConfig) HandlerOption {
	return func(opts *handlerOptions) {
		opts.webAuthnConfig = config
	}
}

func WithEnableDevActorMode(enable bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.enableDevActorMode = enable
	}
}

func WithAllowPasskeyDevBypass(allow bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.allowPasskeyDevBypass = allow
	}
}

func WithAllowUnauthenticatedWrites(allow bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.allowUnauthenticatedWrites = allow
	}
}

func WithAllowLoopbackVerificationReads(allow bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.allowLoopbackVerificationReads = allow
	}
}

func WithInboxRiskHorizon(horizon time.Duration) HandlerOption {
	return func(opts *handlerOptions) {
		opts.inboxRiskHorizon = horizon
	}
}

func WithCoreVersion(version string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.coreVersion = strings.TrimSpace(version)
	}
}

func WithAPIVersion(version string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.apiVersion = strings.TrimSpace(version)
	}
}

func WithMinCLIVersion(version string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.minCLIVersion = strings.TrimSpace(version)
	}
}

func WithRecommendedCLIVersion(version string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.recommendedCLIVersion = strings.TrimSpace(version)
	}
}

func WithCLIDownloadURL(downloadURL string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.cliDownloadURL = strings.TrimSpace(downloadURL)
	}
}

func WithHostEnrollmentVerificationURL(verificationURL string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.hostEnrollmentVerificationURL = verificationURL
	}
}

// WithWebURL sets the workspace web UI root advertised as `web_url` in the
// handshake. See WorkspaceWebURL.
func WithWebURL(webURL string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.webURL = strings.TrimRight(strings.TrimSpace(webURL), "/")
	}
}

func WithCoreInstanceID(instanceID string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.coreInstanceID = strings.TrimSpace(instanceID)
	}
}

func WithMetaCommandsPath(path string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.metaCommandsPath = strings.TrimSpace(path)
	}
}

func WithStreamPollInterval(interval time.Duration) HandlerOption {
	return func(opts *handlerOptions) {
		if interval > 0 {
			opts.streamPollInterval = interval
		}
	}
}

func WithCORSAllowedOrigins(origins string) HandlerOption {
	return func(opts *handlerOptions) {
		raw := strings.TrimSpace(origins)
		if raw == "" {
			return
		}
		for _, o := range strings.Split(raw, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				opts.corsAllowedOrigins = append(opts.corsAllowedOrigins, o)
			}
		}
	}
}

func WithProjectionMaintainer(maintainer *ProjectionMaintainer) HandlerOption {
	return func(opts *handlerOptions) {
		opts.projectionMaintainer = maintainer
	}
}

func WithWorkspaceID(workspaceID string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceID = strings.TrimSpace(workspaceID)
	}
}

func WithSecretsStore(store *secrets.Store) HandlerOption {
	return func(opts *handlerOptions) {
		opts.secretsStore = store
	}
}

func WithWorkspaceAccessMode(mode string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceAccessMode = strings.TrimSpace(mode)
	}
}

func WithRequestBodyLimits(limits RequestBodyLimits) HandlerOption {
	return func(opts *handlerOptions) {
		opts.requestBodyLimits = limits
	}
}

func WithRouteRateLimits(limits RouteRateLimits) HandlerOption {
	return func(opts *handlerOptions) {
		opts.routeRateLimits = limits
	}
}

// WithWorkspaceGrantRateLimits sets the per-address budget for grant
// exchange attempts: each reserves one, and grants that verify return it.
func WithWorkspaceGrantRateLimits(limits RouteRateLimits) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceHumanGrantRateLimiter = newRouteRateLimiter(limits.normalize())
	}
}

// WithWorkspaceGrantSubjectRateLimits sets the budget for verified grant
// exchanges, per principal and grant type. The Auth fields apply.
func WithWorkspaceGrantSubjectRateLimits(limits RouteRateLimits) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceGrantSubjectRateLimiter = newWorkspaceGrantPrincipalRateLimiter(limits)
	}
}

// Principal buckets are created only for grants that verify, so the limiter
// can fail closed when full rather than reset a limited principal. A bucket
// refills within a minute, so filling it takes that many distinct principals
// exchanging grants within a minute.
func newWorkspaceGrantPrincipalRateLimiter(limits RouteRateLimits) *routeRateLimiter {
	limiter := newRouteRateLimiter(limits.normalize())
	limiter.maxKeys = defaultWorkspaceGrantPrincipalMaxKeys
	limiter.failClosedWhenFull = true
	return limiter
}
