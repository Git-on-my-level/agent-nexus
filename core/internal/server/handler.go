package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

type HealthCheckFunc func(ctx context.Context) error

type ProjectionMaintenance interface {
	Start()
	Notify(ctx context.Context) error
	Stop(ctx context.Context) error
}

type ActorRegistry interface {
	Register(ctx context.Context, actor actors.Actor) (actors.Actor, error)
	List(ctx context.Context) ([]actors.Actor, error)
	Exists(ctx context.Context, actorID string) (bool, error)
}

type PrimitiveStore interface {
	AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error)
	GetEvent(ctx context.Context, id string) (map[string]any, error)
	CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error)
	CreateArtifactAndEvent(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string, event map[string]any) (map[string]any, map[string]any, error)
	GetArtifact(ctx context.Context, id string) (map[string]any, error)
	GetArtifactContent(ctx context.Context, id string) ([]byte, string, error)
	ListArtifacts(ctx context.Context, filter primitives.ArtifactListFilter) ([]map[string]any, error)
	GetIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string) (primitives.IdempotencyReplay, error)
	PutIdempotencyReplay(ctx context.Context, scope string, actorID string, requestKey string, requestHash string, status int, response map[string]any) error
	ListDerivedInboxItems(ctx context.Context, filter primitives.DerivedInboxListFilter) ([]primitives.DerivedInboxItem, error)
	GetDerivedInboxItem(ctx context.Context, id string) (primitives.DerivedInboxItem, error)
	ReplaceDerivedInboxItems(ctx context.Context, threadID string, items []primitives.DerivedInboxItem) error
	GetDerivedThreadProjection(ctx context.Context, threadID string) (primitives.DerivedThreadProjection, error)
	ListDerivedThreadProjections(ctx context.Context, threadIDs []string) (map[string]primitives.DerivedThreadProjection, error)
	PutDerivedThreadProjection(ctx context.Context, projection primitives.DerivedThreadProjection) error
	MarkThreadProjectionsDirty(ctx context.Context, threadIDs []string, queuedAt time.Time) error
	GetThreadProjectionRefreshStatuses(ctx context.Context, threadIDs []string) (map[string]primitives.ThreadProjectionRefreshStatus, error)
	ClaimNextDirtyThreadProjection(ctx context.Context, startedAt time.Time) (primitives.ThreadProjectionRefreshStatus, bool, error)
	MarkThreadProjectionRefreshSucceeded(ctx context.Context, threadID string, completedAt time.Time) error
	MarkThreadProjectionRefreshFailed(ctx context.Context, threadID string, failedAt time.Time, message string) error
	ListDocuments(ctx context.Context, filter primitives.DocumentListFilter) ([]map[string]any, error)
	CreateDocument(ctx context.Context, actorID string, document map[string]any, content any, contentType string, refs []string) (map[string]any, map[string]any, error)
	GetDocument(ctx context.Context, documentID string) (map[string]any, map[string]any, error)
	UpdateDocument(ctx context.Context, actorID string, documentID string, documentPatch map[string]any, ifBaseRevision string, content any, contentType string, refs []string) (map[string]any, map[string]any, error)
	ListDocumentHistory(ctx context.Context, documentID string) ([]map[string]any, error)
	GetDocumentRevision(ctx context.Context, documentID string, revisionID string) (map[string]any, error)
	GetDocumentRevisionByID(ctx context.Context, revisionID string) (map[string]any, error)
	ListBoards(ctx context.Context, filter primitives.BoardListFilter) ([]primitives.BoardListItem, error)
	CreateBoard(ctx context.Context, actorID string, board map[string]any) (map[string]any, error)
	GetBoard(ctx context.Context, boardID string) (map[string]any, error)
	UpdateBoard(ctx context.Context, actorID string, boardID string, patch map[string]any, ifUpdatedAt *string) (map[string]any, error)
	ListBoardCards(ctx context.Context, boardID string) ([]map[string]any, error)
	AddBoardCard(ctx context.Context, actorID string, boardID string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error)
	UpdateBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.UpdateBoardCardInput) (primitives.BoardCardMutationResult, error)
	MoveBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error)
	RemoveBoardCard(ctx context.Context, actorID string, boardID string, threadID string, input primitives.RemoveBoardCardInput) (primitives.BoardCardRemovalResult, error)
	ListBoardMembershipsByThread(ctx context.Context, threadID string) ([]primitives.BoardMembership, error)
	GetSnapshot(ctx context.Context, id string) (map[string]any, error)
	CreateThread(ctx context.Context, actorID string, thread map[string]any) (primitives.PatchSnapshotResult, error)
	GetThread(ctx context.Context, id string) (map[string]any, error)
	PatchThread(ctx context.Context, actorID string, id string, patch map[string]any, ifUpdatedAt *string) (primitives.PatchSnapshotResult, error)
	ListThreads(ctx context.Context, filter primitives.ThreadListFilter) ([]map[string]any, error)
	CreateCommitment(ctx context.Context, actorID string, commitment map[string]any) (primitives.PatchSnapshotResult, error)
	GetCommitment(ctx context.Context, id string) (map[string]any, error)
	PatchCommitment(ctx context.Context, actorID string, id string, patch map[string]any, refs []string, ifUpdatedAt *string) (primitives.PatchSnapshotResult, error)
	ListCommitments(ctx context.Context, filter primitives.CommitmentListFilter) ([]map[string]any, error)
	ListEventsByThread(ctx context.Context, threadID string) ([]map[string]any, error)
	ListRecentEventsByThread(ctx context.Context, threadID string, limit int) ([]map[string]any, error)
	ListEvents(ctx context.Context, filter primitives.EventListFilter) ([]map[string]any, error)
	TombstoneArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error)
	TombstoneDocument(ctx context.Context, actorID string, documentID string, reason string) (map[string]any, map[string]any, error)
}

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	healthCheck                HealthCheckFunc
	actorRegistry              ActorRegistry
	authStore                  *auth.Store
	passkeySessionStore        *auth.PasskeySessionStore
	primitiveStore             PrimitiveStore
	contract                   *schema.Contract
	webAuthnConfig             WebAuthnConfig
	allowUnauthenticatedWrites bool
	enableDevActorMode         bool
	inboxRiskHorizon           time.Duration
	projectionMaintenance      ProjectionMaintenance
	coreVersion                string
	apiVersion                 string
	minCLIVersion              string
	recommendedCLIVersion      string
	cliDownloadURL             string
	coreInstanceID             string
	metaCommandsPath           string
	streamPollInterval         time.Duration
	corsAllowedOrigins         []string
}

func WithHealthCheck(healthCheck HealthCheckFunc) HandlerOption {
	return func(opts *handlerOptions) {
		opts.healthCheck = healthCheck
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

func WithAllowUnauthenticatedWrites(allow bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.allowUnauthenticatedWrites = allow
	}
}

func WithEnableDevActorMode(enable bool) HandlerOption {
	return func(opts *handlerOptions) {
		opts.enableDevActorMode = enable
	}
}

func WithInboxRiskHorizon(horizon time.Duration) HandlerOption {
	return func(opts *handlerOptions) {
		opts.inboxRiskHorizon = horizon
	}
}

func WithProjectionMaintenance(maintenance ProjectionMaintenance) HandlerOption {
	return func(opts *handlerOptions) {
		opts.projectionMaintenance = maintenance
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

func NewHandler(schemaVersion string, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		coreVersion:                strings.TrimSpace(schemaVersion),
		apiVersion:                 "v0",
		minCLIVersion:              "0.1.0",
		recommendedCLIVersion:      "0.1.0",
		coreInstanceID:             "core-local",
		streamPollInterval:         time.Second,
		allowUnauthenticatedWrites: false,
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
	if opts.projectionMaintenance != nil {
		opts.projectionMaintenance.Start()
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		if opts.healthCheck != nil {
			if err := opts.healthCheck(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"ok":    false,
					"error": errorPayload("storage_unavailable", "storage health check failed"),
				})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/meta/handshake", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaHandshake(w, r, opts, schemaVersion)
	})

	mux.HandleFunc("/meta/commands", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaCommands(w, r, opts)
	})

	mux.HandleFunc("/meta/commands/", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/meta/concepts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaConcepts(w, r, opts)
	})

	mux.HandleFunc("/meta/concepts/", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/actors", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleRegisterActor(w, r, opts)
		case http.MethodGet:
			handleListActors(w, r, opts.actorRegistry)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	mux.HandleFunc("/auth/agents/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRegisterAgent(w, r, opts)
	})

	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleIssueAuthToken(w, r, opts)
	})

	mux.HandleFunc("/auth/passkey/register/options", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterOptions(w, r, opts)
	})

	mux.HandleFunc("/auth/passkey/register/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterVerify(w, r, opts)
	})

	mux.HandleFunc("/auth/passkey/login/options", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginOptions(w, r, opts)
	})

	mux.HandleFunc("/auth/passkey/login/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginVerify(w, r, opts)
	})

	mux.HandleFunc("/agents/me", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetCurrentAgent(w, r, opts)
		case http.MethodPatch:
			handlePatchCurrentAgent(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	mux.HandleFunc("/agents/me/keys/rotate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRotateCurrentAgentKey(w, r, opts)
	})

	mux.HandleFunc("/agents/me/revoke", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRevokeCurrentAgent(w, r, opts)
	})

	mux.HandleFunc("/threads", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateThread(w, r, opts)
		case http.MethodGet:
			handleListThreads(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	mux.HandleFunc("/threads/", func(w http.ResponseWriter, r *http.Request) {
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

		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetThread(w, r, opts, remainder)
		case http.MethodPatch:
			handlePatchThread(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	mux.HandleFunc("/commitments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateCommitment(w, r, opts)
		case http.MethodGet:
			handleListCommitments(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	mux.HandleFunc("/commitments/", func(w http.ResponseWriter, r *http.Request) {
		commitmentID := strings.TrimPrefix(r.URL.Path, "/commitments/")
		if commitmentID == "" || strings.Contains(commitmentID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			handleGetCommitment(w, r, opts, commitmentID)
		case http.MethodPatch:
			handlePatchCommitment(w, r, opts, commitmentID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListDocuments(w, r, opts)
		case http.MethodPost:
			handleCreateDocument(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	mux.HandleFunc("/docs/", func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/docs/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
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
			handleUpdateDocument(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PATCH are supported")
		}
	})

	mux.HandleFunc("/boards", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListBoards(w, r, opts)
		case http.MethodPost:
			handleCreateBoard(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	mux.HandleFunc("/boards/", func(w http.ResponseWriter, r *http.Request) {
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
				threadID := strings.TrimSuffix(cardRemainder, "/move")
				threadID = strings.TrimSuffix(threadID, "/")
				if threadID == "" || strings.Contains(threadID, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleMoveBoardCard(w, r, opts, boardID, threadID)
				return
			}

			if strings.HasSuffix(cardRemainder, "/remove") {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				threadID := strings.TrimSuffix(cardRemainder, "/remove")
				threadID = strings.TrimSuffix(threadID, "/")
				if threadID == "" || strings.Contains(threadID, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleRemoveBoardCard(w, r, opts, boardID, threadID)
				return
			}

			if strings.Contains(cardRemainder, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodPatch {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only PATCH is supported")
				return
			}
			handleUpdateBoardCard(w, r, opts, boardID, cardRemainder)
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

	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}

		handleAppendEvent(w, r, opts)
	})

	mux.HandleFunc("/events/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleEventsStream(w, r, opts)
	})

	mux.HandleFunc("/events/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		eventID := strings.TrimPrefix(r.URL.Path, "/events/")
		if eventID == "" || strings.Contains(eventID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		handleGetEvent(w, r, opts, eventID)
	})

	mux.HandleFunc("/artifacts", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleCreateArtifact(w, r, opts)
		case http.MethodGet:
			handleListArtifacts(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST and GET are supported")
		}
	})

	mux.HandleFunc("/artifacts/", func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
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

	mux.HandleFunc("/work_orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateWorkOrder(w, r, opts)
	})

	mux.HandleFunc("/receipts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReceipt(w, r, opts)
	})

	mux.HandleFunc("/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateReview(w, r, opts)
	})

	mux.HandleFunc("/inbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetInbox(w, r, opts)
	})

	mux.HandleFunc("/inbox/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		inboxItemID := strings.TrimPrefix(r.URL.Path, "/inbox/")
		if inboxItemID == "" || strings.Contains(inboxItemID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handleGetInboxItem(w, r, opts, inboxItemID)
	})

	mux.HandleFunc("/inbox/stream", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleInboxStream(w, r, opts)
	})

	mux.HandleFunc("/inbox/ack", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleAckInboxItem(w, r, opts)
	})

	mux.HandleFunc("/derived/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRebuildDerived(w, r, opts)
	})

	mux.HandleFunc("/snapshots/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		snapshotID := strings.TrimPrefix(r.URL.Path, "/snapshots/")
		if snapshotID == "" || strings.Contains(snapshotID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}

		handleGetSnapshot(w, r, opts, snapshotID)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
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

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	listed, err := actorRegistry.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list actors")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"actors": listed})
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
	case "/health", "/version", "/meta/handshake", "/auth/token", "/auth/agents/register":
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
