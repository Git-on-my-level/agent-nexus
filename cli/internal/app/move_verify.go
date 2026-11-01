// Post-move verification that destination resources and their refs match the
// source snapshot.

package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

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
		if want := moveRewritePlan(resource.work["plan"], mapping); !moveJSONEqual(want, plan["plan"]) {
			return errnorm.New(errnorm.KindRemote, "destination_verification_failed", fmt.Sprintf("destination card %s failed canonical plan verification", resource.destinationRef))
		}
	case "board":
		expectedBoardRefs := moveRewriteRefs(resource.object["refs"], mapping)
		if topicRef != "" {
			expectedBoardRefs = moveUnique(append(expectedBoardRefs, topicRef))
		}
		expected := map[string]any{
			"title": resource.object["title"], "summary": resource.object["summary"],
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
