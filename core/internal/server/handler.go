// HTTP handler construction. NewHandler wires the full route table and
// middleware chain in one function; individual route handlers live in the
// neighbouring *_handlers.go files.

package server

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/buildinfo"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/server/stream"
)

func NewHandler(schemaVersion string, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		reportPanels:                  &reportPanelCache{},
		coreVersion:                   strings.TrimSpace(schemaVersion),
		apiVersion:                    "v0",
		minCLIVersion:                 buildinfo.MinCompatibleCLI,
		recommendedCLIVersion:         buildinfo.MinCompatibleCLI,
		coreInstanceID:                "core-local",
		streamPollInterval:            time.Second,
		answerWakeFlushWhenNoOpenAsks: true,
		answerWakeQuietWindow:         DefaultAnswerWakeQuietWindow,
		allowUnauthenticatedWrites:    false,
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
		opts.minCLIVersion = buildinfo.MinCompatibleCLI
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
	if strings.TrimSpace(opts.workspaceAccessMode) == "" {
		opts.workspaceAccessMode = WorkspaceAccessModeReadWrite
	}
	opts.requestBodyLimits = opts.requestBodyLimits.normalize()
	opts.routeRateLimits = opts.routeRateLimits.normalize()
	opts.rateLimiter = newRouteRateLimiter(opts.routeRateLimits)
	opts.agentChanges = newAgentChangeHub()
	opts.streamReads = &streamReadHub{}
	if (opts.workspaceHumanGrantVerifier != nil || opts.workspaceManagedGrantVerifier != nil) && opts.workspaceHumanGrantRateLimiter == nil {
		opts.workspaceHumanGrantRateLimiter = newRouteRateLimiter(RouteRateLimits{
			AuthRequestsPerMinute:  10,
			AuthBurst:              10,
			WriteRequestsPerMinute: 1,
			WriteBurst:             1,
		})
	}
	if (opts.workspaceHumanGrantVerifier != nil || opts.workspaceManagedGrantVerifier != nil) && opts.workspaceGrantSubjectRateLimiter == nil {
		opts.workspaceGrantSubjectRateLimiter = newWorkspaceGrantPrincipalRateLimiter(RouteRateLimits{
			AuthRequestsPerMinute:  defaultWorkspaceGrantSubjectRequestsPerMinute,
			AuthBurst:              defaultWorkspaceGrantSubjectBurst,
			WriteRequestsPerMinute: 1,
			WriteBurst:             1,
		})
	}

	mux := http.NewServeMux()
	admission := newReadAdmission()
	opts.readAdmission = admission
	registerRoute := func(pattern string, classify routeAccessClassifier, handler http.HandlerFunc) {
		if opts.routeObserver != nil {
			opts.routeObserver(pattern, classify)
		}
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			w = &faultResponseWriter{ResponseWriter: w, r: r, started: started}
			timed := r.Method == http.MethodGet && (r.URL.Path == "/overview" || r.URL.Path == "/inbox" || r.URL.Path == "/inbox/summary")
			if timed {
				w = &serverTimingWriter{ResponseWriter: w, started: started}
			}
			requirement := enrichRouteMutationPolicy(r, classify(r))
			if !enforceRouteAccess(w, r, opts, requirement) {
				return
			}
			_, authenticated := cachedAuthenticatedPrincipal(r)
			if requirement.supported && requirement.bucket != routeAccessAlwaysPublic && (requirement.bucket != routeAccessPublicAuthCeremony || authenticated) {
				attachResourceAccessScope(r, opts)
				if !authorizeResourceSelectors(w, r) {
					return
				}
			}
			if !enforceWorkspaceWriteAccess(w, opts, requirement) {
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
			// Unsupported routes are rejected by their handlers. Do not create a
			// provisional run before that rejection (or bypass read-only policy).
			attributed := requirement.supported
			if p, ok := cachedAuthenticatedPrincipal(r); ok && p.SeriesAdapter != "" {
				attributed = false
			}
			if attributed && !attachRunAttribution(w, r, opts) {
				return
			}
			if timed {
				addServerTiming(w, "auth", started)
			}
			path := r.URL.Path
			if hotProjectionRead(r) {
				release, err := admission.read(r.Context())
				if err != nil {
					writeError(w, 503, "unavailable", "request was cancelled while waiting for resource data")
					return
				}
				defer release()
			}

			// These routes are finite read-only projections. Admit their complete
			// reads to one immutable, authorized SQLite snapshot. Mutations and
			// stream lifetimes must never borrow it.
			if r.Method == http.MethodGet && (path == "/boards" || r.URL.Query().Get("summary") != "1" && (path == "/cards" || strings.HasPrefix(path, "/boards/") && strings.HasSuffix(path, "/cards"))) {
				r = r.WithContext(primitives.WithInventoryAdmission(r.Context()))
			}
			readProjection := r.Method == http.MethodGet && (path == "/events" || path == "/home/unread" || strings.HasPrefix(path, "/topics/") && strings.HasSuffix(path, "/workspace"))
			cardSummaryPage := r.URL.Query().Get("summary") == "1" && (path == "/cards" || strings.HasPrefix(path, "/boards/") && strings.HasSuffix(path, "/cards"))
			readProjection = readProjection || r.Method == http.MethodGet && (path == "/work" || cardSummaryPage || path == "/cards" || path == "/boards" || strings.HasPrefix(path, "/boards/") && strings.HasSuffix(path, "/cards"))
			if canonical, ok := opts.primitiveStore.(*primitives.Store); ok && readProjection {
				ctx, closeRead, err := canonical.BeginResourceRead(r.Context())
				if err != nil {
					writeError(w, http.StatusServiceUnavailable, "unavailable", "resource data is unavailable")
					return
				}
				defer closeRead()
				r = r.WithContext(ctx)
			}
			if fault := faultFrom(w); fault != nil {
				fault.r = r
			}
			handler(w, r)
		})
	}
	registerStreamRoute := func(sub string, classify routeAccessClassifier, handler http.HandlerFunc) {
		if opts.routeObserver != nil {
			opts.routeObserver(stream.Prefix+sub, classify)
		}
		stream.Mount(mux, sub, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w = &faultResponseWriter{ResponseWriter: w, r: r, started: time.Now()}
			requirement := enrichRouteMutationPolicy(r, classify(r))
			if !enforceRouteAccess(w, r, opts, requirement) {
				return
			}
			attachResourceAccessScope(r, opts)
			if !authorizeResourceSelectors(w, r) {
				return
			}
			if !enforceWorkspaceWriteAccess(w, opts, requirement) {
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
			if fault := faultFrom(w); fault != nil {
				fault.r = r
			}
			handler(w, r)
		}))
	}

	mux.HandleFunc(stream.Prefix, func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "stream_not_found", "stream endpoint not found")
	})

	registerRoute("/pm/ingress/telegram", exactRouteAccess(routeAccessAlwaysPublic, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if opts.pmRuntime == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "PM channel ingress is not configured")
			return
		}
		opts.pmRuntime.Ingress.Telegram(w, r)
	})
	registerRoute("/pm/ingress/discord", exactRouteAccess(routeAccessAlwaysPublic, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if opts.pmRuntime == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "PM channel ingress is not configured")
			return
		}
		opts.pmRuntime.Ingress.Discord(w, r)
	})
	registerRoute("/pm/", pmRouteAccess, func(w http.ResponseWriter, r *http.Request) {
		if opts.pmHandler == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "PM service is not configured")
			return
		}
		opts.pmHandler.ServeHTTP(w, r)
	})

	registerRoute("/sessions", sessionsRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleSessions(w, r, opts) })
	registerRoute("/sessions/", sessionsRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleSessions(w, r, opts) })

	registerRoute("/work", workRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleWork(w, r, opts) })
	registerRoute("/work/", workRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleWork(w, r, opts) })

	registerRoute("/health", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/livez", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/readyz", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		if !checkReadiness(w, r, opts) {
			return
		}
		writeLivenessOK(w)
	})

	registerRoute("/ops/health", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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
		if readinessPayload := readinessErrorPayload(r, opts, true); readinessPayload != nil {
			for key, value := range readinessPayload {
				payload[key] = value
			}
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	})

	registerRoute("/ops/usage-summary", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetUsageSummary(w, r, opts)
	})

	registerRoute("/v1/usage/summary", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetUsageV1Summary(w, r, opts)
	})

	registerRoute("/ops/blob-usage/rebuild", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationInternalMaintenance, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRebuildBlobUsageLedger(w, r, opts)
	})

	registerRoute("/version", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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

	registerRoute("/meta/handshake", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaHandshake(w, r, opts, schemaVersion)
	})

	registerRoute("/meta/commands", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaCommands(w, r, opts)
	})

	registerRoute("/meta/commands/", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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

	registerRoute("/meta/concepts", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleMetaConcepts(w, r, opts)
	})

	registerRoute("/meta/concepts/", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationNone, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessDevOnlyLegacyActor, mutation: routeMutationBusiness, supported: true}
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

	registerRoute("/auth/hosts/", hostRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleHostAuthRoutes(w, r, opts) })
	registerRoute("/auth/hosts/enrollments", hostRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleHostAuthRoutes(w, r, opts) })
	registerRoute("/auth/hosts/enrollment-tokens", hostRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleHostAuthRoutes(w, r, opts) })

	registerRoute("/auth/bootstrap/status", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationAuthCeremony, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleBootstrapStatus(w, r, opts)
	})

	registerRoute("/auth/invites", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationPrincipalGrowth, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListInvites(w, r, opts)
		case http.MethodPost:
			handleCreateInvite(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/auth/admins", authAdminRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAuthAdminRoutes(w, r, opts) })
	registerRoute("/auth/access-requests", accessRequestRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAccessRequestRoutes(w, r, opts) })
	registerRoute("/auth/access-requests/", accessRequestRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAccessRequestRoutes(w, r, opts) })
	registerRoute("/auth/access/summary", accessRequestRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAccessRequestRoutes(w, r, opts) })
	registerRoute("/auth/admins/", authAdminRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAuthAdminRoutes(w, r, opts) })

	registerRoute("/auth/principals", exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationAuthCeremony, http.MethodPost)(r)
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

	registerRoute("/auth/audit", exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationAuthCeremony, http.MethodPost)(r)
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

	for _, path := range []string{"/deliveries", "/deliveries/", "/notification-settings"} {
		registerRoute(path, deliveryRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleDeliveryRoutes(w, r, opts) })
	}
	for _, path := range []string{"/adapters", "/adapters/", "/series", "/series/"} {
		registerRoute(path, seriesRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleSeriesRoutes(w, r, opts) })
	}
	registerRoute("/auth/token", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationAuthAccessCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleIssueAuthToken(w, r, opts)
	})

	registerRoute("/auth/passkey/register/options", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationPrincipalGrowth, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterOptions(w, r, opts)
	})

	registerRoute("/auth/passkey/register/verify", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationPrincipalGrowth, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyRegisterVerify(w, r, opts)
	})

	registerRoute("/auth/passkey/login/options", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationAuthAccessCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginOptions(w, r, opts)
	})

	registerRoute("/auth/passkey/login/verify", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationAuthAccessCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyLoginVerify(w, r, opts)
	})

	registerRoute("/auth/passkey/dev/register", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationPrincipalGrowth, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyDevRegister(w, r, opts)
	})

	registerRoute("/auth/passkey/dev/login", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationAuthAccessCeremony, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handlePasskeyDevLogin(w, r, opts)
	})

	registerRoute("/secrets", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListSecrets(w, r, opts)
		case http.MethodPost:
			handleCreateSecret(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/secrets/reveal-batch", exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRevealSecretsBatch(w, r, opts)
	})

	registerRoute("/secrets/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/secrets/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		if strings.HasSuffix(remainder, "/reveal") {
			secretID := strings.TrimSuffix(remainder, "/reveal")
			if strings.TrimSpace(secretID) == "" || strings.Contains(secretID, "/") {
				return routeAccessRequirement{}
			}
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationBusiness, supported: true}
		}
		if strings.Contains(remainder, "/") {
			return routeAccessRequirement{}
		}
		return exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationBusiness, http.MethodPut, http.MethodDelete)(r)
	}, func(w http.ResponseWriter, r *http.Request) {
		remainder := strings.TrimPrefix(r.URL.Path, "/secrets/")
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		if strings.HasSuffix(remainder, "/reveal") {
			secretID := strings.TrimSuffix(remainder, "/reveal")
			secretID = strings.TrimRight(secretID, "/")
			if secretID == "" || strings.Contains(secretID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			handleRevealSecret(w, r, opts, secretID)
			return
		}
		if strings.Contains(remainder, "/") {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		secretID := remainder
		switch r.Method {
		case http.MethodPut:
			handleUpdateSecret(w, r, opts, secretID)
		case http.MethodDelete:
			handleDeleteSecret(w, r, opts, secretID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only PUT and DELETE are supported")
		}
	})

	registerRoute("/agents/me", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessAuthenticatedPrincipal, mutation: routeMutationNone, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetCurrentAgent(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		}
	})
	registerRoute("/agents", commandCenterRunRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAgents(w, r, opts) })
	registerRoute("/agents/", commandCenterRunRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleAgents(w, r, opts) })
	registerStreamRoute("agents", exactRouteAccess(routeAccessAuthenticatedPrincipal, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		handleAgentChangesStream(w, r, opts)
	})
	registerRoute("/hosts", hostRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleHostRoutes(w, r, opts) })
	registerRoute("/hosts/", hostRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleHostRoutes(w, r, opts) })
	registerRoute("/runs", commandCenterRunRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleRuns(w, r, opts) })
	registerRoute("/runs/", commandCenterRunRouteAccess, func(w http.ResponseWriter, r *http.Request) { handleRuns(w, r, opts) })

	registerRoute("/topics", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
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
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"), strings.HasSuffix(remainder, "/trash"), strings.HasSuffix(remainder, "/restore"):
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

		if strings.HasSuffix(remainder, "/trash") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			topicID := strings.TrimSuffix(remainder, "/trash")
			topicID = strings.TrimSuffix(topicID, "/")
			if topicID == "" || strings.Contains(topicID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTrashTopic(w, r, opts, topicID)
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

	registerRoute("/threads", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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

	registerRoute("/reports/preview", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reports/preview" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		handlePreviewReport(w, r, opts)
	})

	registerRoute("/docs", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListDocuments(w, r, opts)
		case http.MethodPost:
			handleCreateDocument(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/docs/search", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleSearchDocuments(w, r, opts)
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
		case strings.HasSuffix(remainder, "/trash"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/report"):
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: r.Method == http.MethodGet}
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
		case strings.HasSuffix(remainder, "/comments"):
			if r.Method == http.MethodGet || r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/replies") && strings.Contains(remainder, "/comments/"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/comments/"):
			if r.Method == http.MethodPatch || r.Method == http.MethodDelete {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.Contains(remainder, "/"):
			return routeAccessRequirement{}
		case r.Method == http.MethodGet, r.Method == http.MethodPatch, r.Method == http.MethodPut:
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

		if strings.HasSuffix(remainder, "/report") {
			documentID := strings.TrimSuffix(remainder, "/report")
			if r.Method != http.MethodGet {
				writeError(w, 405, "method_not_allowed", "only GET is supported")
				return
			}
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, 404, "not_found", "endpoint not found")
				return
			}
			handleRenderReport(w, r, opts, documentID)
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

		if strings.HasSuffix(remainder, "/trash") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			documentID := strings.TrimSuffix(remainder, "/trash")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTrashDocument(w, r, opts, documentID)
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

		if strings.HasSuffix(remainder, "/comments") {
			documentID := strings.TrimSuffix(remainder, "/comments")
			documentID = strings.TrimSuffix(documentID, "/")
			if documentID == "" || strings.Contains(documentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleListDocumentComments(w, r, opts, documentID)
			case http.MethodPost:
				handleCreateDocumentComment(w, r, opts, documentID, "")
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
			}
			return
		}

		if strings.HasSuffix(remainder, "/replies") {
			without := strings.TrimSuffix(remainder, "/replies")
			without = strings.TrimSuffix(without, "/")
			idx := strings.Index(without, "/comments/")
			if idx <= 0 {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			documentID := strings.TrimSpace(without[:idx])
			commentID := strings.TrimSpace(without[idx+len("/comments/"):])
			if documentID == "" || commentID == "" || strings.Contains(documentID, "/") || strings.Contains(commentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			handleCreateDocumentComment(w, r, opts, documentID, commentID)
			return
		}

		if idx := strings.Index(remainder, "/comments/"); idx > 0 {
			documentID := strings.TrimSpace(remainder[:idx])
			commentID := strings.TrimSpace(remainder[idx+len("/comments/"):])
			if documentID == "" || commentID == "" || strings.Contains(documentID, "/") || strings.Contains(commentID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodPatch:
				handleUpdateDocumentComment(w, r, opts, documentID, commentID)
			case http.MethodDelete:
				handleDeleteDocumentComment(w, r, opts, documentID, commentID)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only PATCH and DELETE are supported")
			}
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
			handlePatchDocument(w, r, opts, remainder)
		case http.MethodPut:
			handlePutDocument(w, r, opts, remainder)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET, PATCH, and PUT are supported")
		}
	})

	registerRoute("/boards", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
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
		case strings.HasSuffix(remainder, "/archive"), strings.HasSuffix(remainder, "/unarchive"), strings.HasSuffix(remainder, "/trash"), strings.HasSuffix(remainder, "/restore"), strings.HasSuffix(remainder, "/purge"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/cards/batch"):
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
			case strings.HasSuffix(cardRemainder, "/archive"), strings.HasSuffix(cardRemainder, "/trash"):
				if r.Method == http.MethodPost {
					return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
				}
				return routeAccessRequirement{}
			case strings.Contains(cardRemainder, "/"):
				return routeAccessRequirement{}
			case r.Method == http.MethodGet:
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

		if strings.HasSuffix(remainder, "/trash") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/trash")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTrashBoard(w, r, opts, boardID)
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

		if strings.HasSuffix(remainder, "/cards/batch") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			boardID := strings.TrimSuffix(remainder, "/cards/batch")
			boardID = strings.TrimSuffix(boardID, "/")
			if boardID == "" || strings.Contains(boardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleBatchAddBoardCards(w, r, opts, boardID)
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

			if strings.HasSuffix(cardRemainder, "/trash") {
				if r.Method != http.MethodPost {
					writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
					return
				}
				cardID := strings.TrimSuffix(cardRemainder, "/trash")
				cardID = strings.TrimSuffix(cardID, "/")
				if cardID == "" || strings.Contains(cardID, "/") {
					writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
					return
				}
				handleTrashBoardCard(w, r, opts, boardID, cardID)
				return
			}

			if strings.Contains(cardRemainder, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			handleGetBoardCard(w, r, opts, boardID, cardRemainder)
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

	registerRoute("/events", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListEvents(w, r, opts)
		case http.MethodPost:
			handleAppendEvent(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/attention/placements", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleAttentionPlace(w, r, opts) })
	registerRoute("/attention/items", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleAttentionItems(w, r, opts) })
	registerRoute("/attention/visits", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleAttentionVisit(w, r, opts) })
	registerRoute("/attention/changes", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleAttentionChanges(w, r, opts) })
	registerRoute("/attention/agent", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		case http.MethodPut:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) { handleAttentionAgent(w, r, opts) })
	registerRoute("/attention/feedback", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		case http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: routeMutationBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) { handleAttentionFeedback(w, r, opts) })
	registerRoute("/brief", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleGetBriefV1(w, r, opts) })
	registerRoute("/watch/trends", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleGetWatchTrends(w, r, opts) })
	registerRoute("/overview", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleGetOverview(w, r, opts) })
	registerRoute("/overview/changes", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleOverviewChanges(w, r, opts) })
	registerRoute("/workspace/dashboard/pins", func(r *http.Request) routeAccessRequirement {
		if r.Method == http.MethodGet {
			return exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet)(r)
		}
		return exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPut)(r)
	}, func(w http.ResponseWriter, r *http.Request) { handleDashboardPins(w, r, opts) })
	registerRoute("/workspace/dashboard/reports", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleListWorkspaceDashboardReports(w, r, opts) })
	registerRoute("/workspace/dashboard", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPut), func(w http.ResponseWriter, r *http.Request) { handleSetWorkspaceDashboard(w, r, opts) })

	registerRoute("/home/unread", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		handleGetHomeUnread(w, r, opts)
	})

	registerRoute("/home/read", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		handleMarkHomeRead(w, r, opts)
	})

	registerRoute("/refs/resolve", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, 405, "method_not_allowed", "only POST is supported")
			return
		}
		handleResolveRefs(w, r, opts)
	})

	registerRoute("/ref-edges", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		handleListRefEdges(w, r, opts)
	})

	registerRoute("/cards", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleListCards(w, r, opts)
		case http.MethodPost:
			handleCreateCardGlobal(w, r, opts)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
		}
	})

	registerRoute("/cards/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/cards/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		switch {
		case strings.HasSuffix(remainder, "/revisions"):
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: r.Method == http.MethodGet || r.Method == http.MethodPost}
		case strings.Contains(remainder, "/revisions/"):
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: r.Method == http.MethodGet}
		case strings.HasSuffix(remainder, "/plan"):
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: r.Method == http.MethodGet || r.Method == http.MethodPut}
		case strings.HasSuffix(remainder, "/timeline"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/archive"):
			if r.Method == http.MethodPost {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/trash"):
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
		case strings.HasSuffix(remainder, "/lease"):
			if r.Method == http.MethodGet {
				return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
			}
			return routeAccessRequirement{}
		case strings.HasSuffix(remainder, "/lease/acquire"), strings.HasSuffix(remainder, "/lease/renew"), strings.HasSuffix(remainder, "/lease/release"):
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
		if strings.HasSuffix(remainder, "/plan") {
			id := strings.TrimSuffix(remainder, "/plan")
			if id == "" || strings.Contains(id, "/") {
				writeError(w, 404, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodPut {
				writeError(w, 405, "method_not_allowed", "only GET and PUT are supported")
				return
			}
			handleCardPlan(w, r, opts, id)
			return
		}
		if remainder == "" {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
			return
		}
		if strings.HasSuffix(remainder, "/timeline") {
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/timeline")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleGetCardTimeline(w, r, opts, cardID)
			return
		}
		if strings.HasSuffix(remainder, "/revisions") {
			cardID := strings.TrimSuffix(remainder, "/revisions")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			switch r.Method {
			case http.MethodGet:
				handleListCardRevisions(w, r, opts, cardID)
			case http.MethodPost:
				handleCreateCardRevision(w, r, opts, cardID)
			default:
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and POST are supported")
			}
			return
		}
		if strings.Contains(remainder, "/revisions/") {
			parts := strings.Split(remainder, "/revisions/")
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.Contains(parts[1], "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
				return
			}
			handleGetCardRevision(w, r, opts, parts[0], parts[1])
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
		if strings.HasSuffix(remainder, "/trash") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			cardID := strings.TrimSuffix(remainder, "/trash")
			cardID = strings.TrimSuffix(cardID, "/")
			if cardID == "" || strings.Contains(cardID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTrashCard(w, r, opts, cardID)
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
		if cardID, action, ok := cardLeaseAction(remainder); ok {
			handleCardLease(w, r, opts, cardID, action)
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

	registerStreamRoute("events", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
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
			strings.HasSuffix(remainder, "/trash"), strings.HasSuffix(remainder, "/restore"):
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

		for _, suffix := range []string{"/archive", "/unarchive", "/trash", "/restore"} {
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
				case "/trash":
					handleTrashEvent(w, r, opts, eventID)
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

	registerRoute("/artifacts/attachments", func(r *http.Request) routeAccessRequirement {
		if r.Method == http.MethodPost {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		return routeAccessRequirement{}
	}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCreateArtifactAttachment(w, r, opts)
	})

	registerRoute("/artifacts", func(r *http.Request) routeAccessRequirement {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		default:
			return routeAccessRequirement{}
		}
	}, func(w http.ResponseWriter, r *http.Request) {
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
		case strings.HasSuffix(remainder, "/trash"):
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

		if strings.HasSuffix(remainder, "/trash") {
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
				return
			}
			artifactID := strings.TrimSuffix(remainder, "/trash")
			artifactID = strings.TrimSuffix(artifactID, "/")
			if artifactID == "" || strings.Contains(artifactID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleTrashArtifact(w, r, opts, artifactID)
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

	registerRoute("/inbox", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetInbox(w, r, opts)
	})

	registerRoute("/agent-inbox/subscriptions", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleAskSubscription(w, r, opts, "") })
	registerRoute("/asks/", func(r *http.Request) routeAccessRequirement {
		rest := strings.TrimPrefix(r.URL.Path, "/asks/")
		parts := strings.Split(rest, "/")
		if len(parts) == 2 && parts[0] != "" && ((parts[1] == "presentation" && r.Method == http.MethodGet) || ((parts[1] == "revise" || parts[1] == "retire") && r.Method == http.MethodPost)) {
			mutation := routeMutationNone
			if r.Method == http.MethodPost {
				mutation = routeMutationBusiness
			}
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, mutation: mutation, supported: true}
		}
		if len(parts) == 1 && parts[0] == "batch" && r.Method == http.MethodPost {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		if len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		if len(parts) == 2 && parts[0] != "" && r.Method == http.MethodPost && (parts[1] == "subscriptions" || parts[1] == "delivery") {
			return routeAccessRequirement{bucket: routeAccessWorkspaceBusiness, supported: true}
		}
		return routeAccessRequirement{}
	}, func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/asks/")
		parts := strings.Split(rest, "/")
		if len(parts) == 1 && parts[0] == "batch" && r.Method == http.MethodPost {
			handleCreateAskBatch(w, r, opts)
			return
		}
		if len(parts) == 2 && r.Method == http.MethodPost {
			switch parts[1] {
			case "retire":
				handleAskRetirement(w, r, opts, parts[0])
				return
			case "revise":
				handleAskPresentation(w, r, opts, parts[0], true)
				return
			case "subscriptions":
				handleAskSubscription(w, r, opts, parts[0])
				return
			case "delivery":
				handleAskDeliveryReceipt(w, r, opts, parts[0])
				return
			}
		}
		if len(parts) == 2 && parts[1] == "presentation" && r.Method == http.MethodGet {
			handleAskPresentation(w, r, opts, parts[0], false)
			return
		}
		handleAskOutcome(w, r, opts, false)
	})
	registerStreamRoute("agent-wakeups", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleAskWakeStream(w, r, opts) })
	registerStreamRoute("asks/{ask_id}", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		handleAskOutcome(w, r, opts, true)
	})

	registerRoute("/curation/candidates", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleCurationCandidates(w, r, opts) })
	registerRoute("/curation/proposals", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationPropose(w, r, opts) })
	registerRoute("/curation/ungroup", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationUngroup(w, r, opts) })
	registerRoute("/curation/run", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) { handleCurationRun(w, r, opts) })
	registerRoute("/curation/metrics", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) { handleCurationMetrics(w, r, opts) })

	registerRoute("/inbox/summary", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, 405, "method_not_allowed", "only GET is supported")
			return
		}
		handleGetInboxSummary(w, r, opts)
	})

	registerRoute("/inbox/", func(r *http.Request) routeAccessRequirement {
		remainder := strings.TrimPrefix(r.URL.Path, "/inbox/")
		if remainder == "" {
			return routeAccessRequirement{}
		}
		if r.Method == http.MethodPost && strings.HasSuffix(remainder, "/respond") {
			prefix := strings.TrimSuffix(remainder, "/respond")
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
		if r.Method == http.MethodPost && strings.HasSuffix(remainder, "/respond") {
			inboxID := strings.TrimSuffix(remainder, "/respond")
			inboxID = strings.TrimSuffix(inboxID, "/")
			if inboxID == "" || strings.Contains(inboxID, "/") {
				writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
				return
			}
			handleRespondInboxItem(w, r, opts, inboxID)
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

	registerStreamRoute("inbox", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleInboxStream(w, r, opts)
	})

	registerStreamRoute("agent-notification-receipts", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleAgentNotificationReceiptsStream(w, r, opts)
	})

	registerRoute("/agent-notifications", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListAgentNotifications(w, r, opts)
	})

	registerRoute("/agent-inbox/asks", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationNone, http.MethodGet), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
			return
		}
		handleListAgentInboxAsks(w, r, opts)
	})

	registerRoute("/agent-inbox/answers/read", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleReadAgentInboxAnswer(w, r, opts)
	})

	registerRoute("/agent-notifications/read", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleReadAgentNotification(w, r, opts)
	})

	registerRoute("/agent-notifications/dismiss", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationBusiness, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleDismissAgentNotification(w, r, opts)
	})

	registerRoute("/agent-wakeups/claim", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationInternalMaintenance, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleClaimAgentWakeup(w, r, opts)
	})

	registerRoute("/agent-wakeups/complete", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationInternalMaintenance, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleCompleteAgentWakeup(w, r, opts)
	})

	registerRoute("/agent-wakeups/fail", exactRouteAccess(routeAccessPublicAuthCeremony, routeMutationInternalMaintenance, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleFailAgentWakeup(w, r, opts)
	})

	registerRoute("/derived/rebuild", exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationInternalMaintenance, http.MethodPost), func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
			return
		}
		handleRebuildDerived(w, r, opts)
	})

	registerRoute("/", exactRouteAccess(routeAccessAlwaysPublic, routeMutationNone), func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
	})

	corsOriginSet := make(map[string]bool, len(opts.corsAllowedOrigins))
	for _, o := range opts.corsAllowedOrigins {
		corsOriginSet[o] = true
	}

	seriesAdmission := newSeriesIngress()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, allowed := beginScopedSeriesRequest(w, r, opts, seriesAdmission)
		if !allowed {
			return
		}
		if release != nil {
			defer release()
		}
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
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-ANX-CLI-Version")
			w.Header().Set("Access-Control-Expose-Headers", "X-ANX-Core-Version, X-ANX-API-Version, X-ANX-Schema-Version, X-ANX-Min-CLI-Version, X-ANX-Recommended-CLI-Version, X-Request-ID")
			w.Header().Set("Access-Control-Max-Age", "3600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		setVersionHeaders(w, opts, schemaVersion)
		if enforceCLIVersion(w, r, opts) {
			return
		}
		mux.ServeHTTP(w, r)
	})
}
