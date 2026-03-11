package server

import (
	"context"
	"encoding/json"
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

func handleCreateThread(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID    string         `json:"actor_id"`
		RequestKey string         `json:"request_key"`
		Thread     map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	if req.Thread == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread is required")
		return
	}
	if _, has := req.Thread["open_commitments"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread.open_commitments is core-maintained and cannot be set")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RequestKey) != "" && firstNonEmptyString(req.Thread["id"]) == "" {
		req.Thread["id"] = deriveRequestScopedID("threads.create", actorID, req.RequestKey, "thread")
	}
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, "threads.create", actorID, req.RequestKey, req)
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load idempotency replay")
		return
	}
	if replayed {
		writeJSON(w, replayStatus, replayPayload)
		return
	}
	if err := validateThreadCreate(opts.contract, req.Thread); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.CreateThread(r.Context(), actorID, req.Thread)
	if err != nil {
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			threadID := firstNonEmptyString(req.Thread["id"])
			thread, loadErr := opts.primitiveStore.GetThread(r.Context(), threadID)
			if loadErr == nil {
				response := map[string]any{"thread": thread}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "threads.create", actorID, req.RequestKey, req, http.StatusCreated, response)
				if writeIdempotencyError(w, replayErr) {
					return
				}
				if replayErr != nil {
					writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
					return
				}
				writeJSON(w, status, payload)
				return
			}
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "thread already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create thread")
		return
	}

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, "threads.create", actorID, req.RequestKey, req, http.StatusCreated, map[string]any{"thread": result.Snapshot})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

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

func handlePatchThread(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	var req struct {
		ActorID     string         `json:"actor_id"`
		Patch       map[string]any `json:"patch"`
		IfUpdatedAt *string        `json:"if_updated_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	if req.Patch == nil || len(req.Patch) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "patch is required")
		return
	}
	if req.IfUpdatedAt != nil {
		ifUpdatedAt := strings.TrimSpace(*req.IfUpdatedAt)
		if _, err := time.Parse(time.RFC3339, ifUpdatedAt); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "if_updated_at must be an RFC3339 datetime string")
			return
		}
		req.IfUpdatedAt = &ifUpdatedAt
	}
	if _, has := req.Patch["open_commitments"]; has {
		writeError(w, http.StatusBadRequest, "invalid_request", "thread.open_commitments is core-maintained and cannot be patched")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}

	if err := validateThreadPatch(opts.contract, req.Patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	result, err := opts.primitiveStore.PatchThread(r.Context(), actorID, threadID, req.Patch, req.IfUpdatedAt)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "thread not found")
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "thread has been updated; refresh and retry")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to patch thread")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"thread": result.Snapshot})
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

	threads, err := opts.primitiveStore.ListThreads(r.Context(), primitives.ThreadListFilter{
		Status:   strings.TrimSpace(query.Get("status")),
		Priority: strings.TrimSpace(query.Get("priority")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list threads")
		return
	}

	if len(tagsFilter) > 0 || len(cadenceFilter) > 0 {
		filtered := make([]map[string]any, 0, len(threads))
		for _, thread := range threads {
			if !threadMatchesTagsAndCadence(thread, tagsFilter, cadenceFilter) {
				continue
			}
			filtered = append(filtered, thread)
		}
		threads = filtered
	}

	events, err := opts.primitiveStore.ListEvents(r.Context(), primitives.EventListFilter{
		Types: []string{"receipt_added", "decision_made"},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to evaluate thread staleness")
		return
	}

	now := time.Now().UTC()
	staleByThread := stalenessByThread(threads, events, now)

	withStale := make([]map[string]any, 0, len(threads))
	for _, thread := range threads {
		threadID, _ := thread["id"].(string)
		stale := staleByThread[threadID]
		thread["stale"] = stale
		if staleFilter != nil && stale != *staleFilter {
			continue
		}
		withStale = append(withStale, thread)
	}
	threads = withStale

	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
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

	documents, err := opts.primitiveStore.ListDocuments(ctx, primitives.DocumentListFilter{
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

func validateThreadCreate(contract *schema.Contract, thread map[string]any) error {
	threadSchema, ok := contract.Snapshots["thread"]
	if !ok {
		return fmt.Errorf("thread schema is not loaded")
	}

	required := make([]string, 0)
	for name, field := range threadSchema.Fields {
		if field.Required && name != "open_commitments" {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	for _, name := range required {
		value, exists := thread[name]
		if !exists {
			return fmt.Errorf("thread.%s is required", name)
		}
		if err := validateThreadField(contract, name, value, true); err != nil {
			return err
		}
	}

	for name, value := range thread {
		if err := validateThreadField(contract, name, value, true); err != nil {
			return err
		}
	}

	return nil
}

func validateThreadPatch(contract *schema.Contract, patch map[string]any) error {
	for name, value := range patch {
		if err := validateThreadField(contract, name, value, false); err != nil {
			return err
		}
	}
	return nil
}

func validateThreadField(contract *schema.Contract, fieldName string, value any, createMode bool) error {
	threadSchema, ok := contract.Snapshots["thread"]
	if !ok {
		return fmt.Errorf("thread schema is not loaded")
	}
	field, known := threadSchema.Fields[fieldName]
	if !known {
		// Unknown fields are allowed and preserved by patch/merge semantics.
		return nil
	}

	switch field.Type {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("thread.%s must be a string", fieldName)
		}
		if createMode && field.Required && strings.TrimSpace(text) == "" {
			return fmt.Errorf("thread.%s must be non-empty", fieldName)
		}
		if fieldName == "cadence" {
			if err := schedule.ValidateCadence(text); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
		if strings.HasPrefix(field.Ref, "enums.") {
			enumName := strings.TrimPrefix(field.Ref, "enums.")
			if err := schema.ValidateEnum(contract, enumName, text); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
	case "datetime":
		if value == nil {
			return nil
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("thread.%s must be an RFC3339 datetime string", fieldName)
		}
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return fmt.Errorf("thread.%s must be an RFC3339 datetime string", fieldName)
		}
	case "list<string>":
		if _, err := extractStringSlice(value); err != nil {
			return fmt.Errorf("thread.%s must be a list of strings", fieldName)
		}
	case "list<typed_ref>":
		refs, err := extractStringSlice(value)
		if err != nil {
			return fmt.Errorf("thread.%s must be a list of strings", fieldName)
		}
		if err := schema.ValidateTypedRefs(contract, refs); err != nil {
			return fmt.Errorf("thread.%s: %w", fieldName, err)
		}
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("thread.%s must be an object", fieldName)
		}
		if field.Ref == "provenance" {
			if err := schema.ValidateProvenance(contract, obj); err != nil {
				return fmt.Errorf("thread.%s: %w", fieldName, err)
			}
		}
	}

	return nil
}
