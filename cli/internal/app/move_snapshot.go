// Deterministic move identity (stable ids, journal keys), source revision and
// object ids, snapshot fingerprints, and the provenance/manifest records
// attached to moved resources.

package app

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

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
		if expectedVersion := moveInt64(marker["source_archive_version"]); expectedVersion > 0 {
			currentVersion := moveInt64(object["version"])
			// The completed journal stores the pre-archive fence. Current cores
			// advance it once in the archive transaction; older cores leave it
			// unchanged. Only an archived, completed source may take that one
			// extra step. Restore or later edits must still conflict, even when
			// the snapshotted content and move marker are unchanged.
			ownArchive := moveState(object) == "archived" && moveFieldString(marker, "status") == "complete" && currentVersion == expectedVersion+1
			if currentVersion != expectedVersion && !ownArchive {
				return errnorm.New(errnorm.KindRemote, "source_changed", fmt.Sprintf("source card %s revision changed after the verified move journal (expected %d, now %d); source remains active", sourceRef, expectedVersion, currentVersion))
			}
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

func moveRewritePlan(value any, mapping map[string]string) any {
	// Plan's schema defines only steps[*].ref as a resource reference. Keep
	// titles and any other plan content byte-for-byte as provided.
	plan := asMap(value)
	steps, ok := plan["steps"].([]any)
	if plan == nil || !ok {
		return value
	}
	out := cloneMoveMap(plan)
	rewrittenSteps := make([]any, len(steps))
	for index, rawStep := range steps {
		step := asMap(rawStep)
		if step == nil {
			rewrittenSteps[index] = rawStep
			continue
		}
		rewrittenStep := cloneMoveMap(step)
		if ref, ok := step["ref"].(string); ok {
			rewrittenStep["ref"] = moveRewriteRef(ref, mapping)
		}
		rewrittenSteps[index] = rewrittenStep
	}
	out["steps"] = rewrittenSteps
	return out
}

func moveCopyProvenance(raw any) any {
	provenance := asMap(raw)
	if provenance == nil {
		return raw
	}
	return cloneMoveMap(provenance)
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
