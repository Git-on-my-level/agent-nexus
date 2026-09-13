package server

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

// PMRuntimeConfig selects an existing bridge identity, never a new model
// registry. BridgeEnabled is deployment configuration for a separately bounded
// PM adapter; it is not an isolation guarantee. External source writes stay
// unavailable unless a dedicated executor is supplied. RuntimeEnvelopeEnforced
// must be true only when the selected PM actor already has an independently
// enforced read-only capability envelope. A flag or process wrapper is not that
// envelope.
type PMRuntimeConfig struct {
	PM                      pm.Config
	BridgeEnabled           bool
	RuntimeEnvelopeEnforced bool
	Observation             *ObservationRuntime
	TelegramWebhookSecret   string
	TelegramBotID           string
	TelegramBotToken        string
	TelegramAPIBase         string
	DiscordPublicKeyHex     string
	DiscordApplicationID    string
	DiscordBotToken         string
	DiscordAPIBase          string
}

// PMRuntime is the mounted PM HTTP surface plus the durable service used by
// native event follow-through and channel ingress.
type PMRuntime struct {
	Handler http.Handler
	Service *pm.Service
	Ingress pm.ChannelIngress
	Sender  pm.Sender
	cfg     PMRuntimeConfig
}

func (rt *PMRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rt == nil || rt.Handler == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "PM service is not configured")
		return
	}
	rt.Handler.ServeHTTP(w, r)
}

func (rt *PMRuntime) AgentActorID() string {
	if rt == nil {
		return ""
	}
	return rt.cfg.PM.AgentActorID
}

func (rt *PMRuntime) ChannelIngressConfigured() bool {
	if rt == nil {
		return false
	}
	return (len(rt.Ingress.TelegramSecret) >= 32 && rt.Ingress.TelegramBotID != "") ||
		(len(rt.Ingress.DiscordPublicKey) == ed25519.PublicKeySize && rt.Ingress.DiscordApplicationID != "")
}

// Tick runs turn expiry even when no runner or channel sender is attached.
func (rt *PMRuntime) Tick(ctx context.Context) error {
	if rt == nil || rt.Service == nil {
		return nil
	}
	return errors.Join(rt.Service.ExpireTurns(ctx, time.Now().UTC()), rt.Drain(ctx))
}

func (rt *PMRuntime) Drain(ctx context.Context) error {
	if rt == nil || rt.Service == nil || rt.Sender == nil {
		return nil
	}
	_, err := rt.Service.DeliverPending(ctx, rt.Sender, 50)
	return err
}

func (rt *PMRuntime) SyncBridgeReply(ctx context.Context, store *primitives.Store, event map[string]any) error {
	if rt == nil || rt.Service == nil || store == nil || event == nil {
		return nil
	}
	payload, _ := event["payload"].(map[string]any)
	turnID := strings.TrimSpace(anyString(payload["pm_turn_id"]))
	eventID := strings.TrimSpace(anyString(event["id"]))
	if turnID == "" || eventID == "" {
		return nil
	}
	_, err := rt.Service.SyncBridgeReply(ctx, store, turnID, eventID)
	if errors.Is(err, pm.ErrNotFound) || errors.Is(err, pm.ErrForbidden) {
		return nil
	}
	return err
}

// NewPMRuntime binds the peer PM service to the canonical SQLite, card store,
// current auth principals, and existing agent wake queue.
func NewPMRuntime(db *sql.DB, store *primitives.Store, authStore *auth.Store, cfg PMRuntimeConfig) (*PMRuntime, error) {
	if db == nil || store == nil || authStore == nil {
		return nil, pm.ErrUnavailable
	}
	if cfg.BridgeEnabled && !cfg.RuntimeEnvelopeEnforced {
		return nil, pm.ErrUnavailable
	}
	ps, err := pm.NewStore(db)
	if err != nil {
		return nil, err
	}
	principalLookup := newPMPrincipalLookup(authStore)
	findPrincipal := principalLookup.find
	authorize := func(ctx context.Context, p pm.Principal, permission, ref string) error {
		if p.WorkspaceID != cfg.PM.WorkspaceID {
			return pm.ErrForbidden
		}
		actual, err := findPrincipal(ctx, p.ActorID)
		if err != nil {
			return pm.ErrForbidden
		}
		if ref != "" && permission != "pm.respond" {
			if _, err := store.GetWork(ctx, ref); err != nil {
				return pm.ErrNotFound
			}
		}
		switch permission {
		case "pm.access", "pm.read", "pm.delivery.reconcile":
			return nil
		case "pm.respond":
			if strings.TrimSpace(cfg.PM.AgentActorID) == "" {
				return pm.ErrPMIdentity
			}
			if p.ActorID == cfg.PM.AgentActorID {
				return nil
			}
		case "pm.bind.target":
			if p.Human && actual.PrincipalKind != string(auth.PrincipalKindHuman) {
				return pm.ErrForbidden
			}
			return nil
		case "pm.propose", "pm.approve", "pm.bind":
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				return nil
			}
		case "pm.action.work.annotate":
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				w, err := store.GetWork(ctx, ref)
				if err == nil && anyString(workSourceMap(w)["authority"]) == "nexus" {
					return nil
				}
			}
		case "pm.action.work.phase", "pm.action.github", "pm.action.multica", "pm.action.ssh_git":
			// Humans may request source follow-through. Execute stays unavailable
			// until a dedicated authorized source executor is supplied.
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				return nil
			}
		}
		return pm.ErrForbidden
	}
	bridge := pm.NexusBridge{Store: store, ActorID: actors.SystemActorID}
	if cfg.BridgeEnabled {
		bridge.Ready = func(ctx context.Context, actorID, workspaceID string) error {
			if !cfg.RuntimeEnvelopeEnforced {
				return pm.ErrUnavailable
			}
			if actorID != cfg.PM.AgentActorID || workspaceID != cfg.PM.WorkspaceID {
				return pm.ErrForbidden
			}
			principal, err := findPrincipal(ctx, actorID)
			if err != nil {
				return err
			}
			status := auth.DescribeWakeRouting(principal, workspaceID, time.Now().UTC())
			if !status.Online || status.Handle != cfg.PM.AgentHandle {
				return pm.ErrUnavailable
			}
			return nil
		}
	}
	deps := pm.Dependencies{Authorize: authorize, EnsureThread: bridge.EnsureThread}
	if cfg.BridgeEnabled {
		deps.Dispatch = bridge.Dispatch
	}
	deps.ReadContextPage = func(ctx context.Context, p pm.Principal, ref, query, cursor string, limit int) (pm.ContextPage, error) {
		if ref != "" {
			if cursor != "" {
				return pm.ContextPage{}, pm.ErrInvalid
			}
			w, err := store.GetWork(ctx, ref)
			if err != nil {
				return pm.ContextPage{}, err
			}
			return pm.ContextPage{Items: []any{publicWork(w)}}, nil
		}
		page, err := store.ListWork(ctx, primitives.WorkListFilter{Query: query, Cursor: cursor, Limit: limit})
		if errors.Is(err, primitives.ErrInvalidCursor) {
			return pm.ContextPage{}, pm.ErrInvalid
		}
		if err != nil {
			return pm.ContextPage{}, err
		}
		items := make([]any, 0, len(page.Work))
		for _, w := range page.Work {
			items = append(items, publicWork(w))
		}
		return pm.ContextPage{Items: items, NextCursor: page.NextCursor}, nil
	}
	deps.CurrentRevision = func(ctx context.Context, p pm.Principal, ref string) (string, error) {
		return currentWorkDecisionRevision(ctx, store, ref)
	}

	// The registry is the single source of configured native execution paths.
	nativeExecutors := map[string]func(context.Context, pm.Action) (pm.Receipt, error){
		"work.phase": func(ctx context.Context, a pm.Action) (pm.Receipt, error) { return executeWorkPhase(ctx, store, a) },
		"work.annotate": func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
			return executeNativeAnnotation(ctx, store, a)
		},
	}
	deps.CheckDelivery = func(ctx context.Context, a pm.Action) error {
		w, err := store.GetWork(ctx, a.WorkRef)
		if err != nil {
			return err
		}
		authority := anyString(workSourceMap(w)["authority"])
		if authority == "nexus" && nativeExecutors[a.Scope] != nil {
			return nil
		}
		source := authority
		if source == "github" {
			source = "GitHub"
		}
		if source == "nexus" || source == "" {
			source = a.Scope
		}
		return pm.NoDeliveryPath(source)
	}
	// Native mutations use canonical stores; source writes require a dedicated executor.
	deps.Execute = func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
		if execute := nativeExecutors[a.Scope]; execute != nil {
			return execute(ctx, a)
		}
		return pm.Receipt{}, pm.ErrUnavailable
	}
	deps.Reconcile = func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
		if a.Scope == "work.phase" {
			w, err := store.GetWork(ctx, a.WorkRef)
			if err != nil {
				return pm.Receipt{}, err
			}
			if anyString(workSourceMap(w)["authority"]) == "nexus" {
				return readBackWorkPhase(ctx, store, a, true)
			}
		}
		if a.Scope == "work.annotate" {
			return reconcileNativeAnnotation(ctx, store, a)
		}
		if cfg.Observation == nil {
			return pm.Receipt{}, pm.ErrUnavailable
		}
		return reconcileSourceRead(ctx, store, cfg.Observation, a)
	}
	service, err := pm.NewService(ps, cfg.PM, deps)
	if err != nil {
		return nil, err
	}
	handler := pm.Handler{Service: service, Authenticate: func(r *http.Request) (pm.Principal, error) {
		token, err := parseBearerToken(r.Header.Get("Authorization"))
		if err != nil {
			return pm.Principal{}, pm.ErrForbidden
		}
		p, err := authStore.AuthenticateAccessToken(r.Context(), token)
		if err != nil {
			return pm.Principal{}, pm.ErrForbidden
		}
		return pm.Principal{WorkspaceID: cfg.PM.WorkspaceID, ActorID: p.ActorID, Human: p.PrincipalKind == string(auth.PrincipalKindHuman)}, nil
	}}
	runtime := &PMRuntime{Handler: handler, Service: service, cfg: cfg}
	if len(cfg.TelegramWebhookSecret) >= 32 && cfg.TelegramBotID != "" {
		runtime.Ingress.TelegramSecret = cfg.TelegramWebhookSecret
		runtime.Ingress.TelegramBotID = cfg.TelegramBotID
	}
	if cfg.DiscordPublicKeyHex != "" && cfg.DiscordApplicationID != "" {
		key, decodeErr := hex.DecodeString(strings.TrimSpace(cfg.DiscordPublicKeyHex))
		if decodeErr != nil || len(key) != ed25519.PublicKeySize {
			return nil, pm.ErrInvalid
		}
		runtime.Ingress.DiscordPublicKey = ed25519.PublicKey(key)
		runtime.Ingress.DiscordApplicationID = cfg.DiscordApplicationID
	}
	runtime.Ingress.Service = service
	if cfg.TelegramBotToken != "" || cfg.DiscordBotToken != "" {
		runtime.Sender = pm.HTTPSender{
			TelegramToken:        cfg.TelegramBotToken,
			TelegramBotID:        cfg.TelegramBotID,
			TelegramAPIBase:      cfg.TelegramAPIBase,
			DiscordToken:         cfg.DiscordBotToken,
			DiscordApplicationID: cfg.DiscordApplicationID,
			DiscordAPIBase:       cfg.DiscordAPIBase,
		}
	}
	return runtime, nil
}

func executeNativeAnnotation(ctx context.Context, store *primitives.Store, a pm.Action) (pm.Receipt, error) {
	version, err := strconv.ParseInt(a.TargetRevision, 10, 64)
	if err != nil {
		return pm.Receipt{}, pm.ErrInvalid
	}
	patch := map[string]any{}
	if err = json.Unmarshal([]byte(a.Instruction), &patch); err != nil {
		return pm.Receipt{}, pm.ErrInvalid
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	if anyString(workSourceMap(w)["authority"]) != "nexus" {
		return pm.Receipt{}, pm.ErrForbidden
	}
	_, err = store.PatchWork(ctx, a.ActorID, a.WorkRef, version, patch)
	if err != nil {
		return pm.Receipt{}, err
	}
	return pm.Receipt{Status: pm.Reported, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, Detail: "Nexus annotation mutation committed; reconcile for read-back verification"}, nil
}

func reconcileNativeAnnotation(ctx context.Context, store *primitives.Store, a pm.Action) (pm.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	patch := map[string]any{}
	if json.Unmarshal([]byte(a.Instruction), &patch) != nil || len(patch) == 0 {
		return pm.Receipt{}, pm.ErrInvalid
	}
	for key, want := range patch {
		actual, _ := json.Marshal(w[key])
		expected, _ := json.Marshal(want)
		if string(actual) != string(expected) {
			return pm.Receipt{Status: pm.Unknown, Detail: "Current local fields do not establish the requested outcome"}, nil
		}
	}
	return pm.Receipt{Status: pm.Verified, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, IndependentlyVerified: true, Detail: "Read back requested Nexus annotation fields from canonical work"}, nil
}

func reconcileSourceRead(ctx context.Context, store *primitives.Store, runtime *ObservationRuntime, a pm.Action) (pm.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	source := workSourceMap(w)
	authority := anyString(source["authority"])
	if authority != "github" && authority != "multica" && authority != "git" && authority != "ssh_git" {
		return pm.Receipt{}, pm.ErrUnavailable
	}
	report, err := runtime.ReadOne(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, pm.ErrUnavailable
	}
	refs := []string{a.WorkRef}
	for _, evidence := range report.Evidence {
		if evidence.Reference != "" {
			refs = append(refs, evidence.Reference)
		}
	}
	revision := strings.TrimSpace(report.SourceRevision)
	if revision == "" {
		return pm.Receipt{Status: pm.Unknown, Detail: "Source read succeeded but did not establish a revision for verification"}, nil
	}
	// TargetRevision is the authorized pre-write snapshot. A read-only reader
	// cannot prove that this action's mutation was applied, whether or not the
	// live revision still matches that snapshot.
	detail := "Read-only source reader cannot independently verify a source write"
	if a.TargetRevision != "" && revision == a.TargetRevision {
		detail = "Current source revision still matches the pre-action snapshot; a source write was not independently verified"
	} else if a.TargetRevision != "" && revision != a.TargetRevision {
		detail = "Current source revision does not match the authorized target revision"
	}
	return pm.Receipt{Status: pm.Unknown, ExternalID: a.ID, EvidenceRefs: refs, Detail: detail}, nil
}

func workSourceMap(w map[string]any) map[string]any {
	source, _ := w["source"].(map[string]any)
	return source
}

func executeWorkPhase(ctx context.Context, store *primitives.Store, a pm.Action) (pm.Receipt, error) {
	if a.Payload == nil || a.Payload.Phase == "" {
		return pm.Receipt{}, pm.ErrInvalid
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	if anyString(workSourceMap(w)["authority"]) != "nexus" {
		return pm.Receipt{}, pm.ErrUnavailable
	}
	version, err := strconv.ParseInt(a.TargetRevision, 10, 64)
	if err != nil {
		return pm.Receipt{}, pm.ErrInvalid
	}
	input := primitives.MoveBoardCardInput{ColumnKey: a.Payload.Phase, IfWorkVersion: &version}
	if len(a.Payload.ResolutionRefs) > 0 {
		input.ResolutionRefs = &a.Payload.ResolutionRefs
	}
	_, err = store.MoveBoardCard(ctx, a.ActorID, "", anyString(w["id"]), input)
	if errors.Is(err, primitives.ErrConflict) {
		return pm.Receipt{}, pm.ErrStale
	}
	if err != nil {
		return pm.Receipt{}, err
	}
	return readBackWorkPhase(ctx, store, a, false)
}
func readBackWorkPhase(ctx context.Context, store *primitives.Store, a pm.Action, reconcile bool) (pm.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	if a.Payload == nil || anyString(workSourceMap(w)["authority"]) != "nexus" {
		return pm.Receipt{}, pm.ErrInvalid
	}
	phase := anyString(w["phase"])
	if phase != a.Payload.Phase {
		return pm.Receipt{Status: pm.Unknown, Detail: "Canonical phase does not match requested phase: " + phase}, nil
	}
	status := pm.Reported
	if reconcile {
		status = pm.Verified
	}
	return pm.Receipt{Status: status, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, IndependentlyVerified: reconcile, Detail: "Read back canonical Nexus phase: " + phase}, nil
}

func currentWorkDecisionRevision(ctx context.Context, store *primitives.Store, ref string) (string, error) {
	w, err := store.GetWork(ctx, ref)
	if err != nil {
		return "", err
	}
	return primitives.WorkDecisionRevision(w), nil
}
