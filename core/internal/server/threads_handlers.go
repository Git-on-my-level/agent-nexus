package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schedule"
	"organization-autorunner-core/internal/schema"
)

const (
	defaultThreadContextMaxEvents    = 20
	threadContextContentPreviewChars = 500
)

func handleGetThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handleListThreads(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	query := r.URL.Query()
	tagsFilter := normalizedQueryValues(query["tag"])
	cadenceFilter := normalizedQueryValues(query["cadence"])
	var staleFilter *bool
	staleRaw := strings.TrimSpace(query.Get("stale"))
	if staleRaw != "" {
		parsed, err := strconv.ParseBool(staleRaw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "stale must be true or false")
			return
		}
		staleFilter = &parsed
	}

	var limitFilter *int
	limitRaw := strings.TrimSpace(query.Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 1 || parsed > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 1000")
			return
		}
		limitFilter = &parsed
	}

	threads, nextCursor, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{
		Status:            strings.TrimSpace(query.Get("status")),
		Priority:          strings.TrimSpace(query.Get("priority")),
		Tags:              tagsFilter,
		Cadences:          cadenceFilter,
		Stale:             staleFilter,
		Query:             strings.TrimSpace(query.Get("q")),
		Limit:             limitFilter,
		Cursor:            strings.TrimSpace(query.Get("cursor")),
		IncludeArchived:   strings.TrimSpace(query.Get("include_archived")) == "true",
		ArchivedOnly:      strings.TrimSpace(query.Get("archived_only")) == "true",
		IncludeTombstoned: strings.TrimSpace(query.Get("include_tombstoned")) == "true",
		TombstonedOnly:    strings.TrimSpace(query.Get("tombstoned_only")) == "true",
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list threads")
		return
	}

	threadIDs := make([]string, 0, len(threads))
	for _, thread := range threads {
		threadIDs = append(threadIDs, anyString(thread["id"]))
	}
	states, err := loadThreadProjectionStates(r.Context(), opts, threadIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread projection status")
		return
	}

	withStale := make([]map[string]any, 0, len(threads))
	for _, thread := range threads {
		threadID, _ := thread["id"].(string)
		state := states[threadID]
		stale := state.Projection.Stale
		thread["stale"] = stale
		thread["projection_freshness"] = cloneWorkspaceMap(state.Freshness)
		if staleFilter != nil && stale != *staleFilter {
			continue
		}
		withStale = append(withStale, thread)
	}
	threads = withStale

	response := map[string]any{"threads": threads}
	if nextCursor != "" {
		response["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, response)
}

func writeThreadLifecycleStoreError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, primitives.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "thread not found")
		return true
	case errors.Is(err, primitives.ErrNotTombstoned):
		writeError(w, http.StatusConflict, "not_tombstoned", "thread is not currently tombstoned")
		return true
	case errors.Is(err, primitives.ErrNotArchived):
		writeError(w, http.StatusConflict, "not_archived", "thread is not archived")
		return true
	case errors.Is(err, primitives.ErrAlreadyTombstoned):
		writeError(w, http.StatusConflict, "already_tombstoned", "thread is tombstoned")
		return true
	default:
		msg := err.Error()
		if strings.Contains(msg, "actor_id is required") || strings.Contains(msg, "thread_id is required") {
			writeError(w, http.StatusBadRequest, "invalid_request", msg)
			return true
		}
		return false
	}
}

func handleArchiveThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	thread, err := opts.primitiveStore.ArchiveThread(r.Context(), actorID, threadID)
	if err != nil {
		if writeThreadLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to archive thread")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handleUnarchiveThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	thread, err := opts.primitiveStore.UnarchiveThread(r.Context(), actorID, threadID)
	if err != nil {
		if writeThreadLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to unarchive thread")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handleTombstoneThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
		Reason  string `json:"reason"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	thread, err := opts.primitiveStore.TombstoneThread(r.Context(), actorID, threadID, req.Reason)
	if err != nil {
		if writeThreadLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to tombstone thread")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handleRestoreThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		ActorID string `json:"actor_id"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	thread, err := opts.primitiveStore.RestoreThread(r.Context(), actorID, threadID)
	if err != nil {
		if writeThreadLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to restore thread")
		return
	}
	enqueueThreadProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]any{"thread": thread})
}

func handlePurgeThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete threads")
		return
	}
	if err := opts.primitiveStore.PurgeThread(r.Context(), threadID); err != nil {
		if writeThreadLifecycleStoreError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to purge thread")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "thread_id": threadID})
}

func normalizedQueryValues(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func threadMatchesTagsAndCadence(thread map[string]any, tags []string, cadences []string) bool {
	if len(tags) > 0 {
		threadTags, err := extractStringSlice(thread["tags"])
		if err != nil {
			return false
		}
		for _, wantedTag := range tags {
			if !containsStringValue(threadTags, wantedTag) {
				return false
			}
		}
	}

	if len(cadences) > 0 {
		threadCadence, _ := thread["cadence"].(string)
		matchedCadence := false
		for _, cadenceFilter := range cadences {
			if schedule.CadenceMatchesFilter(threadCadence, cadenceFilter) {
				matchedCadence = true
				break
			}
		}
		if !matchedCadence {
			return false
		}
	}

	return true
}

func containsStringValue(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func handleThreadTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	if _, err := opts.primitiveStore.GetThread(r.Context(), threadID); err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread")
		return
	}

	events, err := opts.primitiveStore.ListEventsByThread(r.Context(), threadID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread timeline")
		return
	}

	snapshotIDs, artifactIDs, documentIDs, documentRevisionIDs := collectTimelineReferencedObjectIDs(events)

	snapshots := make(map[string]map[string]any, len(snapshotIDs))
	for _, snapshotID := range snapshotIDs {
		snapshot, err := opts.primitiveStore.GetSnapshot(r.Context(), snapshotID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced snapshots")
			return
		}
		snapshots[snapshotID] = snapshot
	}

	artifacts := make(map[string]map[string]any, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifact, err := opts.primitiveStore.GetArtifact(r.Context(), artifactID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced artifacts")
			return
		}
		artifacts[artifactID] = artifact
	}

	documents := make(map[string]map[string]any, len(documentIDs))
	for _, documentID := range documentIDs {
		document, _, err := opts.primitiveStore.GetDocument(r.Context(), documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced documents")
			return
		}
		documents[documentID] = document
	}

	documentRevisions := make(map[string]map[string]any, len(documentRevisionIDs))
	for _, revisionID := range documentRevisionIDs {
		revision, err := opts.primitiveStore.GetDocumentRevisionByID(r.Context(), revisionID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced document revisions")
			return
		}
		documentRevisions[revisionID] = revision

		documentID, _ := revision["document_id"].(string)
		documentID = strings.TrimSpace(documentID)
		if documentID == "" {
			continue
		}
		if _, exists := documents[documentID]; exists {
			continue
		}
		document, _, err := opts.primitiveStore.GetDocument(r.Context(), documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load referenced documents")
			return
		}
		documents[documentID] = document
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events":             events,
		"snapshots":          snapshots,
		"artifacts":          artifacts,
		"documents":          documents,
		"document_revisions": documentRevisions,
	})
}

func handleThreadContext(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	options, ok := resolveThreadContextOptions(w, r)
	if !ok {
		return
	}

	body, err := buildThreadContextPayload(r.Context(), opts, threadID, options)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread")
		return
	}

	writeJSON(w, http.StatusOK, body)
}

func buildThreadContextArtifacts(ctx context.Context, opts handlerOptions, thread map[string]any, includeArtifactContent bool) ([]map[string]any, error) {
	rawRefs, exists := thread["key_artifacts"]
	if !exists || rawRefs == nil {
		return []map[string]any{}, nil
	}
	refs, err := extractStringSlice(rawRefs)
	if err != nil {
		return nil, fmt.Errorf("thread.key_artifacts: %w", err)
	}
	if len(refs) == 0 {
		return []map[string]any{}, nil
	}

	artifacts := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		prefix, artifactID, err := schema.SplitTypedRef(ref)
		if err != nil || prefix != "artifact" {
			continue
		}

		artifact, err := opts.primitiveStore.GetArtifact(ctx, artifactID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return nil, err
		}

		item := map[string]any{
			"ref":      ref,
			"artifact": artifact,
		}

		if includeArtifactContent {
			content, _, err := opts.primitiveStore.GetArtifactContent(ctx, artifactID)
			if err != nil {
				if !errors.Is(err, primitives.ErrNotFound) {
					return nil, err
				}
			} else if preview := artifactContentPreview(content); preview != "" {
				item["content_preview"] = preview
			}
		}

		artifacts = append(artifacts, item)
	}
	return artifacts, nil
}

func buildThreadContextOpenCommitments(ctx context.Context, opts handlerOptions, threadID string, thread map[string]any) ([]map[string]any, error) {
	rawOpenCommitments, exists := thread["open_commitments"]
	if !exists || rawOpenCommitments == nil {
		return []map[string]any{}, nil
	}
	openCommitmentIDs, err := extractStringSlice(rawOpenCommitments)
	if err != nil {
		return nil, fmt.Errorf("thread.open_commitments: %w", err)
	}
	if len(openCommitmentIDs) == 0 {
		return []map[string]any{}, nil
	}

	commitments, err := opts.primitiveStore.ListCommitments(ctx, primitives.CommitmentListFilter{ThreadID: threadID})
	if err != nil {
		return nil, err
	}

	commitmentsByID := make(map[string]map[string]any, len(commitments))
	for _, commitment := range commitments {
		commitmentID, _ := commitment["id"].(string)
		commitmentID = strings.TrimSpace(commitmentID)
		if commitmentID == "" {
			continue
		}
		commitmentsByID[commitmentID] = commitment
	}

	ordered := make([]map[string]any, 0, len(openCommitmentIDs))
	for _, commitmentID := range openCommitmentIDs {
		commitmentID = strings.TrimSpace(commitmentID)
		if commitmentID == "" {
			continue
		}
		commitment, ok := commitmentsByID[commitmentID]
		if !ok {
			continue
		}
		ordered = append(ordered, commitment)
	}
	return ordered, nil
}

func buildThreadContextDocuments(ctx context.Context, opts handlerOptions, threadID string) ([]map[string]any, error) {
	if strings.TrimSpace(threadID) == "" {
		return []map[string]any{}, nil
	}

	documents, _, err := opts.primitiveStore.ListDocuments(ctx, primitives.DocumentListFilter{
		ThreadID: threadID,
	})
	if err != nil {
		return nil, err
	}
	if len(documents) == 0 {
		return []map[string]any{}, nil
	}
	return documents, nil
}

func artifactContentPreview(content []byte) string {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= threadContextContentPreviewChars {
		return text
	}
	runes := []rune(text)
	return string(runes[:threadContextContentPreviewChars])
}

func collectTimelineReferencedObjectIDs(events []map[string]any) ([]string, []string, []string, []string) {
	snapshotSet := make(map[string]struct{})
	artifactSet := make(map[string]struct{})
	documentSet := make(map[string]struct{})
	documentRevisionSet := make(map[string]struct{})

	for _, event := range events {
		refs, err := extractStringSlice(event["refs"])
		if err != nil {
			continue
		}
		for _, ref := range refs {
			prefix, id, err := schema.SplitTypedRef(ref)
			if err != nil {
				continue
			}
			switch prefix {
			case "snapshot":
				snapshotSet[id] = struct{}{}
			case "artifact":
				artifactSet[id] = struct{}{}
			case "document":
				documentSet[id] = struct{}{}
			case "document_revision":
				documentRevisionSet[id] = struct{}{}
			}
		}
	}

	snapshotIDs := mapKeysSorted(snapshotSet)
	artifactIDs := mapKeysSorted(artifactSet)
	documentIDs := mapKeysSorted(documentSet)
	documentRevisionIDs := mapKeysSorted(documentRevisionSet)
	return snapshotIDs, artifactIDs, documentIDs, documentRevisionIDs
}

func mapKeysSorted(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
