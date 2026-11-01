// Event append and fetch: preparing an event for insert, writing it with its
// provenance, and decoding stored rows back into API event maps.

package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/handles"
)

type eventExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type preparedEvent struct {
	Body        map[string]any
	Type        string
	ThreadID    string
	RefsJSON    string
	RefTargets  []refEdgeTarget
	PayloadJSON string
}

func (s *Store) AppendEvent(ctx context.Context, actorID string, event map[string]any) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}

	prepared, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, err
	}
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 512)}, blobLedgerWritePlan{}); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if prepared.Type == "run_recorded" {
		if err := ValidateRunRecorded(event); err != nil {
			return nil, err
		}
		refs, _ := normalizeStringSlice(event["refs"])
		subject := ""
		for i, ref := range refs {
			if strings.HasPrefix(ref, "card:") {
				resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: ref})
				if err != nil {
					return nil, err
				}
				subject = "card:" + resolved.ID
				refs[i] = subject
				var thread string
				if err := tx.QueryRowContext(ctx, `SELECT COALESCE(thread_id,'') FROM cards WHERE id=?`, resolved.ID).Scan(&thread); err != nil {
					return nil, err
				}
				prepared.Body["thread_id"] = thread
			}
		}
		var original string
		err := tx.QueryRowContext(ctx, `SELECT id FROM events WHERE type='run_recorded' AND json_extract(payload_json,'$.subject_ref')=? AND json_extract(payload_json,'$.payload.run_id')=?`, subject, workMap(event["payload"])["run_id"]).Scan(&original)
		if err == nil {
			tx.Rollback()
			return s.GetEvent(ctx, original)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		prepared.Body["refs"] = refs
		prepared.Body["subject_ref"] = subject
		p := workMap(prepared.Body["payload"])
		if _, ok := p["finished_at"]; !ok {
			p["finished_at"] = prepared.Body["ts"]
		}
		prepared.Body["payload"] = p
		at := prepared.Body["ts"]
		prepared, err = prepareEventForInsert(actorID, prepared.Body)
		prepared.Body["ts"] = at
		if err != nil {
			return nil, err
		}
	}
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return prepared.Body, nil
}

func overlayEventLifecycleFromSQLColumns(body map[string]any, archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString) {
	lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, trashedAt, trashedBy, trashReason).apply(body)
}

func decodeEventBodyFromRow(
	ctx context.Context, q queryRower,
	eventID, eventHandle, typeValue, ts, actorID string,
	threadID sql.NullString,
	refsJSON, payloadJSON string,
) (map[string]any, error) {
	var refsList []string
	if err := json.Unmarshal([]byte(refsJSON), &refsList); err != nil {
		return nil, fmt.Errorf("decode event refs: %w", err)
	}

	var wrapper map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &wrapper); err != nil {
		return nil, fmt.Errorf("decode event payload: %w", err)
	}
	if wrapper == nil {
		wrapper = map[string]any{}
	}

	ctx = batchPublicRefValue(ctx, q, map[string]any{"refs": refsList, "thread_ref": "thread:" + threadID.String, "payload": wrapper})
	refsList = publicTypedRefs(ctx, q, refsList)
	// Use []any for refs so API consumers that walk raw maps (e.g. thread ref discovery) match
	// json.Unmarshal into map[string]any, which decodes JSON arrays as []any.
	refs := make([]any, len(refsList))
	for i, r := range refsList {
		refs[i] = r
	}

	handle := strings.TrimSpace(eventHandle)
	if handle == "" {
		handle = eventID
	}
	out := map[string]any{
		"id":       eventID,
		"ref":      "event:" + handle,
		"handle":   handle,
		"type":     typeValue,
		"ts":       ts,
		"actor_id": actorID,
		"refs":     refs,
	}
	if threadID.Valid {
		out["thread_id"] = threadID.String
		out["thread_ref"] = makePublicTypedRef(ctx, q, "thread", threadID.String)
	}

	if inner, ok := wrapper["payload"].(map[string]any); ok {
		out["payload"] = inner
		out["payload"] = publicRefsInValue(ctx, q, out["payload"])
		for k, v := range wrapper {
			if k == "payload" {
				continue
			}
			out[k] = publicRefsInField(ctx, q, k, v)
		}
		return out, nil
	}

	// Legacy: payload_json was only the inner object (per-type fields), or a flat mix; strip known
	// top-level fields into the map root and keep the rest as payload.
	flat := make(map[string]any, len(wrapper))
	for k, v := range wrapper {
		flat[k] = v
	}
	for _, k := range []string{"summary", "provenance"} {
		if v, ok := flat[k]; ok {
			out[k] = v
			delete(flat, k)
		}
	}
	if len(flat) == 0 {
		out["payload"] = map[string]any{}
	} else {
		out["payload"] = publicRefsInValue(ctx, q, flat)
	}
	return out, nil
}

func (s *Store) GetEvent(ctx context.Context, id string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	var (
		eventID     string
		eventHandle string
		typeValue   string
		ts          string
		actorID     string
		threadID    sql.NullString
		refsJSON    string
		payloadJSON string
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM events WHERE id = ?`,
		id,
	).Scan(&eventID, &eventHandle, &typeValue, &ts, &actorID, &threadID, &refsJSON, &payloadJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query event: %w", err)
	}

	body, err := decodeEventBodyFromRow(ctx, s.db, eventID, eventHandle, typeValue, ts, actorID, threadID, refsJSON, payloadJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	return body, nil
}

func prepareEventForInsert(actorID string, event map[string]any) (preparedEvent, error) {
	if anyStringValue(event["type"]) == "human_attention_requested" && asMapValue(event["payload"])["curation"] != nil {
		return preparedEvent{}, workInvalid("curation suggestions are agent work; use /curation/proposals")
	}
	body := cloneMap(event)
	eventID, _ := body["id"].(string)
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		eventID = uuid.NewString()
	}
	body["id"] = eventID
	body["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	body["actor_id"] = actorID
	replaceEventProvenancePlaceholder(body, eventID)

	typeValue, _ := body["type"].(string)
	threadID, _ := body["thread_id"].(string)
	threadID = strings.TrimSpace(threadID)
	body["thread_id"] = threadID
	refs, err := normalizeStringSlice(body["refs"])
	if err != nil {
		return preparedEvent{}, fmt.Errorf("event.refs: %w", err)
	}

	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event refs: %w", err)
	}

	if rawPayload, ok := body["payload"]; ok && rawPayload != nil {
		if _, ok := rawPayload.(map[string]any); !ok {
			return preparedEvent{}, fmt.Errorf("event.payload must be an object when provided")
		}
	}

	wrapper := EventPayloadWrapperFromBodyMap(body)
	payloadJSON, err := json.Marshal(wrapper)
	if err != nil {
		return preparedEvent{}, fmt.Errorf("marshal event payload: %w", err)
	}

	return preparedEvent{
		Body:        body,
		Type:        typeValue,
		ThreadID:    threadID,
		RefsJSON:    string(refsJSON),
		RefTargets:  typedRefEdgeTargets(refEdgeTypeRef, refs),
		PayloadJSON: string(payloadJSON),
	}, nil
}

func insertPreparedEvent(ctx context.Context, exec *accessTx, prepared preparedEvent) error {
	// Capture canonical references before response fields are converted to handles.
	threadIDs := []string{prepared.ThreadID}
	if payload, ok := prepared.Body["payload"].(map[string]any); ok {
		for _, field := range []string{"related_refs", "previous_related_refs"} {
			for _, ref := range anyStringSlice(payload[field]) {
				if kind, _, ok := splitTypedRef(ref); ok && kind == "thread" {
					resolved, err := resolveResourceRef(ctx, exec, ResourceRefInput{Type: kind, Ref: ref})
					if err != nil {
						if errors.Is(err, ErrNotFound) {
							continue
						}
						return err
					}
					threadIDs = append(threadIDs, resolved.ID)
				}
			}
		}
	}
	if attribution, ok := commandcenter.AttributionFrom(ctx); ok {
		prepared.Body["run_attribution"] = attribution
		var wrapper map[string]any
		if err := json.Unmarshal([]byte(prepared.PayloadJSON), &wrapper); err != nil {
			return fmt.Errorf("decode event payload for attribution: %w", err)
		}
		wrapper["run_attribution"] = attribution
		encoded, err := json.Marshal(wrapper)
		if err != nil {
			return fmt.Errorf("encode event attribution: %w", err)
		}
		prepared.PayloadJSON = string(encoded)
	}
	eventID := anyStringValue(prepared.Body["id"])
	// Events share a type across an unbounded history. Using only that type
	// exhausted the allocator after 1,000 events and rolled back valid writes.
	// Keep a readable prefix while deriving uniqueness from the event identity.
	prefix := handles.Candidate(prepared.Type, "event-"+eventID)
	if len(prefix) > 31 {
		prefix = strings.TrimRight(prefix[:31], "-")
	}
	digest := sha256.Sum256([]byte(eventID))
	candidate := prefix + "-" + hex.EncodeToString(digest[:16])
	handle, err := uniqueHandleTx(ctx, exec, "event", candidate, "event-"+eventID)
	if err != nil {
		return fmt.Errorf("allocate event handle: %w", err)
	}
	prepared.Body["handle"] = handle
	prepared.Body["ref"] = "event:" + handle
	ctx = batchPublicRefValue(ctx, exec, map[string]any{"payload": prepared.Body, "thread_ref": "thread:" + prepared.ThreadID})
	if prepared.ThreadID != "" {
		prepared.Body["thread_ref"] = makePublicTypedRef(ctx, exec, "thread", prepared.ThreadID)
	}
	if refs, err := normalizeStringSlice(prepared.Body["refs"]); err == nil {
		prepared.Body["refs"] = publicTypedRefs(ctx, exec, refs)
	}
	if payload, ok := prepared.Body["payload"]; ok {
		prepared.Body["payload"] = publicRefsInValue(ctx, exec, payload)
	}
	for k, v := range prepared.Body {
		switch k {
		case "id", "type", "ts", "actor_id", "thread_id", "thread_ref", "refs", "payload":
			continue
		default:
			prepared.Body[k] = publicRefsInField(ctx, exec, k, v)
		}
	}
	if _, err := exec.ExecContext(
		ctx,
		`INSERT INTO events(id, handle, type, ts, actor_id, thread_id, refs_json, payload_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		eventID,
		handle,
		prepared.Type,
		prepared.Body["ts"],
		prepared.Body["actor_id"],
		prepared.ThreadID,
		prepared.RefsJSON,
		prepared.PayloadJSON,
	); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("insert event: %w", err)
	}
	if err := replaceRefEdges(ctx, exec, "event", eventID, prepared.RefTargets); err != nil {
		return err
	}

	if err := refreshTrackingAfterEventTx(ctx, exec, prepared); err != nil {
		return err
	}
	return markTopicProjectionsDirtyTx(ctx, exec, threadIDs, time.Now().UTC())
}

func eventProvenance() map[string]any {
	return map[string]any{
		"sources": []string{"event:" + provenanceEventIDPlaceholder},
	}
}

func replaceEventProvenancePlaceholder(body map[string]any, eventID string) {
	rawProvenance, ok := body["provenance"].(map[string]any)
	if !ok {
		return
	}

	rawSources, hasSources := rawProvenance["sources"]
	if !hasSources {
		return
	}

	sources, err := normalizeStringSlice(rawSources)
	if err != nil {
		return
	}

	changed := false
	placeholder := "event:" + provenanceEventIDPlaceholder
	for idx, source := range sources {
		if source == placeholder {
			sources[idx] = "event:" + eventID
			changed = true
		}
	}
	if !changed {
		return
	}

	provenance := cloneMap(rawProvenance)
	provenance["sources"] = sources
	body["provenance"] = provenance
}

const provenanceEventIDPlaceholder = "<event_id>"
