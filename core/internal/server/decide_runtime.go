package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/decide"
	"agent-nexus-core/internal/primitives"
)

// DecideRuntimeConfig selects an existing bridge identity, never a new model
// registry. External source writes stay unavailable unless a dedicated
// executor is supplied.
type DecideRuntimeConfig struct {
	Decide      decide.Config
	Observation *ObservationRuntime
}

// DecideRuntime is the mounted decision HTTP surface plus the durable service
// used by native event follow-through.
type pmDeliverySourcesKey struct{}

type DecideRuntime struct {
	Handler http.Handler
	Service *decide.Service
	cfg     DecideRuntimeConfig
}

func (rt *DecideRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rt == nil || rt.Handler == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "Decision service is not configured")
		return
	}
	rt.Handler.ServeHTTP(w, r)
}

// NewDecideRuntime binds the decision service to the canonical SQLite, card
// store, current auth principals, and existing agent wake queue.
func NewDecideRuntime(db *sql.DB, store *primitives.Store, authStore *auth.Store, cfg DecideRuntimeConfig) (*DecideRuntime, error) {
	if db == nil || store == nil || authStore == nil {
		return nil, decide.ErrUnavailable
	}
	ps, err := decide.NewStore(db)
	if err != nil {
		return nil, err
	}
	principalLookup := newPMPrincipalLookup(authStore)
	authorize := func(ctx context.Context, p decide.Principal, permission, ref string) error {
		if p.WorkspaceID != cfg.Decide.WorkspaceID {
			return decide.ErrForbidden
		}
		if ref != "" {
			if err := store.CheckResourceValues(ctx, []string{ref}); err != nil {
				return decide.ErrNotFound
			}
		}
		actual, err := principalLookup.findForAuthorization(ctx, p.ActorID)
		if err != nil {
			return err
		}
		// Decision reads/answers and source actions authorize the principal first;
		// their domain preflight handles missing or non-live work.
		if ref != "" && permission != "pm.read" && permission != "pm.approve" && permission != "pm.propose" && !strings.HasPrefix(permission, "pm.action.") {
			if _, err := store.GetWork(ctx, ref); err != nil {
				if errors.Is(err, primitives.ErrNotFound) {
					return decide.ErrNotFound
				}
				return err
			}
		}
		switch permission {
		case "pm.access", "pm.read", "pm.delivery.reconcile":
			return nil
		case "pm.propose":
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindAgent) {
				return &decide.ConversationOwnerNotHumanError{}
			}
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				if ref == "" {
					return nil
				}
				_, err := liveDecisionWork(ctx, store, ref)
				return err
			}
		case "pm.approve":
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				return nil
			}
		case "pm.action.work.annotate", "pm.action.work.note":
			if p.Human && actual.PrincipalKind == string(auth.PrincipalKindHuman) {
				w, err := store.GetWork(ctx, ref)
				if errors.Is(err, primitives.ErrNotFound) || (err == nil && anyString(workSourceMap(w)["authority"]) == "nexus") {
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
		return decide.ErrForbidden
	}
	deps := decide.Dependencies{Authorize: authorize}
	deps.AuthorizeReadBatch = func(ctx context.Context, p decide.Principal, permission string, refs []string) (map[string]bool, error) {
		if permission != "pm.read" && permission != "pm.approve" {
			return nil, decide.ErrForbidden
		}
		// This is one read operation, not a principal cache. Reload authority for
		// every batch; mutation permissions always use the original authorizer.
		if err := authorize(ctx, p, permission, ""); err != nil {
			return nil, err
		}
		// Empty legacy work refs have no resource check, matching the point
		// authorizer after the fresh principal check above.
		allowed := map[string]bool{"": true}
		var check func([]string) error
		check = func(batch []string) error {
			if len(batch) == 0 {
				return nil
			}
			err := store.CheckResourceValues(ctx, batch)
			if err == nil {
				for _, ref := range batch {
					allowed[ref] = true
				}
				return nil
			}
			if !errors.Is(err, primitives.ErrNotFound) {
				return fmt.Errorf("%w: resource authorization could not be read", decide.ErrUnavailable)
			}
			if len(batch) == 1 {
				return nil
			}
			middle := len(batch) / 2
			if err := check(batch[:middle]); err != nil {
				return err
			}
			return check(batch[middle:])
		}
		return allowed, check(uniqueServerStrings(refs))
	}
	deps.ReadContextPage = func(ctx context.Context, p decide.Principal, ref, query, cursor string, limit int) (decide.ContextPage, error) {
		ctx = primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: p.ActorID})
		if ref != "" {
			if cursor != "" {
				return decide.ContextPage{}, decide.ErrContextWorkCursor
			}
			if !strings.HasPrefix(ref, "card:") && !strings.HasPrefix(ref, "work:") {
				previews, err := store.ResolveRefs(ctx, []string{ref}, nil, time.Now().UTC())
				if err != nil {
					return decide.ContextPage{}, err
				}
				items := make([]any, 0, len(previews))
				for _, preview := range previews {
					items = append(items, preview)
				}
				return decide.ContextPage{Items: items}, nil
			}
			w, err := store.GetWork(ctx, ref)
			if err != nil {
				return decide.ContextPage{}, err
			}
			return decide.ContextPage{Items: []any{publicWork(w)}}, nil
		}
		page, err := store.ListWork(ctx, primitives.WorkListFilter{Query: query, Cursor: cursor, Limit: limit})
		if errors.Is(err, primitives.ErrInvalidCursor) {
			return decide.ContextPage{}, decide.ErrContextCursor
		}
		if err != nil {
			return decide.ContextPage{}, err
		}
		items := make([]any, 0, len(page.Work))
		for _, w := range page.Work {
			items = append(items, publicWork(w))
		}
		return decide.ContextPage{Items: items, NextCursor: page.NextCursor}, nil
	}
	deps.CurrentRevision = func(ctx context.Context, p decide.Principal, ref string) (string, error) {
		return currentWorkDecisionRevision(ctx, store, ref)
	}
	deps.DecisionWork = func(ctx context.Context, p decide.Principal, ref string) (decide.DecisionWork, error) {
		w, err := liveDecisionWork(ctx, store, ref)
		if err != nil {
			return decide.DecisionWork{}, err
		}
		return decide.DecisionWork{Revision: primitives.WorkDecisionRevision(w), Phase: anyString(w["phase"]), SourceAuthority: anyString(workSourceMap(w)["authority"])}, nil
	}

	deps.DecisionWorkBatch = func(ctx context.Context, p decide.Principal, refs []string) (map[string]decide.DecisionWork, error) {
		snapshots, err := store.LiveWorkSnapshots(ctx, refs)
		if err != nil {
			return nil, err
		}
		out := map[string]decide.DecisionWork{}
		for ref, w := range snapshots {
			out[ref] = decide.DecisionWork{Revision: primitives.WorkDecisionRevision(w), Phase: anyString(w["phase"]), SourceAuthority: anyString(workSourceMap(w)["authority"])}
		}
		return out, nil
	}
	// The registry is the single source of configured native execution paths.
	nativeExecutors := map[string]func(context.Context, decide.Action) (decide.Receipt, error){
		"work.phase": func(ctx context.Context, a decide.Action) (decide.Receipt, error) {
			return executeWorkPhase(ctx, store, a)
		},
		"work.note": func(ctx context.Context, a decide.Action) (decide.Receipt, error) {
			return executeCardNote(ctx, store, a)
		},
		"work.annotate": func(ctx context.Context, a decide.Action) (decide.Receipt, error) {
			return executeNativeAnnotation(ctx, store, a)
		},
	}
	deps.BeginDeliveryRead = func(ctx context.Context, refs []string) (context.Context, error) {
		snapshots, err := store.WorkRoutingSnapshots(ctx, uniqueServerStrings(refs))
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, pmDeliverySourcesKey{}, snapshots), nil
	}
	deps.DeliveryPath = func(ctx context.Context, a decide.Action) (string, error) {
		authority := a.SourceAuthority
		if authority == "" {
			// Legacy records lack the trusted routing snapshot. Resolve it from
			// live or trashed work where possible; never infer it from a scope.
			var w map[string]any
			var err error
			if snapshots, ok := ctx.Value(pmDeliverySourcesKey{}).(map[string]map[string]any); ok {
				w = snapshots[a.WorkRef]
				if w == nil {
					err = primitives.ErrNotFound
				}
			} else {
				w, err = store.GetWork(ctx, a.WorkRef)
			}
			if errors.Is(err, primitives.ErrNotFound) {
				return "unknown", decide.ErrNotFound
			}
			if err != nil {
				return "none", err
			}
			authority = anyString(workSourceMap(w)["authority"])
		}
		if authority == "nexus" && nativeExecutors[a.Scope] != nil {
			return "nexus", nil
		}
		source := authority
		if source == "github" {
			source = "GitHub"
		}
		if source == "nexus" || source == "" {
			source = a.Scope
		}
		return "none", decide.NoDeliveryPath(source)
	}
	// Native mutations use canonical stores; source writes require a dedicated executor.
	deps.Execute = func(ctx context.Context, a decide.Action) (decide.Receipt, error) {
		if execute := nativeExecutors[a.Scope]; execute != nil {
			return execute(ctx, a)
		}
		return decide.Receipt{}, decide.ErrUnavailable
	}
	deps.Reconcile = func(ctx context.Context, a decide.Action) (decide.Receipt, error) {
		if a.Scope == "work.phase" {
			w, err := store.GetWork(ctx, a.WorkRef)
			if err != nil {
				return decide.Receipt{}, err
			}
			if anyString(workSourceMap(w)["authority"]) == "nexus" {
				return readBackWorkPhase(ctx, store, a)
			}
		}
		if a.Scope == "work.note" {
			return readBackCardNote(ctx, store, a)
		}
		if a.Scope == "work.annotate" {
			return reconcileNativeAnnotation(ctx, store, a)
		}
		if cfg.Observation == nil {
			return decide.Receipt{}, decide.ErrUnavailable
		}
		return reconcileSourceRead(ctx, store, cfg.Observation, a)
	}
	deps.ResolveResolution = func(ctx context.Context, p decide.Principal, ref string) (decide.ResolutionRef, error) {
		if p.WorkspaceID != cfg.Decide.WorkspaceID {
			return decide.ResolutionRef{}, decide.ErrForbidden
		}
		resolved, err := store.ResolveResolutionRef(ctx, ref)
		if err != nil {
			return decide.ResolutionRef{}, err
		}
		// Decision summaries inherit the requesting actor's resource
		// visibility. Decisions are workspace-readable while the conversations
		// behind their evidence may not be; see pm_decision_visibility.go.
		resolved.TitleOrSummary = redactedDecisionSummary(ctx, store, p.ActorID, resolved)
		return decide.ResolutionRef{Ref: resolved.Ref, Kind: resolved.Kind, TitleOrSummary: resolved.TitleOrSummary, Exists: resolved.Exists}, err
	}
	service, err := decide.NewService(ps, cfg.Decide, deps)
	if err != nil {
		return nil, err
	}
	handler := decide.Handler{Service: service, Authenticate: func(r *http.Request) (decide.Principal, error) {
		p, ok := cachedAuthenticatedPrincipal(r)
		if !ok {
			return decide.Principal{}, decide.ErrForbidden
		}
		return decide.Principal{WorkspaceID: cfg.Decide.WorkspaceID, ActorID: p.ActorID, Human: p.PrincipalKind == string(auth.PrincipalKindHuman)}, nil
	}}
	authenticated := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if _, ok := authenticatePrincipalFromHeader(w, r, handlerOptions{authStore: authStore}, true); !ok {
			return
		}
		attachResourceAccessScope(r, handlerOptions{primitiveStore: store})
		if !authorizeResourceSelectors(w, r) {
			return
		}
		if r.Method == http.MethodGet && (r.URL.Path == "/pm/actions" || r.URL.Path == "/pm/decisions") {
			ctx, closeRead, err := store.BeginResourceRead(r.Context())
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "unavailable", "Decision data is unavailable")
				return
			}
			defer closeRead()
			r = r.WithContext(ctx)
		}
		handler.ServeHTTP(w, r)
	})
	return &DecideRuntime{Handler: authenticated, Service: service, cfg: cfg}, nil
}

type nativeMutationStore interface {
	GetWork(context.Context, string) (map[string]any, error)
	PatchWork(context.Context, string, string, int64, map[string]any) (map[string]any, error)
	MoveBoardCard(context.Context, string, string, string, primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error)
}

func executeNativeAnnotation(ctx context.Context, store nativeMutationStore, a decide.Action) (receipt decide.Receipt, execErr error) {
	writeCompleted := false
	defer func() {
		if execErr != nil {
			var uncertain *primitives.MutationOutcomeUnknown
			execErr = &decide.NativeExecutionError{Cause: execErr, WriteStarted: writeCompleted || errors.As(execErr, &uncertain)}
		}
	}()
	metadataRevision, _, _ := strings.Cut(a.TargetRevision, ".")
	version, err := strconv.ParseInt(metadataRevision, 10, 64)
	if err != nil {
		return decide.Receipt{}, fmt.Errorf("%w: invalid native target revision: %v", decide.ErrInvalid, err)
	}
	patch := map[string]any{}
	if err = json.Unmarshal([]byte(a.Instruction), &patch); err != nil {
		return decide.Receipt{}, fmt.Errorf("%w: invalid annotation JSON: %v", decide.ErrInvalid, err)
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, err
	}
	if anyString(workSourceMap(w)["authority"]) != "nexus" {
		return decide.Receipt{}, decide.ErrForbidden
	}
	if primitives.WorkDecisionRevision(w) != a.TargetRevision {
		return decide.Receipt{}, decide.ErrStale
	}
	_, err = store.PatchWork(ctx, a.ActorID, a.WorkRef, version, patch)
	if err != nil {
		return decide.Receipt{}, err
	}
	writeCompleted = true
	return reconcileNativeAnnotation(ctx, store, a)
}

func reconcileNativeAnnotation(ctx context.Context, store nativeMutationStore, a decide.Action) (decide.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, err
	}
	patch := map[string]any{}
	if json.Unmarshal([]byte(a.Instruction), &patch) != nil || len(patch) == 0 {
		return decide.Receipt{}, decide.ErrInvalid
	}
	for key, want := range patch {
		actual, _ := json.Marshal(w[key])
		expected, _ := json.Marshal(want)
		if string(actual) != string(expected) {
			return decide.Receipt{Status: decide.Failed, Detail: "Canonical annotation fields do not match the requested outcome"}, nil
		}
	}
	return decide.Receipt{NativeReadBack: true, Status: decide.Verified, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, IndependentlyVerified: true, Detail: "Read back requested Nexus annotation fields from canonical work"}, nil
}

func reconcileSourceRead(ctx context.Context, store *primitives.Store, runtime *ObservationRuntime, a decide.Action) (decide.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, err
	}
	source := workSourceMap(w)
	authority := anyString(source["authority"])
	if authority != "github" && authority != "forgejo" && authority != "gitea" && authority != "multica" && authority != "git" && authority != "ssh_git" {
		return decide.Receipt{}, decide.ErrUnavailable
	}
	report, err := runtime.ReadOne(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, decide.ErrUnavailable
	}
	refs := []string{a.WorkRef}
	for _, evidence := range report.Evidence {
		if evidence.Reference != "" {
			refs = append(refs, evidence.Reference)
		}
	}
	revision := strings.TrimSpace(report.SourceRevision)
	if revision == "" {
		return decide.Receipt{Status: decide.Unknown, Detail: "Source read succeeded but did not establish a revision for verification"}, nil
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
	return decide.Receipt{Status: decide.Unknown, ExternalID: a.ID, EvidenceRefs: refs, Detail: detail}, nil
}

func workSourceMap(w map[string]any) map[string]any {
	source, _ := w["source"].(map[string]any)
	return source
}

func executeWorkPhase(ctx context.Context, store nativeMutationStore, a decide.Action) (receipt decide.Receipt, execErr error) {
	writeCompleted := false
	defer func() {
		if execErr != nil {
			var uncertain *primitives.MutationOutcomeUnknown
			execErr = &decide.NativeExecutionError{Cause: execErr, WriteStarted: writeCompleted || errors.As(execErr, &uncertain)}
		}
	}()
	if a.Payload == nil || a.Payload.Phase == "" {
		return decide.Receipt{}, fmt.Errorf("%w: work.phase payload requires a phase", decide.ErrInvalid)
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, err
	}
	if anyString(workSourceMap(w)["authority"]) != "nexus" {
		return decide.Receipt{}, decide.ErrUnavailable
	}
	metadataRevision, _, _ := strings.Cut(a.TargetRevision, ".")
	version, err := strconv.ParseInt(metadataRevision, 10, 64)
	if err != nil {
		return decide.Receipt{}, fmt.Errorf("%w: invalid native target revision: %v", decide.ErrInvalid, err)
	}
	if primitives.WorkDecisionRevision(w) != a.TargetRevision {
		return decide.Receipt{}, decide.ErrStale
	}
	input := primitives.MoveBoardCardInput{ColumnKey: a.Payload.Phase, IfWorkVersion: &version}
	if len(a.Payload.ResolutionRefs) > 0 {
		input.ResolutionRefs = &a.Payload.ResolutionRefs
	}
	_, err = store.MoveBoardCard(ctx, a.ActorID, "", anyString(w["id"]), input)
	if errors.Is(err, primitives.ErrConflict) {
		return decide.Receipt{}, decide.ErrStale
	}
	if err != nil {
		return decide.Receipt{}, err
	}
	writeCompleted = true
	return readBackWorkPhase(ctx, store, a)
}
func readBackWorkPhase(ctx context.Context, store nativeMutationStore, a decide.Action) (decide.Receipt, error) {
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return decide.Receipt{}, err
	}
	if a.Payload == nil || anyString(workSourceMap(w)["authority"]) != "nexus" {
		return decide.Receipt{}, decide.ErrInvalid
	}
	phase := anyString(w["phase"])
	if phase != a.Payload.Phase {
		return decide.Receipt{Status: decide.Failed, Detail: "Canonical phase does not match requested phase: " + phase}, nil
	}
	return decide.Receipt{NativeReadBack: true, Status: decide.Verified, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, IndependentlyVerified: true, Detail: "Read back canonical Nexus phase: " + phase}, nil
}

func currentWorkDecisionRevision(ctx context.Context, store *primitives.Store, ref string) (string, error) {
	w, err := liveDecisionWork(ctx, store, ref)
	if err != nil {
		return "", err
	}
	return primitives.WorkDecisionRevision(w), nil
}

// Decision targets must be live even though Work GET intentionally exposes
// lifecycle records. Durable decisions remain readable and declineable after
// removal.
func liveDecisionWork(ctx context.Context, store *primitives.Store, ref string) (map[string]any, error) {
	w, err := store.GetWork(ctx, ref)
	if errors.Is(err, primitives.ErrNotFound) {
		return nil, decide.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if anyString(w["trashed_at"]) != "" || anyString(w["archived_at"]) != "" {
		return nil, decide.ErrNotFound
	}
	return w, nil
}
