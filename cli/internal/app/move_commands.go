package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type parsedMoveCommand struct {
	kind, ref, target string
	dryRun            bool
}

func parseMoveCommand(args []string) (parsedMoveCommand, error) {
	out := parsedMoveCommand{}
	if len(args) == 0 {
		return out, errnorm.Usage("subcommand_required", "usage: anx move card|topic <ref> --to <workspace-alias> [--dry-run]")
	}
	out.kind = strings.TrimSpace(args[0])
	if out.kind != "card" && out.kind != "topic" {
		return out, errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown move subcommand %q; expected card or topic", out.kind))
	}
	fs := newSilentFlagSet("move " + out.kind)
	var target trackedString
	fs.Var(&target, "to", "Destination workspace alias")
	fs.BoolVar(&out.dryRun, "dry-run", false, "Show the planned move without writing")
	tail := args[1:]
	var leading []string
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		leading = append(leading, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positions := append(leading, fs.Args()...)
	if len(positions) != 1 || strings.TrimSpace(positions[0]) == "" {
		return out, errnorm.Usage("invalid_request", "one source resource ref is required")
	}
	out.ref = strings.TrimSpace(positions[0])
	if out.ref == "." || out.ref == ".." || strings.ContainsAny(out.ref, "/\\?#%") {
		return out, errnorm.Usage("invalid_request", "resource id must be a ref, handle or id, not a URL or path")
	}
	if !target.set || strings.TrimSpace(target.value) == "" {
		return out, errnorm.Usage("invalid_request", "--to <workspace-alias> is required")
	}
	out.target = strings.TrimSpace(target.value)
	return out, nil
}

func (a *App) runMoveCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	parsed, err := parseMoveCommand(args)
	if err != nil {
		name := "move"
		if parsed.kind != "" {
			name += " " + parsed.kind
		}
		return nil, name, err
	}
	name := "move " + parsed.kind
	targetCfg, err := a.moveDestinationConfig(ctx, cfg, parsed.target)
	if err != nil {
		return nil, name, err
	}
	if strings.TrimRight(cfg.BaseURL, "/") == strings.TrimRight(targetCfg.BaseURL, "/") {
		return nil, name, errnorm.Usage("invalid_request", "source and destination workspaces are the same")
	}
	if parsed.kind == "card" {
		result, err := a.moveCard(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun)
		return result, name, err
	}
	result, err := a.moveTopic(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun)
	return result, name, err
}

func (a *App) moveDestinationConfig(ctx context.Context, source config.Resolved, alias string) (config.Resolved, error) {
	catalog, _, err := a.workspaceCatalog(source)
	if err != nil {
		return config.Resolved{}, errnorm.Wrap(errnorm.KindLocal, "workspace_config_invalid", "cannot read user-global workspace aliases: "+err.Error(), err)
	}
	base, ok := catalog.File.Aliases[alias]
	if !ok {
		return config.Resolved{}, errnorm.Usage("workspace_unknown", "unknown workspace alias "+alias+"; run anx config workspaces")
	}
	name, identitySource, err := a.identityName(source)
	if err != nil {
		return config.Resolved{}, err
	}
	dest := source
	dest.BaseURL = strings.TrimRight(base, "/")
	dest.Sources = copyStringMap(source.Sources)
	dest.Sources["base_url"] = "flag:--workspace"
	dest.As = name
	dest.IdentitySource = identitySource
	// Workspace host tokens are scoped to one local enrollment. Never send the
	// source bearer token, actor id, or host identity to the destination.
	dest.AccessToken = ""
	dest.AccessTokenExpiresAt = ""
	dest.HostID = ""
	dest.HostKeyID = ""
	dest.HostKeyPath = ""
	dest.AgentID = ""
	dest.ActorID = ""
	dest.Username = ""
	return a.resolveHostAgent(ctx, dest)
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for key, value := range in {
		out[key] = value
	}
	return out
}

func moveStableID(sourceBase, sourceRef, destinationBase string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(sourceBase, "/") + "\n" + sourceRef + "\n" + strings.TrimRight(destinationBase, "/")))
	return "mv_" + hex.EncodeToString(sum[:16])
}

func moveDeterministicUUID(moveID, kind, sourceRef string) string {
	sum := sha1.Sum([]byte(moveID + "\n" + kind + "\n" + sourceRef))
	b := append([]byte(nil), sum[:16]...)
	b[6] = b[6]&0x0f | 0x50
	b[8] = b[8]&0x3f | 0x80
	encoded := hex.EncodeToString(b)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func moveResourceRef(kind, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return kind + ":" + id
}

func moveRefID(ref string) string {
	_, suffix, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if ok {
		return suffix
	}
	return strings.TrimSpace(ref)
}

func moveURL(cfg config.Resolved, kind, ref, boardRef string) string {
	loc := resourceLocator{Kind: kind, ID: moveRefID(ref)}
	if kind == "card" {
		loc.BoardID = moveRefID(boardRef)
	}
	if result, err := resourceURL(cfg, loc); err == nil {
		return result
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	return base + "/" + map[string]string{"topic": "topics", "card": "cards", "document": "docs", "board": "boards"}[kind] + "/" + url.PathEscape(moveRefID(ref))
}

func moveMarker(moveID, kind, sourceRef, sourceURL, destinationURL, destinationRef, status string) map[string]any {
	return map[string]any{
		"move_id":                   moveID,
		"resource_kind":             kind,
		"source_ref":                sourceRef,
		"source_url":                sourceURL,
		"destination_workspace_url": strings.TrimRight(destinationURL, "/"),
		"destination_ref":           destinationRef,
		"status":                    status,
	}
}

func markCuratedTombstone(marker map[string]any, destinationRef, destinationURL string) {
	marker["source_action"] = "tombstone"
	marker["tombstone"] = map[string]any{
		"destination_ref": destinationRef,
		"destination_url": destinationURL,
	}
}

func moveCall(ctx context.Context, a *App, cfg config.Resolved, method, path string, body any) (map[string]any, error) {
	result, err := a.invokeRawJSON(ctx, cfg, "move", method, path, body)
	if err != nil {
		return nil, err
	}
	return commandResultBody(result), nil
}

func moveRead(ctx context.Context, a *App, cfg config.Resolved, path, key string) (map[string]any, error) {
	body, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return body, nil
	}
	value := asMap(body[key])
	if value == nil {
		return nil, errnorm.Internal("move_response_invalid", "move source API response omitted "+key)
	}
	return value, nil
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

func moveFieldString(object map[string]any, key string) string {
	return strings.TrimSpace(anyString(object[key]))
}

func moveStringList(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s := strings.TrimSpace(anyString(value)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func moveUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func moveRewriteRef(ref string, mapping map[string]string) string {
	ref = strings.TrimSpace(ref)
	if mapped := mapping[ref]; mapped != "" {
		return mapped
	}
	kind, suffix, ok := strings.Cut(ref, ":")
	if ok {
		if mapped := mapping[kind+":"+suffix]; mapped != "" {
			return mapped
		}
	}
	return ref
}

func moveRewriteRefs(refs any, mapping map[string]string) []string {
	values := moveStringList(refs)
	for index := range values {
		values[index] = moveRewriteRef(values[index], mapping)
	}
	return moveUnique(values)
}

func moveRewriteProvenance(raw any, mapping map[string]string) any {
	provenance := asMap(raw)
	if provenance == nil {
		return raw
	}
	copy := make(map[string]any, len(provenance))
	for key, value := range provenance {
		copy[key] = value
	}
	if sources, ok := provenance["sources"]; ok {
		rewritten := moveStringList(sources)
		for index := range rewritten {
			rewritten[index] = moveRewriteRef(rewritten[index], mapping)
		}
		copy["sources"] = rewritten
	}
	return copy
}

func moveAddIdentity(mapping map[string]string, object map[string]any, kind, destinationRef string) {
	if destinationRef == "" {
		return
	}
	for _, candidate := range []string{moveFieldString(object, "ref"), moveFieldString(object, "id"), moveFieldString(object, "handle")} {
		if candidate == "" {
			continue
		}
		mapping[candidate] = destinationRef
		if !strings.Contains(candidate, ":") {
			mapping[kind+":"+candidate] = destinationRef
		}
	}
}

func moveExtractObject(raw any, key string) map[string]any {
	entry := asMap(raw)
	if nested := asMap(entry[key]); nested != nil {
		return nested
	}
	return entry
}

func moveMapGet(ctx context.Context, a *App, cfg config.Resolved, method, path string, body any, key string) (map[string]any, error) {
	result, err := moveCall(ctx, a, cfg, method, path, body)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return result, nil
	}
	value := asMap(result[key])
	if value == nil {
		return nil, errnorm.Internal("move_response_invalid", "move API response omitted "+key)
	}
	return value, nil
}

func (a *App) updateMoveTopicMarker(ctx context.Context, cfg config.Resolved, ref string, marker map[string]any, patch map[string]any) (map[string]any, error) {
	current, err := moveRead(ctx, a, cfg, "/topics/"+url.PathEscape(ref), "topic")
	if err != nil {
		return nil, err
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
	current, err := moveRead(ctx, a, cfg, "/work/"+url.PathEscape(ref), "work")
	if err != nil {
		return nil, err
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

func moveState(object map[string]any) string { return strings.TrimSpace(anyString(object["state"])) }

func (a *App) moveCard(ctx context.Context, sourceCfg, destCfg config.Resolved, requestedRef string, dryRun bool) (*commandResult, error) {
	source, err := moveRead(ctx, a, sourceCfg, "/work/"+url.PathEscape(requestedRef), "work")
	if err != nil {
		return nil, err
	}
	sourceRef := firstNonEmpty(moveFieldString(source, "ref"), requestedRef)
	boardRef := moveFieldString(source, "board_ref")
	sourceURL := moveURL(sourceCfg, "card", sourceRef, boardRef)
	moveID := moveStableID(sourceCfg.BaseURL, sourceRef, destCfg.BaseURL)
	marker := asMap(source["workspace_move"])
	if len(marker) > 0 && !moveHasMarker(marker, moveID) {
		if err := moveCheckExistingMarker(source, moveID); err != nil {
			return nil, err
		}
	}
	if state := moveState(source); state == "archived" && !moveHasMarker(marker, moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "source_archived", "archived source card has no matching move marker")
	}
	if moveState(source) == "archived" && moveHasMarker(marker, moveID) && anyString(marker["status"]) == "complete" {
		return movePlanResult("card", moveID, sourceRef, sourceURL, destCfg.BaseURL, anyString(marker["destination_ref"]), []map[string]any{}, dryRun), nil
	}
	sourceMarker := moveMarker(moveID, "card", sourceRef, sourceURL, destCfg.BaseURL, "", "pending")
	source = map[string]any(source)

	targetRef := ""
	sourceIdentity := asMap(source["source"])
	authority := moveFieldString(sourceIdentity, "authority")
	if authority != "" && authority != "nexus" {
		if existing, findErr := a.findMoveExternalWork(ctx, destCfg, sourceIdentity); findErr != nil {
			return nil, findErr
		} else if existing != nil {
			targetRef = moveFieldString(existing, "ref")
		}
	}
	if targetRef == "" {
		targetRef = moveResourceRef("card", moveDeterministicUUID(moveID, "card", sourceRef))
	}
	sourceMarker["destination_ref"] = targetRef
	if authority == "" || authority == "nexus" {
		markCuratedTombstone(sourceMarker, targetRef, moveURL(destCfg, "card", targetRef, ""))
	} else {
		sourceMarker["source_action"] = "archive"
	}
	destExisting, destExists, err := moveGetWorkIfExists(ctx, a, destCfg, targetRef)
	if err != nil {
		return nil, err
	}
	if destExists && (authority == "" || authority == "nexus") && !moveHasMarker(asMap(destExisting["workspace_move"]), moveID) {
		return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card id is already used by an unrelated card")
	}

	workInput, err := moveCardCreateInput(source, moveID, sourceRef, sourceURL, destCfg.BaseURL, targetRef, nil)
	if err != nil {
		return nil, err
	}
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
	actions = append(actions, map[string]any{"action": archiveAction, "kind": "card", "ref": sourceRef, "url": sourceURL})
	if dryRun {
		return movePlanResult("card", moveID, sourceRef, sourceURL, destCfg.BaseURL, targetRef, actions, true), nil
	}

	if _, err := a.updateMoveWorkMarker(ctx, sourceCfg, sourceRef, sourceMarker); err != nil {
		return nil, err
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
	finalMarker := moveMarker(moveID, "card", sourceRef, sourceURL, destCfg.BaseURL, destinationRef, "complete")
	if _, err := a.updateMoveWorkMarker(ctx, destCfg, destinationRef, finalMarker); err != nil {
		return nil, err
	}
	sourceMarker["destination_ref"] = destinationRef
	sourceMarker["status"] = "complete"
	if authority == "" || authority == "nexus" {
		markCuratedTombstone(sourceMarker, destinationRef, moveURL(destCfg, "card", destinationRef, moveFieldString(destWork, "board_ref")))
	}
	if _, err := a.updateMoveWorkMarker(ctx, sourceCfg, sourceRef, sourceMarker); err != nil {
		return nil, err
	}
	if moveState(source) != "archived" {
		if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(sourceRef)+"/archive", map[string]any{}); err != nil {
			return nil, err
		}
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

func moveCardCreateInput(source map[string]any, moveID, sourceRef, sourceURL, destinationURL, destinationRef string, mapping map[string]string) (map[string]any, error) {
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
		"plan":               source["plan"],
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
	if len(related) > 0 {
		input["related_refs"] = moveUnique(related)
	}
	if raw := asMap(source["source"]); len(raw) == 0 {
		input["source"] = map[string]any{"authority": "nexus"}
	}
	return input, nil
}

type topicMoveResource struct {
	kind, sourceRef, sourceURL, destinationID, destinationRef, destinationThreadID string
	object                                                                         map[string]any
	work                                                                           map[string]any
	marker                                                                         map[string]any
}

func moveListRefs(workspace map[string]any, key, kind string) []string {
	refs := []string{}
	for _, raw := range asSlice(workspace[key]) {
		object := moveExtractObject(raw, strings.TrimSuffix(kind, "s"))
		ref := moveFieldString(object, "ref")
		if ref == "" {
			ref = moveResourceRef(kind, moveFieldString(object, "id"))
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func (a *App) moveTopic(ctx context.Context, sourceCfg, destCfg config.Resolved, requestedRef string, dryRun bool) (*commandResult, error) {
	sourceTopic, err := moveRead(ctx, a, sourceCfg, "/topics/"+url.PathEscape(requestedRef), "topic")
	if err != nil {
		return nil, err
	}
	sourceRef := firstNonEmpty(moveFieldString(sourceTopic, "ref"), requestedRef)
	sourceURL := moveURL(sourceCfg, "topic", sourceRef, "")
	moveID := moveStableID(sourceCfg.BaseURL, sourceRef, destCfg.BaseURL)
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
	workspaceBody, err := moveCall(ctx, a, sourceCfg, http.MethodGet, "/topics/"+url.PathEscape(sourceRef)+"/workspace", nil)
	if err != nil {
		return nil, err
	}
	workspace := workspaceBody
	if topic := asMap(workspaceBody["topic"]); topic != nil {
		sourceTopic = topic
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
		resource.destinationID = moveDeterministicUUID(moveID, resource.kind, resource.sourceRef)
		resource.destinationRef = moveResourceRef(resource.kind, resource.destinationID)
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
				if existing, findErr := a.findMoveExternalWork(ctx, destCfg, identity); findErr != nil {
					return nil, findErr
				} else if existing != nil {
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

	manifest := make([]any, 0, len(resources))
	for _, resource := range resources {
		manifest = append(manifest, map[string]any{
			"kind": resource.kind, "source_ref": resource.sourceRef, "source_url": resource.sourceURL,
			"destination_ref": resource.destinationRef, "destination_id": resource.destinationID,
		})
	}
	topicMarker := moveMarker(moveID, "topic", sourceRef, sourceURL, destCfg.BaseURL, destinationTopicRef, "pending")
	topicMarker["resources"] = manifest
	topicMarker["source_owner_refs"] = moveStringList(sourceTopic["owner_refs"])

	if dryRun {
		actions := make([]map[string]any, 0, len(resources)+2)
		topicAction, err := moveExistingAction(ctx, a, destCfg, "/topics/"+url.PathEscape(topicID), "topic", moveID)
		if err != nil {
			return nil, err
		}
		actions = append(actions, map[string]any{"action": topicAction, "kind": "topic", "source_ref": sourceRef, "destination_ref": destinationTopicRef})
		for _, resource := range resources {
			action, err := moveExistingAction(ctx, a, destCfg, "/"+map[string]string{"document": "docs", "board": "boards", "card": "work"}[resource.kind]+"/"+url.PathEscape(resource.destinationID), map[string]string{"document": "document", "board": "board", "card": "work"}[resource.kind], moveID)
			if err != nil {
				return nil, err
			}
			if resource.kind == "card" {
				identity := asMap(resource.work["source"])
				if authority := moveFieldString(identity, "authority"); authority != "" && authority != "nexus" {
					action = "resync"
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
			actions = append(actions, map[string]any{"action": action, "kind": resource.kind, "source_ref": resource.sourceRef, "destination_ref": resource.destinationRef})
		}
		for _, resource := range resources {
			action := "archive"
			if resource.kind == "card" && (moveFieldString(asMap(resource.work["source"]), "authority") == "" || moveFieldString(asMap(resource.work["source"]), "authority") == "nexus") {
				action = "tombstone"
			}
			actions = append(actions, map[string]any{"action": action, "kind": resource.kind, "ref": resource.sourceRef, "url": resource.sourceURL})
		}
		actions = append(actions, map[string]any{"action": "archive", "kind": "topic", "ref": sourceRef, "url": sourceURL})
		return movePlanResult("topic", moveID, sourceRef, sourceURL, destCfg.BaseURL, destinationTopicRef, actions, true), nil
	}

	patch := map[string]any{}
	if _, err := a.updateMoveTopicMarker(ctx, sourceCfg, sourceRef, topicMarker, patch); err != nil {
		return nil, err
	}
	topicCreateMarker := moveMarker(moveID, "topic", sourceRef, sourceURL, destCfg.BaseURL, destinationTopicRef, "pending")
	topicCreateMarker["source_owner_refs"] = moveStringList(sourceTopic["owner_refs"])
	topicInput := map[string]any{
		"id": topicID, "thread_id": topicThreadID,
		"title": sourceTopic["title"], "summary": sourceTopic["summary"],
		"owner_refs":    []string{},
		"document_refs": []string{}, "board_refs": []string{}, "related_refs": []string{},
		"provenance": moveRewriteProvenance(sourceTopic["provenance"], mapping), "workspace_move": topicCreateMarker,
	}
	if _, err := moveCall(ctx, a, destCfg, http.MethodPost, "/topics", map[string]any{"request_key": moveID + ":topic", "topic": topicInput}); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.kind == "document" {
			if _, err := a.createMovedDocument(ctx, sourceCfg, destCfg, resource, moveID, mapping); err != nil {
				return nil, err
			}
		}
	}
	for _, resource := range resources {
		if resource.kind == "board" {
			if _, err := a.createMovedBoard(ctx, destCfg, resource, moveID, sourceRef, sourceURL, mapping); err != nil {
				return nil, err
			}
		}
	}
	for _, resource := range resources {
		if resource.kind == "card" {
			input, err := moveCardCreateInput(resource.work, moveID, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, mapping)
			if err != nil {
				return nil, err
			}
			if board := moveFieldString(resource.work, "board_ref"); board != "" {
				if mapped := moveRewriteRef(board, mapping); mapped != board {
					input["board_ref"] = mapped
				}
			}
			destWork, exists, err := moveGetWorkIfExists(ctx, a, destCfg, resource.destinationRef)
			if err != nil {
				return nil, err
			}
			authority := moveFieldString(asMap(resource.work["source"]), "authority")
			if exists && (authority == "" || authority == "nexus") && !moveHasMarker(asMap(destWork["workspace_move"]), moveID) {
				return nil, errnorm.New(errnorm.KindRemote, "destination_id_conflict", "destination card id is already used by an unrelated card")
			}
			if !exists || authority != "" && authority != "nexus" {
				if _, err := moveCall(ctx, a, destCfg, http.MethodPost, "/work", input); err != nil {
					return nil, err
				}
			}
			finalMarker := moveMarker(moveID, "card", resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "complete")
			if _, err := a.updateMoveWorkMarker(ctx, destCfg, resource.destinationRef, finalMarker); err != nil {
				return nil, err
			}
		}
	}

	documentRefs := moveRewriteRefs(sourceTopic["document_refs"], mapping)
	boardRefs := moveRewriteRefs(sourceTopic["board_refs"], mapping)
	relatedRefs := moveRewriteRefs(sourceTopic["related_refs"], mapping)
	topicMarker["status"] = "archiving"
	if _, err := a.updateMoveTopicMarker(ctx, destCfg, destinationTopicRef, topicMarker, map[string]any{
		"owner_refs":    []string{},
		"document_refs": documentRefs, "board_refs": boardRefs, "related_refs": relatedRefs,
	}); err != nil {
		return nil, err
	}
	topicMarker["status"] = "complete"
	if _, err := a.updateMoveTopicMarker(ctx, destCfg, destinationTopicRef, topicMarker, map[string]any{}); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		resource.marker = moveMarker(moveID, resource.kind, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "complete")
		if resource.kind == "card" {
			authority := moveFieldString(asMap(resource.work["source"]), "authority")
			if authority == "" || authority == "nexus" {
				destinationBoardRef := moveRewriteRef(moveFieldString(resource.work, "board_ref"), mapping)
				markCuratedTombstone(resource.marker, resource.destinationRef, moveURL(destCfg, "card", resource.destinationRef, destinationBoardRef))
			} else {
				resource.marker["source_action"] = "archive"
			}
			if _, err := a.updateMoveWorkMarker(ctx, sourceCfg, resource.sourceRef, resource.marker); err != nil {
				return nil, err
			}
		}
	}
	if _, err := a.updateMoveTopicMarker(ctx, sourceCfg, sourceRef, topicMarker, map[string]any{}); err != nil {
		return nil, err
	}
	for _, resource := range resources {
		if resource.kind == "board" && moveState(resource.object) != "archived" {
			if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/boards/"+url.PathEscape(resource.sourceRef)+"/archive", map[string]any{}); err != nil {
				return nil, err
			}
		}
		if resource.kind == "document" && moveState(resource.object) != "archived" {
			if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/docs/"+url.PathEscape(resource.sourceRef)+"/archive", map[string]any{}); err != nil {
				return nil, err
			}
		}
		if resource.kind == "card" && moveState(resource.work) != "archived" {
			if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/cards/"+url.PathEscape(resource.sourceRef)+"/archive", map[string]any{}); err != nil {
				return nil, err
			}
		}
	}
	if _, err := moveCall(ctx, a, sourceCfg, http.MethodPost, "/topics/"+url.PathEscape(sourceRef)+"/archive", map[string]any{}); err != nil {
		return nil, err
	}
	actions := make([]map[string]any, 0, len(resources)*2+2)
	actions = append(actions, map[string]any{"action": "create_or_resume", "kind": "topic", "source_ref": sourceRef, "destination_ref": destinationTopicRef})
	for _, resource := range resources {
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

func (a *App) readTopicMoveResources(ctx context.Context, sourceCfg, destCfg config.Resolved, topic, workspace map[string]any, moveID string) ([]*topicMoveResource, error) {
	refs := map[string]map[string]struct{}{"document": {}, "board": {}, "card": {}}
	for _, spec := range []struct{ key, kind string }{{"documents", "document"}, {"boards", "board"}, {"cards", "card"}} {
		for _, ref := range moveListRefs(workspace, spec.key, spec.kind) {
			if refs[spec.kind] == nil {
				refs[spec.kind] = map[string]struct{}{}
			}
			refs[spec.kind][ref] = struct{}{}
		}
	}
	for _, ref := range moveStringList(topic["document_refs"]) {
		refs["document"][ref] = struct{}{}
	}
	for _, ref := range moveStringList(topic["board_refs"]) {
		refs["board"][ref] = struct{}{}
	}
	for _, ref := range moveStringList(topic["related_refs"]) {
		kind, _, ok := strings.Cut(ref, ":")
		if ok && refs[kind] != nil {
			refs[kind][ref] = struct{}{}
		}
	}
	if previous := asMap(topic["workspace_move"]); moveHasMarker(previous, moveID) {
		for _, raw := range asSlice(previous["resources"]) {
			item := asMap(raw)
			kind, ref := moveFieldString(item, "kind"), moveFieldString(item, "source_ref")
			if refs[kind] != nil && ref != "" {
				refs[kind][ref] = struct{}{}
			}
		}
	}
	items := make([]*topicMoveResource, 0)
	for _, kind := range []string{"document", "board", "card"} {
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
			object, err := moveRead(ctx, a, sourceCfg, path, responseKey)
			if err != nil {
				return nil, fmt.Errorf("read source %s %s: %w", kind, ref, err)
			}
			if kind == "document" {
				full, err := moveCall(ctx, a, sourceCfg, http.MethodGet, path, nil)
				if err != nil {
					return nil, err
				}
				object["revision"] = full["revision"]
			}
			resourceRef := firstNonEmpty(moveFieldString(object, "ref"), ref)
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

func (a *App) createMovedDocument(ctx context.Context, sourceCfg, destCfg config.Resolved, resource *topicMoveResource, moveID string, mapping map[string]string) (map[string]any, error) {
	doc := resource.object
	marker := moveMarker(moveID, "document", resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "pending")
	docInput := map[string]any{
		"document_id": resource.destinationID,
		"thread_id":   resource.destinationThreadID,
		"title":       doc["title"], "summary": doc["summary"], "source": doc["source"],
		"tags": doc["tags"], "hosts": doc["hosts"], "verified_at": doc["verified_at"],
		"provenance": moveRewriteProvenance(doc["provenance"], mapping), "workspace_move": marker,
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
	marker := moveMarker(moveID, "board", resource.sourceRef, resource.sourceURL, cfg.BaseURL, resource.destinationRef, "pending")
	boardInput := map[string]any{
		"id": resource.destinationID, "thread_id": resource.destinationThreadID, "title": board["title"], "summary": board["summary"],
		"primary_topic_ref": moveRewriteRef(moveFieldString(board, "primary_topic_ref"), mapping),
		"document_refs":     moveRewriteRefs(board["document_refs"], mapping),
		"pinned_refs":       moveRewriteRefs(board["pinned_refs"], mapping),
		"column_schema":     board["column_schema"],
		"provenance":        moveRewriteProvenance(board["provenance"], mapping), "workspace_move": marker,
	}
	if moveFieldString(boardInput, "primary_topic_ref") == "" {
		boardInput["primary_topic_ref"] = topicRef
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

func moveDocumentExistingMarker(object map[string]any) map[string]any {
	return asMap(asMap(asMap(object["revision"])["artifact"])["workspace_move"])
}
