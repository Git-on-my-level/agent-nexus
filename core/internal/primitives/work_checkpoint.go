package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const maxWorkCheckpointBytes = 16 * 1024

func checkpointObjectFields(m map[string]any, allowed ...string) error {
	set := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		set[key] = true
	}
	for key := range m {
		if !set[key] {
			return workInvalid("unknown checkpoint field %q", key)
		}
	}
	return nil
}

func checkpointText(value any, field string, min, max int) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", workInvalid("checkpoint %s must be a string", field)
	}
	n := utf8.RuneCountInString(text)
	if n < min || n > max || min > 0 && strings.TrimSpace(text) == "" {
		return "", workInvalid("checkpoint %s must contain %d..%d characters", field, min, max)
	}
	return text, nil
}

// normalizeWorkCheckpoint accepts only the small public checkpoint schema.
// JSON round-tripping also normalizes caller-provided typed slices/maps before
// validation and digesting.
func normalizeWorkCheckpoint(input map[string]any) (map[string]any, string, error) {
	if input == nil {
		return nil, "", workInvalid("checkpoint object required")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, "", workInvalid("checkpoint must be JSON encodable")
	}
	var content map[string]any
	if err = json.Unmarshal(raw, &content); err != nil || content == nil {
		return nil, "", workInvalid("checkpoint object required")
	}
	if err = checkpointObjectFields(content, "summary", "next_action", "blockers", "artifacts", "execution"); err != nil {
		return nil, "", err
	}
	if _, ok := content["summary"]; !ok {
		return nil, "", workInvalid("checkpoint summary required")
	}
	if _, ok := content["next_action"]; !ok {
		return nil, "", workInvalid("checkpoint next_action required")
	}
	content["summary"], err = checkpointText(content["summary"], "summary", 1, 4096)
	if err != nil {
		return nil, "", err
	}
	content["next_action"], err = checkpointText(content["next_action"], "next_action", 1, 4096)
	if err != nil {
		return nil, "", err
	}

	if value, exists := content["blockers"]; exists {
		items, ok := value.([]any)
		if !ok || len(items) > 16 {
			return nil, "", workInvalid("checkpoint blockers must contain at most 16 strings")
		}
		for i, item := range items {
			text, err := checkpointText(item, fmt.Sprintf("blockers[%d]", i), 0, 512)
			if err != nil {
				return nil, "", err
			}
			items[i] = text
		}
	}
	if value, exists := content["artifacts"]; exists {
		items, ok := value.([]any)
		if !ok || len(items) > 16 {
			return nil, "", workInvalid("checkpoint artifacts must contain at most 16 objects")
		}
		for i, item := range items {
			artifact, ok := item.(map[string]any)
			if !ok {
				return nil, "", workInvalid("checkpoint artifacts[%d] must be an object", i)
			}
			if err := checkpointObjectFields(artifact, "ref", "portability", "host"); err != nil {
				return nil, "", err
			}
			ref, exists := artifact["ref"]
			if !exists {
				return nil, "", workInvalid("checkpoint artifacts[%d].ref required", i)
			}
			artifact["ref"], err = checkpointText(ref, fmt.Sprintf("artifacts[%d].ref", i), 1, 1024)
			if err != nil {
				return nil, "", err
			}
			portability, exists := artifact["portability"]
			if !exists {
				return nil, "", workInvalid("checkpoint artifacts[%d].portability required", i)
			}
			artifact["portability"], err = checkpointText(portability, fmt.Sprintf("artifacts[%d].portability", i), 1, 32)
			if err != nil {
				return nil, "", err
			}
			portability = artifact["portability"]
			if portability != "portable" && portability != "local_only" {
				return nil, "", workInvalid("checkpoint artifacts[%d].portability must be portable or local_only", i)
			}
			if host, exists := artifact["host"]; exists {
				artifact["host"], err = checkpointText(host, fmt.Sprintf("artifacts[%d].host", i), 1, 256)
				if err != nil {
					return nil, "", err
				}
			}
			if portability == "local_only" {
				if _, exists := artifact["host"]; !exists {
					return nil, "", workInvalid("checkpoint artifacts[%d].host required for local_only artifacts", i)
				}
			}
			items[i] = artifact
		}
	}
	if value, exists := content["execution"]; exists {
		execution, ok := value.(map[string]any)
		if !ok {
			return nil, "", workInvalid("checkpoint execution must be an object")
		}
		if err := checkpointObjectFields(execution, "authority", "run_id"); err != nil {
			return nil, "", err
		}
		for _, key := range []string{"authority", "run_id"} {
			value, exists := execution[key]
			if !exists {
				return nil, "", workInvalid("checkpoint execution.%s required", key)
			}
			execution[key], err = checkpointText(value, "execution."+key, 1, 256)
			if err != nil {
				return nil, "", err
			}
		}
		content["execution"] = execution
	}

	canonical, err := json.Marshal(content)
	if err != nil {
		return nil, "", workInvalid("checkpoint must be JSON encodable")
	}
	return content, string(canonical), nil
}

func storedCheckpoint(meta map[string]any) (map[string]any, int64, error) {
	raw, exists := meta[recoveryCheckpointKey]
	if !exists || raw == nil {
		return nil, 0, nil
	}
	envelope, ok := raw.(map[string]any)
	if !ok {
		return nil, 0, fmt.Errorf("stored recovery checkpoint is not an object")
	}
	version, ok := workInt(envelope["version"])
	if !ok || version < 1 {
		return nil, 0, fmt.Errorf("stored recovery checkpoint has an invalid version")
	}
	if _, ok = envelope["content"].(map[string]any); !ok {
		return nil, 0, fmt.Errorf("stored recovery checkpoint has invalid content")
	}
	return envelope, version, nil
}

func checkpointResult(envelope map[string]any, version int64, unchanged bool) map[string]any {
	return map[string]any{"checkpoint": envelope, "version": version, "unchanged": unchanged}
}

// PutWorkCheckpoint replaces the single attributed recovery checkpoint for a
// card. Its independent CAS version leaves work progress and decision versions
// untouched, and the metadata_json ownership trigger keeps references private
// with the card.
func (s *Store) PutWorkCheckpoint(ctx context.Context, actor, identifier string, version int64, input map[string]any) (result map[string]any, mutationErr error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actor) == "" {
		return nil, workInvalid("actor required")
	}
	if version < 0 {
		return nil, workInvalid("checkpoint version must be nonnegative")
	}
	content, contentJSON, err := normalizeWorkCheckpoint(input)
	if err != nil {
		return nil, err
	}
	digest := sha256Hex([]byte(contentJSON))

	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() {
		if mutationErr != nil {
			mutationErr = mutationOutcomeAfterBegin(mutationErr)
		}
	}()
	defer tx.Rollback()

	resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: identifier})
	if err != nil {
		return nil, err
	}
	card, err := s.loadBoardCardByGlobalID(ctx, tx, resolved.ID, true)
	if err != nil {
		return nil, err
	}
	if err = ensureBoardCardMutable(card); err != nil {
		return nil, err
	}

	var metadataJSON string
	err = tx.QueryRowContext(ctx, `SELECT metadata_json FROM work_metadata WHERE card_id=?`, card.CardID).Scan(&metadataJSON)
	metaExists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	meta := map[string]any{"source": map[string]any{"authority": "nexus"}}
	if metaExists {
		if err = json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
			return nil, err
		}
	}
	current, currentVersion, err := storedCheckpoint(meta)
	if err != nil {
		return nil, err
	}
	if current != nil && workString(current["reporter"]) == actor && workString(current["digest"]) == digest && workJSON(workMap(current["content"])) == contentJSON {
		return checkpointResult(current, currentVersion, true), nil
	}
	if version != currentVersion {
		return nil, ErrConflict
	}
	if currentVersion == int64(^uint64(0)>>1) {
		return nil, workInvalid("checkpoint version exhausted")
	}
	nextVersion := currentVersion + 1
	envelope := map[string]any{
		"version": nextVersion, "reporter": actor,
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
		"digest":     digest, "content": content,
	}
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return nil, workInvalid("checkpoint must be JSON encodable")
	}
	if len(envelopeJSON) > maxWorkCheckpointBytes {
		return nil, workInvalid("checkpoint must be at most %d UTF-8 JSON bytes", maxWorkCheckpointBytes)
	}
	if metaExists {
		res, updateErr := tx.ExecContext(ctx, `UPDATE work_metadata
			SET metadata_json=json_set(metadata_json,'$._recovery_checkpoint',json(?))
			WHERE card_id=? AND COALESCE(CAST(json_extract(metadata_json,'$._recovery_checkpoint.version') AS INTEGER),0)=?`, string(envelopeJSON), card.CardID, currentVersion)
		if updateErr != nil {
			return nil, updateErr
		}
		n, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if n != 1 {
			return nil, ErrConflict
		}
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by)
			VALUES(?,'nexus',json_set('{"source":{"authority":"nexus"}}','$._recovery_checkpoint',json(?)),0,?,?)`, card.CardID, string(envelopeJSON), card.UpdatedAt, card.UpdatedBy)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, &MutationOutcomeUnknown{Cause: err}
	}
	return checkpointResult(envelope, nextVersion, false), nil
}
