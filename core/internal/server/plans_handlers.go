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

// cardMutationResponse guards the complete envelope, including siblings of the
// card. Keyed writes can still roll back; already-committed writes keep their
// successful status but expose identities only if any projection/check fails.
func cardMutationResponse(ctx context.Context, r *http.Request, opts handlerOptions, payload map[string]any, requestKey string) (map[string]any, error) {
	view, err := projectCardMutationResponse(ctx, r, opts, payload)
	if err == nil {
		return view, nil
	}
	if strings.TrimSpace(requestKey) != "" {
		return nil, err
	}
	card, _ := payload["card"].(map[string]any)
	log.Printf("anx-core: card mutation response projection failed card_id=%s error_type=%T", anyString(card["id"]), err)
	echo := make(map[string]any)
	// Fail closed for the entire envelope. No content, placement, ancillary
	// metadata or future response object can escape via a sibling field.
	for _, key := range []string{"card", "board", "revision"} {
		record, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		identity := make(map[string]any)
		for _, field := range []string{"id", "ref", "version"} {
			if value, ok := record[field]; ok {
				identity[field] = value
			}
		}
		echo[key] = identity
	}
	return echo, nil
}

func cardMutationEcho(r *http.Request, opts handlerOptions, payload map[string]any) map[string]any {
	echo, _ := cardMutationResponse(r.Context(), r, opts, payload, "")
	return echo
}

func projectCardMutationResponse(ctx context.Context, r *http.Request, opts handlerOptions, payload map[string]any) (map[string]any, error) {
	for field := range payload {
		switch field {
		case "card", "board", "revision", "removed_thread_id", "markdown_hygiene":
		default:
			return nil, errors.New("unsupported card mutation response field")
		}
	}
	store, ok := opts.primitiveStore.(interface {
		BeginResourceRead(context.Context) (context.Context, func(), error)
		CheckCardMutationResponseAccess(context.Context, string, string, string) error
	})
	if !ok {
		return nil, errors.New("card mutation response scope unavailable")
	}
	// The card projection and sibling checks must use the same current read
	// boundary. In keyed mutations this borrows the existing write snapshot.
	ctx, closeRead, err := store.BeginResourceRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()
	card, _ := payload["card"].(map[string]any)
	cardID := anyString(card["id"])
	view, err := mutatedCardView(ctx, r, opts, cardID)
	if err != nil {
		return nil, err
	}
	board, _ := payload["board"].(map[string]any)
	revision, _ := payload["revision"].(map[string]any)
	for _, field := range []string{"board", "revision"} {
		if value, present := payload[field]; present {
			record, ok := value.(map[string]any)
			if !ok || anyString(record["id"]) == "" {
				return nil, errors.New("card mutation response sibling identity unavailable")
			}
		}
	}
	if err := store.CheckCardMutationResponseAccess(ctx, cardID, anyString(board["id"]), anyString(revision["id"])); err != nil {
		return nil, err
	}
	out := copyStringAnyMap(payload)
	out["card"] = view
	return out, nil
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
