package app

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	connectionMaps    map[string]string
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
	var connectionMaps trackedStrings
	fs.Var(&target, "to", "Destination workspace alias")
	fs.Var(&connectionMaps, "connection-map", "Map a source connection id to a destination-local connection id (source=destination)")
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
	out.connectionMaps = make(map[string]string, len(connectionMaps.values))
	for _, raw := range connectionMaps.values {
		sourceID, destinationID, ok := strings.Cut(raw, "=")
		sourceID, destinationID = strings.TrimSpace(sourceID), strings.TrimSpace(destinationID)
		if !ok || sourceID == "" || destinationID == "" {
			return out, errnorm.Usage("invalid_request", "--connection-map must be <source-connection-id>=<destination-connection-id>")
		}
		if previous, exists := out.connectionMaps[sourceID]; exists && previous != destinationID {
			return out, errnorm.Usage("invalid_request", "--connection-map contains conflicting mappings for source connection "+sourceID)
		}
		out.connectionMaps[sourceID] = destinationID
	}
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
		result, err := a.moveCard(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun, parsed.connectionMaps)
		return result, name, err
	}
	result, err := a.moveTopic(ctx, cfg, targetCfg, parsed.ref, parsed.dryRun, parsed.connectionMaps)
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
	_, suffix, ok := strings.Cut(moveNormalizeRef(ref), ":")
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
		"phase":                     "snapshot",
	}
}

func moveSourceRevision(kind string, object map[string]any) string {
	switch kind {
	case "card":
		return fmt.Sprintf("version:%v|head:%s|number:%v", object["version"], moveFieldString(object, "head_revision_ref"), object["head_revision_number"])
	case "document":
		revision := asMap(object["revision"])
		return fmt.Sprintf("updated_at:%s|head:%s|revision:%v", moveFieldString(object, "updated_at"), firstNonEmpty(moveFieldString(object, "head_revision_ref"), moveFieldString(revision, "ref")), revision["revision_number"])
	default:
		return "updated_at:" + moveFieldString(object, "updated_at")
	}
}

func moveSnapshotFingerprint(kind string, object map[string]any) string {
	fields := map[string][]string{
		"topic":    {"id", "ref", "handle", "title", "summary", "owner_refs", "document_refs", "board_refs", "related_refs", "provenance"},
		"board":    {"id", "ref", "handle", "thread_id", "title", "summary", "primary_topic_ref", "document_refs", "pinned_refs", "column_schema", "provenance"},
		"card":     {"id", "ref", "handle", "board_ref", "title", "summary", "definition_of_done", "phase", "priority", "due_at", "risk", "source", "topic_ref", "document_ref", "related_refs", "plan", "project_ref", "relations"},
		"document": {"document", "revision"},
	}
	selected := make(map[string]any, len(fields[kind]))
	for _, key := range fields[kind] {
		if value, ok := object[key]; ok {
			selected[key] = value
		}
	}
	if kind == "document" {
		doc := asMap(object["document"])
		if doc == nil {
			doc = object
		}
		revision := asMap(object["revision"])
		metadata := map[string]any{}
		for _, key := range []string{"id", "ref", "handle", "thread_id", "title", "summary", "source", "tags", "hosts", "verified_at", "provenance", "refs", "subject_ref"} {
			if value, ok := doc[key]; ok {
				metadata[key] = value
			}
		}
		content := map[string]any{}
		for _, key := range []string{"ref", "revision_number", "content", "content_base64", "content_type", "refs"} {
			if value, ok := revision[key]; ok {
				content[key] = value
			}
		}
		selected = map[string]any{"document": metadata, "revision": content}
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

func moveSourceObjectID(kind, sourceRef string, object map[string]any) string {
	id := moveFieldString(object, "id")
	if id == "" && kind == "document" {
		id = moveFieldString(asMap(object["document"]), "id")
	}
	if id == "" {
		id = moveRefID(sourceRef)
	}
	return id
}

func moveJournalKey(sourceWorkspace, kind, sourceObjectID, sourceRevision string) string {
	encoded := strings.TrimRight(sourceWorkspace, "/") + "\n" + kind + "\n" + sourceObjectID + "\n" + sourceRevision
	sum := sha256.Sum256([]byte(encoded))
	return "mj_" + hex.EncodeToString(sum[:16])
}

func moveAttachSnapshot(marker map[string]any, sourceWorkspace, kind, sourceRef string, object map[string]any) {
	revision := moveSourceRevision(kind, object)
	objectID := moveSourceObjectID(kind, sourceRef, object)
	marker["source_workspace_url"] = strings.TrimRight(sourceWorkspace, "/")
	marker["source_object_id"] = objectID
	marker["source_revision"] = revision
	marker["source_fingerprint"] = moveSnapshotFingerprint(kind, object)
	marker["journal_key"] = moveJournalKey(sourceWorkspace, kind, objectID, revision)
}

func moveValidateSnapshot(marker map[string]any, sourceWorkspace, kind, sourceRef string, object map[string]any, allowJournalRevision bool) error {
	wantRevision := moveFieldString(marker, "source_revision")
	wantFingerprint := moveFieldString(marker, "source_fingerprint")
	gotRevision := moveSourceRevision(kind, object)
	gotFingerprint := moveSnapshotFingerprint(kind, object)
	if wantRevision == "" || wantFingerprint == "" {
		return errnorm.New(errnorm.KindRemote, "move_journal_missing_snapshot", "destination move journal has no source revision snapshot; refusing to resume without a safe fence")
	}
	if wantFingerprint != gotFingerprint {
		return errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s changed after move snapshot (%s, now %s); no remaining source resources will be archived", kind, sourceRef, wantRevision, gotRevision))
	}
	if kind == "card" && allowJournalRevision {
		if expectedVersion := moveInt64(marker["source_archive_version"]); expectedVersion > 0 && moveInt64(object["version"]) != expectedVersion {
			return errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source card %s revision changed after the verified move journal; source remains active", sourceRef))
		}
	}
	if wantRevision != gotRevision && !allowJournalRevision {
		return errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source %s %s revision changed after move snapshot (%s, now %s); no remaining source resources will be archived", kind, sourceRef, wantRevision, gotRevision))
	}
	objectID := moveSourceObjectID(kind, sourceRef, object)
	if moveFieldString(marker, "source_object_id") != objectID {
		return errnorm.New(errnorm.KindRemote, "move_journal_key_mismatch", "move journal source object id does not match the object being resumed")
	}
	if expected := moveJournalKey(sourceWorkspace, kind, objectID, wantRevision); moveFieldString(marker, "journal_key") != expected {
		return errnorm.New(errnorm.KindRemote, "move_journal_key_mismatch", "move journal source key does not match its source workspace, object, and revision")
	}
	return nil
}

func moveRewriteValue(value any, mapping map[string]string) any {
	switch typed := value.(type) {
	case string:
		return moveRewriteRef(typed, mapping)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = moveRewriteValue(item, mapping)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for index, item := range typed {
			out[index] = moveRewriteRef(item, mapping)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = moveRewriteValue(item, mapping)
		}
		return out
	default:
		return value
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

func moveReadCardPlan(ctx context.Context, a *App, cfg config.Resolved, ref string) (map[string]any, error) {
	return moveCall(ctx, a, cfg, http.MethodGet, "/cards/"+url.PathEscape(ref)+"/plan", nil)
}

func moveHydrateCardIdentity(ctx context.Context, a *App, cfg config.Resolved, work map[string]any) error {
	ref := moveFieldString(work, "ref")
	if ref == "" {
		return errnorm.Internal("move_response_invalid", "card read omitted its ref")
	}
	card, err := moveRead(ctx, a, cfg, "/cards/"+url.PathEscape(ref), "card")
	if err != nil {
		return err
	}
	cardID := moveFieldString(card, "id")
	cardRef := moveFieldString(card, "ref")
	if cardID == "" || cardRef == "" {
		return errnorm.Internal("move_response_invalid", "canonical card read omitted its id or ref")
	}
	if workID := moveFieldString(work, "id"); workID != "" && workID != cardID {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card identity changed while the move snapshot was being read; retry the move")
	}
	if workUpdatedAt, cardUpdatedAt := moveFieldString(work, "updated_at"), moveFieldString(card, "updated_at"); workUpdatedAt != "" && cardUpdatedAt != "" && workUpdatedAt != cardUpdatedAt {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card changed while its canonical identity was being read; retry the move")
	}
	work["id"] = cardID
	work["ref"] = cardRef
	if handle := moveFieldString(card, "handle"); handle != "" {
		work["handle"] = handle
	}
	return nil
}

// Cards expose a plan in several read projections, but only /cards/{ref}/plan
// reads the canonical plan store. Hydrate the snapshot from that API and fence
// it against the card read so a concurrent plan edit cannot be lost.
func moveHydrateCardPlan(ctx context.Context, a *App, cfg config.Resolved, work map[string]any) error {
	ref := moveFieldString(work, "ref")
	if ref == "" {
		return errnorm.Internal("move_response_invalid", "card read omitted its ref")
	}
	if err := moveHydrateCardIdentity(ctx, a, cfg, work); err != nil {
		return err
	}
	ref = moveFieldString(work, "ref")
	plan, err := moveReadCardPlan(ctx, a, cfg, ref)
	if err != nil {
		return err
	}
	cardUpdatedAt := moveFieldString(work, "updated_at")
	planUpdatedAt := moveFieldString(plan, "if_updated_at")
	if cardUpdatedAt != "" && planUpdatedAt != cardUpdatedAt {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card or plan changed while the move snapshot was being read; retry the move")
	}
	work["plan"] = plan["plan"]
	return nil
}

func moveWriteCardPlan(ctx context.Context, a *App, cfg config.Resolved, ref string, expected any) error {
	if expected == nil {
		return nil
	}
	path := "/cards/" + url.PathEscape(ref) + "/plan"
	current, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if moveJSONEqual(expected, current["plan"]) {
		return nil
	}
	updatedAt := moveFieldString(current, "if_updated_at")
	if updatedAt == "" {
		return errnorm.Internal("move_response_invalid", "canonical plan read omitted its concurrency token")
	}
	_, err = moveCall(ctx, a, cfg, http.MethodPut, path, map[string]any{
		"plan": expected, "if_updated_at": updatedAt,
	})
	return err
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
	normalized := moveNormalizeRef(ref)
	if mapped := mapping[normalized]; mapped != "" {
		return moveNormalizeRef(mapped)
	}
	if normalized != ref {
		if mapped := mapping[ref]; mapped != "" {
			return moveNormalizeRef(mapped)
		}
	}
	return normalized
}

// Normalize aliases once before they enter the move plan. Core accepts doc:
// for documents and bare source URLs for cards; every move phase uses this form.
func moveNormalizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return "card:" + ref
	}
	kind, suffix, ok := strings.Cut(ref, ":")
	if !ok {
		return ref
	}
	kind, suffix = strings.TrimSpace(kind), strings.TrimSpace(suffix)
	if kind == "doc" {
		kind = "document"
	}
	return kind + ":" + suffix
}

func moveRefKind(ref string) string {
	kind, _, ok := strings.Cut(moveNormalizeRef(ref), ":")
	if !ok {
		return ""
	}
	return kind
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

func moveInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func moveInt64Pointer(value int64) *int64 { return &value }

func moveState(object map[string]any) string { return strings.TrimSpace(anyString(object["state"])) }

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
	if err := moveWriteCardPlan(ctx, a, destCfg, destinationRef, moveRewriteValue(source["plan"], refMapping)); err != nil {
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

type topicMoveResource struct {
	kind, sourceRef, sourceURL, destinationID, destinationRef, destinationThreadID string
	object                                                                         map[string]any
	work                                                                           map[string]any
	marker                                                                         map[string]any
	problem                                                                        string
	destinationConnection                                                          string
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

func cloneMoveMap(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func stringPointer(value string) *string { return &value }

func resourceMarker(moveID string, resource *topicMoveResource, sourceCfg, destCfg config.Resolved) map[string]any {
	marker := moveMarker(moveID, resource.kind, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, "pending")
	moveAttachSnapshot(marker, sourceCfg.BaseURL, resource.kind, resource.sourceRef, resource.object)
	marker["phase"] = "create_destination"
	return marker
}

func moveManifestResource(resources any, kind, sourceRef string) map[string]any {
	sourceRef = moveNormalizeRef(sourceRef)
	for _, raw := range asSlice(resources) {
		item := asMap(raw)
		if moveFieldString(item, "kind") == kind && moveNormalizeRef(moveFieldString(item, "source_ref")) == sourceRef {
			return item
		}
	}
	return nil
}

func moveJSONEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func moveVerifyFields(kind, ref string, expected, actual map[string]any, keys []string) error {
	for _, key := range keys {
		want, ok := expected[key]
		if !ok || want == nil {
			continue
		}
		got := actual[key]
		if moveIsCollectionField(key) {
			want = moveCanonicalCollection(want)
			got = moveCanonicalCollection(got)
		}
		if !moveJSONEqual(want, got) {
			return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination %s %s failed verification for %s", kind, ref, key))
		}
	}
	return nil
}

func moveCanonicalDestinationRef(ctx context.Context, a *App, cfg config.Resolved, ref string) (string, error) {
	ref = moveNormalizeRef(ref)
	kind, value, ok := strings.Cut(ref, ":")
	if !ok || value == "" {
		return ref, nil
	}
	path, key := "", ""
	switch kind {
	case "card":
		// The work projection intentionally omits the card's internal UUID.
		// Canonical card reads carry it and let verification treat public handles
		// and UUID refs as aliases of the same destination object.
		path, key = "/cards/"+url.PathEscape(value), "card"
	case "document":
		path, key = "/docs/"+url.PathEscape(value), "document"
	case "topic":
		path, key = "/topics/"+url.PathEscape(value), "topic"
	case "board":
		path, key = "/boards/"+url.PathEscape(value), "board"
	case "thread":
		path, key = "/threads/"+url.PathEscape(value), "thread"
	default:
		return ref, nil
	}
	object, err := moveRead(ctx, a, cfg, path, key)
	if err != nil {
		if errnorm.Normalize(err).Code == "not_found" {
			return ref, nil
		}
		return "", err
	}
	id := moveFieldString(object, "id")
	if id == "" {
		return ref, nil
	}
	return moveNormalizeRef(kind + ":" + id), nil
}

func moveVerifyRef(ctx context.Context, a *App, cfg config.Resolved, kind, ref string, expected, actual map[string]any) error {
	want, wantOK := expected[ref]
	if !wantOK || want == nil {
		return nil
	}
	wantRef, gotRef := strings.TrimSpace(anyString(want)), strings.TrimSpace(anyString(actual[ref]))
	canonicalWant, err := moveCanonicalDestinationRef(ctx, a, cfg, wantRef)
	if err != nil {
		return err
	}
	canonicalGot, err := moveCanonicalDestinationRef(ctx, a, cfg, gotRef)
	if err != nil {
		return err
	}
	if canonicalWant != canonicalGot {
		return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination %s failed verification for %s: expected %s, got %s", kind, ref, canonicalWant, canonicalGot))
	}
	return nil
}

func moveVerifyRefList(ctx context.Context, a *App, cfg config.Resolved, kind, field string, expected, actual map[string]any) error {
	want := moveStringList(expected[field])
	got := moveStringList(actual[field])
	canonicalWant := make([]string, 0, len(want))
	canonicalGot := make([]string, 0, len(got))
	for _, ref := range want {
		canonical, err := moveCanonicalDestinationRef(ctx, a, cfg, ref)
		if err != nil {
			return err
		}
		canonicalWant = append(canonicalWant, canonical)
	}
	for _, ref := range got {
		canonical, err := moveCanonicalDestinationRef(ctx, a, cfg, ref)
		if err != nil {
			return err
		}
		canonicalGot = append(canonicalGot, canonical)
	}
	canonicalWant, canonicalGot = moveUnique(canonicalWant), moveUnique(canonicalGot)
	if !moveJSONEqual(canonicalWant, canonicalGot) {
		return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination %s failed verification for %s: expected %v, got %v", kind, field, canonicalWant, canonicalGot))
	}
	return nil
}

func moveIsCollectionField(key string) bool {
	switch key {
	case "owner_refs", "document_refs", "board_refs", "related_refs", "refs", "pinned_refs":
		return true
	default:
		return false
	}
}

func moveCanonicalCollection(value any) []string {
	refs := moveStringList(value)
	for index := range refs {
		refs[index] = moveNormalizeRef(refs[index])
	}
	return moveUnique(refs)
}

func (a *App) readMoveSourceResource(ctx context.Context, cfg config.Resolved, resource *topicMoveResource) (map[string]any, error) {
	path, key := "", ""
	switch resource.kind {
	case "document":
		path, key = "/docs/"+url.PathEscape(resource.sourceRef), "document"
	case "board":
		path, key = "/boards/"+url.PathEscape(resource.sourceRef), "board"
	case "card":
		path, key = "/work/"+url.PathEscape(resource.sourceRef), "work"
	default:
		return nil, errnorm.Internal("move_resource_kind_invalid", "unsupported move resource kind "+resource.kind)
	}
	if resource.kind != "document" {
		object, err := moveRead(ctx, a, cfg, path, key)
		if err != nil {
			return nil, err
		}
		if resource.kind == "card" {
			if err := moveHydrateCardPlan(ctx, a, cfg, object); err != nil {
				return nil, err
			}
		}
		return object, nil
	}
	body, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	doc := asMap(body["document"])
	if doc == nil {
		return nil, errnorm.Internal("move_response_invalid", "document read omitted document")
	}
	out := cloneMoveMap(doc)
	out["revision"] = body["revision"]
	return out, nil
}

func (a *App) verifyMovedResource(ctx context.Context, sourceCfg, destCfg config.Resolved, resource *topicMoveResource, moveID string, mapping map[string]string, topicRef, topicURL string) error {
	path := "/" + map[string]string{"document": "docs", "board": "boards", "card": "work"}[resource.kind] + "/" + url.PathEscape(resource.destinationRef)
	var actual map[string]any
	var err error
	switch resource.kind {
	case "document":
		var body map[string]any
		body, err = moveCall(ctx, a, destCfg, http.MethodGet, path, nil)
		if err == nil {
			actual = cloneMoveMap(asMap(body["document"]))
			actual["revision"] = asMap(body["revision"])
		}
	case "board":
		actual, err = moveRead(ctx, a, destCfg, path, "board")
	case "card":
		actual, err = moveRead(ctx, a, destCfg, path, "work")
	default:
		return errnorm.Internal("move_resource_kind_invalid", "unsupported move resource kind "+resource.kind)
	}
	if err != nil {
		return err
	}
	marker := asMap(actual["workspace_move"])
	switch resource.kind {
	case "document":
		marker = moveDocumentExistingMarker(map[string]any{"revision": actual["revision"]})
	case "board":
		marker, err = moveBoardExistingMarker(ctx, a, destCfg, actual)
		if err != nil {
			return err
		}
	}
	if !moveHasMarker(marker, moveID) || moveFieldString(marker, "source_fingerprint") != moveFieldString(resource.marker, "source_fingerprint") || moveFieldString(marker, "source_revision") != moveFieldString(resource.marker, "source_revision") {
		return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination %s %s is missing the matching snapshot journal", resource.kind, resource.destinationRef))
	}
	switch resource.kind {
	case "card":
		expected, err := moveCardCreateInput(resource.work, moveID, resource.sourceRef, resource.sourceURL, destCfg.BaseURL, resource.destinationRef, mapping, resource.destinationConnection)
		if err != nil {
			return err
		}
		if board := moveFieldString(resource.work, "board_ref"); board != "" {
			if mapped := moveRewriteRef(board, mapping); mapped != board {
				expected["board_ref"] = mapped
			}
		}
		if err := moveVerifyFields("card", resource.destinationRef, expected, actual, []string{"title", "summary", "definition_of_done", "priority", "due_at", "risk", "source", "phase"}); err != nil {
			return err
		}
		for _, field := range []string{"board_ref", "topic_ref", "document_ref"} {
			if err := moveVerifyRef(ctx, a, destCfg, "card", field, expected, actual); err != nil {
				return err
			}
		}
		if err := moveVerifyRefList(ctx, a, destCfg, "card", "related_refs", expected, actual); err != nil {
			return err
		}
		plan, err := moveReadCardPlan(ctx, a, destCfg, resource.destinationRef)
		if err != nil {
			return err
		}
		if want := moveRewriteValue(resource.work["plan"], mapping); !moveJSONEqual(want, plan["plan"]) {
			return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination card %s failed canonical plan verification", resource.destinationRef))
		}
	case "board":
		expectedBoardRefs := moveRewriteRefs(resource.object["refs"], mapping)
		if topicRef != "" {
			expectedBoardRefs = moveUnique(append(expectedBoardRefs, topicRef))
		}
		expected := map[string]any{
			"title": moveRewriteValue(resource.object["title"], mapping), "summary": resource.object["summary"],
			"primary_topic_ref": moveRewriteRef(moveFieldString(resource.object, "primary_topic_ref"), mapping),
			"refs":              expectedBoardRefs,
			"document_refs":     moveRewriteRefs(resource.object["document_refs"], mapping), "pinned_refs": moveRewriteRefs(resource.object["pinned_refs"], mapping),
			"column_schema": resource.object["column_schema"],
		}
		if moveFieldString(expected, "primary_topic_ref") == "" {
			expected["primary_topic_ref"] = topicRef
		}
		if err := moveVerifyFields("board", resource.destinationRef, expected, actual, []string{"title", "summary", "column_schema"}); err != nil {
			return err
		}
		for _, field := range []string{"primary_topic_ref"} {
			if err := moveVerifyRef(ctx, a, destCfg, "board", field, expected, actual); err != nil {
				return err
			}
		}
		for _, field := range []string{"refs", "document_refs", "pinned_refs"} {
			if err := moveVerifyRefList(ctx, a, destCfg, "board", field, expected, actual); err != nil {
				return err
			}
		}
	case "document":
		expectedRefs := moveRewriteRefs(resource.object["refs"], mapping)
		expected := map[string]any{"title": resource.object["title"], "summary": resource.object["summary"], "refs": expectedRefs, "source": resource.object["source"]}
		if err := moveVerifyFields("document", resource.destinationRef, expected, actual, []string{"title", "summary", "source"}); err != nil {
			return err
		}
		if err := moveVerifyRefList(ctx, a, destCfg, "document", "refs", expected, actual); err != nil {
			return err
		}
		wantRevision, gotRevision := asMap(resource.object["revision"]), asMap(actual["revision"])
		if moveFieldString(wantRevision, "content_type") != moveFieldString(gotRevision, "content_type") {
			return errnorm.New(errnorm.KindRemote, "destination_verification_failed", "destination document content type differs from its source snapshot")
		}
		if moveFieldString(wantRevision, "content_type") == "binary" {
			wantBytes, wantErr := base64.StdEncoding.DecodeString(moveFieldString(wantRevision, "content_base64"))
			gotBytes, gotErr := base64.StdEncoding.DecodeString(moveFieldString(gotRevision, "content_base64"))
			if wantErr != nil || gotErr != nil || string(wantBytes) != string(gotBytes) {
				return errnorm.New(errnorm.KindRemote, "destination_verification_failed", "destination document bytes differ from the source snapshot")
			}
		} else if !moveJSONEqual(wantRevision["content"], gotRevision["content"]) {
			return errnorm.New(errnorm.KindRemote, "destination_verification_failed", "destination document content differs from the source snapshot")
		}
		if err := moveVerifyRefList(ctx, a, destCfg, "document revision", "refs", map[string]any{"refs": moveRewriteRefs(wantRevision["refs"], mapping)}, gotRevision); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) verifyMovedTopic(ctx context.Context, cfg config.Resolved, destinationRef string, source map[string]any, moveID, sourceRef, sourceURL string, mapping map[string]string) error {
	destination, err := moveRead(ctx, a, cfg, "/topics/"+url.PathEscape(destinationRef), "topic")
	if err != nil {
		return err
	}
	marker := asMap(destination["workspace_move"])
	expectedRevision := moveSourceRevision("topic", source)
	if sourceMarker := asMap(source["workspace_move"]); moveHasMarker(sourceMarker, moveID) && moveFieldString(sourceMarker, "source_revision") != "" {
		expectedRevision = moveFieldString(sourceMarker, "source_revision")
	}
	if !moveHasMarker(marker, moveID) || moveFieldString(marker, "source_fingerprint") != moveSnapshotFingerprint("topic", source) || moveFieldString(marker, "source_revision") != expectedRevision {
		return errnorm.New(errnorm.KindRemote, "destination_verification_failed", "destination topic is missing the matching source snapshot journal")
	}
	expected := map[string]any{
		"title": source["title"], "summary": source["summary"],
		"document_refs": moveRewriteRefs(source["document_refs"], mapping),
		// Topic moves leave source boards in place. Workspace projections expose
		// associated boards as shared context, not as owned move resources.
		"board_refs":   []string{},
		"related_refs": moveRewriteRefs(source["related_refs"], mapping),
	}
	if err := moveVerifyFields("topic", destinationRef, expected, destination, []string{"title", "summary"}); err != nil {
		return err
	}
	for _, field := range []string{"document_refs", "board_refs", "related_refs"} {
		if err := moveVerifyRefList(ctx, a, cfg, "topic", field, expected, destination); err != nil {
			return err
		}
	}
	_ = sourceRef
	_ = sourceURL
	return nil
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
		"provenance": moveRewriteProvenance(sourceTopic["provenance"], mapping), "workspace_move": topicCreateMarker,
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
			if err := moveWriteCardPlan(ctx, a, destCfg, resource.destinationRef, moveRewriteValue(resource.work["plan"], mapping)); err != nil {
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
		"provenance":    moveRewriteProvenance(board["provenance"], mapping), "workspace_move": marker,
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
