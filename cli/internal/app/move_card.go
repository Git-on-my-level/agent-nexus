// The cross-workspace card move: external work discovery, card create input,
// identity attachment, phase mapping, and the moveCard driver.

package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func moveAddIdentity(mapping map[string]string, object map[string]any, kind, destinationRef string) {
	if destinationRef == "" {
		return
	}
	destinationRef = moveNormalizeRef(destinationRef)
	candidates := []string{moveFieldString(object, "ref"), moveFieldString(object, "id"), moveFieldString(object, "handle")}
	if kind == "card" {
		if sourceURL := moveFieldString(asMap(object["source"]), "url"); sourceURL != "" {
			candidates = append(candidates, sourceURL)
		}
	}
	for _, rawCandidate := range candidates {
		candidate := strings.TrimSpace(rawCandidate)
		if candidate == "" {
			continue
		}
		mapping[candidate] = destinationRef
		normalized := moveNormalizeRef(candidate)
		mapping[normalized] = destinationRef
		if !strings.Contains(candidate, ":") {
			mapping[moveNormalizeRef(moveResourceRef(kind, candidate))] = destinationRef
		}
	}
}

func moveState(object map[string]any) string {
	// Work projects lifecycle timestamps rather than the canonical card's state.
	// Prefer those fields when present, with trash taking precedence over archive.
	if moveFieldString(object, "trashed_at") != "" {
		return "trashed"
	}
	if moveFieldString(object, "archived_at") != "" {
		return "archived"
	}
	return strings.TrimSpace(anyString(object["state"]))
}

func moveSetPhase(marker map[string]any, phase string) {
	order := map[string]int{"snapshot": 0, "create_destination": 1, "refs_rewritten": 2, "verified": 3, "source_transition": 4, "complete": 5}
	current := moveFieldString(marker, "phase")
	if order[phase] >= order[current] {
		marker["phase"] = phase
	}
}

func (a *App) moveCard(ctx context.Context, sourceCfg, destCfg config.Resolved, requestedRef string, dryRun bool, connectionMappings map[string]string) (*commandResult, error) {
	source, err := moveRead(ctx, a, sourceCfg, "/work/"+url.PathEscape(requestedRef), "work")
	if err != nil {
		return nil, err
	}
	if err := moveHydrateCardPlan(ctx, a, sourceCfg, source); err != nil {
		return nil, err
	}
	sourceRef := firstNonEmpty(moveFieldString(source, "ref"), requestedRef)
	boardRef := moveFieldString(source, "board_ref")
	sourceURL := moveURL(sourceCfg, "card", sourceRef, boardRef)
	moveID := moveStableID(sourceCfg.BaseURL, sourceRef, destCfg.BaseURL)
	sourceMarker := asMap(source["workspace_move"])
	if len(sourceMarker) > 0 && !moveHasMarker(sourceMarker, moveID) {
		if err := moveCheckExistingMarker(source, moveID); err != nil {
			return nil, err
		}
	}
	if state := moveState(source); state == "archived" && !moveHasMarker(sourceMarker, moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "source_archived", "archived source card has no matching move marker")
	}
	if moveState(source) == "archived" && moveHasMarker(sourceMarker, moveID) && anyString(sourceMarker["status"]) == "complete" {
		if err := moveValidateSnapshot(sourceMarker, sourceCfg.BaseURL, "card", sourceRef, source, true); err != nil {
			return nil, err
		}
		return movePlanResult("card", moveID, sourceRef, sourceURL, destCfg.BaseURL, anyString(sourceMarker["destination_ref"]), []map[string]any{}, dryRun), nil
	}
	snapshotRevision := moveSourceRevision("card", source)
	snapshotFingerprint := moveSnapshotFingerprint("card", source)
	journalMarker := moveMarker(moveID, "card", sourceRef, sourceURL, destCfg.BaseURL, "", "pending")
	moveAttachSnapshot(journalMarker, sourceCfg.BaseURL, "card", sourceRef, source)
	if moveHasMarker(sourceMarker, moveID) {
		if err := moveValidateSnapshot(sourceMarker, sourceCfg.BaseURL, "card", sourceRef, source, true); err != nil {
			return nil, err
		}
		journalMarker = sourceMarker
		snapshotRevision = moveFieldString(sourceMarker, "source_revision")
		snapshotFingerprint = moveFieldString(sourceMarker, "source_fingerprint")
	}

	targetRef := ""
	sourceIdentity := asMap(source["source"])
	authority := moveFieldString(sourceIdentity, "authority")
	destinationIdentity := map[string]any{}
	for key, value := range sourceIdentity {
		destinationIdentity[key] = value
	}
	destinationConnection := ""
	if authority != "" && authority != "nexus" {
		destinationConnection = strings.TrimSpace(connectionMappings[moveFieldString(sourceIdentity, "connection_id")])
		if destinationConnection == "" {
			return nil, errnorm.New(errnorm.KindUsage, "connection_mapping_required", "source-backed card requires --connection-map <source-connection-id>=<destination-connection-id>")
		}
		if savedConnection := moveFieldString(sourceMarker, "destination_connection_id"); savedConnection != "" && savedConnection != destinationConnection {
			return nil, errnorm.New(errnorm.KindRemote, "connection_mapping_changed", "source-backed card move was snapshotted with a different destination connection mapping")
		}
		destinationIdentity["connection_id"] = destinationConnection
		if existing, findErr := a.findMoveExternalWork(ctx, destCfg, destinationIdentity); findErr != nil {
			return nil, findErr
		} else if existing != nil {
			if !moveHasMarker(asMap(existing["workspace_move"]), moveID) {
				return nil, errnorm.New(errnorm.KindRemote, "destination_source_identity_conflict", "destination already contains this source identity under the mapped connection but it belongs to a different move")
			}
			targetRef = moveFieldString(existing, "ref")
		}
	}
	if targetRef == "" {
		targetRef = moveResourceRef("card", moveDeterministicUUID(moveID, "card", sourceRef))
	}
	journalMarker["destination_ref"] = targetRef
	if authority == "" || authority == "nexus" {
		markCuratedTombstone(journalMarker, targetRef, moveURL(destCfg, "card", targetRef, ""))
	} else {
		journalMarker["source_action"] = "archive"
		journalMarker["destination_connection_id"] = destinationConnection
	}
	destExisting, destExists, err := moveGetWorkIfExists(ctx, a, destCfg, targetRef)
	if err != nil {
		return nil, err
	}
	if destExists && !moveHasMarker(asMap(destExisting["workspace_move"]), moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card id is already used by an unrelated card")
	}
	if destExists {
		if err := moveValidateSnapshot(asMap(destExisting["workspace_move"]), sourceCfg.BaseURL, "card", sourceRef, source, moveHasMarker(sourceMarker, moveID)); err != nil {
			return nil, err
		}
	}

	refMapping := map[string]string{}
	moveAddIdentity(refMapping, source, "card", targetRef)
	workInput, err := moveCardCreateInput(source, moveID, sourceRef, sourceURL, destCfg.BaseURL, targetRef, refMapping, destinationConnection)
	if err != nil {
		return nil, err
	}
	workMarker := asMap(workInput["workspace_move"])
	if moveHasMarker(sourceMarker, moveID) {
		workMarker = cloneMoveMap(journalMarker)
		workMarker["destination_ref"] = targetRef
		workInput["workspace_move"] = workMarker
	} else {
		moveAttachSnapshot(workMarker, sourceCfg.BaseURL, "card", sourceRef, source)
	}
	workMarker["phase"] = "create_destination"
	createAction := "create"
	if authority != "" && authority != "nexus" {
		createAction = "resync"
	} else if destExists {
		createAction = "reuse"
	}
	actions := []map[string]any{{"action": createAction, "kind": "card", "source_ref": sourceRef, "destination_ref": targetRef}}
	archiveAction := "archive"
	if authority == "" || authority == "nexus" {
		archiveAction = "tombstone"
	}
	if authority != "" && authority != "nexus" {
		actions[0]["source_connection_id"] = moveFieldString(sourceIdentity, "connection_id")
		actions[0]["destination_connection_id"] = destinationConnection
	}
	actions = append(actions, map[string]any{"action": archiveAction, "kind": "card", "ref": sourceRef, "url": sourceURL})
	if dryRun {
		return movePlanResult("card", moveID, sourceRef, sourceURL, destCfg.BaseURL, targetRef, actions, true), nil
	}
	if !moveHasMarker(sourceMarker, moveID) {
		sourceSnapshot := cloneMoveMap(journalMarker)
		sourceSnapshot["status"] = "pending"
		moveSetPhase(sourceSnapshot, "snapshot")
		sourceSnapshot["destination_ref"] = targetRef
		if authority == "" || authority == "nexus" {
			markCuratedTombstone(sourceSnapshot, targetRef, moveURL(destCfg, "card", targetRef, ""))
		} else {
			sourceSnapshot["source_action"] = "archive"
		}
		patched, patchErr := a.updateMoveWorkMarkerAt(ctx, sourceCfg, sourceRef, sourceSnapshot, moveInt64Pointer(moveInt64(source["version"])))
		if patchErr != nil {
			return nil, patchErr
		}
		sourceMarker = asMap(patched["workspace_move"])
	}

	destWork := destExisting
	if !destExists || (authority != "" && authority != "nexus") {
		destBody, callErr := moveCall(ctx, a, destCfg, http.MethodPost, "/work", workInput)
		if callErr != nil {
			return nil, callErr
		}
		destWork = asMap(destBody["work"])
		if destWork == nil {
			return nil, errnorm.Internal("move_response_invalid", "destination work.create omitted work")
		}
	}
	destinationRef := firstNonEmpty(moveFieldString(destWork, "ref"), targetRef)
	destMarker := asMap(destWork["workspace_move"])
	if !moveHasMarker(destMarker, moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card create resolved to a card without the expected move journal")
	}
	if err := moveValidateSnapshot(destMarker, sourceCfg.BaseURL, "card", sourceRef, source, moveHasMarker(sourceMarker, moveID)); err != nil {
		return nil, err
	}
	if err := moveWriteCardPlan(ctx, a, destCfg, destinationRef, moveRewritePlan(source["plan"], refMapping)); err != nil {
		return nil, err
	}
	verifyResource := &topicMoveResource{
		kind: "card", sourceRef: sourceRef, sourceURL: sourceURL, destinationRef: destinationRef,
		object: source, work: source, marker: journalMarker, destinationConnection: destinationConnection,
	}
	if err := a.verifyMovedResource(ctx, sourceCfg, destCfg, verifyResource, moveID, refMapping, "", ""); err != nil {
		return nil, err
	}
	destMarker["destination_ref"] = destinationRef
	moveSetPhase(destMarker, "verified")
	destMarker["status"] = "verified"
	if _, err := a.updateMoveWorkMarker(ctx, destCfg, destinationRef, destMarker); err != nil {
		return nil, err
	}
	currentSource, err := moveRead(ctx, a, sourceCfg, "/work/"+url.PathEscape(sourceRef), "work")
	if err != nil {
		return nil, err
	}
	if err := moveHydrateCardPlan(ctx, a, sourceCfg, currentSource); err != nil {
		return nil, err
	}
	if !moveHasMarker(asMap(currentSource["workspace_move"]), moveID) {
		if moveSourceRevision("card", currentSource) != snapshotRevision || moveSnapshotFingerprint("card", currentSource) != snapshotFingerprint {
			return nil, errnorm.New(errnorm.KindRemote, "source_changed", "source card changed after snapshot; destination copy is retained for resume and source was not archived")
		}
	} else if err := moveValidateSnapshot(asMap(currentSource["workspace_move"]), sourceCfg.BaseURL, "card", sourceRef, currentSource, true); err != nil {
		return nil, err
	}
	sourceMarker = cloneMoveMap(journalMarker)
	sourceMarker["move_id"], sourceMarker["resource_kind"], sourceMarker["source_ref"] = moveID, "card", sourceRef
	sourceMarker["source_url"], sourceMarker["destination_workspace_url"], sourceMarker["destination_ref"] = sourceURL, strings.TrimRight(destCfg.BaseURL, "/"), destinationRef
	sourceMarker["status"] = "complete"
	if authority == "" || authority == "nexus" {
		markCuratedTombstone(sourceMarker, destinationRef, moveURL(destCfg, "card", destinationRef, moveFieldString(destWork, "board_ref")))
	} else {
		sourceMarker["source_action"] = "archive"
	}
	currentVersion := moveInt64(currentSource["version"])
	if moveState(currentSource) != "archived" {
		archiveVersion := moveInt64(asMap(currentSource["workspace_move"])["source_archive_version"])
		if archiveVersion == 0 || archiveVersion != currentVersion {
			moveSetPhase(sourceMarker, "source_transition")
			sourceMarker["source_archive_version"] = currentVersion + 1
			patched, patchErr := a.updateMoveWorkMarkerAt(ctx, sourceCfg, sourceRef, sourceMarker, &currentVersion)
			if patchErr != nil {
				return nil, patchErr
			}
			archiveVersion = moveInt64(patched["version"])
			if archiveVersion == 0 {
				archiveVersion = currentVersion + 1
			}
			if archiveVersion != moveInt64(sourceMarker["source_archive_version"]) {
				return nil, errnorm.New(errnorm.KindRemote, "source_changed", "source card revision changed while recording the completed move; source was not archived")
			}
		}
		if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(sourceRef)+"/archive", map[string]any{"if_version": archiveVersion}); err != nil {
			return nil, err
		}
	} else if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(sourceRef)+"/archive", map[string]any{"if_version": currentVersion}); err != nil {
		return nil, err
	}
	return movePlanResult("card", moveID, sourceRef, sourceURL, destCfg.BaseURL, destinationRef, actions, false), nil
}

func movePlanResult(kind, moveID, sourceRef, sourceURL, destinationURL, destinationRef string, actions []map[string]any, dryRun bool) *commandResult {
	status := "moved"
	if dryRun {
		status = "dry_run"
	}
	data := map[string]any{
		"kind": kind, "move_id": moveID, "status": status,
		"source_ref": sourceRef, "source_url": sourceURL,
		"destination_workspace_url": strings.TrimRight(destinationURL, "/"),
		"destination_ref":           destinationRef, "actions": actions,
	}
	return &commandResult{Data: data, Text: formatPrettyBody(data)}
}

func (a *App) findMoveExternalWork(ctx context.Context, cfg config.Resolved, source map[string]any) (map[string]any, error) {
	wantAuthority := moveFieldString(source, "authority")
	wantConnection := moveFieldString(source, "connection_id")
	wantNative := moveFieldString(source, "native_id")
	if wantAuthority == "" || wantConnection == "" || wantNative == "" {
		return nil, nil
	}
	cursor := ""
	for page := 0; page < 100; page++ {
		query := url.Values{"limit": []string{"200"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		body, err := moveCall(ctx, a, cfg, http.MethodGet, "/work?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		for _, raw := range asSlice(body["work"]) {
			work := asMap(raw)
			identity := asMap(work["source"])
			if moveFieldString(identity, "authority") == wantAuthority && moveFieldString(identity, "connection_id") == wantConnection && moveFieldString(identity, "native_id") == wantNative {
				return work, nil
			}
		}
		cursor = moveFieldString(body, "next_cursor")
		if cursor == "" {
			return nil, nil
		}
	}
	return nil, errnorm.Internal("move_scan_limit", "destination work list exceeded the bounded 100-page dedupe scan")
}

func moveGetWorkIfExists(ctx context.Context, a *App, cfg config.Resolved, ref string) (map[string]any, bool, error) {
	work, err := moveRead(ctx, a, cfg, "/work/"+url.PathEscape(ref), "work")
	if err != nil {
		if errnorm.Normalize(err).Code == "not_found" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return work, true, nil
}

func moveCardCreateInput(source map[string]any, moveID, sourceRef, sourceURL, destinationURL, destinationRef string, mapping map[string]string, destinationConnection string) (map[string]any, error) {
	if mapping == nil {
		mapping = map[string]string{}
	}
	marker := moveMarker(moveID, "card", sourceRef, sourceURL, destinationURL, destinationRef, "pending")
	input := map[string]any{
		"id":                 moveRefID(destinationRef),
		"title":              source["title"],
		"summary":            source["summary"],
		"definition_of_done": source["definition_of_done"],
		"priority":           source["priority"],
		"due_at":             source["due_at"],
		"risk":               source["risk"],
		"source":             source["source"],
		"workspace_move":     marker,
	}
	phase := moveFieldString(source, "phase")
	if phase == "done" {
		phase = "review" // work.create requires evidence before a destination item may be done.
	}
	if phase == "" {
		phase = "backlog"
	}
	input["phase"] = phase
	if project := moveFieldString(source, "project_ref"); project != "" {
		if mapped := moveRewriteRef(project, mapping); mapped != project {
			input["project_ref"] = mapped
		}
	}
	related := moveRewriteRefs(source["related_refs"], mapping)
	for _, key := range []string{"project_ref", "topic_ref", "document_ref"} {
		ref := moveFieldString(source, key)
		if ref == "" {
			continue
		}
		mapped := moveRewriteRef(ref, mapping)
		if mapped != ref {
			if key == "topic_ref" {
				input["topic_ref"] = mapped
			}
			if key == "document_ref" {
				input["document_ref"] = mapped
			}
			if key == "project_ref" {
				input["project_ref"] = mapped
			}
		} else {
			related = append(related, ref)
		}
	}
	for _, raw := range asSlice(source["relations"]) {
		relation := asMap(raw)
		ref := moveFieldString(relation, "ref")
		if mapped := moveRewriteRef(ref, mapping); mapped == ref {
			if ref != "" {
				related = append(related, ref)
			}
		}
	}
	input["related_refs"] = moveUnique(related)
	if raw := asMap(source["source"]); len(raw) == 0 {
		input["source"] = map[string]any{"authority": "nexus"}
	} else if authority := moveFieldString(raw, "authority"); authority != "" && authority != "nexus" {
		identity := map[string]any{}
		for key, value := range raw {
			identity[key] = value
		}
		if strings.TrimSpace(destinationConnection) == "" {
			return nil, errnorm.New(errnorm.KindUsage, "connection_mapping_required", "source-backed card requires an explicit destination connection mapping")
		}
		identity["connection_id"] = destinationConnection
		input["source"] = identity
	}
	return input, nil
}
