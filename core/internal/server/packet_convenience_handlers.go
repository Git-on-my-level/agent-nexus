package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func handleCreateReceipt(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	createPacketArtifactAndEvent(w, r, opts, packetCreateRequest{
		PacketKind: "receipt",
		EventType:  "receipt_added",
		Summary:    "receipt added",
		EventRefs: func(artifactID string, threadID string, subjectRef string, packet map[string]any) []string {
			return []string{
				"artifact:" + artifactID,
				subjectRef,
			}
		},
	})
}

func handleCreateReview(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	createPacketArtifactAndEvent(w, r, opts, packetCreateRequest{
		PacketKind: "review",
		EventType:  "review_completed",
		Summary:    "review completed",
		EventRefs: func(artifactID string, threadID string, subjectRef string, packet map[string]any) []string {
			return []string{
				"artifact:" + artifactID,
				packetArtifactLinkRef(packet, "receipt_ref", "receipt_id"),
				subjectRef,
			}
		},
	})
}

type packetCreateRequest struct {
	PacketKind string
	EventType  string
	Summary    string
	EventRefs  func(artifactID string, threadID string, subjectRef string, packet map[string]any) []string
}

func createPacketArtifactAndEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, request packetCreateRequest) {
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
		Artifact   map[string]any `json:"artifact"`
		Packet     map[string]any `json:"packet"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Artifact == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "artifact is required")
		return
	}
	if req.Packet == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "packet is required")
		return
	}

	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	if rawKind, hasKind := req.Artifact["kind"]; hasKind {
		kindText, ok := rawKind.(string)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "artifact.kind must be a string")
			return
		}
		if strings.TrimSpace(kindText) != request.PacketKind {
			writeError(w, http.StatusBadRequest, "invalid_request", "artifact.kind must be "+request.PacketKind)
			return
		}
	}
	req.Artifact["kind"] = request.PacketKind
	scope := "packets." + request.PacketKind + ".create"
	packetIDLabel := "artifact-" + strings.ReplaceAll(request.PacketKind, "_", "-")
	idField, hasIDField := packetIDFieldName(request.PacketKind)
	if !hasIDField {
		writeError(w, http.StatusBadRequest, "invalid_request", "packet id rule is not defined")
		return
	}
	artifactID := firstNonEmptyString(req.Artifact["id"])
	if artifactID == "" {
		if strings.TrimSpace(req.RequestKey) != "" {
			artifactID = deriveRequestScopedID(scope, actorID, req.RequestKey, packetIDLabel)
		} else {
			artifactID = uuid.NewString()
		}
		req.Artifact["id"] = artifactID
	}
	if firstNonEmptyString(req.Packet[idField]) == "" {
		req.Packet[idField] = artifactID
	}
	if request.PacketKind == "review" {
		if firstNonEmptyString(req.Packet["receipt_ref"]) == "" {
			if receiptID := firstNonEmptyString(req.Packet["receipt_id"]); receiptID != "" {
				req.Packet["receipt_ref"] = "artifact:" + receiptID
			}
		}
	}
	artifactRefs, err := extractStringSlice(req.Artifact["refs"])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "artifact.refs must be a list of strings")
		return
	}
	artifactRefs = append(artifactRefs, "artifact:"+artifactID)
	if subjectRef := firstNonEmptyString(req.Packet["subject_ref"]); subjectRef != "" {
		artifactRefs = append(artifactRefs, subjectRef)
	}
	if receiptRef := packetArtifactLinkRef(req.Packet, "receipt_ref", "receipt_id"); receiptRef != "" {
		artifactRefs = append(artifactRefs, receiptRef)
	}
	req.Artifact["refs"] = uniqueStringRefs(artifactRefs)
	replayStatus, replayPayload, replayed, err := readIdempotencyReplay(r.Context(), opts.primitiveStore, scope, actorID, req.RequestKey, req)
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

	subjectRef, err := validatePacketArtifactAndContent(opts.contract, request.PacketKind, req.Artifact, req.Packet)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	threadID, err := resolveSubjectRefThreadID(r.Context(), opts, subjectRef)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve packet subject")
		return
	}

	eventRefs := request.EventRefs(artifactID, threadID, subjectRef, req.Packet)
	eventRefs = uniqueStringRefs(eventRefs)

	eventPayload := map[string]any{
		"subject_ref": subjectRef,
	}
	if receiptRef := packetArtifactLinkRef(req.Packet, "receipt_ref", "receipt_id"); receiptRef != "" {
		eventPayload["receipt_ref"] = receiptRef
	}
	event := map[string]any{
		"type":       request.EventType,
		"thread_id":  threadID,
		"refs":       eventRefs,
		"summary":    request.Summary,
		"payload":    eventPayload,
		"provenance": actorStatementProvenance(),
	}
	if strings.TrimSpace(req.RequestKey) != "" {
		event["id"] = deriveRequestScopedID(scope, actorID, req.RequestKey, packetIDLabel+"-event")
	}

	artifact, storedEvent, err := opts.primitiveStore.CreateArtifactAndEvent(r.Context(), actorID, req.Artifact, req.Packet, "structured", event)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrConflict) && strings.TrimSpace(req.RequestKey) != "" {
			existingArtifact, artifactErr := opts.primitiveStore.GetArtifact(r.Context(), artifactID)
			existingEvent, eventErr := opts.primitiveStore.GetEvent(r.Context(), firstNonEmptyString(event["id"]))
			if artifactErr == nil && eventErr == nil {
				response := map[string]any{
					"artifact": existingArtifact,
					"event":    existingEvent,
				}
				status, payload, replayErr := persistIdempotencyReplay(r.Context(), opts.primitiveStore, scope, actorID, req.RequestKey, req, http.StatusCreated, response)
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
		if errors.Is(err, primitives.ErrInvalidArtifactID) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if errors.Is(err, primitives.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "packet already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create packet artifact and event")
		return
	}
	enqueueTopicProjectionsBestEffort(r.Context(), opts, []string{threadID}, time.Now().UTC())

	status, payload, err := persistIdempotencyReplay(r.Context(), opts.primitiveStore, scope, actorID, req.RequestKey, req, http.StatusCreated, map[string]any{
		"artifact": artifact,
		"event":    storedEvent,
	})
	if writeIdempotencyError(w, err) {
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist idempotency replay")
		return
	}
	writeJSON(w, status, payload)
}

func resolveSubjectRefThreadID(ctx context.Context, opts handlerOptions, subjectRef string) (string, error) {
	subjectRef = strings.TrimSpace(subjectRef)
	prefix, value, err := schema.SplitTypedRef(subjectRef)
	if err != nil {
		return "", err
	}

	switch prefix {
	case "thread":
		thread, err := opts.primitiveStore.GetThread(ctx, value)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", invalidPacketSubjectRef(subjectRef)
			}
			return "", err
		}
		return strings.TrimSpace(anyString(thread["id"])), nil
	case "document":
		document, _, err := opts.primitiveStore.GetDocument(ctx, value)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", invalidPacketSubjectRef(subjectRef)
			}
			return "", err
		}
		threadID := documentBackingThreadID(document)
		if threadID == "" {
			return "", invalidPacketSubjectRef(subjectRef)
		}
		return threadID, nil
	case "topic":
		topic, err := opts.primitiveStore.GetTopic(ctx, value)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", invalidPacketSubjectRef(subjectRef)
			}
			return "", err
		}
		threadID := topicPrimaryThreadID(topic)
		if threadID == "" {
			return "", invalidPacketSubjectRef(subjectRef)
		}
		return threadID, nil
	case "board":
		board, err := opts.primitiveStore.GetBoard(ctx, value)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", invalidPacketSubjectRef(subjectRef)
			}
			return "", err
		}
		threadID := strings.TrimSpace(anyString(board["thread_id"]))
		if threadID == "" {
			return "", invalidPacketSubjectRef(subjectRef)
		}
		return threadID, nil
	case "card":
		card, err := opts.primitiveStore.GetBoardCard(ctx, "", value)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				return "", invalidPacketSubjectRef(subjectRef)
			}
			return "", err
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID == "" {
			return "", invalidPacketSubjectRef(subjectRef)
		}
		return threadID, nil
	default:
		return "", invalidPacketSubjectRef(subjectRef)
	}
}

func invalidPacketSubjectRef(subjectRef string) error {
	return errors.New("packet.subject_ref does not resolve to a backing thread: " + strings.TrimSpace(subjectRef))
}

func uniqueStringRefs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
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
