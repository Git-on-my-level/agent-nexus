package server

import (
	"context"
	"errors"
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
	suggestions, suggestionsTruncated, err := store.CurationSuggestions(r.Context(), planVisibility(r, opts))
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": curationCandidatesJSON(candidates), "suggestions": suggestions, "suggestions_truncated": suggestionsTruncated, "limitations": limitations})
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

// handleCurationPropose records or resolves quiet agent work. The PM chooses among current
// formula candidates and supplies a rationale; new targets must match one,
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
		Action     string   `json:"action"`
		Targets    []string `json:"targets"`
		Rationale  string   `json:"rationale"`
		Resolution string   `json:"resolution"`
		Title      string   `json:"title"`
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
	if body.Resolution != "" && body.Resolution != "suggest" && body.Resolution != "apply" && body.Resolution != "dismiss" {
		writeError(w, 400, "invalid_request", "resolution must be suggest, apply or dismiss")
		return
	}
	if body.Resolution == "apply" || body.Resolution == "dismiss" {
		if principal.PrincipalKind != "agent" {
			writeError(w, 403, "forbidden", "curation resolution requires an agent principal")
			return
		}
		rec, err := store.ReadableCurationProposal(r.Context(), curation.Key(curation.Action(body.Action), body.Targets), planVisibility(r, opts))
		if err != nil {
			curationStoreError(w, r, err)
			return
		}
		if err = store.ResolveAgentCuration(r.Context(), principal.ActorID, rec, body.Resolution, body.Rationale); err != nil {
			curationStoreError(w, r, err)
			return
		}
		rec, err = store.ReadableCurationProposal(r.Context(), rec.Key, planVisibility(r, opts))
		if err != nil {
			curationStoreError(w, r, err)
			return
		}
		writeJSON(w, 201, map[string]any{"proposal": curationProposalJSON(rec, false)})
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

// handleCurationUngroup undoes an accepted batch: its members become separate
// decisions again. Agents or humans may undo this delivery grouping.
func handleCurationUngroup(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
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
	var body struct {
		Key string `json:"key"`
	}
	if !decodeJSONBody(w, r, &body) {
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" || len(body.Key) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_request", "key is required")
		return
	}
	rec, err := store.ReadableCurationProposal(r.Context(), body.Key, planVisibility(r, opts))
	if err == nil && rec.Action != string(curation.ActionBatch) {
		err = primitives.ErrNotFound
	}
	if err != nil {
		curationStoreError(w, r, err)
		return
	}
	removed, err := store.UngroupCurationBatch(r.Context(), rec.Key)
	if err != nil {
		curationStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ungrouped": removed})
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
	// The pass files as the system principal, exactly like the hourly loop.
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
			// A closed subject refuses new asks; the lifecycle sweep withdraws
			// asks on closed subjects itself, so there is nothing to propose.
			if errors.Is(err, primitives.ErrNotFound) || errors.Is(err, primitives.ErrInvalidWorkRequest) || errors.Is(err, primitives.ErrHumanAttentionAlreadyResponded) || errors.Is(err, primitives.ErrConflict) {
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
		"type":       "curation_suggested",
		"thread_id":  thread,
		"refs":       mergeUniqueSortedRefs(refs...),
		"summary":    c.Title,
		"provenance": eventProvenance(),
		"payload": map[string]any{
			"title": c.Title, "body": c.Body,
			"subject_ref":  c.SubjectRef,
			"related_refs": c.Targets,
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
	case rec.Status == primitives.CurationProposalAccepted:
		status = "accepted"
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
		// Metrics returns read/storage failures, not work mutation errors. Keep
		// contention classification, but retain the sanitized SQL cause for 500s.
		if !writeStoreContentionError(w, r, err) {
			writeInternalError(w, r, err, "work operation failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": metrics})
}
