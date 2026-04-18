package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schedule"
	"agent-nexus-core/internal/schema"
)

const (
	defaultThreadContextMaxEvents    = 20
	threadContextContentPreviewChars = 500
)

// Backing threads are read-only on the public HTTP API (list, get, timeline, context, workspace).
// Archive, trash, restore, and purge use topic/board/card/document lifecycle routes instead.

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
		Status:          strings.TrimSpace(query.Get("status")),
		Priority:        strings.TrimSpace(query.Get("priority")),
		Tags:            tagsFilter,
		Cadences:        cadenceFilter,
		Stale:           staleFilter,
		Query:           strings.TrimSpace(query.Get("q")),
		Limit:           limitFilter,
		Cursor:          strings.TrimSpace(query.Get("cursor")),
		IncludeArchived: strings.TrimSpace(query.Get("include_archived")) == "true",
		ArchivedOnly:    strings.TrimSpace(query.Get("archived_only")) == "true",
		IncludeTrashed:  strings.TrimSpace(query.Get("include_trashed")) == "true",
		TrashedOnly:     strings.TrimSpace(query.Get("trashed_only")) == "true",
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
	states, err := loadTopicProjectionStates(r.Context(), opts, threadIDs)
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

type threadTimelineExpansion struct {
	Events            []map[string]any
	Artifacts         map[string]map[string]any
	Documents         map[string]map[string]any
	DocumentRevisions map[string]map[string]any
}

func expandThreadTimeline(ctx context.Context, opts handlerOptions, threadID string) (threadTimelineExpansion, error) {
	var out threadTimelineExpansion
	if _, err := opts.primitiveStore.GetThread(ctx, threadID); err != nil {
		return out, err
	}

	events, err := opts.primitiveStore.ListEventsByThread(ctx, threadID)
	if err != nil {
		return out, err
	}

	artifactIDs, documentIDs, documentRevisionIDs := collectTimelineReferencedObjectIDs(events)

	artifacts := make(map[string]map[string]any, len(artifactIDs))
	for _, artifactID := range artifactIDs {
		artifact, err := opts.primitiveStore.GetArtifact(ctx, artifactID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return out, err
		}
		artifacts[artifactID] = artifact
	}

	documents := make(map[string]map[string]any, len(documentIDs))
	for _, documentID := range documentIDs {
		document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return out, err
		}
		documents[documentID] = document
	}

	documentRevisions := make(map[string]map[string]any, len(documentRevisionIDs))
	for _, revisionID := range documentRevisionIDs {
		revision, err := opts.primitiveStore.GetDocumentRevisionByID(ctx, revisionID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return out, err
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
		document, _, err := opts.primitiveStore.GetDocument(ctx, documentID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				continue
			}
			return out, err
		}
		documents[documentID] = document
	}

	out.Events = events
	out.Artifacts = artifacts
	out.Documents = documents
	out.DocumentRevisions = documentRevisions
	return out, nil
}

func handleThreadTimeline(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	exp, err := expandThreadTimeline(r.Context(), opts, threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load thread timeline")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events":             exp.Events,
		"artifacts":          exp.Artifacts,
		"documents":          exp.Documents,
		"document_revisions": exp.DocumentRevisions,
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
	refs := []string{}
	if exists && rawRefs != nil {
		parsed, err := extractStringSlice(rawRefs)
		if err != nil {
			return nil, fmt.Errorf("thread.key_artifacts: %w", err)
		}
		refs = parsed
	}
	if len(refs) == 0 {
		threadID := strings.TrimSpace(anyString(thread["id"]))
		subjectRef := strings.TrimSpace(anyString(thread["subject_ref"]))
		candidates := make([][2]string, 0, 2)
		if threadID != "" {
			candidates = append(candidates, [2]string{"thread", threadID})
		}
		if subjectRef != "" {
			if prefix, id, err := schema.SplitTypedRef(subjectRef); err == nil && strings.TrimSpace(id) != "" {
				candidates = append(candidates, [2]string{prefix, id})
			}
		}
		if len(candidates) == 0 {
			return []map[string]any{}, nil
		}
		seenArtifactIDs := map[string]struct{}{}
		out := make([]map[string]any, 0)
		for _, candidate := range candidates {
			edges, err := opts.primitiveStore.ListRefEdgesByTarget(ctx, candidate[0]+":"+candidate[1], "ref")
			if err != nil {
				return nil, err
			}
			for _, edge := range edges {
				if edge.SourceRef == "" || !strings.HasPrefix(edge.SourceRef, "artifact:") {
					continue
				}
				artifactID := strings.TrimSpace(strings.TrimPrefix(edge.SourceRef, "artifact:"))
				if artifactID == "" {
					continue
				}
				if _, exists := seenArtifactIDs[artifactID]; exists {
					continue
				}
				artifact, err := opts.primitiveStore.GetArtifact(ctx, artifactID)
				if err != nil {
					if errors.Is(err, primitives.ErrNotFound) {
						continue
					}
					return nil, err
				}
				if strings.TrimSpace(anyString(artifact["document_id"])) != "" || strings.TrimSpace(anyString(artifact["revision_id"])) != "" {
					continue
				}
				seenArtifactIDs[artifactID] = struct{}{}
				item := map[string]any{
					"ref":      "artifact:" + artifactID,
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
				out = append(out, item)
			}
		}
		return out, nil
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

func collectTimelineReferencedObjectIDs(events []map[string]any) ([]string, []string, []string) {
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
			case "artifact":
				artifactSet[id] = struct{}{}
			case "document":
				documentSet[id] = struct{}{}
			case "document_revision":
				documentRevisionSet[id] = struct{}{}
			}
		}
	}

	artifactIDs := mapKeysSorted(artifactSet)
	documentIDs := mapKeysSorted(documentSet)
	documentRevisionIDs := mapKeysSorted(documentRevisionSet)
	return artifactIDs, documentIDs, documentRevisionIDs
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

func mapsByIDToSortedSlice(byID map[string]map[string]any) []map[string]any {
	if len(byID) == 0 {
		return nil
	}
	keys := make([]string, 0, len(byID))
	for k := range byID {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, byID[k])
	}
	return out
}
