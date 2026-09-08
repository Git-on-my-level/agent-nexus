package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

// PMRuntimeConfig selects an existing bridge identity, never a new model
// registry. BridgeEnabled is deployment configuration for a separately bounded
// PM adapter; it is not an isolation guarantee. External action tools remain
// absent unless explicitly supplied by an integrator.
type PMRuntimeConfig struct {
	PM            pm.Config
	BridgeEnabled bool
}

// NewPMRuntime binds the peer PM service to the canonical SQLite, card store,
// current auth principals, and existing agent wake queue.
func NewPMRuntime(db *sql.DB, store *primitives.Store, authStore *auth.Store, cfg PMRuntimeConfig) (http.Handler, error) {
	if db == nil || store == nil || authStore == nil {
		return nil, pm.ErrUnavailable
	}
	ps, err := pm.NewStore(db)
	if err != nil {
		return nil, err
	}
	findPrincipal := func(ctx context.Context, actorID string) (auth.AuthPrincipalSummary, error) {
		limit := 200
		cursor := ""
		for {
			items, next, err := authStore.ListPrincipals(ctx, auth.AuthPrincipalListFilter{Limit: &limit, Cursor: cursor})
			if err != nil {
				return auth.AuthPrincipalSummary{}, err
			}
			for _, item := range items {
				if item.ActorID == actorID && !item.Revoked {
					return item, nil
				}
			}
			if next == "" {
				return auth.AuthPrincipalSummary{}, pm.ErrForbidden
			}
			cursor = next
		}
	}
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
		case "pm.access", "pm.read", "pm.propose", "pm.respond", "pm.delivery.reconcile":
			return nil
		case "pm.bind.target":
			if p.Human && actual.PrincipalKind != string(auth.PrincipalKindHuman) {
				return pm.ErrForbidden
			}
			return nil
		case "pm.approve", "pm.bind":
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
		}
		return pm.ErrForbidden
	}
	bridge := pm.NexusBridge{Store: store, ActorID: actors.SystemActorID}
	if cfg.BridgeEnabled {
		bridge.Ready = func(ctx context.Context, actorID, workspaceID string) error {
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
	deps.ReadContext = func(ctx context.Context, p pm.Principal, ref, query string, limit int) (pm.ContextPage, error) {
		if ref != "" {
			w, err := store.GetWork(ctx, ref)
			if err != nil {
				return pm.ContextPage{}, err
			}
			return pm.ContextPage{Items: []any{publicWork(w)}}, nil
		}
		page, err := store.ListWork(ctx, primitives.WorkListFilter{Query: query, Limit: limit})
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
		w, err := store.GetWork(ctx, ref)
		if err != nil {
			return "", err
		}
		source := workSourceMap(w)
		if anyString(source["authority"]) != "nexus" {
			revision := anyString(source["revision"])
			if revision == "" {
				return "", pm.ErrUnavailable
			}
			return revision, nil
		}
		v, _ := w["version"].(int64)
		return strconv.FormatInt(v, 10), nil
	}
	// The built-in action scope only changes Nexus-native local annotations.
	// Source workflows, shell commands, deployment and remote APIs are not tools
	// of this default runtime. Source integrations may supply their own executors.
	deps.Execute = func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
		if a.Scope != "work.annotate" {
			return pm.Receipt{}, pm.ErrForbidden
		}
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
	deps.Reconcile = func(ctx context.Context, a pm.Action) (pm.Receipt, error) {
		if a.Scope != "work.annotate" {
			return pm.Receipt{}, pm.ErrUnavailable
		}
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
	return handler, nil
}
func workSourceMap(w map[string]any) map[string]any {
	source, _ := w["source"].(map[string]any)
	return source
}
