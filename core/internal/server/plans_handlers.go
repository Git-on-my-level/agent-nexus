package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

type planStore interface {
	SetCardPlan(context.Context, string, string, string, plans.Plan) error
	EnrichCardPlans(context.Context, []map[string]any, func(string, string) bool, time.Time) error
	ResolveRefs(context.Context, []string, func(string, string) bool, time.Time) ([]primitives.RefPreview, error)
}

func planVisibility(r *http.Request, opts handlerOptions) func(string, string) bool {
	return func(threadID, privateOwner string) bool {
		return canAccessPMThread(r, opts, map[string]any{"id": threadID, "pm_actor_id": privateOwner})
	}
}

func filterPlanCards(w http.ResponseWriter, r *http.Request, opts handlerOptions, cards []map[string]any) ([]map[string]any, bool) {
	store, ok := opts.primitiveStore.(interface {
		FilterCardAccess(context.Context, []map[string]any, func(string, string) bool) ([]map[string]any, error)
	})
	if !ok {
		return cards, true
	}
	out, err := store.FilterCardAccess(r.Context(), cards, planVisibility(r, opts))
	if err != nil {
		workStoreError(w, r, err)
		return nil, false
	}
	return out, true
}

func requirePlanCardAccess(w http.ResponseWriter, r *http.Request, opts handlerOptions, card map[string]any) bool {
	cards, ok := filterPlanCards(w, r, opts, []map[string]any{card})
	if !ok {
		return false
	}
	if len(cards) == 0 {
		denyPMNotFound(w, "card")
		return false
	}
	return true
}

func enrichPlans(w http.ResponseWriter, r *http.Request, opts handlerOptions, cards []map[string]any) bool {
	if err := enrichCardsWithContext(r.Context(), r, opts, cards); err != nil {
		workStoreError(w, r, err)
		return false
	}
	if r.URL.Query().Get("summary") == "1" {
		for _, card := range cards {
			card["summary_format"] = true
		}
	}
	return true
}

// enrichCardsWithContext is the projection core shared by card reads and
// mutation echoes: plan enrichment plus the work-hierarchy grouping fields
// (parent_ref, rollup). ctx may carry a mutation transaction; its snapshot is
// then borrowed so the echo matches what a read of the same commit returns.
func enrichCardsWithContext(ctx context.Context, r *http.Request, opts handlerOptions, cards []map[string]any) error {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		return nil
	}
	viewer, human := workRequestViewer(r)
	ctx = primitives.WithWorkHierarchyViewer(ctx, viewer, human)
	if r.URL.Query().Get("summary") != "1" && !strings.HasSuffix(r.URL.Path, "/plan") {
		ctx = primitives.WithLegacyCardPlans(ctx)
	}
	return store.EnrichCardPlans(ctx, cards, planVisibility(r.WithContext(ctx), opts), time.Now().UTC())
}

// mutatedCardView projects a mutated card through the same projection as
// GET /cards/{id}, so mutation responses report the same grouping fields a
// fresh read returns. cardID is the canonical card id from the store result.
func mutatedCardView(ctx context.Context, r *http.Request, opts handlerOptions, cardID string) (map[string]any, error) {
	card, err := opts.primitiveStore.GetBoardCardCurrent(ctx, "", cardID)
	if err != nil {
		return nil, err
	}
	if err := enrichCardsWithContext(ctx, r, opts, []map[string]any{card}); err != nil {
		return nil, err
	}
	return publicCardView(card), nil
}

// cardMutationTarget deliberately carries identities only. Mutation maps and
// response extras cannot be passed to the response constructor.
type cardMutationTarget struct {
	CardID, BoardID, RevisionID string
	Creation                    bool
}

func cardMutationReplayTarget(payload map[string]any) cardMutationTarget {
	card, _ := payload["card"].(map[string]any)
	board, _ := payload["board"].(map[string]any)
	revision, _ := payload["revision"].(map[string]any)
	return cardMutationTarget{CardID: anyString(card["id"]), BoardID: anyString(board["id"]), RevisionID: anyString(revision["id"])}
}

func mutationReference(kind, id string) map[string]any {
	return map[string]any{"id": id, "ref": kind + ":" + id}
}

// cardMutationResponse constructs every business field from current scoped
// reads. An already-committed write succeeds even when a read fails; only that
// object is reduced to its known identity. Keyed writes can still roll back.
func cardMutationResponse(ctx context.Context, r *http.Request, opts handlerOptions, target cardMutationTarget, requestKey string) (map[string]any, error) {
	response, err := projectCardMutationResponse(ctx, r, opts, target)
	if err != nil {
		if strings.TrimSpace(requestKey) != "" {
			return nil, err
		}
		log.Printf("anx-core: card mutation response projection failed card_id=%s error_type=%T", target.CardID, err)
	}
	return response, nil
}

func cardMutationEcho(r *http.Request, opts handlerOptions, target cardMutationTarget) map[string]any {
	response, _ := cardMutationResponse(r.Context(), r, opts, target, "")
	return response
}

func projectCardMutationResponse(ctx context.Context, r *http.Request, opts handlerOptions, target cardMutationTarget) (map[string]any, error) {
	out := map[string]any{"card": mutationReference("card", target.CardID)}
	if target.BoardID != "" {
		out["board"] = mutationReference("board", target.BoardID)
	}
	if target.RevisionID != "" {
		out["revision"] = mutationReference("card_revision", target.RevisionID)
	}
	store, ok := opts.primitiveStore.(interface {
		BeginResourceRead(context.Context) (context.Context, func(), error)
		GetBoardMutationReference(context.Context, string) (map[string]any, error)
		GetCardRevisionMutationReference(context.Context, string, string) (map[string]any, error)
	})
	if !ok {
		return out, errors.New("card mutation response scope unavailable")
	}
	ctx, closeRead, err := store.BeginResourceRead(ctx)
	if err != nil {
		return out, err
	}
	defer closeRead()
	var failures []error
	view, err := mutatedCardView(ctx, r, opts, target.CardID)
	if err != nil {
		failures = append(failures, err)
	} else {
		out["card"] = view
		if target.Creation && anyString(view["parent_ref"]) == "" {
			hints, supported := opts.primitiveStore.(interface {
				GetCardCreationHints(context.Context, string) ([]primitives.LikelyHome, error)
			})
			if supported {
				homes, hintErr := hints.GetCardCreationHints(ctx, target.CardID)
				if hintErr != nil {
					failures = append(failures, hintErr)
				} else if len(homes) > 0 {
					out["likely_home"] = homes
					out["next_actions"] = primitives.GroupingNextActions(anyString(view["ref"]), homes, "group")
				}
			}
		}
	}
	// Full GET board projections materialize membership; revision GET resolves
	// historical content. Mutation receipts use bounded reference projections
	// instead, with no thread, plan, membership, placement or content fields.
	if target.BoardID != "" {
		reference, readErr := store.GetBoardMutationReference(ctx, target.BoardID)
		if readErr != nil {
			failures = append(failures, readErr)
		} else {
			out["board"] = reference
		}
	}
	if target.RevisionID != "" {
		reference, readErr := store.GetCardRevisionMutationReference(ctx, target.CardID, target.RevisionID)
		if readErr != nil {
			failures = append(failures, readErr)
		} else {
			out["revision"] = reference
		}
	}
	return out, errors.Join(failures...)
}

const summaryCandidateLimit = 50

// Bound the complete enrichment input, not just individual SQL statements.
// Bundle callers deduplicate and order first so the selected preview is stable.
func summaryCandidates(cards []map[string]any) ([]map[string]any, bool) {
	if len(cards) > summaryCandidateLimit {
		return cards[:summaryCandidateLimit], true
	}
	return cards, false
}

func handleCardPlan(w http.ResponseWriter, r *http.Request, opts handlerOptions, identifier string) {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		writeError(w, 503, "unavailable", "plans are unavailable")
		return
	}
	id, ok := resolveHTTPResourceID(w, r, opts, "card", identifier, "card")
	if !ok {
		return
	}
	card, err := opts.primitiveStore.GetBoardCard(r.Context(), "", id)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	if !requirePlanCardAccess(w, r, opts, card) {
		return
	}
	if r.Method == http.MethodPut {
		var req struct {
			Plan        json.RawMessage `json:"plan"`
			IfUpdatedAt string          `json:"if_updated_at"`
			ActorID     string          `json:"actor_id"`
			RequestKey  string          `json:"request_key"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		var p plans.Plan
		decoder := json.NewDecoder(strings.NewReader(string(req.Plan)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil || string(req.Plan) == "null" {
			writeError(w, 400, "invalid_request", "plan must be an object containing steps")
			return
		}
		if _, err := time.Parse(time.RFC3339Nano, req.IfUpdatedAt); err != nil {
			writeError(w, 400, "invalid_request", "if_updated_at must be the timestamp from plan show or cards get")
			return
		}
		actor, ok := resolveWriteActorID(w, r, opts, req.ActorID)
		if !ok {
			return
		}
		status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "plan.set", actor, req.RequestKey, struct {
			ID   string
			Body any
		}{id, req}, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
			// SetCardPlan's nested transaction writes the graph, card/board tokens,
			// work version and lifecycle event. Enrichment borrows this same write
			// snapshot, so both the edit and its original receipt commit together.
			if err := store.SetCardPlan(ctx, actor, id, req.IfUpdatedAt, p); err != nil {
				return nil, err
			}
			updated, err := opts.primitiveStore.GetBoardCard(ctx, "", id)
			if err != nil {
				return nil, err
			}
			if err := store.EnrichCardPlans(ctx, []map[string]any{updated}, planVisibility(r.WithContext(ctx), opts), time.Now().UTC()); err != nil {
				return nil, err
			}
			return cardPlanResponse(updated), nil
		})
		if err != nil {
			workStoreError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, status, response)
		return
	}
	if !enrichPlans(w, r, opts, []map[string]any{card}) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, cardPlanResponse(card))
}

func cardPlanResponse(card map[string]any) map[string]any {
	return map[string]any{"summary": card["work_summary"], "card_ref": card["ref"], "if_updated_at": card["updated_at"], "plan": card["plan"], "plan_state": card["plan_state"], "plan_health": card["plan_health"], "next_step": card["next_step"], "status_mismatch": card["status_mismatch"], "possibly_moot": card["possibly_moot"]}
}

func handleResolveRefs(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		writeError(w, 503, "unavailable", "ref resolution is unavailable")
		return
	}
	var req struct {
		Refs []string `json:"refs"`
	}
	if !decodeJSONReadBody(w, r, &req) {
		return
	}
	for _, ref := range req.Refs {
		if len(ref) > 2048 {
			writeError(w, 400, "invalid_request", "refs must be at most 2048 bytes each")
			return
		}
	}
	items, err := store.ResolveRefs(r.Context(), req.Refs, planVisibility(r, opts), time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}

func scopedThreadVisible(ctx context.Context, opts handlerOptions, id string) bool {
	if opts.readVisibility == nil || id == "" {
		return true
	}
	thread, err := opts.primitiveStore.GetThread(ctx, id)
	if err != nil {
		return errors.Is(err, primitives.ErrNotFound)
	}
	return opts.readVisibility(id, anyString(thread["pm_actor_id"]))
}
func scopedCards(ctx context.Context, opts handlerOptions, cards []map[string]any) ([]map[string]any, error) {
	if opts.readVisibility == nil {
		return cards, nil
	}
	if store, ok := opts.primitiveStore.(interface {
		FilterCardAccess(context.Context, []map[string]any, func(string, string) bool) ([]map[string]any, error)
	}); ok {
		return store.FilterCardAccess(ctx, cards, opts.readVisibility)
	}
	return cards, nil
}
