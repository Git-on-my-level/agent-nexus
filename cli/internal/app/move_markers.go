// Move markers: the idempotency markers written on moved resources and their
// source objects, and existing-move detection.

package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func moveMarker(moveID, kind, sourceRef, sourceURL, destinationURL, destinationRef, status string) map[string]any {
	return map[string]any{
		"move_id":                   moveID,
		"resource_kind":             kind,
		"source_ref":                sourceRef,
		"source_url":                sourceURL,
		"destination_workspace_url": strings.TrimRight(destinationURL, "/"),
		"destination_ref":           destinationRef,
		"status":                    status,
		"phase":                     "snapshot",
	}
}

func markCuratedTombstone(marker map[string]any, destinationRef, destinationURL string) {
	marker["source_action"] = "tombstone"
	marker["tombstone"] = map[string]any{
		"destination_ref": destinationRef,
		"destination_url": destinationURL,
	}
}

func moveHasMarker(marker map[string]any, moveID string) bool {
	return strings.TrimSpace(anyString(marker["move_id"])) == moveID
}

func moveCheckExistingMarker(existing map[string]any, moveID string) error {
	marker := asMap(existing["workspace_move"])
	if len(marker) == 0 || moveHasMarker(marker, moveID) {
		return nil
	}
	status := anyString(marker["status"])
	if status == "pending" || status == "copied" || status == "archiving" {
		return errnorm.New(errnorm.KindRemote, "move_in_progress", "resource already has an unfinished workspace move")
	}
	return nil
}

func (a *App) updateMoveTopicMarker(ctx context.Context, cfg config.Resolved, ref string, marker map[string]any, patch map[string]any) (map[string]any, error) {
	return a.updateMoveTopicMarkerAt(ctx, cfg, ref, marker, patch, nil)
}

func (a *App) updateMoveTopicMarkerAt(ctx context.Context, cfg config.Resolved, ref string, marker map[string]any, patch map[string]any, expectedUpdatedAt *string) (map[string]any, error) {
	current, err := moveRead(ctx, a, cfg, "/topics/"+url.PathEscape(ref), "topic")
	if err != nil {
		return nil, err
	}
	if expectedUpdatedAt != nil && moveFieldString(current, "updated_at") != *expectedUpdatedAt {
		return nil, errnorm.New(errnorm.KindRemote, "source_changed", "source topic revision changed after snapshot; destination remains available for resume and source was not archived")
	}
	if err := moveCheckExistingMarker(current, anyString(marker["move_id"])); err != nil {
		return nil, err
	}
	patch["workspace_move"] = marker
	body, err := moveCall(ctx, a, cfg, http.MethodPatch, "/topics/"+url.PathEscape(ref), map[string]any{
		"if_updated_at": current["updated_at"], "patch": patch,
	})
	if err != nil {
		return nil, err
	}
	if topic := asMap(body["topic"]); topic != nil {
		return topic, nil
	}
	return current, nil
}

func (a *App) updateMoveWorkMarker(ctx context.Context, cfg config.Resolved, ref string, marker map[string]any) (map[string]any, error) {
	return a.updateMoveWorkMarkerAt(ctx, cfg, ref, marker, nil)
}

func (a *App) updateMoveWorkMarkerAt(ctx context.Context, cfg config.Resolved, ref string, marker map[string]any, expectedVersion *int64) (map[string]any, error) {
	current, err := moveRead(ctx, a, cfg, "/work/"+url.PathEscape(ref), "work")
	if err != nil {
		return nil, err
	}
	if expectedVersion != nil && moveInt64(current["version"]) != *expectedVersion {
		return nil, errnorm.New(errnorm.KindRemote, "source_changed", "source card revision changed after snapshot; destination remains available for resume and source was not archived")
	}
	if err := moveCheckExistingMarker(current, anyString(marker["move_id"])); err != nil {
		return nil, err
	}
	version := current["version"]
	body, err := moveCall(ctx, a, cfg, http.MethodPatch, "/work/"+url.PathEscape(ref), map[string]any{
		"if_version": version, "patch": map[string]any{"workspace_move": marker},
	})
	if err != nil {
		return nil, err
	}
	if work := asMap(body["work"]); work != nil {
		return work, nil
	}
	return current, nil
}

func resourceMarker(moveID string, resource *topicMoveResource, sourceCfg, destCfg config.Resolved) map[string]any {
	marker := moveMarker(moveID, resource.kind, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "pending")
	moveAttachSnapshot(marker, sourceCfg.BaseURL, resource.kind, resource.sourceRef, resource.object)
	marker["phase"] = "create_destination"
	return marker
}

func moveExistingAction(ctx context.Context, a *App, cfg config.Resolved, path, key, moveID string) (string, error) {
	var object map[string]any
	var err error
	if key == "document" {
		object, err = moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	} else {
		object, err = moveRead(ctx, a, cfg, path, key)
	}
	if err != nil {
		if errnorm.Normalize(err).Code == "not_found" {
			return "create", nil
		}
		return "", err
	}
	marker := asMap(object["workspace_move"])
	if key == "document" {
		marker = moveDocumentExistingMarker(object)
	} else if key == "board" {
		marker, err = moveBoardExistingMarker(ctx, a, cfg, object)
		if err != nil {
			return "", err
		}
	}
	if moveHasMarker(marker, moveID) {
		return "reuse", nil
	}
	return "", errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination resource id is already used by an unrelated resource")
}

func moveBoardExistingMarker(ctx context.Context, a *App, cfg config.Resolved, board map[string]any) (map[string]any, error) {
	threadID := moveFieldString(board, "thread_id")
	if threadID == "" {
		return nil, nil
	}
	timeline, err := moveCall(ctx, a, cfg, http.MethodGet, "/threads/"+url.PathEscape(threadID)+"/timeline", nil)
	if err != nil {
		return nil, err
	}
	for _, raw := range asSlice(timeline["events"]) {
		event := asMap(raw)
		if moveFieldString(event, "type") != "board_created" {
			continue
		}
		if marker := asMap(asMap(event["payload"])["workspace_move"]); len(marker) > 0 {
			return marker, nil
		}
	}
	return nil, nil
}

func moveDocumentExistingMarker(object map[string]any) map[string]any {
	return asMap(asMap(asMap(object["revision"])["artifact"])["workspace_move"])
}
