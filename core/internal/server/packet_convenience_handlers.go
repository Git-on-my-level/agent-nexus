package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
)

func handleCreateWorkOrder(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	createPacketArtifactAndEvent(w, r, opts, packetCreateRequest{
		PacketKind: "work_order",
		EventType:  "work_order_created",
		Summary:    "work order created",
		EventRefs: func(artifactID string, threadID string, packet map[string]any) []string {
			return []string{
				"artifact:" + artifactID,
				"thread:" + threadID,
			}
		},
	})
}

func handleCreateReceipt(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	createPacketArtifactAndEvent(w, r, opts, packetCreateRequest{
		PacketKind: "receipt",
		EventType:  "receipt_added",
		Summary:    "receipt added",
		EventRefs: func(artifactID string, threadID string, packet map[string]any) []string {
			workOrderID, _ := packet["work_order_id"].(string)
			return []string{
				"artifact:" + artifactID,
				"artifact:" + strings.TrimSpace(workOrderID),
				"thread:" + threadID,
			}
		},
	})
}

func handleCreateReview(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	createPacketArtifactAndEvent(w, r, opts, packetCreateRequest{
		PacketKind: "review",
		EventType:  "review_completed",
		Summary:    "review completed",
		EventRefs: func(artifactID string, threadID string, packet map[string]any) []string {
			workOrderID, _ := packet["work_order_id"].(string)
			receiptID, _ := packet["receipt_id"].(string)
			return []string{
				"artifact:" + artifactID,
				"artifact:" + strings.TrimSpace(receiptID),
				"artifact:" + strings.TrimSpace(workOrderID),
				"thread:" + threadID,
			}
		},
	})
}

type packetCreateRequest struct {
	PacketKind string
	EventType  string
	Summary    string
	EventRefs  func(artifactID string, threadID string, packet map[string]any) []string
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
		ActorID  string         `json:"actor_id"`
		Artifact map[string]any `json:"artifact"`
		Packet   map[string]any `json:"packet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
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

	actorID, ok := requireRegisteredActorID(w, r, opts.actorRegistry, req.ActorID)
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

	threadID, err := validatePacketArtifactAndContent(opts.contract, request.PacketKind, req.Artifact, req.Packet)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	artifact, err := opts.primitiveStore.CreateArtifact(r.Context(), actorID, req.Artifact, req.Packet, "structured")
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidArtifactID) {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create artifact")
		return
	}
	artifactID, _ := artifact["id"].(string)

	eventRefs := request.EventRefs(artifactID, threadID, req.Packet)
	eventRefs = uniqueStringRefs(eventRefs)

	event, err := opts.primitiveStore.AppendEvent(r.Context(), actorID, map[string]any{
		"type":       request.EventType,
		"thread_id":  threadID,
		"refs":       eventRefs,
		"summary":    request.Summary,
		"payload":    map[string]any{},
		"provenance": actorStatementProvenance(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to append event")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"artifact": artifact,
		"event":    event,
	})
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
