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

type ActorRegistry interface {
	Register(ctx context.Context, actor actors.Actor) (actors.Actor, error)
	List(ctx context.Context) ([]actors.Actor, error)
	Exists(ctx context.Context, actorID string) (bool, error)
}

type PrimitiveStore interface {
	AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error)
	GetEvent(ctx context.Context, id string) (map[string]any, error)
	CreateArtifact(ctx context.Context, actorID string, artifact map[string]any, content any, contentType string) (map[string]any, error)
	GetArtifact(ctx context.Context, id string) (map[string]any, error)
	GetArtifactContent(ctx context.Context, id string) ([]byte, string, error)
	ListArtifacts(ctx context.Context, filter primitives.ArtifactListFilter) ([]map[string]any, error)
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
	ListEvents(ctx context.Context, filter primitives.EventListFilter) ([]map[string]any, error)
}

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	healthCheck           HealthCheckFunc
	actorRegistry         ActorRegistry
	authStore             *auth.Store
	primitiveStore        PrimitiveStore
	contract              *schema.Contract
	inboxRiskHorizon      time.Duration
	coreVersion           string
	apiVersion            string
	minCLIVersion         string
	recommendedCLIVersion string
	cliDownloadURL        string
	coreInstanceID        string
	metaCommandsPath      string
	streamPollInterval    time.Duration
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

func NewHandler(schemaVersion string, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		coreVersion:           strings.TrimSpace(schemaVersion),
		apiVersion:            "v0",
		minCLIVersion:         "0.1.0",
		recommendedCLIVersion: "0.1.0",
		coreInstanceID:        "core-local",
		streamPollInterval:    time.Second,
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

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		if opts.healthCheck != nil {
			if err := opts.healthCheck(r.Context()); err != nil {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"ok": false,
					"error": map[string]string{
						"code":    "storage_unavailable",
						"message": "storage health check failed",
					},
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
		writeJSON(w, http.StatusOK, map[string]any{"schema_version": schemaVersion})
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
			handleRegisterActor(w, r, opts.actorRegistry)
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
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}

		remainder := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func handleRegisterActor(w http.ResponseWriter, r *http.Request, actorRegistry ActorRegistry) {
	if actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
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

	registered, err := actorRegistry.Register(r.Context(), actors.Actor{
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
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode response"}}`))
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
		"error": map[string]any{
			"code":    "cli_outdated",
			"message": "CLI version is below the minimum compatible version for this core instance",
		},
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
