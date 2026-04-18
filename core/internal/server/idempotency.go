package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

var errIdempotencyKeyMismatch = errors.New("idempotency key reuse does not match the original request body")

func readIdempotencyReplay(ctx context.Context, store PrimitiveStore, scope string, actorID string, requestKey string, requestPayload any) (int, map[string]any, bool, error) {
	requestKey = strings.TrimSpace(requestKey)
	if requestKey == "" {
		return 0, nil, false, nil
	}

	requestHash, err := hashJSONPayload(requestPayload)
	if err != nil {
		return 0, nil, false, err
	}

	replay, err := store.GetIdempotencyReplay(ctx, scope, actorID, requestKey)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return 0, nil, false, nil
		}
		return 0, nil, false, err
	}
	if replay.RequestHash != requestHash {
		return 0, nil, false, errIdempotencyKeyMismatch
	}
	return replay.Status, replay.Response, true, nil
}

func persistIdempotencyReplay(ctx context.Context, store PrimitiveStore, scope string, actorID string, requestKey string, requestPayload any, status int, response map[string]any) (int, map[string]any, error) {
	requestKey = strings.TrimSpace(requestKey)
	if requestKey == "" {
		return status, response, nil
	}

	requestHash, err := hashJSONPayload(requestPayload)
	if err != nil {
		return 0, nil, err
	}

	if err := store.PutIdempotencyReplay(ctx, scope, actorID, requestKey, requestHash, status, response); err != nil {
		if !errors.Is(err, primitives.ErrConflict) {
			return 0, nil, err
		}
		replay, replayErr := store.GetIdempotencyReplay(ctx, scope, actorID, requestKey)
		if replayErr != nil {
			return 0, nil, replayErr
		}
		if replay.RequestHash != requestHash {
			return 0, nil, errIdempotencyKeyMismatch
		}
		return replay.Status, replay.Response, nil
	}

	return status, response, nil
}

func deriveRequestScopedID(scope string, actorID string, requestKey string, label string) string {
	sum := sha256.Sum256([]byte(scope + "\n" + actorID + "\n" + requestKey + "\n" + label))
	return label + "-" + hex.EncodeToString(sum[:])[:20]
}

func hashJSONPayload(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func writeIdempotencyError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errIdempotencyKeyMismatch) {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return true
	}
	return false
}
