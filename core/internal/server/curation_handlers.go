package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/curation"
	"agent-nexus-core/internal/primitives"
)

const (
	curationSourceFormula   = "formula"
	curationSourcePM        = "pm"
	curationMaxRationale    = 2000
	curationDefaultWindow   = 30
	curationMaxWindowDays   = 90
	curationFormulaInterval = time.Hour
)

func curationStore(w http.ResponseWriter, opts handlerOptions) (*primitives.Store, bool) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok || store == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "curation store is not configured")
		return nil, false
	}
	return store, true
}

func handleCurationCandidates(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}
	store, ok := curationStore(w, opts)
	if !ok {
		return
	}
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	candidates, limitations, err := store.CurationCandidates(r.Context(), planVisibility(r, opts), time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": curationCandidatesJSON(candidates), "limitations": limitations})
}

func curationCandidatesJSON(candidates []curation.Candidate) []map[string]any {
	out := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, map[string]any{
			"key": c.Key, "action": string(c.Action), "targets": c.Targets, "title": c.Title, "body": c.Body,
			"rationale": c.Rationale, "score": c.Score, "response_proposals": c.ResponseProposals,
		})
	}
	return out
}

// handleCurationPropose files a PM proposal. The PM chooses among the current
// formula candidates and supplies the rationale; the targets must match one,
// so a proposal never names anything the formula's privacy rule excludes.
func handleCurationPropose(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}
	store, ok := curationStore(w, opts)
	if !ok {
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	var body struct {
		Action    string   `json:"action"`
		Targets   []string `json:"targets"`
		Rationale string   `json:"rationale"`
		Title     string   `json:"title"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	body.Rationale = strings.TrimSpace(body.Rationale)
	body.Title = strings.Join(strings.Fields(body.Title), " ")
	if body.Rationale == "" || utf8.RuneCountInString(body.Rationale) > curationMaxRationale {
		writeError(w, http.StatusBadRequest, "invalid_request", "rationale must contain 1 to 2000 characters")
		return
	}
	if utf8.RuneCountInString(body.Title) > curation.MaxTitleRunes {
		writeError(w, http.StatusBadRequest, "invalid_request", "title must be at most 160 characters")
		return
	}
	switch curation.Action(body.Action) {
	case curation.ActionWithdraw, curation.ActionMerge, curation.ActionBatch:
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "action must be withdraw, merge or batch")
		return
	}
	if len(body.Targets) == 0 || len(body.Targets) > curation.MaxBatchMembers {
		writeError(w, http.StatusBadRequest, "invalid_request", "targets must contain 1 to 10 refs")
		return
	}
	key := curation.Key(curation.Action(body.Action), body.Targets)
	candidates, _, err := store.CurationCandidates(r.Context(), planVisibility(r, opts), time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	var match *curation.Candidate
	for i := range candidates {
		if candidates[i].Key == key {
			match = &candidates[i]
			break
		}
	}
	if match == nil {
		writeError(w, http.StatusNotFound, "not_found", "no current curation candidate matches this action and targets; run `anx curation candidates`")
		return
	}
	match.Rationale = body.Rationale
	match.Body = body.Rationale + "\n\n" + match.Body
	if body.Title != "" {
		match.Title = body.Title
	}
	rec, created, err := fileCurationProposal(r.Context(), store, principal.ActorID, *match, curationSourcePM)
	if err != nil {
		curationStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"proposal": curationProposalJSON(rec, created)})
}

func handleCurationRun(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}
	store, ok := curationStore(w, opts)
	if !ok {
		return
	}
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	proposals, skipped, err := RunCurationFormula(r.Context(), store)
	if err != nil {
		curationStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": proposals, "skipped": skipped})
}

// RunCurationFormula files every current formula candidate from the system
// principal. Candidates come from workspace-visible items only; the run never
// acts on anything.
func RunCurationFormula(ctx context.Context, store *primitives.Store) ([]map[string]any, int, error) {
	candidates, _, err := store.CurationCandidates(ctx, nil, time.Now().UTC())
	if err != nil {
		return nil, 0, err
	}
	proposals := []map[string]any{}
	skipped := 0
	for _, c := range candidates {
		rec, created, err := fileCurationProposal(ctx, store, actors.SystemActorID, c, curationSourceFormula)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) || errors.Is(err, primitives.ErrInvalidWorkRequest) {
				skipped++
				continue
			}
			return proposals, skipped, err
		}
		if !created {
			skipped++
			continue
		}
		proposals = append(proposals, curationProposalJSON(rec, true))
	}
	return proposals, skipped, nil
}

// RunCurationFormulaLoop runs the formula on a fixed interval so a workspace
// with no PM still gets proposals. Each pass is bounded by the candidate caps.
func RunCurationFormulaLoop(ctx context.Context, store *primitives.Store, report func(error)) {
	ticker := time.NewTicker(curationFormulaInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, _, err := RunCurationFormula(ctx, store); err != nil && report != nil {
				report(err)
			}
		}
	}
}

func fileCurationProposal(ctx context.Context, store *primitives.Store, actor string, c curation.Candidate, source string) (primitives.CurationProposalRecord, bool, error) {
	thread := c.ThreadID
	if thread == "" && strings.HasPrefix(c.SubjectRef, "card:") {
		var err error
		if thread, err = store.CurationCardThread(ctx, c.SubjectRef); err != nil {
			return primitives.CurationProposalRecord{}, false, err
		}
	}
	if thread == "" {
		return primitives.CurationProposalRecord{}, false, primitives.ErrNotFound
	}
	refs := []string{"thread:" + thread}
	if c.SubjectRef != "" {
		refs = append(refs, c.SubjectRef)
	}
	refs = append(refs, c.Targets...)
	event := map[string]any{
		"type":       humanAttentionRequestedEventType,
		"thread_id":  thread,
		"refs":       mergeUniqueSortedRefs(refs...),
		"summary":    c.Title,
		"provenance": eventProvenance(),
		"payload": map[string]any{
			"kind": "ask", "title": c.Title, "body": c.Body,
			"subject_ref":        c.SubjectRef,
			"related_refs":       c.Targets,
			"requester_actor_id": actor,
			"response_proposals": c.ResponseProposals,
			"curation": map[string]any{
				"action": string(c.Action), "targets": c.Targets, "key": c.Key,
				"source": source, "rationale": c.Rationale,
			},
		},
	}
	return store.FileCurationProposal(ctx, actor, c, source, event)
}

func curationProposalJSON(rec primitives.CurationProposalRecord, created bool) map[string]any {
	status := "existing"
	switch {
	case created:
		status = "created"
	case rec.Status == primitives.CurationProposalDismissed:
		status = "dismissed"
	}
	return map[string]any{"key": rec.Key, "status": status, "action": rec.Action, "targets": rec.Targets, "event_id": rec.ProposalEventID, "source": rec.Source}
}

func curationStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, primitives.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "curation target not found")
		return
	}
	workStoreError(w, r, err)
}

func handleCurationMetrics(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}
	store, ok := curationStore(w, opts)
	if !ok {
		return
	}
	if _, ok := requireAuthenticatedPrincipal(w, r, opts); !ok {
		return
	}
	days := curationDefaultWindow
	if raw := strings.TrimSpace(r.URL.Query().Get("window_days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > curationMaxWindowDays {
			writeError(w, http.StatusBadRequest, "invalid_request", "window_days must be 1 to 90")
			return
		}
		days = n
	}
	metrics, err := store.CurationMetrics(r.Context(), time.Duration(days)*24*time.Hour, time.Now().UTC())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics})
}

// applyCurationAnswer runs after a human answer to a proposal ask commits.
// The first response proposal accepts and performs the action as the
// answering human; anything else dismisses. The outcome, including a failure,
// lands as a trail note in the proposal thread with what undo needs.
func applyCurationAnswer(ctx context.Context, opts handlerOptions, responder string, human bool, proposalEventID, threadID string, payload map[string]any, responseText, outcome string) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok || store == nil || proposalEventID == "" {
		return
	}
	rec, err := store.CurationProposalByEvent(ctx, proposalEventID)
	if err != nil {
		return
	}
	proposals := stringSliceAny(payload["response_proposals"])
	accepted := len(proposals) > 0 && outcome != "needs_context" && strings.EqualFold(strings.TrimSpace(responseText), strings.TrimSpace(proposals[0]))
	if !accepted {
		if _, err := store.ResolveCurationProposal(ctx, rec.Key, primitives.CurationProposalDismissed); err != nil {
			log.Printf("curation dismiss %s: %v", rec.Key, err)
		}
		return
	}
	moved, err := store.ResolveCurationProposal(ctx, rec.Key, primitives.CurationProposalAccepted)
	if err != nil || !moved {
		return
	}
	trail := map[string]any{"proposal_event_id": proposalEventID, "action": rec.Action, "targets": rec.Targets, "applied_by": responder}
	text := ""
	if !human {
		// I4: only a human answer performs the action.
		trail["result"], text = "skipped", "Curation proposal accepted by a non-human principal; nothing was changed."
	} else {
		undo, summary, err := store.ApplyCurationAction(ctx, responder, proposalEventID, rec)
		if err != nil {
			trail["result"], trail["error"] = "failed", err.Error()
			text = "Curation proposal accepted but not applied: " + curationFailureText(err)
		} else {
			trail["result"], text = "applied", summary
			if undo != nil {
				trail["undo"] = undo
			}
		}
	}
	if _, err := store.AppendEvent(ctx, responder, map[string]any{
		"type": "message_posted", "thread_id": threadID, "summary": text,
		"refs":       mergeUniqueSortedRefs(append([]string{"thread:" + threadID, "event:" + proposalEventID}, rec.Targets...)...),
		"provenance": eventProvenance(),
		"payload":    map[string]any{"text": text, "curation_applied": trail},
	}); err != nil {
		log.Printf("curation trail %s: %v", rec.Key, err)
	}
}

func curationFailureText(err error) string {
	switch {
	case errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded):
		return "the ask was already resolved."
	case errors.Is(err, primitives.ErrNotFound), errors.Is(err, primitives.ErrCurationTargetGone):
		return "a target no longer exists."
	case errors.Is(err, primitives.ErrForbidden):
		return "the action is not allowed for this target."
	}
	return "the action failed; try again from the target."
}
