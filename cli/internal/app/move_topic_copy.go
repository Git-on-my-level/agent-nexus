// Creating destination resources for a topic move: identity sets, moved
// documents and boards, and archiving the moved source card.

package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func moveResourceIdentitySet(object map[string]any, kind string) map[string]struct{} {
	values := map[string]struct{}{}
	for _, candidate := range []string{moveFieldString(object, "ref"), moveFieldString(object, "id"), moveFieldString(object, "handle")} {
		if candidate == "" {
			continue
		}
		values[moveNormalizeRef(candidate)] = struct{}{}
		if !strings.Contains(candidate, ":") {
			values[moveNormalizeRef(moveResourceRef(kind, candidate))] = struct{}{}
		}
	}
	return values
}

func moveRefInIdentitySet(ref string, values map[string]struct{}) bool {
	ref = moveNormalizeRef(ref)
	if _, ok := values[ref]; ok {
		return true
	}
	_, suffix, ok := strings.Cut(ref, ":")
	if !ok {
		return false
	}
	for candidate := range values {
		_, existingSuffix, existing := strings.Cut(moveNormalizeRef(candidate), ":")
		if existing && existingSuffix == suffix {
			return true
		}
	}
	return false
}

func (a *App) archiveMovedSourceCard(ctx context.Context, sourceCfg, destCfg config.Resolved, resource *topicMoveResource, moveID string, mapping map[string]string) error {
	current, err := moveRead(ctx, a, sourceCfg, "/work/"+url.PathEscape(resource.sourceRef), "work")
	if err != nil {
		return err
	}
	if err := moveHydrateCardPlan(ctx, a, sourceCfg, current); err != nil {
		return err
	}
	marker := asMap(current["workspace_move"])
	if moveState(current) == "archived" {
		return nil
	}
	if moveHasMarker(marker, moveID) {
		if err := moveValidateSnapshot(marker, sourceCfg.BaseURL, "card", resource.sourceRef, current, true); err != nil {
			return err
		}
		archiveVersion := moveInt64(marker["source_archive_version"])
		if archiveVersion == 0 || moveInt64(current["version"]) != archiveVersion {
			return errnorm.New(errnorm.KindRemote, "source_changed", "source card changed after the verified move journal; source remains active")
		}
		_, err = moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(resource.sourceRef)+"/archive", map[string]any{"if_version": archiveVersion})
		return err
	}
	if moveSourceRevision("card", current) != moveSourceRevision("card", resource.work) || moveSnapshotFingerprint("card", current) != moveSnapshotFingerprint("card", resource.work) {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card changed after snapshot; destination is verified and source remains active")
	}
	sourceMarker := moveMarker(moveID, "card", resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "complete")
	moveAttachSnapshot(sourceMarker, sourceCfg.BaseURL, "card", resource.sourceRef, resource.work)
	if authority := moveFieldString(asMap(resource.work["source"]), "authority"); authority == "" || authority == "nexus" {
		boardRef := moveRewriteRef(moveFieldString(resource.work, "board_ref"), mapping)
		markCuratedTombstone(sourceMarker, resource.destinationRef, moveURL(destCfg, "card", resource.destinationRef, boardRef))
	} else {
		sourceMarker["source_action"] = "archive"
	}
	version := moveInt64(current["version"])
	sourceMarker["phase"] = "source_transition"
	sourceMarker["source_archive_version"] = version + 1
	patched, err := a.updateMoveWorkMarkerAt(ctx, sourceCfg, resource.sourceRef, sourceMarker, &version)
	if err != nil {
		return err
	}
	archiveVersion := moveInt64(patched["version"])
	if archiveVersion != version+1 {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card revision changed while writing the completed move marker; source remains active")
	}
	_, err = moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(resource.sourceRef)+"/archive", map[string]any{"if_version": archiveVersion})
	return err
}

func (a *App) createMovedDocument(ctx context.Context, sourceCfg, destCfg config.Resolved, resource *topicMoveResource, moveID string, mapping map[string]string) (map[string]any, error) {
	doc := resource.object
	marker := resource.marker
	if marker == nil {
		marker = resourceMarker(moveID, resource, sourceCfg, destCfg)
	}
	docInput := map[string]any{
		"document_id": resource.destinationID,
		"thread_id":   resource.destinationThreadID,
		"title":       doc["title"], "summary": doc["summary"], "source": doc["source"],
		"tags": doc["tags"], "hosts": doc["hosts"], "verified_at": doc["verified_at"],
		"provenance": moveCopyProvenance(doc["provenance"]), "workspace_move": marker,
		"refs": moveRewriteRefs(doc["refs"], mapping),
	}
	if subject := moveFieldString(doc, "subject_ref"); subject != "" {
		mapped := moveRewriteRef(subject, mapping)
		if strings.HasPrefix(mapped, "thread:") {
			docInput["subject_ref"] = mapped
		}
	}
	revision := asMap(doc["revision"])
	contentType := moveFieldString(revision, "content_type")
	if contentType == "" {
		contentType = moveFieldString(asMap(revision["artifact"]), "content_type")
	}
	if contentType == "" {
		contentType = "text"
	}
	request := map[string]any{
		"request_key": moveID + ":document:" + resource.sourceRef,
		"document":    docInput, "content_type": contentType,
		"refs": moveRewriteRefs(revision["refs"], mapping),
	}
	if contentType == "binary" {
		request["content_base64"] = revision["content_base64"]
	} else {
		request["content"] = revision["content"]
	}
	return moveMapGet(ctx, a, destCfg, http.MethodPost, "/docs", request, "document")
}

func (a *App) createMovedBoard(ctx context.Context, cfg config.Resolved, resource *topicMoveResource, moveID, topicRef, topicURL string, mapping map[string]string) (map[string]any, error) {
	board := resource.object
	marker := resource.marker
	if marker == nil {
		marker = moveMarker(moveID, "board", resource.sourceRef, resource.sourceURL, cfg.BaseURL, resource.destinationRef, "pending")
	}
	refs := moveRewriteRefs(board["refs"], mapping)
	refs = moveUnique(append(refs, topicRef))
	boardInput := map[string]any{
		"id": resource.destinationID, "thread_id": resource.destinationThreadID, "title": board["title"], "summary": board["summary"],
		// Core derives primary_topic_ref from canonical typed refs. Sending the
		// projection field alone does not establish a board-topic relationship.
		"refs":          refs,
		"document_refs": moveRewriteRefs(board["document_refs"], mapping),
		"pinned_refs":   moveRewriteRefs(board["pinned_refs"], mapping),
		"column_schema": board["column_schema"],
		"provenance":    moveCopyProvenance(board["provenance"]), "workspace_move": marker,
	}
	body, err := moveCall(ctx, a, cfg, http.MethodPost, "/boards", map[string]any{"request_key": moveID + ":board:" + resource.sourceRef, "board": boardInput})
	if err != nil {
		return nil, err
	}
	if created := asMap(body["board"]); created != nil {
		return created, nil
	}
	return map[string]any{"ref": resource.destinationRef, "source_url": topicURL}, nil
}
