package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/controlplaneauth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

type HealthCheckFunc func(ctx context.Context) error
type OpsHealthSectionFunc func(ctx context.Context) map[string]any

type namedReadinessCheck struct {
	Name    string
	Code    string
	Message string
	Check   HealthCheckFunc
}

type ActorRegistry interface {
	Register(ctx context.Context, actor actors.Actor) (actors.Actor, error)
	List(ctx context.Context, filter actors.ActorListFilter) ([]actors.Actor, string, error)
	Exists(ctx context.Context, actorID string) (bool, error)
	Get(ctx context.Context, actorID string) (actors.Actor, error)
}

type PrimitiveStore interface {
	AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error)
	GetEvent(ctx context.Context, id string) (map[string]any, error)
	ArchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	UnarchiveEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	TombstoneEvent(ctx context.Context, actorID, eventID, reason string) (map[string]any, error)
	RestoreEvent(ctx context.Context, actorID, eventID string) (map[string]any, error)
	CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error)
	CreateArtifactAndEvent(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string, event map[string]any) (map[string]any, map[string]any, error)
	GetArtifact(ctx context.Context, id string) (map[string]any, error)
	GetArtifactContent(ctx context.Context, id string) ([]byte, string, error)
	ListArtifacts(ctx context.Context, filter primitives.ArtifactListFilter) ([]map[string]any, error)
	GetWorkspaceUsageSummary(ctx context.Context) (primitives.WorkspaceUsageSummary, error)
	RebuildBlobUsageLedger(ctx context.Context) (primitives.BlobUsageLedgerRebuildResult, error)
	GetIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string) (primitives.IdempotencyReplay, error)
	PutIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string, requestHash string, status int, response map[string]any) error
	ListDerivedInboxItems(ctx context.Context, filter primitives.DerivedInboxListFilter) ([]primitives.DerivedInboxItem, error)
	GetDerivedInboxItem(ctx context.Context, id string) (primitives.DerivedInboxItem, error)
	ReplaceDerivedInboxItems(ctx context.Context, threadID string, items []primitives.DerivedInboxItem) error
	GetDerivedThreadProjection(ctx context.Context, threadID string) (primitives.DerivedThreadProjection, error)
	ListDerivedThreadProjections(ctx context.Context, threadIDs []string) (map[string]primitives.DerivedThreadProjection, error)
	PutDerivedThreadProjection(ctx context.Context, projection primitives.DerivedThreadProjection) error
	MarkDerivedThreadProjectionDirty(ctx context.Context, threadID string, dirtyAt string) error
	ClearDerivedThreadProjectionDirty(ctx context.Context, threadID string) error
	ListDerivedThreadProjectionDirtyEntries(ctx context.Context, limit int) ([]primitives.DerivedThreadProjectionDirtyEntry, error)
	GetDerivedThreadProjectionQueueStats(ctx context.Context) (primitives.DerivedThreadProjectionQueueStats, error)
	ListDocuments(ctx context.Context, filter primitives.DocumentListFilter) ([]map[string]any, string, error)
	MarkThreadProjectionsDirty(ctx context.Context, threadIDs []string, queuedAt time.Time) error
	RequeueThreadProjectionRefresh(ctx context.Context, threadID string, queuedAt time.Time) error
	GetThreadProjectionRefreshStatuses(ctx context.Context, threadIDs []string) (map[string]primitives.ThreadProjectionRefreshStatus, error)
	MarkThreadProjectionRefreshStarted(ctx context.Context, threadID string, startedAt time.Time) (int64, error)
	MarkThreadProjectionRefreshSucceeded(ctx context.Context, threadID string, completedGeneration int64, completedAt time.Time) error
	MarkThreadProjectionRefreshFailed(ctx context.Context, threadID string, failedGeneration int64, failedAt time.Time, message string) error
	CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error)
	GetDocument(ctx context.Context, documentID string) (map[string]any, map[string]any, error)
	UpdateDocument(ctx context.Context, actorID string, documentID string, documentPatch map[string]any, ifBaseRevision string, content any, contentType string, refs []string, revisionProvenance map[string]any) (map[string]any, map[string]any, error)
	ListDocumentHistory(ctx context.Context, documentID string) ([]map[string]any, error)
	GetDocumentRevision(ctx context.Context, documentID string, revisionID string) (map[string]any, error)
	GetDocumentRevisionByID(ctx context.Context, revisionID string) (map[string]any, error)
	ListBoards(ctx context.Context, filter primitives.BoardListFilter) ([]primitives.BoardListItem, string, error)
	CreateBoard(ctx context.Context, actorID string, board map[string]any) (map[string]any, error)
	GetBoard(ctx context.Context, boardID string) (map[string]any, error)
	GetBoardSummary(ctx context.Context, boardID string) (map[string]any, error)
	UpdateBoard(ctx context.Context, actorID string, boardID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, error)
	ListCards(ctx context.Context, filter primitives.CardListFilter) ([]map[string]any, error)
	ListBoardCards(ctx context.Context, boardID string) ([]map[string]any, error)
	GetBoardCard(ctx context.Context, boardID string, identifier string) (map[string]any, error)
	CreateBoardCard(ctx context.Context, actorID string, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error)
	AddBoardCard(ctx context.Context, actorID string, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error)
	UpdateBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.UpdateBoardCardInput) (primitives.BoardCardMutationResult, error)
	MoveBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	RemoveBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardRemovalResult, error)
	ArchiveBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	RestoreArchivedBoardCard(ctx context.Context, actorID string, boardID string, identifier string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	PurgeArchivedBoardCard(ctx context.Context, boardID string, identifier string) error
	ListBoardCardHistory(ctx context.Context, cardID string) ([]map[string]any, error)
	ListBoardMembershipsByThread(ctx context.Context, threadID string) ([]primitives.BoardMembership, error)
	ListRefEdgesBySource(ctx context.Context, sourceType, sourceID string) ([]primitives.RefEdge, error)
	GetSnapshot(ctx context.Context, id string) (map[string]any, error)
	ListTopics(ctx context.Context, filter primitives.TopicListFilter) ([]map[string]any, string, error)
	CreateTopic(ctx context.Context, actorID string, topic map[string]any) (primitives.TopicPatchResult, error)
	GetTopic(ctx context.Context, topicID string) (map[string]any, error)
	PatchTopic(ctx context.Context, actorID string, topicID string, patch map[string]any, ifUpdatedAt *string) (primitives.TopicPatchResult, error)
	ArchiveTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	UnarchiveTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	TombstoneTopic(ctx context.Context, actorID string, topicID string, reason string) (map[string]any, error)
	RestoreTopic(ctx context.Context, actorID string, topicID string) (map[string]any, error)
	CreateThread(ctx context.Context, actorID string, thread map[string]any) (primitives.PatchSnapshotResult, error)
	GetThread(ctx context.Context, id string) (map[string]any, error)
	PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (primitives.PatchSnapshotResult, error)
	ListThreads(ctx context.Context, filter primitives.ThreadListFilter) ([]map[string]any, string, error)
	CreateCommitment(ctx context.Context, actorID string, commitment map[string]any) (primitives.PatchSnapshotResult, error)
	GetCommitment(ctx context.Context, id string) (map[string]any, error)
	PatchCommitment(ctx context.Context, actorID string, id string, patch map[string]any, refs []string, ifUpdatedAt *string) (primitives.PatchSnapshotResult, error)
	ListCommitments(ctx context.Context, filter primitives.CommitmentListFilter) ([]map[string]any, error)
	ListEventsByThread(ctx context.Context, threadID string) ([]map[string]any, error)
	ListRecentEventsByThread(ctx context.Context, threadID string, limit int) ([]map[string]any, error)
	ListEvents(ctx context.Context, filter primitives.EventListFilter) ([]map[string]any, error)
	TombstoneArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error)
	ArchiveArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	UnarchiveArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	RestoreArtifact(ctx context.Context, actorID string, artifactID string) (map[string]any, error)
	PurgeTombstonedArtifact(ctx context.Context, artifactID string) error
	TombstoneDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error)
	ArchiveDocument(ctx context.Context, actorID string, documentID string) (map[string]any, map[string]any, error)
	UnarchiveDocument(ctx context.Context, actorID string, documentID string) (map[string]any, map[string]any, error)
	RestoreDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error)
	PurgeDocument(ctx context.Context, documentID string) error
	ArchiveThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	UnarchiveThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	TombstoneThread(ctx context.Context, actorID string, threadID string, reason string) (map[string]any, error)
	RestoreThread(ctx context.Context, actorID string, threadID string) (map[string]any, error)
	PurgeThread(ctx context.Context, threadID string) error
	ArchiveBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	UnarchiveBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	TombstoneBoard(ctx context.Context, actorID string, boardID string, reason string) (map[string]any, error)
	RestoreBoard(ctx context.Context, actorID string, boardID string) (map[string]any, error)
	PurgeBoard(ctx context.Context, boardID string) error
}

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	healthCheck                    HealthCheckFunc
	actorRegistry                  ActorRegistry
	authStore                      *auth.Store
	passkeySessionStore            *auth.PasskeySessionStore
	primitiveStore                 PrimitiveStore
	contract                       *schema.Contract
	webAuthnConfig                 WebAuthnConfig
	enableDevActorMode             bool
	allowUnauthenticatedWrites     bool
	allowLoopbackVerificationReads bool
	inboxRiskHorizon               time.Duration
	coreVersion                    string
	apiVersion                     string
	minCLIVersion                  string
	recommendedCLIVersion          string
	cliDownloadURL                 string
	coreInstanceID                 string
	metaCommandsPath               string
	streamPollInterval             time.Duration
	corsAllowedOrigins             []string
	projectionMaintainer           *ProjectionMaintainer
	requestBodyLimits              RequestBodyLimits
	routeRateLimits                RouteRateLimits
	rateLimiter                    *routeRateLimiter
	humanAuthMode                  string
	controlPlaneHumanVerifier      *controlplaneauth.WorkspaceHumanVerifier
	workspaceServiceIdentity       *controlplaneauth.WorkspaceServiceIdentity
	workspaceID                    string
	readinessChecks                []namedReadinessCheck
	opsHealthSections              map[string]OpsHealthSectionFunc
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

func WithPasskeySessionStore(store *auth.PasskeySessionStore) HandlerOption {
	return func(opts *handlerOptions) {
		opts.passkeySessionStore = store
	}
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

func WithHumanAuthMode(mode string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.humanAuthMode = strings.TrimSpace(mode)
	}
}

func WithControlPlaneHumanVerifier(verifier *controlplaneauth.WorkspaceHumanVerifier) HandlerOption {
	return func(opts *handlerOptions) {
		opts.controlPlaneHumanVerifier = verifier
	}
}

func WithWorkspaceServiceIdentity(identity *controlplaneauth.WorkspaceServiceIdentity) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceServiceIdentity = identity
	}
}

func WithWorkspaceID(workspaceID string) HandlerOption {
	return func(opts *handlerOptions) {
		opts.workspaceID = strings.TrimSpace(workspaceID)
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

type routeAccessBucket string

const (
	routeAccessAlwaysPublic           routeAccessBucket = "always_public"
	routeAccessPublicAuthCeremony     routeAccessBucket = "public_auth_ceremony"
	routeAccessWorkspaceBusiness      routeAccessBucket = "workspace_business_surface"
	routeAccessDevOnlyLegacyActor     routeAccessBucket = "dev_only_legacy_actor_surface"
	routeAccessAuthenticatedPrincipal routeAccessBucket = "authenticated_principal_surface"
)

type routeAccessRequirement struct {
	bucket    routeAccessBucket
	supported bool
}

type routeAccessClassifier func(*http.Request) routeAccessRequirement

func exactRouteAccess(bucket routeAccessBucket, methods ...string) routeAccessClassifier {
	allowed := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method != "" {
			allowed[method] = struct{}{}
		}
	}

	return func(r *http.Request) routeAccessRequirement {
		if len(allowed) > 0 {
			if _, ok := allowed[strings.ToUpper(strings.TrimSpace(r.Method))]; !ok {
				return routeAccessRequirement{}
			}
		}
		return routeAccessRequirement{bucket: bucket, supported: true}
	}
}

func isReadOnlyRequest(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return true
	default:
		return false
	}
}

func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeLivenessOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func readinessErrorPayload(r *http.Request, opts handlerOptions) map[string]any {
	if opts.healthCheck != nil {
		if err := opts.healthCheck(r.Context()); err != nil {
			return map[string]any{
				"ok":    false,
				"error": errorPayload("storage_unavailable", "storage health check failed"),
			}
		}
	}
	for _, check := range opts.readinessChecks {
		if check.Check == nil {
			continue
		}
		if err := check.Check(r.Context()); err != nil {
			code := firstNonEmptyString(check.Code, "service_unavailable")
			message := firstNonEmptyString(check.Message, "readiness check failed")
			return map[string]any{
				"ok":    false,
				"error": errorPayload(code, message),
				"check": map[string]any{
					"name":  check.Name,
					"error": err.Error(),
				},
			}
		}
	}
	return nil
}

func checkReadiness(w http.ResponseWriter, r *http.Request, opts handlerOptions) bool {
	if payload := readinessErrorPayload(r, opts); payload != nil {
		writeJSON(w, http.StatusServiceUnavailable, payload)
		return false
	}
	return true
}

func enforceRouteAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, requirement routeAccessRequirement) bool {
	if !requirement.supported {
		return true
	}

	switch requirement.bucket {
	case routeAccessAlwaysPublic, routeAccessPublicAuthCeremony:
		return true
	case routeAccessWorkspaceBusiness:
		if isReadOnlyRequest(r.Method) && opts.enableDevActorMode {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		if isReadOnlyRequest(r.Method) && opts.allowLoopbackVerificationReads && isLoopbackRequest(r) {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		if !isReadOnlyRequest(r.Method) && opts.allowUnauthenticatedWrites {
			_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
			return ok
		}
		_, ok := authenticatePrincipalFromHeader(w, r, opts, true)
		return ok
	case routeAccessDevOnlyLegacyActor:
		if !opts.enableDevActorMode {
			writeError(w, http.StatusForbidden, "dev_actor_mode_required", "legacy actor flows require explicit development mode")
			return false
		}
		_, ok := authenticatePrincipalFromHeader(w, r, opts, false)
		return ok
	case routeAccessAuthenticatedPrincipal:
		_, ok := authenticatePrincipalFromHeader(w, r, opts, true)
		return ok
	default:
		writeError(w, http.StatusForbidden, "access_denied", "request is not allowed on this deployment")
		return false
	}
}

func NewHandler(schemaVersion string, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		coreVersion:                strings.TrimSpace(schemaVersion),
		apiVersion:                 "v0",
		minCLIVersion:              "0.1.0",
		recommendedCLIVersion:      "0.1.0",
		coreInstanceID:             "core-local",
		streamPollInterval:         time.Second,
		allowUnauthenticatedWrites: false,
		humanAuthMode:              controlplaneauth.HumanAuthModeWorkspaceLocal,
	}
	for _, option := range options {
		option(&opts)
	}
	if opts.coreVersion == "" {
		opts.coreVersion = strings.TrimSpace(schemaVersion)
	}
	if opts.apiVersion == "" {
		opts.apiVersion = "v0"
	}
	if opts.minCLIVersion == "" {
		opts.minCLIVersion = "0.1.0"
	}
	if opts.recommendedCLIVersion == "" {
		opts.recommendedCLIVersion = opts.minCLIVersion
	}
	if opts.coreInstanceID == "" {
		opts.coreInstanceID = "core-local"
	}
	if opts.streamPollInterval <= 0 {
		opts.streamPollInterval = time.Second
	}
	if opts.humanAuthMode == "" {
		opts.humanAuthMode = controlplaneauth.HumanAuthModeWorkspaceLocal
	}
	opts.requestBodyLimits = opts.requestBodyLimits.normalize()
	opts.routeRateLimits = opts.routeRateLimits.normalize()
	opts.rateLimiter = newRouteRateLimiter(opts.routeRateLimits)

	mux := http.NewServeMux()
	registerRoute := func(pattern string, classify routeAccessClassifier, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			requirement := classify(r)
			if !enforceRouteAccess(w, r, opts, requirement) {
				return
			}
			if bucket, scope := routeRateLimitForRequest(r, requirement); bucket != "" {
				if ok, retryAfter := opts.rateLimiter.allow(bucket, scope, time.Now().UTC()); !ok {
					writeRateLimitedError(w, bucket, retryAfter)
					return
				}
			}
			if limit := requestBodyLimitForRequest(r.URL.Path, r.Method, requirement, opts.requestBodyLimits); limit > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			handler(w, r)
		})
	}

	registerRoute("/health", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/livez", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/readyz", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		if !checkReadiness(w, r, opts) {
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/ops/health", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		payload := map[string]any{"ok": true}
		if opts.projectionMaintainer != nil {
			payload["projection_maintenance"] = opts.projectionMaintainer.Snapshot(r.Context(), time.Now().UTC())
		}
		if len(opts.opsHealthSections) > 0 {
			keys := make([]string, 0, len(opts.opsHealthSections))
			for key := range opts.opsHealthSections {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				payload[key] = opts.opsHealthSections[key](r.Context())
			}
		}
		if readinessPayload := readinessErrorPayload(r, opts); readinessPayload != nil {
			for key, value := range readinessPayload {
				payload[key] = value
			}
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	})

	registerRoute("/ops/usage-summary", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetUsageSummary(w, r, opts)
	})

	registerRoute("/ops/blob-usage/rebuild", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRebuildBlobUsageLedger(w, r, opts)
	})

	registerRoute("/version", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		payload, err := versionPayload(opts, schemaVersion)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "meta_unavailable", "generated command metadata is not available")
			return
		}
		writeJSON(w, http.StatusOK, payload)
	})

	registerRoute("/meta/handshake", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaHandshake(w, r, opts, schemaVersion)
	})

	registerRoute("/meta/commands", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaCommands(w, r, opts)
	})

	registerRoute("/meta/commands/", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		commandID := strings.TrimPrefix(r.URL.Path, "/meta/commands/")
		commandID = strings.TrimSpace(commandID)
		if commandID == "" || strings.Contains(commandID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "command metadata not found")
			return
		}
		handleMetaCommandByID(w, r, opts, commandID)
	})

	registerRoute("/meta/concepts", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaConcepts(w, r, opts)
	})

	registerRoute("/meta/concepts/", exactRouteAccess(routeAccessAlwaysPublic, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		conceptName := strings.TrimPrefix(r.URL.Path, "/meta/concepts/")
		conceptName = strings.TrimSpace(conceptName)
		if conceptName == "" || strings.Contains(conceptName, "/") {
			writeError(w, http.StatusNotFound, "not_found", "concept metadata not found")
			return
		}
		handleMetaConceptByName(w, r, opts, conceptName)
	})

	registerRoute("/actors", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessDevOnlyLegacyActor, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleRegisterActor(w, r, opts)
		case http.MethodGet:
			handleListActors(w, r, opts.actorRegistry)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	registerRoute("/auth/agents/register", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRegisterAgent(w, r, opts)
	})

	registerRoute("/auth/bootstrap/status", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleBootstrapStatus(w, r, opts)
	})

	registerRoute("/auth/invites", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListInvites(w, r, opts)
		case http.MethodPost:
			handleCreateInvite(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/auth/principals", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListAuthPrincipals(w, r, opts)
	})

	registerRoute("/auth/principals/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/auth/principals/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		if strings.Count(remainder, "/") != 1 || !strings.HasSuffix(remainder, "/revoke") {
			return routeAccessRequirement{}
		}
		agentID := strings.TrimSuffix(remainder, "/revoke")
		agentID = strings.TrimSuffix(agentID, "/")
		if strings.TrimSpace(agentID) == "" || strings.Contains(agentID, "/") {
			return routeAccessRequirement{}
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodPost)(r)
	}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		remainder := strings.TrimPrefix(r.URL.Path, "/auth/principals/")
		agentID := strings.TrimSuffix(strings.TrimSuffix(remainder, "/revoke"), "/")
		if agentID == "" || strings.Contains(agentID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handleRevokePrincipal(w, r, opts, agentID)
	})

	registerRoute("/auth/audit", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListAuthAudit(w, r, opts)
	})

	registerRoute("/auth/invites/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/auth/invites/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		if strings.Count(remainder, "/") != 1 || !strings.HasSuffix(remainder, "/revoke") {
			return routeAccessRequirement{}
		}
		inviteID := strings.TrimSuffix(remainder, "/revoke")
		inviteID = strings.TrimSuffix(inviteID, "/")
		if strings.TrimSpace(inviteID) == "" || strings.Contains(inviteID, "/") {
			return routeAccessRequirement{}
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodPost)(r)
	}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		remainder := strings.TrimPrefix(r.URL.Path, "/auth/invites/")
		inviteID := strings.TrimSuffix(strings.TrimSuffix(remainder, "/revoke"), "/")
		if inviteID == "" || strings.Contains(inviteID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handleRevokeInvite(w, r, opts, inviteID)
	})

	registerRoute("/auth/token", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleIssueAuthToken(w, r, opts)
	})

	registerRoute("/auth/passkey/register/options", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterOptions(w, r, opts)
	})

	registerRoute("/auth/passkey/register/verify", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterVerify(w, r, opts)
	})

	registerRoute("/auth/passkey/login/options", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginOptions(w, r, opts)
	})

	registerRoute("/auth/passkey/login/verify", exactRouteAccess(routeAccessPublicAuthCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginVerify(w, r, opts)
	})

	registerRoute("/agents/me", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodGet, http.MethodPatch), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetCurrentAgent(w, r, opts)
		case http.MethodPatch:
			handlePatchCurrentAgent(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	registerRoute("/agents/me/keys/rotate", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRotateCurrentAgentKey(w, r, opts)
	})

	registerRoute("/agents/me/revoke", exactRouteAccess(routeAccessAuthenticatedPrincipal, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRevokeCurrentAgent(w, r, opts)
	})

	registerRoute("/topics", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateTopic(w, r, opts)
		case http.MethodGet:
			handleListTopics(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	registerRoute("/topics/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/topics/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/timeline"), strings.HasSuffix(remainder, "/workspace"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"), strings.HasSuffix(remainder, "/tombstone"), strings.HasSuffix(remainder, "/restore"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet || r.Method == http.MethodPatch:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/topics/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if strings.HasSuffix(remainder, "/timeline") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/timeline")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetTopicTimeline(w, r, opts, topicID)
			return
		}

		if strings.HasSuffix(remainder, "/workspace") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/workspace")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetTopicWorkspace(w, r, opts, topicID)
			return
		}

		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/archive")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveTopic(w, r, opts, topicID)
			return
		}

		if strings.HasSuffix(remainder, "/unarchive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/unarchive")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleUnarchiveTopic(w, r, opts, topicID)
			return
		}

		if strings.HasSuffix(remainder, "/tombstone") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/tombstone")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTombstoneTopic(w, r, opts, topicID)
			return
		}

		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/restore")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreTopic(w, r, opts, topicID)
			return
		}

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetTopic(w, r, opts, remainder)
		case http.MethodPatch:
			handlePatchTopic(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	registerRoute("/threads", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListThreads(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		}
	})

	registerRoute("/threads/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/threads/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/timeline"), strings.HasSuffix(remainder, "/context"), strings.HasSuffix(remainder, "/workspace"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"), strings.HasSuffix(remainder, "/tombstone"), strings.HasSuffix(remainder, "/restore"), strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/threads/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if strings.HasSuffix(remainder, "/timeline") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}

			threadID := strings.TrimSuffix(remainder, "/timeline")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleThreadTimeline(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/context") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}

			threadID := strings.TrimSuffix(remainder, "/context")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleThreadContext(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/workspace") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}

			threadID := strings.TrimSuffix(remainder, "/workspace")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleThreadWorkspace(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			threadID := strings.TrimSuffix(remainder, "/archive")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveThread(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/unarchive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			threadID := strings.TrimSuffix(remainder, "/unarchive")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleUnarchiveThread(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/tombstone") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			threadID := strings.TrimSuffix(remainder, "/tombstone")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTombstoneThread(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			threadID := strings.TrimSuffix(remainder, "/restore")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreThread(w, r, opts, threadID)
			return
		}

		if strings.HasSuffix(remainder, "/purge") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			threadID := strings.TrimSuffix(remainder, "/purge")
			threadID = strings.TrimSuffix(threadID, "/")
			if threadID == "" || strings.Contains(threadID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handlePurgeThread(w, r, opts, threadID)
			return
		}

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetThread(w, r, opts, remainder)
	})

	registerRoute("/docs", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListDocuments(w, r, opts)
		case http.MethodPost:
			handleCreateDocument(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/docs/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/docs/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/restore"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/unarchive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/tombstone"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/history"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/revisions"):
			if r.Method == http.MethodGet || r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/revisions/"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet || r.Method == http.MethodPatch:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/docs/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if strings.HasSuffix(remainder, "/purge") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/purge")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handlePurgeDocument(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/restore")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreDocument(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/archive")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveDocument(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/unarchive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/unarchive")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleUnarchiveDocument(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/tombstone") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/tombstone")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTombstoneDocument(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/history") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/history")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleListDocumentHistory(w, r, opts, documentID)
			return
		}

		if strings.HasSuffix(remainder, "/revisions") {
			documentID := strings.TrimSuffix(remainder, "/revisions")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleListDocumentHistory(w, r, opts, documentID)
			case http.MethodPost:
				handleCreateDocumentRevision(w, r, opts, documentID)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
			}
			return
		}

		revisionSuffix := "/revisions/"
		if idx := strings.Index(remainder, revisionSuffix); idx > 0 {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			documentID := strings.TrimSpace(remainder[:idx])
			revisionID := strings.TrimSpace(remainder[idx+len(revisionSuffix):])
			if documentID == "" || revisionID == "" || strings.Contains(documentID, "/") || strings.Contains(revisionID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetDocumentRevision(w, r, opts, documentID, revisionID)
			return
		}

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetDocument(w, r, opts, remainder)
		case http.MethodPatch:
			handleUpdateDocument(w, r, opts, remainder, http.StatusOK)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	registerRoute("/boards", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListBoards(w, r, opts)
		case http.MethodPost:
			handleCreateBoard(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/boards/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/boards/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/workspace"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"), strings.HasSuffix(remainder, "/tombstone"), strings.HasSuffix(remainder, "/restore"), strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/cards"):
			if r.Method == http.MethodGet || r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/cards/"):
			cardRemainder := strings.TrimSpace(strings.SplitN(remainder, "/cards/", 2)[1])
			switch {
			case strings.HasSuffix(cardRemainder, "/move"), strings.HasSuffix(cardRemainder, "/remove"), strings.HasSuffix(cardRemainder, "/archive"):
				if r.Method == http.MethodPost {
					return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
				}
				return routeAccessRequirement{}
			case strings.Contains(cardRemainder, "/"):
				return routeAccessRequirement{}
			case r.Method == http.MethodGet || r.Method == http.MethodPatch:
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			default:
				return routeAccessRequirement{}
			}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet || r.Method == http.MethodPatch:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/boards/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if strings.HasSuffix(remainder, "/workspace") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/workspace")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetBoardWorkspace(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/archive")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveBoard(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/unarchive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/unarchive")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleUnarchiveBoard(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/tombstone") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/tombstone")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTombstoneBoard(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/restore")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreBoard(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/purge") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/purge")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handlePurgeBoard(w, r, opts, boardID)
			return
		}

		if strings.HasSuffix(remainder, "/cards") {
			boardID := strings.TrimSuffix(remainder, "/cards")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleListBoardCards(w, r, opts, boardID)
			case http.MethodPost:
				handleAddBoardCard(w, r, opts, boardID)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
			}
			return
		}

		if strings.Contains(remainder, "/cards/") {
			prefix, suffix, found := strings.Cut(remainder, "/cards/")
			if !found {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			boardID := strings.TrimSuffix(strings.TrimSpace(prefix), "/")
			cardRemainder := strings.TrimSpace(suffix)
			if boardID == "" || cardRemainder == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}

			if strings.HasSuffix(cardRemainder, "/move") {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				cardKey := strings.TrimSuffix(cardRemainder, "/move")
				cardKey = strings.TrimSuffix(cardKey, "/")
				if cardKey == "" || strings.Contains(cardKey, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleMoveBoardCard(w, r, opts, boardID, cardKey)
				return
			}

			if strings.HasSuffix(cardRemainder, "/archive") {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				cardID := strings.TrimSuffix(cardRemainder, "/archive")
				cardID = strings.TrimSuffix(cardID, "/")
				if cardID == "" || strings.Contains(cardID, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleArchiveBoardCard(w, r, opts, boardID, cardID)
				return
			}

			if strings.HasSuffix(cardRemainder, "/remove") {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				identifier := strings.TrimSuffix(cardRemainder, "/remove")
				identifier = strings.TrimSuffix(identifier, "/")
				if identifier == "" || strings.Contains(identifier, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleRemoveBoardCard(w, r, opts, boardID, identifier)
				return
			}

			if strings.Contains(cardRemainder, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleGetBoardCard(w, r, opts, boardID, cardRemainder)
			case http.MethodPatch:
				handleUpdateBoardCard(w, r, opts, boardID, cardRemainder)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
			}
			return
		}

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetBoard(w, r, opts, remainder)
		case http.MethodPatch:
			handleUpdateBoard(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	registerRoute("/events", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListEvents(w, r, opts)
		case http.MethodPost:
			handleAppendEvent(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/cards", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListCards(w, r, opts)
	})

	registerRoute("/cards/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/cards/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/archive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/restore"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/move"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet || r.Method == http.MethodPatch:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/cards/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/archive")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveCard(w, r, opts, cardID)
			return
		}
		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/restore")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreArchivedCard(w, r, opts, cardID)
			return
		}
		if strings.HasSuffix(remainder, "/purge") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/purge")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handlePurgeArchivedCard(w, r, opts, cardID)
			return
		}
		if strings.HasSuffix(remainder, "/move") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/move")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleMoveCard(w, r, opts, cardID)
			return
		}
		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			handleGetCard(w, r, opts, remainder)
		case http.MethodPatch:
			handlePatchCard(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	registerRoute("/events/stream", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleEventsStream(w, r, opts)
	})

	registerRoute("/events/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/events/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"),
			strings.HasSuffix(remainder, "/tombstone"), strings.HasSuffix(remainder, "/restore"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		default:
			if !strings.Contains(remainder, "/") && r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/events/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		for _, suffix := range []string{"/archive", "/unarchive", "/tombstone", "/restore"} {
			if strings.HasSuffix(remainder, suffix) {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				eventID := strings.TrimSuffix(remainder, suffix)
				eventID = strings.TrimSuffix(eventID, "/")
				if eventID == "" || strings.Contains(eventID, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				switch suffix {
				case "/archive":
					handleArchiveEvent(w, r, opts, eventID)
				case "/unarchive":
					handleUnarchiveEvent(w, r, opts, eventID)
				case "/tombstone":
					handleTombstoneEvent(w, r, opts, eventID)
				case "/restore":
					handleRestoreEvent(w, r, opts, eventID)
				}
				return
			}
		}

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handleGetEvent(w, r, opts, remainder)
	})

	registerRoute("/artifacts", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateArtifact(w, r, opts)
		case http.MethodGet:
			handleListArtifacts(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	registerRoute("/artifacts/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/restore"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/tombstone"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/unarchive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/content"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		if strings.HasSuffix(remainder, "/restore") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/restore")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRestoreArtifact(w, r, opts, artifactID)
			return
		}

		if strings.HasSuffix(remainder, "/purge") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/purge")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handlePurgeArtifact(w, r, opts, artifactID)
			return
		}

		if strings.HasSuffix(remainder, "/tombstone") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/tombstone")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTombstoneArtifact(w, r, opts, artifactID)
			return
		}

		if strings.HasSuffix(remainder, "/archive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/archive")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleArchiveArtifact(w, r, opts, artifactID)
			return
		}

		if strings.HasSuffix(remainder, "/unarchive") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/unarchive")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleUnarchiveArtifact(w, r, opts, artifactID)
			return
		}

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		if strings.HasSuffix(remainder, "/content") {
			artifactID := strings.TrimSuffix(remainder, "/content")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetArtifactContent(w, r, opts, artifactID)
			return
		}

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		handleGetArtifact(w, r, opts, remainder)
	})

	registerRoute("/work_orders", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateWorkOrder(w, r, opts)
	})

	registerRoute("/receipts", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReceipt(w, r, opts)
	})

	registerRoute("/reviews", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReview(w, r, opts)
	})

	registerRoute("/packets/work-orders", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateWorkOrder(w, r, opts)
	})

	registerRoute("/packets/receipts", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReceipt(w, r, opts)
	})

	registerRoute("/packets/reviews", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReview(w, r, opts)
	})

	registerRoute("/inbox", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetInbox(w, r, opts)
	})

	registerRoute("/inbox/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/inbox/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		if r.Method == http.MethodPost && strings.HasSuffix(remainder, "/acknowledge") {
			prefix := strings.TrimSuffix(remainder, "/acknowledge")
			prefix = strings.TrimSuffix(prefix, "/")
			if prefix == "" || strings.Contains(prefix, "/") {
				return routeAccessRequirement{}
			}
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		if r.Method == http.MethodGet && !strings.Contains(remainder, "/") {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		return routeAccessRequirement{}
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/inbox/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(remainder, "/acknowledge") {
			inboxID := strings.TrimSuffix(remainder, "/acknowledge")
			inboxID = strings.TrimSuffix(inboxID, "/")
			if inboxID == "" || strings.Contains(inboxID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleAckInboxItem(w, r, opts, inboxID)
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		inboxItemID := remainder
		if strings.Contains(inboxItemID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handleGetInboxItem(w, r, opts, inboxItemID)
	})

	registerRoute("/inbox/stream", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleInboxStream(w, r, opts)
	})

	registerRoute("/inbox/ack", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleAckInboxItem(w, r, opts, "")
	})

	registerRoute("/agent-notifications", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListAgentNotifications(w, r, opts)
	})

	registerRoute("/agent-notifications/read", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleReadAgentNotification(w, r, opts)
	})

	registerRoute("/agent-notifications/dismiss", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleDismissAgentNotification(w, r, opts)
	})

	registerRoute("/derived/rebuild", exactRouteAccess(routeAccessWorkspaceBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRebuildDerived(w, r, opts)
	})

	registerRoute("/", exactRouteAccess(routeAccessAlwaysPublic), func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	})

	corsOriginSet := make(map[string]bool, len(opts.corsAllowedOrigins))
	for _, o := range opts.corsAllowedOrigins {
		corsOriginSet[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-XSS-Protection", "0")

		if len(corsOriginSet) > 0 {
			origin := r.Header.Get("Origin")
			if corsOriginSet["*"] {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if origin != "" && corsOriginSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-OAR-CLI-Version")
			w.Header().Set("Access-Control-Expose-Headers", "X-OAR-Core-Version, X-OAR-API-Version, X-OAR-Schema-Version, X-OAR-Min-CLI-Version, X-OAR-Recommended-CLI-Version")
			w.Header().Set("Access-Control-Max-Age", "3600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		setVersionHeaders(w, opts, schemaVersion)
		if shouldEnforceCLIVersion(r.URL.Path) {
			if clientVersion := strings.TrimSpace(r.Header.Get("X-OAR-CLI-Version")); clientVersion != "" {
				outdated, compareErr := isCLIVersionOutdated(clientVersion, opts.minCLIVersion)
				if compareErr == nil && outdated {
					writeCLIOutdated(w, opts)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func handleRegisterActor(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return
	}

	if !opts.enableDevActorMode {
		writeError(w, http.StatusForbidden, "dev_actor_mode_disabled", "actor creation is disabled outside development mode")
		return
	}

	var req struct {
		Actor struct {
			ID          string   `json:"id"`
			DisplayName string   `json:"display_name"`
			Tags        []string `json:"tags"`
			CreatedAt   string   `json:"created_at"`
		} `json:"actor"`
	}

	if !decodeJSONBody(w, r, &req) {
		return
	}

	req.Actor.ID = strings.TrimSpace(req.Actor.ID)
	req.Actor.DisplayName = strings.TrimSpace(req.Actor.DisplayName)
	req.Actor.CreatedAt = strings.TrimSpace(req.Actor.CreatedAt)

	if req.Actor.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.id is required")
		return
	}
	if req.Actor.DisplayName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.display_name is required")
		return
	}
	if req.Actor.CreatedAt == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.created_at is required")
		return
	}
	if _, err := time.Parse(time.RFC3339, req.Actor.CreatedAt); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "actor.created_at must be an RFC3339 timestamp")
		return
	}

	registered, err := opts.actorRegistry.Register(r.Context(), actors.Actor{
		ID:          req.Actor.ID,
		DisplayName: req.Actor.DisplayName,
		Tags:        req.Actor.Tags,
		CreatedAt:   req.Actor.CreatedAt,
	})
	if err != nil {
		if errors.Is(err, actors.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, "actor_exists", "actor with this id already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to register actor")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"actor": registered})
}

func handleListActors(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry) {
	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	listed, nextCursor, err := actorRegistry.List(r.Context(), actors.ActorListFilter{
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:  limitFilter,
		Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, actors.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list actors")
		return
	}

	response := map[string]any{"actors": listed}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": errorPayload(code, message),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode response","recoverable":false,"hint":"Retry once; if it persists, escalate with logs and request context."}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func setVersionHeaders(w http.ResponseWriter, opts handlerOptions, schemaVersion string) {
	w.Header().Set("X-OAR-Core-Version", strings.TrimSpace(opts.coreVersion))
	w.Header().Set("X-OAR-API-Version", strings.TrimSpace(opts.apiVersion))
	w.Header().Set("X-OAR-Schema-Version", strings.TrimSpace(schemaVersion))
	if strings.TrimSpace(opts.minCLIVersion) != "" {
		w.Header().Set("X-OAR-Min-CLI-Version", strings.TrimSpace(opts.minCLIVersion))
	}
	if strings.TrimSpace(opts.recommendedCLIVersion) != "" {
		w.Header().Set("X-OAR-Recommended-CLI-Version", strings.TrimSpace(opts.recommendedCLIVersion))
	}
}

func writeCLIOutdated(w http.ResponseWriter, opts handlerOptions) {
	payload := map[string]any{
		"error": errorPayload("cli_outdated", "CLI version is below the minimum compatible version for this core instance"),
		"upgrade": map[string]any{
			"min_cli_version":         strings.TrimSpace(opts.minCLIVersion),
			"recommended_cli_version": strings.TrimSpace(opts.recommendedCLIVersion),
			"cli_download_url":        strings.TrimSpace(opts.cliDownloadURL),
		},
	}
	writeJSON(w, http.StatusUpgradeRequired, payload)
}

func shouldEnforceCLIVersion(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch path {
	case "/health", "/livez", "/readyz", "/ops/health", "/ops/usage-summary", "/ops/blob-usage/rebuild", "/version", "/meta/handshake", "/auth/token", "/auth/agents/register", "/auth/bootstrap/status":
		return false
	}
	if strings.HasPrefix(path, "/auth/passkey/") {
		return false
	}
	return true
}

func isCLIVersionOutdated(clientVersion string, minVersion string) (bool, error) {
	clientParts, err := parseSemanticVersion(clientVersion)
	if err != nil {
		return false, err
	}
	minParts, err := parseSemanticVersion(minVersion)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if clientParts[i] < minParts[i] {
			return true, nil
		}
		if clientParts[i] > minParts[i] {
			return false, nil
		}
	}
	return false, nil
}

func parseSemanticVersion(raw string) ([3]int, error) {
	var out [3]int
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	if raw == "" {
		return out, fmt.Errorf("empty version")
	}
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, fmt.Errorf("invalid semantic version: %s", raw)
	}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			out[i] = 0
			continue
		}
		segment := strings.TrimSpace(parts[i])
		if segment == "" {
			return out, fmt.Errorf("invalid semantic version segment")
		}
		value, err := strconv.Atoi(segment)
		if err != nil {
			return out, err
		}
		if value < 0 {
			return out, fmt.Errorf("invalid negative segment")
		}
		out[i] = value
	}
	return out, nil
}

func defaultMetaCommandsPathCandidates() []string {
	return []string{
		"../contracts/gen/meta/commands.json",
		filepath.Join("..", "..", "..", "contracts", "gen", "meta", "commands.json"),
		filepath.Join("contracts", "gen", "meta", "commands.json"),
	}
}
