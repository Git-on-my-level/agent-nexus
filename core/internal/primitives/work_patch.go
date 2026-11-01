// Work patching: annotation validation, reference validation, native-mutation
// checks, and PatchWork.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// WorkAnnotationKeys returns the keys writable through local work annotations.
func WorkAnnotationKeys() []string {
	return []string{"top_level", "cadence", "goal", "labels", "roles", "project_ref", "priority", "next_actor", "next_action", "blockers", "wake_condition", "start_at", "due_at", "relations", "executions", "workspace_move", "plan", "source_refs"}
}

// ValidateWorkAnnotations is shared by proposal validation and canonical writes.
func ValidateWorkAnnotations(patch map[string]any) error {
	allowed := WorkAnnotationKeys()
	allowedSet := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = true
	}
	invalid := []string{}
	for key := range patch {
		if !allowedSet[key] {
			invalid = append(invalid, key)
		}
	}
	sort.Strings(invalid)
	if len(invalid) > 0 {
		return workInvalid("annotation keys %s are not allowed; allowed keys: %s", strings.Join(invalid, ", "), strings.Join(allowed, ", "))
	}
	if len(patch) == 0 {
		return workInvalid("annotation patch required; expected a nonempty JSON object")
	}
	return validateWorkLocal(patch)
}

func (s *Store) PatchWork(ctx context.Context, actor, identifier string, version int64, patch map[string]any) (workResult map[string]any, workErr error) {
	if actor == "" {
		return nil, workInvalid("actor required")
	}
	if err := ValidateWorkAnnotations(patch); err != nil {
		return nil, err
	}
	// The work tree decides initiatives; a top_level annotation would be a
	// silent no-op. Point at the one hierarchy verb instead.
	if _, ok := patch["top_level"]; ok {
		return nil, workInvalid("top_level is replaced by the work tree: use anx work move %s --root --reason <why> (or --under <parent>)", firstNonEmptyString(identifier, "<card>"))
	}
	patch = workClone(patch)
	if err := s.validateWorkReferences(ctx, patch); err != nil {
		return nil, err
	}
	w, err := s.GetWork(ctx, identifier)
	if err != nil {
		return nil, err
	}
	id := workString(w["id"])
	if err = s.ensureWorkMetadata(ctx, id, actor); err != nil {
		return nil, err
	}
	m, v, _, _, _, err := s.workMetadata(ctx, id)
	if err != nil {
		return nil, err
	}
	if v != version {
		return nil, ErrConflict
	}
	sourceChanged := sourceEvidenceChanged(m["source_refs"], patch["source_refs"])
	for k, val := range patch {
		m[k] = val
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, ok := patch["cadence"]; ok {
		m["cadence_set_at"] = now
	}
	if _, ok := patch["next_action"]; ok {
		m["next_action_at"] = now
	}
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return nil, err
	}
	// The transaction now holds the workspace writer: from here any
	// failure leaves the outcome unknown unless it is a deterministic
	// request rejection.
	defer func() {
		if workErr != nil {
			workErr = mutationOutcomeAfterBegin(workErr)
		}
	}()
	defer tx.Rollback()
	// Preserve concurrent observation progress: observations do not bump the
	// annotation version, and must not be overwritten by this earlier snapshot.
	var evidenceAt, evidenceRefs any
	if sourceChanged {
		evidenceAt = time.Now().UTC().Format(time.RFC3339Nano)
		raw, _ := json.Marshal(m["source_refs"])
		evidenceRefs = string(raw)
	}
	res, err := tx.ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(?, '$._anx_source_evidence_at', COALESCE(?,json_extract(metadata_json,'$._anx_source_evidence_at')), '$._anx_source_evidence_refs', json(COALESCE(?,json_extract(metadata_json,'$._anx_source_evidence_refs'),'null'))),version=version+1,updated_at=?,updated_by=? WHERE card_id=? AND version=?`, workJSON(m), evidenceAt, evidenceRefs, now, actor, id, version)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrConflict
	}
	if err = insertWorkEvent(ctx, tx, actor, w, "card_updated", "Work annotations updated", map[string]any{"changed_fields": []string{"work_annotations"}, "patch": patch}); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, &MutationOutcomeUnknown{Cause: err}
	}
	out, err := s.GetWork(ctx, id)
	if err != nil {
		return nil, &MutationOutcomeUnknown{Cause: err}
	}
	return out, nil
}

func (s *Store) validateWorkReferences(ctx context.Context, m map[string]any) error {
	if ref := workString(m["project_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "topic", Ref: ref})
		if err != nil {
			return workLocalInvalid("project_ref", "must resolve to an existing workspace topic")
		}
		m["project_ref"] = resolved.CanonicalRef
	}
	if ref := workString(m["topic_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "topic", Ref: ref})
		if err != nil {
			return workLocalInvalid("topic_ref", "must resolve to an existing workspace topic")
		}
		m["topic_ref"] = resolved.CanonicalRef
	}
	if ref := workString(m["document_ref"]); ref != "" {
		resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "document", Ref: ref})
		if err != nil {
			return workLocalInvalid("document_ref", "must resolve to an existing workspace document")
		}
		m["document_ref"] = resolved.CanonicalRef
	}
	if _, exists := m["related_refs"]; exists {
		refs, err := optionalStringListField(m, "related_refs")
		if err != nil {
			return workLocalInvalid("related_refs", "must be an array of typed refs")
		}
		m["related_refs"] = refs
	}
	if value, ok := m["relations"]; ok {
		b, _ := json.Marshal(value)
		var relations []map[string]any
		if json.Unmarshal(b, &relations) != nil {
			return workLocalInvalid("relations", "must contain objects")
		}
		for _, relation := range relations {
			ref := workString(relation["ref"])
			kind := workString(relation["kind"])
			typ := ""
			if kind == "parent" || kind == "child" || kind == "depends_on" || kind == "merged_into" {
				typ = "card"
			}
			resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: typ, Ref: ref})
			if err != nil {
				if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidResourceRef) {
					return workLocalInvalid("relations", "relation ref must resolve inside this workspace")
				}
				return fmt.Errorf("resolve work relation: %w", err)
			}
			relation["ref"] = resolved.CanonicalRef
		}
		m["relations"] = relations
	}
	return nil
}

// ensureNativeWorkMutation prevents the legacy card surface from silently
// overriding an external system's source-owned state.
func ensureNativeWorkMutation(ctx context.Context, q queryRower, cardID string) error {
	var authority string
	err := q.QueryRowContext(ctx, `SELECT authority FROM work_metadata WHERE card_id=?`, cardID).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if authority != "nexus" {
		return invalidBoardRequest("external work is source-owned; request an authorized PM action")
	}
	return nil
}
