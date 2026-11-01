// The cross-workspace topic move: readTopicMoveResources loads what will be
// copied and moveTopic copies the topic and its resources, rewriting refs.

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type topicMoveResource struct {
	kind, sourceRef, sourceURL, destinationID, destinationRef, destinationThreadID string
	object                                                                         map[string]any
	work                                                                           map[string]any
	marker                                                                         map[string]any
	problem                                                                        string
	destinationConnection                                                          string
}

func (a *App) moveTopic(ctx context.Context, sourceCfg, destCfg config.Resolved, requestedRef string, dryRun bool, connectionMappings map[string]string) (*commandResult, error) {
	sourceTopic, err := moveRead(ctx, a, sourceCfg, "/topics/"+url.PathEscape(requestedRef), "topic")
	if err != nil {
		return nil, err
	}
	sourceRef := firstNonEmpty(moveFieldString(sourceTopic, "ref"), requestedRef)
	sourceURL := moveURL(sourceCfg, "topic", sourceRef, "")
	moveID := moveStableID(sourceCfg.BaseURL, sourceRef, destCfg.BaseURL)
	initialMarker := asMap(sourceTopic["workspace_move"])
	if moveState(sourceTopic) == "archived" {
		if moveHasMarker(initialMarker, moveID) && anyString(initialMarker["status"]) == "complete" {
			return movePlanResult("topic", moveID, sourceRef, sourceURL, destCfg.BaseURL, anyString(initialMarker["destination_ref"]), []map[string]any{}, dryRun), nil
		}
		return nil, errnorm.New(errnorm.KindRemote, "source_archived", "archived source topic has no completed matching move marker")
	}
	workspaceBody, err := moveCall(ctx, a, sourceCfg, http.MethodGet, "/topics/"+url.PathEscape(sourceRef)+"/workspace", nil)
	if err != nil {
		return nil, err
	}
	workspace := workspaceBody
	if topic := asMap(workspaceBody["topic"]); topic != nil {
		sourceTopic = topic
	}
	sourceMarker := asMap(sourceTopic["workspace_move"])
	if len(sourceMarker) > 0 && !moveHasMarker(sourceMarker, moveID) {
		if err := moveCheckExistingMarker(sourceTopic, moveID); err != nil {
			return nil, err
		}
	}
	if moveState(sourceTopic) == "archived" {
		if moveHasMarker(sourceMarker, moveID) && anyString(sourceMarker["status"]) == "complete" {
			return movePlanResult("topic", moveID, sourceRef, sourceURL, destCfg.BaseURL, anyString(sourceMarker["destination_ref"]), []map[string]any{}, dryRun), nil
		}
		return nil, errnorm.New(errnorm.KindRemote, "source_archived", "archived source topic has no completed matching move marker")
	}
	topicMarker := moveMarker(moveID, "topic", sourceRef, sourceURL, destCfg.BaseURL, "", "pending")
	moveAttachSnapshot(topicMarker, sourceCfg.BaseURL, "topic", sourceRef, sourceTopic)
	isResuming := moveHasMarker(sourceMarker, moveID)
	if isResuming {
		if err := moveValidateSnapshot(sourceMarker, sourceCfg.BaseURL, "topic", sourceRef, sourceTopic, true); err != nil {
			return nil, err
		}
		topicMarker = sourceMarker
	}

	resources, err := a.readTopicMoveResources(ctx, sourceCfg, destCfg, sourceTopic, workspace, moveID)
	if err != nil {
		return nil, err
	}
	topicID := moveDeterministicUUID(moveID, "topic", sourceRef)
	topicThreadID := moveDeterministicUUID(moveID, "topic_thread", sourceRef)
	destinationTopicRef := moveResourceRef("topic", topicID)
	mapping := map[string]string{}
	moveAddIdentity(mapping, sourceTopic, "topic", destinationTopicRef)
	mapping[moveResourceRef("thread", moveFieldString(sourceTopic, "thread_id"))] = moveResourceRef("thread", topicThreadID)
	for _, resource := range resources {
		if resource.problem != "" {
			continue
		}
		resource.destinationID = moveDeterministicUUID(moveID, resource.kind, resource.sourceRef)
		resource.destinationRef = moveResourceRef(resource.kind, resource.destinationID)
		resource.marker = resourceMarker(moveID, resource, sourceCfg, destCfg)
		savedDestinationConnection := ""
		if isResuming {
			saved := moveManifestResource(topicMarker["resources"], resource.kind, resource.sourceRef)
			if saved == nil {
				return nil, errnorm.New(errnorm.KindRemote, "move_journal_incomplete", fmt.Sprintf("move journal has no snapshot entry for source %s %s", resource.kind, resource.sourceRef))
			}
			if moveFieldString(saved, "source_fingerprint") != moveSnapshotFingerprint(resource.kind, resource.object) {
				return nil, errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s changed after snapshot; destination remains available for resume and source was not archived", resource.kind, resource.sourceRef))
			}
			if moveFieldString(saved, "source_revision") != moveSourceRevision(resource.kind, resource.object) {
				savedSourceMarker := asMap(resource.object["workspace_move"])
				ownCardTransition := resource.kind == "card" && moveHasMarker(savedSourceMarker, moveID)
				ownArchiveTransition := (resource.kind == "document" || resource.kind == "board") && moveState(resource.object) == "archived"
				if !ownCardTransition && !ownArchiveTransition {
					return nil, errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s revision changed after snapshot; destination remains available for resume and source was not archived", resource.kind, resource.sourceRef))
				}
				if ownCardTransition {
					if err := moveValidateSnapshot(savedSourceMarker, sourceCfg.BaseURL, "card", resource.sourceRef, resource.object, true); err != nil {
						return nil, err
					}
				} else if moveFieldString(saved, "source_fingerprint") != moveSnapshotFingerprint(resource.kind, resource.object) {
					return nil, errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("archived source %s %s no longer matches the snapshotted content", resource.kind, resource.sourceRef))
				}
			}
			resource.destinationRef = moveFieldString(saved, "destination_ref")
			resource.destinationID = firstNonEmpty(moveFieldString(saved, "destination_id"), moveRefID(resource.destinationRef))
			savedDestinationConnection = moveFieldString(saved, "destination_connection_id")
			if savedDestinationConnection != "" {
				resource.marker["destination_connection_id"] = savedDestinationConnection
			}
			resource.marker["source_revision"] = saved["source_revision"]
			resource.marker["source_fingerprint"] = saved["source_fingerprint"]
			resource.marker["journal_key"] = saved["journal_key"]
		}
		if resource.kind == "document" {
			resource.destinationThreadID = moveDeterministicUUID(moveID, "document_thread", resource.sourceRef)
			if sourceThreadID := moveFieldString(resource.object, "thread_id"); sourceThreadID != "" {
				mapping[moveResourceRef("thread", sourceThreadID)] = moveResourceRef("thread", resource.destinationThreadID)
			}
		}
		if resource.kind == "board" {
			resource.destinationThreadID = moveDeterministicUUID(moveID, "board_thread", resource.sourceRef)
			if sourceThreadID := moveFieldString(resource.object, "thread_id"); sourceThreadID != "" {
				mapping[moveResourceRef("thread", sourceThreadID)] = moveResourceRef("thread", resource.destinationThreadID)
			}
		}
		if resource.kind == "card" {
			identity := asMap(resource.work["source"])
			if authority := moveFieldString(identity, "authority"); authority != "" && authority != "nexus" {
				resource.destinationConnection = strings.TrimSpace(connectionMappings[moveFieldString(identity, "connection_id")])
				if resource.destinationConnection == "" {
					resource.problem = "source-backed card requires --connection-map <source-connection-id>=<destination-connection-id>"
					continue
				}
				if savedDestinationConnection != "" && savedDestinationConnection != resource.destinationConnection {
					resource.problem = "destination connection mapping differs from the source move journal"
					continue
				}
				resource.marker["destination_connection_id"] = resource.destinationConnection
				destinationIdentity := cloneMoveMap(identity)
				destinationIdentity["connection_id"] = resource.destinationConnection
				if existing, findErr := a.findMoveExternalWork(ctx, destCfg, destinationIdentity); findErr != nil {
					return nil, findErr
				} else if existing != nil {
					if !moveHasMarker(asMap(existing["workspace_move"]), moveID) {
						resource.problem = "destination already contains the mapped source identity but it belongs to another move"
						continue
					}
					resource.destinationRef = moveFieldString(existing, "ref")
					resource.destinationID = moveRefID(resource.destinationRef)
				}
			}
		}
		moveAddIdentity(mapping, resource.object, resource.kind, resource.destinationRef)
		if resource.kind == "card" {
			moveAddIdentity(mapping, resource.work, resource.kind, resource.destinationRef)
		}
	}
	blocked := false
	for _, resource := range resources {
		blocked = blocked || resource.problem != ""
	}
	if blocked && !dryRun {
		for _, resource := range resources {
			if resource.problem != "" {
				return nil, errnorm.New(errnorm.KindRemote, "move_source_unavailable", fmt.Sprintf("cannot move topic: %s %s: %s; source resources were left unchanged", resource.kind, resource.sourceRef, resource.problem))
			}
		}
	}

	if !isResuming {
		manifest := make([]any, 0, len(resources))
		for _, resource := range resources {
			item := map[string]any{
				"kind": resource.kind, "source_ref": resource.sourceRef, "source_url": resource.sourceURL,
				"destination_ref": resource.destinationRef, "destination_id": resource.destinationID,
			}
			if resource.problem != "" {
				item["unavailable"] = resource.problem
			} else {
				moveAttachSnapshot(item, sourceCfg.BaseURL, resource.kind, resource.sourceRef, resource.object)
			}
			if resource.destinationConnection != "" {
				item["destination_connection_id"] = resource.destinationConnection
			}
			manifest = append(manifest, item)
		}
		topicMarker["resources"] = manifest
	}
	topicMarker["destination_ref"] = destinationTopicRef
	topicMarker["source_owner_refs"] = moveStringList(sourceTopic["owner_refs"])
	moveSetPhase(topicMarker, "snapshot")

	rootAction, err := moveExistingAction(ctx, a, destCfg, "/topics/"+url.PathEscape(topicID), "topic", moveID)
	if err != nil {
		return nil, err
	}
	if rootAction == "reuse" {
		existing, err := moveRead(ctx, a, destCfg, "/topics/"+url.PathEscape(topicID), "topic")
		if err != nil {
			return nil, err
		}
		if err := moveValidateSnapshot(asMap(existing["workspace_move"]), sourceCfg.BaseURL, "topic", sourceRef, sourceTopic, isResuming); err != nil {
			return nil, err
		}
	}

	if dryRun {
		actions := make([]map[string]any, 0, len(resources)*2+3)
		actions = append(actions, map[string]any{"action": rootAction, "kind": "topic", "source_ref": sourceRef, "destination_ref": destinationTopicRef})
		for _, boardRef := range moveStringList(sourceTopic["board_refs"]) {
			actions = append(actions, map[string]any{"action": "preserve", "kind": "board", "ref": boardRef, "reason": "shared topic context is not part of the transfer set"})
		}
		for _, resource := range resources {
			if resource.problem != "" {
				actions = append(actions, map[string]any{"action": "fail", "kind": resource.kind, "source_ref": resource.sourceRef, "reason": resource.problem})
				continue
			}
			action, err := moveExistingAction(ctx, a, destCfg, "/"+map[string]string{"document": "docs", "board": "boards", "card": "work"}[resource.kind]+"/"+url.PathEscape(resource.destinationID), map[string]string{"document": "document", "board": "board", "card": "work"}[resource.kind], moveID)
			if err != nil {
				return nil, err
			}
			if resource.kind == "card" {
				identity := asMap(resource.work["source"])
				if authority := moveFieldString(identity, "authority"); authority != "" && authority != "nexus" {
					action = "resync"
					if existing, exists, getErr := moveGetWorkIfExists(ctx, a, destCfg, resource.destinationRef); getErr != nil {
						return nil, getErr
					} else if exists && !moveHasMarker(asMap(existing["workspace_move"]), moveID) {
						return nil, errnorm.New(errnorm.KindRemote, "destination_source_identity_conflict", "destination already contains the mapped source identity but it belongs to another move")
					}
				} else if action == "reuse" {
					existing, getErr := moveRead(ctx, a, destCfg, "/work/"+url.PathEscape(resource.destinationRef), "work")
					if getErr != nil {
						return nil, getErr
					}
					if !moveHasMarker(asMap(existing["workspace_move"]), moveID) {
						return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card id is already used by an unrelated card")
					}
				}
			}
			moveAction := map[string]any{"action": action, "kind": resource.kind, "source_ref": resource.sourceRef, "destination_ref": resource.destinationRef}
			if resource.destinationConnection != "" {
				moveAction["destination_connection_id"] = resource.destinationConnection
			}
			actions = append(actions, moveAction)
		}
		if blocked {
			actions = append(actions, map[string]any{"action": "blocked", "kind": "source_set", "reason": "one or more linked resources are inaccessible or lack an explicit connection mapping; no source resources will be archived"})
		} else {
			for _, resource := range resources {
				action := "archive"
				if resource.kind == "card" && (moveFieldString(asMap(resource.work["source"]), "authority") == "" || moveFieldString(asMap(resource.work["source"]), "authority") == "nexus") {
					action = "tombstone"
				}
				actions = append(actions, map[string]any{"action": action, "kind": resource.kind, "ref": resource.sourceRef, "url": resource.sourceURL})
			}
			actions = append(actions, map[string]any{"action": "archive", "kind": "topic", "ref": sourceRef, "url": sourceURL})
		}
		return movePlanResult("topic", moveID, sourceRef, sourceURL, destCfg.BaseURL, destinationTopicRef, actions, true), nil
	}
	if !isResuming {
		persisted, persistErr := a.updateMoveTopicMarkerAt(ctx, sourceCfg, sourceRef, topicMarker, map[string]any{}, stringPointer(moveFieldString(sourceTopic, "updated_at")))
		if persistErr != nil {
			return nil, persistErr
		}
		sourceMarker = asMap(persisted["workspace_move"])
		topicMarker = sourceMarker
		isResuming = true
	}

	topicCreateMarker := cloneMoveMap(topicMarker)
	moveSetPhase(topicCreateMarker, "create_destination")
	topicInput := map[string]any{
		"id": topicID, "thread_id": topicThreadID,
		"title": sourceTopic["title"], "summary": sourceTopic["summary"],
		"owner_refs":    []string{},
		"document_refs": []string{}, "board_refs": []string{}, "related_refs": []string{},
		"provenance": moveCopyProvenance(sourceTopic["provenance"]), "workspace_move": topicCreateMarker,
	}
	var destTopic map[string]any
	if rootAction == "reuse" {
		// The request key is bound to the exact body on first use. Once a
		// journal advances, its marker changes, so a resume reuses the object
		// already written instead of replaying a changed body under that key.
		destTopic, err = moveRead(ctx, a, destCfg, "/topics/"+url.PathEscape(destinationTopicRef), "topic")
	} else {
		var topicBody map[string]any
		topicBody, err = moveCall(ctx, a, destCfg, http.MethodPost, "/topics", map[string]any{"request_key": moveID + ":topic", "topic": topicInput})
		if err == nil {
			destTopic = asMap(topicBody["topic"])
			if destTopic == nil {
				destTopic, err = moveRead(ctx, a, destCfg, "/topics/"+url.PathEscape(destinationTopicRef), "topic")
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if !moveHasMarker(asMap(destTopic["workspace_move"]), moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination topic id is used by a topic without the expected move journal")
	}
	if err := moveValidateSnapshot(asMap(destTopic["workspace_move"]), sourceCfg.BaseURL, "topic", sourceRef, sourceTopic, isResuming); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.problem == "" && resource.kind == "document" {
			if _, err := a.createMovedDocument(ctx, sourceCfg, destCfg, resource, moveID, mapping); err != nil {
				return nil, err
			}
		}
	}
	for _, resource := range resources {
		if resource.problem == "" && resource.kind == "card" {
			input, err := moveCardCreateInput(resource.work, moveID, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, mapping, resource.destinationConnection)
			if err != nil {
				return nil, err
			}
			if board := moveFieldString(resource.work, "board_ref"); board != "" {
				if mapped := moveRewriteRef(board, mapping); mapped != board {
					input["board_ref"] = mapped
				}
			}
			cardMarker := cloneMoveMap(resource.marker)
			cardMarker["phase"] = "create_destination"
			input["workspace_move"] = cardMarker
			destWork, exists, err := moveGetWorkIfExists(ctx, a, destCfg, resource.destinationRef)
			if err != nil {
				return nil, err
			}
			authority := moveFieldString(asMap(resource.work["source"]), "authority")
			if exists && !moveHasMarker(asMap(destWork["workspace_move"]), moveID) {
				return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card id is already used by an unrelated card")
			}
			if exists {
				if err := moveValidateSnapshot(asMap(destWork["workspace_move"]), sourceCfg.BaseURL, "card", resource.sourceRef, resource.work, isResuming); err != nil {
					return nil, err
				}
			}
			if !exists || authority != "" && authority != "nexus" {
				body, err := moveCall(ctx, a, destCfg, http.MethodPost, "/work", input)
				if err != nil {
					return nil, err
				}
				destWork = asMap(body["work"])
			}
			if err := moveWriteCardPlan(ctx, a, destCfg, resource.destinationRef, moveRewritePlan(resource.work["plan"], mapping)); err != nil {
				return nil, err
			}
		}
	}

	documentRefs := moveRewriteRefs(sourceTopic["document_refs"], mapping)
	boardRefs := []string{}
	relatedRefs := moveRewriteRefs(sourceTopic["related_refs"], mapping)
	moveSetPhase(topicMarker, "refs_rewritten")
	if _, err := a.updateMoveTopicMarker(ctx, destCfg, destinationTopicRef, topicMarker, map[string]any{
		"owner_refs":    []string{},
		"document_refs": documentRefs, "board_refs": boardRefs, "related_refs": relatedRefs,
	}); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.problem != "" {
			continue
		}
		if err := a.verifyMovedResource(ctx, sourceCfg, destCfg, resource, moveID, mapping, sourceRef, sourceURL); err != nil {
			return nil, err
		}
	}
	if err := a.verifyMovedTopic(ctx, destCfg, destinationTopicRef, sourceTopic, moveID, sourceRef, sourceURL, mapping); err != nil {
		return nil, err
	}
	moveSetPhase(topicMarker, "verified")
	topicMarker["status"] = "verified"
	if _, err := a.updateMoveTopicMarker(ctx, destCfg, destinationTopicRef, topicMarker, map[string]any{}); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.problem == "" && resource.kind == "card" {
			destMarker := asMap(asMap(resource.work["workspace_move"]))
			if destMarker == nil {
				destMarker = moveMarker(moveID, resource.kind, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "verified")
				moveAttachSnapshot(destMarker, sourceCfg.BaseURL, resource.kind, resource.sourceRef, resource.work)
			}
			moveSetPhase(destMarker, "verified")
			destMarker["status"] = "verified"
			if _, err := a.updateMoveWorkMarker(ctx, destCfg, resource.destinationRef, destMarker); err != nil {
				return nil, err
			}
		}
	}

	// Re-read every source object before the first destructive transition. The
	// final archive calls also carry atomic revision preconditions.
	currentTopic, err := moveRead(ctx, a, sourceCfg, "/topics/"+url.PathEscape(sourceRef), "topic")
	if err != nil {
		return nil, err
	}
	if err := moveValidateSnapshot(topicMarker, sourceCfg.BaseURL, "topic", sourceRef, currentTopic, moveHasMarker(asMap(currentTopic["workspace_move"]), moveID)); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.problem != "" {
			continue
		}
		current, err := a.readMoveSourceResource(ctx, sourceCfg, resource)
		if err != nil {
			return nil, err
		}
		if moveSourceRevision(resource.kind, current) != moveSourceRevision(resource.kind, resource.object) || moveSnapshotFingerprint(resource.kind, current) != moveSnapshotFingerprint(resource.kind, resource.object) {
			return nil, errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s changed after snapshot; destination is complete and verified, but no source resources will be archived", resource.kind, resource.sourceRef))
		}
	}

	moveSetPhase(topicMarker, "source_transition")
	currentTopic, err = a.updateMoveTopicMarkerAt(ctx, sourceCfg, sourceRef, topicMarker, map[string]any{}, stringPointer(moveFieldString(currentTopic, "updated_at")))
	if err != nil {
		return nil, err
	}
	// Archive docs and boards before cards; card archival updates board clocks.
	for _, resource := range resources {
		if resource.problem != "" || (resource.kind != "document" && resource.kind != "board") || moveState(resource.object) == "archived" {
			continue
		}
		current, err := a.readMoveSourceResource(ctx, sourceCfg, resource)
		if err != nil {
			return nil, err
		}
		if moveSnapshotFingerprint(resource.kind, current) != moveSnapshotFingerprint(resource.kind, resource.object) || moveSourceRevision(resource.kind, current) != moveSourceRevision(resource.kind, resource.object) {
			return nil, errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s changed before archive; it remains active", resource.kind, resource.sourceRef))
		}
		archiveBody := map[string]any{"if_updated_at": moveFieldString(current, "updated_at")}
		path := "/docs/" + url.PathEscape(resource.sourceRef) + "/archive"
		if resource.kind == "board" {
			path = "/boards/" + url.PathEscape(resource.sourceRef) + "/archive"
		}
		if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, path, archiveBody); err != nil {
			return nil, err
		}
	}
	for _, resource := range resources {
		if resource.problem != "" || resource.kind != "card" || moveState(resource.work) == "archived" {
			continue
		}
		if err := a.archiveMovedSourceCard(ctx, sourceCfg, destCfg, resource, moveID, mapping); err != nil {
			return nil, err
		}
	}
	currentTopic, err = moveRead(ctx, a, sourceCfg, "/topics/"+url.PathEscape(sourceRef), "topic")
	if err != nil {
		return nil, err
	}
	if err := moveValidateSnapshot(topicMarker, sourceCfg.BaseURL, "topic", sourceRef, currentTopic, true); err != nil {
		return nil, err
	}
	moveSetPhase(topicMarker, "complete")
	topicMarker["status"] = "complete"
	currentTopic, err = a.updateMoveTopicMarkerAt(ctx, sourceCfg, sourceRef, topicMarker, map[string]any{}, stringPointer(moveFieldString(currentTopic, "updated_at")))
	if err != nil {
		return nil, err
	}
	if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/topics/"+url.PathEscape(sourceRef)+"/archive", map[string]any{"if_updated_at": moveFieldString(currentTopic, "updated_at")}); err != nil {
		return nil, err
	}
	moveSetPhase(topicMarker, "complete")
	topicMarker["status"] = "complete"
	if _, err := a.updateMoveTopicMarker(ctx, destCfg, destinationTopicRef, topicMarker, map[string]any{}); err != nil {
		return nil, err
	}
	actions := make([]map[string]any, 0, len(resources)*2+2)
	actions = append(actions, map[string]any{"action": "create_or_resume", "kind": "topic", "source_ref": sourceRef, "destination_ref": destinationTopicRef})
	for _, boardRef := range moveStringList(sourceTopic["board_refs"]) {
		actions = append(actions, map[string]any{"action": "preserve", "kind": "board", "ref": boardRef, "reason": "shared topic context is not part of the transfer set"})
	}
	for _, resource := range resources {
		if resource.problem != "" {
			continue
		}
		actions = append(actions, map[string]any{"action": "create_or_resume", "kind": resource.kind, "source_ref": resource.sourceRef, "destination_ref": resource.destinationRef})
	}
	for _, resource := range resources {
		action := "archive"
		if resource.kind == "card" && (moveFieldString(asMap(resource.work["source"]), "authority") == "" || moveFieldString(asMap(resource.work["source"]), "authority") == "nexus") {
			action = "tombstone"
		}
		actions = append(actions, map[string]any{"action": action, "kind": resource.kind, "ref": resource.sourceRef, "url": resource.sourceURL})
	}
	actions = append(actions, map[string]any{"action": "archive", "kind": "topic", "ref": sourceRef, "url": sourceURL})
	return movePlanResult("topic", moveID, sourceRef, sourceURL, destCfg.BaseURL, destinationTopicRef, actions, false), nil
}

func (a *App) readTopicMoveResources(ctx context.Context, sourceCfg, destCfg config.Resolved, topic, workspace map[string]any, moveID string) ([]*topicMoveResource, error) {
	_ = destCfg
	refs := map[string]map[string]struct{}{"document": {}, "card": {}}
	// Only direct topic links establish ownership. /workspace includes
	// contextual resources from associated boards, which can be shared with
	// unrelated active cards and must not enter the transfer set.
	for _, ref := range moveStringList(topic["document_refs"]) {
		refs["document"][moveNormalizeRef(ref)] = struct{}{}
	}
	for _, ref := range moveStringList(topic["related_refs"]) {
		normalized := moveNormalizeRef(ref)
		kind := moveRefKind(normalized)
		if kind == "document" || kind == "card" {
			refs[kind][normalized] = struct{}{}
		}
	}
	topicIdentities := moveResourceIdentitySet(topic, "topic")
	for _, raw := range asSlice(workspace["cards"]) {
		card := moveExtractObject(raw, "card")
		if work := asMap(card["work"]); work != nil {
			card = work
		}
		if moveRefInIdentitySet(moveFieldString(card, "topic_ref"), topicIdentities) {
			if ref := moveFieldString(card, "ref"); ref != "" {
				refs["card"][ref] = struct{}{}
			}
		}
	}
	if previous := asMap(topic["workspace_move"]); moveHasMarker(previous, moveID) {
		for _, raw := range asSlice(previous["resources"]) {
			item := asMap(raw)
			kind, ref := moveFieldString(item, "kind"), moveNormalizeRef(moveFieldString(item, "source_ref"))
			if (kind == "document" || kind == "card") && ref != "" {
				refs[kind][ref] = struct{}{}
			}
		}
	}
	// Read the selected cards before planning the doc set. A card's pinned
	// document is owned by that card's transfer even when the topic does not
	// link the document directly. Reuse these exact card snapshots below so the
	// membership decision and the eventual copy are fenced to the same read.
	cardSnapshots := make(map[string]map[string]any, len(refs["card"]))
	cardRefs := make([]string, 0, len(refs["card"]))
	for ref := range refs["card"] {
		cardRefs = append(cardRefs, ref)
	}
	sort.Strings(cardRefs)
	for _, ref := range cardRefs {
		card, err := moveRead(ctx, a, sourceCfg, "/work/"+url.PathEscape(ref), "work")
		if err != nil {
			return nil, fmt.Errorf("read source card %s: %w", ref, err)
		}
		if err := moveHydrateCardPlan(ctx, a, sourceCfg, card); err != nil {
			return nil, err
		}
		cardSnapshots[ref] = card
		if documentRef := moveFieldString(card, "document_ref"); documentRef != "" {
			refs["document"][moveNormalizeRef(documentRef)] = struct{}{}
		}
		for _, relatedRef := range moveStringList(card["related_refs"]) {
			normalized := moveNormalizeRef(relatedRef)
			if moveRefKind(normalized) == "document" {
				refs["document"][normalized] = struct{}{}
			}
		}
	}
	items := make([]*topicMoveResource, 0)
	canonicalSeen := map[string]struct{}{}
	for _, kind := range []string{"document", "card"} {
		keys := make([]string, 0, len(refs[kind]))
		for ref := range refs[kind] {
			keys = append(keys, ref)
		}
		sort.Strings(keys)
		for _, ref := range keys {
			path, responseKey := "/", kind
			switch kind {
			case "document":
				path, responseKey = "/docs/"+url.PathEscape(ref), "document"
			case "board":
				path, responseKey = "/boards/"+url.PathEscape(ref), "board"
			case "card":
				path, responseKey = "/work/"+url.PathEscape(ref), "work"
			}
			var object map[string]any
			if kind == "document" {
				full, err := moveCall(ctx, a, sourceCfg, http.MethodGet, path, nil)
				if err != nil {
					resourceRef := ref
					items = append(items, &topicMoveResource{kind: kind, sourceRef: resourceRef, sourceURL: moveURL(sourceCfg, kind, resourceRef, ""), problem: err.Error()})
					continue
				}
				doc := asMap(full["document"])
				if doc == nil {
					return nil, fmt.Errorf("read source document %s: response omitted document", ref)
				}
				object = cloneMoveMap(doc)
				object["revision"] = full["revision"]
			} else if kind == "card" {
				object = cardSnapshots[ref]
				if object == nil {
					return nil, errnorm.Internal("move_response_invalid", "selected card snapshot was not loaded")
				}
			} else {
				var err error
				object, err = moveRead(ctx, a, sourceCfg, path, responseKey)
				if err != nil {
					return nil, fmt.Errorf("read source %s %s: %w", kind, ref, err)
				}
			}
			resourceRef := firstNonEmpty(moveFieldString(object, "ref"), ref)
			canonicalKey := kind + ":" + moveSourceObjectID(kind, resourceRef, object)
			if _, duplicate := canonicalSeen[canonicalKey]; duplicate {
				continue
			}
			canonicalSeen[canonicalKey] = struct{}{}
			boardRef := ""
			if kind == "card" {
				boardRef = moveFieldString(object, "board_ref")
			}
			items = append(items, &topicMoveResource{kind: kind, sourceRef: resourceRef, sourceURL: moveURL(sourceCfg, kind, resourceRef, boardRef), object: object})
			if kind == "card" {
				items[len(items)-1].work = object
			}
		}
	}
	return items, nil
}
