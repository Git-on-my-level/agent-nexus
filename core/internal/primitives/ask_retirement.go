package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/workprojection"
	"github.com/google/uuid"
)

const AskRetiredEventType = "human_attention_retired"
const AskReopenedEventType = "human_attention_reopened"
const AskRetirementAuditedEventType = "human_attention_retirement_audited"

// RetireAsk competes with answers for the same unique resolution claim. Undo
// can release only the exact retirement named by its caller, never an answer.
// The immutable request, its options and semantic revisions are never written.
func (s *Store) RetireAsk(ctx context.Context, principal auth.Principal, ref, key, hash, reason string, evidence []string, undo bool, retirement string) (map[string]any, error) {
	if key == "" || len(key) > 128 || strings.TrimSpace(key) != key || hash == "" {
		return nil, workInvalid("request_key is required")
	}
	if undo {
		if retirement == "" || reason != "" || len(evidence) != 0 {
			return nil, workInvalid("undo requires if_retirement and forbids reason and evidence")
		}
	} else {
		if strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 2000 || len(evidence) < 1 || len(evidence) > 32 || retirement != "" {
			return nil, workInvalid("retire requires reason (1 to 2000 characters) and 1 to 32 evidence refs; if_retirement is only for undo")
		}
		for _, e := range evidence {
			if strings.TrimSpace(e) == "" || utf8.RuneCountInString(e) > 2048 {
				return nil, workInvalid("invalid evidence ref")
			}
		}
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
	}
	var result map[string]any
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		// Lock before reads so a concurrent answer/retirement observes this commit.
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope='asks.retire' AND actor_id=? AND request_key=?`, principal.ActorID, key); err != nil {
			return err
		}
		resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "event", Ref: ref})
		if err != nil {
			return err
		}
		var raw, thread, requester string
		err = tx.QueryRowContext(ctx, `SELECT payload_json,COALESCE(thread_id,''),actor_id FROM events WHERE id=? AND type='human_attention_requested' AND COALESCE(trashed_at,'')=''`, resolved.ID).Scan(&raw, &thread, &requester)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var wrapper map[string]any
		if err = json.Unmarshal([]byte(raw), &wrapper); err != nil {
			return err
		}
		payload := asMapValue(wrapper["payload"])
		if id := strings.TrimPrefix(anyStringValue(payload["requester_actor_id"]), "actor:"); id != "" {
			requester = id
		}
		if err = auth.RequireAskCuratorTx(ctx, tx, principal, requester); err != nil {
			return err
		}
		subject := anyStringValue(payload["subject_ref"])
		var cardID string
		if subject != "" {
			card, e := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: subject})
			if e != nil {
				return e
			}
			cardID = card.ID
		}
		var savedHash, saved string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_replays WHERE scope='asks.retire' AND actor_id=? AND request_key=?`, principal.ActorID, key).Scan(&savedHash, &saved)
		if err == nil {
			if savedHash != hash {
				return ErrConflict
			}
			return json.Unmarshal([]byte(saved), &result)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Access-request lifecycle has its own security workflow; curation cannot
		// reopen an enrollment/grant or disable the human-only decision boundary.
		var access int
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_requests WHERE request_event_id=?)`, resolved.ID).Scan(&access); err != nil {
			return err
		}
		if access != 0 {
			return ErrForbidden
		}
		var current, typeName string
		err = tx.QueryRowContext(ctx, `SELECT r.resolution_event_id,e.type FROM human_attention_request_resolutions r JOIN events e ON e.id=r.resolution_event_id WHERE r.request_event_id=?`, resolved.ID).Scan(&current, &typeName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		retirementID := current
		if undo {
			target, e := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "event", Ref: retirement})
			if e != nil {
				return e
			}
			if typeName != AskRetiredEventType || current != target.ID {
				return ErrConflict
			}
			var closed bool
			if cardID != "" {
				if err = tx.QueryRowContext(ctx, `SELECT `+workprojection.ClosedSQL()+` FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=?`, cardID).Scan(&closed); err != nil {
					return err
				}
			}
			var expired int
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(julianday(?)<=julianday('now'),0)`, anyStringValue(payload["expires_at"])).Scan(&expired); err != nil {
				return err
			}
			if closed || expired != 0 {
				return ErrHumanAttentionAlreadyResponded
			}
		} else {
			if current != "" {
				return ErrHumanAttentionAlreadyResponded
			}
			var closed int
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ask_subjects WHERE ask_id=? AND (open=0 OR due_at<=julianday('now')))`, resolved.ID).Scan(&closed); err != nil {
				return err
			}
			if closed != 0 {
				return ErrHumanAttentionAlreadyResponded
			}
			for _, ref := range evidence {
				if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
					u, e := url.Parse(ref)
					if e != nil || u.Hostname() == "" || u.User != nil {
						return workInvalid("evidence must be a resource ref or absolute HTTP(S) URL without credentials")
					}
					continue
				}
				if err = validateResolutionRefs(ctx, tx, []string{ref}); err != nil {
					return err
				}
			}
		}
		// The canonical lifecycle record is deliberately independent of the
		// curator's evidence and run. Status/await never need the private audit.
		recipientCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: requester})
		publicReason := reason
		readable, err := tx.ReadableValues(recipientCtx, reason)
		if err != nil {
			return err
		}
		if !readable {
			publicReason = "reason not visible to you"
		}
		evidenceNote := "Evidence recorded in curator audit."
		for _, ref := range evidence {
			readable, err := tx.ReadableValues(recipientCtx, ref)
			if err != nil {
				return err
			}
			if !readable {
				evidenceNote = "Evidence not visible to you."
				break
			}
		}
		eventType, summary, status := AskRetiredEventType, "Ask retired as stale: "+publicReason, "withdrawn_as_stale"
		eventPayload := map[string]any{"request_event_id": resolved.ID, "request_event_ref": "event:" + resolved.ID, "requester_actor_id": requester, "retiring_actor_id": principal.ActorID, "reason": publicReason, "status": status, "evidence_note": evidenceNote}
		refs := []string{"event:" + resolved.ID, subject}
		if thread != "" {
			refs = append(refs, "thread:"+thread)
		}
		if undo {
			eventType, summary, status = AskReopenedEventType, "Ask reopened after retirement", "open"
			eventPayload = map[string]any{"request_event_id": resolved.ID, "requester_actor_id": requester, "retirement_event_ref": "event:" + current, "status": status}
			refs = append(refs, "event:"+current)
		}
		event, err := prepareEventForInsert(principal.ActorID, map[string]any{"type": eventType, "thread_id": thread, "summary": summary, "refs": refs, "payload": eventPayload})
		if err != nil {
			return err
		}
		if !undo {
			retirementID = anyStringValue(event.Body["id"])
		}
		// Both members of the original pair are identified by this public key.
		// Undo must not copy a hidden audit ref/evidence into public history or
		// require the requester to read that audit in order to reopen their ask.
		auditPayload := map[string]any{"request_event_id": resolved.ID, "retirement_event_ref": "event:" + retirementID, "audit_key": retirementID, "action": "retire", "reason": reason, "evidence": evidence}
		auditID := askRetirementAuditID(retirementID)
		auditRefs := append(append([]string{}, refs...), evidence...)
		if undo {
			auditID = uuid.NewString()
			auditPayload = map[string]any{"request_event_id": resolved.ID, "retirement_event_ref": "event:" + retirementID, "audit_key": retirementID, "action": "undo", "reopened_event_ref": "event:" + anyStringValue(event.Body["id"])}
		}
		auditRefs = append(auditRefs, "event:"+anyStringValue(event.Body["id"]))
		audit, err := prepareEventForInsert(principal.ActorID, map[string]any{"id": auditID, "type": AskRetirementAuditedEventType, "thread_id": thread, "summary": "Ask retirement curator audit", "refs": auditRefs, "payload": auditPayload})
		if err != nil {
			return err
		}
		// The notice is a projection of the public record alone, not the audit.
		text := "For " + requester + ": " + summary
		if !undo {
			text += "\n" + anyStringValue(eventPayload["evidence_note"])
		}
		notification, err := prepareEventForInsert(principal.ActorID, map[string]any{"type": "message_posted", "thread_id": thread, "summary": summary, "refs": append(append([]string{}, refs...), "event:"+anyStringValue(event.Body["id"])), "payload": map[string]any{"text": text, "recipient_actor_id": requester, "request_event_ref": "event:" + resolved.ID, "retirement_event_ref": "event:" + retirementID, "status": status}})
		if err != nil {
			return err
		}
		result = map[string]any{"ask_id": "event:" + resolved.ID, "status": status, "event": event.Body, "retirement_event_ref": "event:" + retirementID, "audit_event_ref": "event:" + auditID, "notified_actor_id": requester, "notification_event_ref": "event:" + anyStringValue(notification.Body["id"])}
		encoded, _ := json.Marshal(result)
		if err = s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(event.PayloadJSON) + len(event.RefsJSON) + len(audit.PayloadJSON) + len(audit.RefsJSON) + len(notification.PayloadJSON) + len(notification.RefsJSON) + len(encoded) + 12288)}, blobLedgerWritePlan{}); err != nil {
			return err
		}
		if err = insertAskVisibleEvent(ctx, tx, requester, event); err != nil {
			return err
		}
		if err = insertPreparedEvent(ctx, tx, audit); err != nil {
			return err
		}
		if err = insertAskVisibleEvent(ctx, tx, requester, notification); err != nil {
			return err
		}
		// Event insertion assigns its public handle; persist exactly what is returned.
		encoded, _ = json.Marshal(result)
		if undo {
			changed, e := tx.ExecContext(ctx, `DELETE FROM human_attention_request_resolutions WHERE request_event_id=? AND resolution_event_id=? AND resolution_type='withdrawn'`, resolved.ID, current)
			if e != nil {
				return e
			}
			n, e := changed.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `UPDATE ask_subjects SET open=1 WHERE ask_id=?`, resolved.ID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE brief_asks SET state='open',answered_at=NULL,responder='',response_event_id='' WHERE ask_id=?`, resolved.ID); err != nil {
				return err
			}
			// Restore the original deadline to the pending-normalization ledger. Never
			// extend the deadline or reset any original option/revision metadata.
			if _, err = tx.ExecContext(ctx, `INSERT INTO ask_deadlines(request_event_id,requester_actor_id,ask_ts,by_raw,blocks_count) SELECT id,?,ts,trim(COALESCE(json_extract(payload_json,'$.payload.decide_by'),json_extract(payload_json,'$.payload.by'),'')),CASE WHEN json_type(payload_json,'$.payload.blocks')='array' THEN json_array_length(payload_json,'$.payload.blocks') ELSE 0 END FROM events WHERE id=? ON CONFLICT DO NOTHING`, requester, resolved.ID); err != nil {
				return err
			}
		} else {
			if _, err = tx.ExecContext(ctx, `INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'withdrawn',?,?)`, resolved.ID, event.Body["id"], principal.ActorID, event.Body["ts"]); err != nil {
				return askRevisionConflict(err)
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('asks.retire',?,?,?,200,?)`, principal.ActorID, key, hash, string(encoded))
		return askRevisionConflict(err)
	})
	return result, err
}

// The public pair key identifies the curator audit without exposing its private
// event ref or requiring an authorization bypass to undo another curator's ask.
func askRetirementAuditID(key string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("ask-retirement-audit:"+key)).String()
}

// insertAskVisibleEvent enforces the visibility boundary on the final stored
// event, after all insertion-time decoration and ownership triggers. It keeps
// the writer's authorization and strips attribution explicitly, rather than
// trusting a visibility check of an incomplete prepared event.
func insertAskVisibleEvent(ctx context.Context, tx *accessTx, requester string, event preparedEvent) error {
	ctx = commandcenter.WithoutAttribution(ctx)
	delete(event.Body, "run_attribution")
	raw, err := json.Marshal(EventPayloadWrapperFromBodyMap(event.Body))
	if err != nil {
		return err
	}
	event.PayloadJSON = string(raw)
	if err = insertPreparedEvent(ctx, tx, event); err != nil {
		return err
	}
	return requireAskVisibleEvent(ctx, tx, requester, anyStringValue(event.Body["id"]))
}

func requireAskVisibleEvent(ctx context.Context, tx *accessTx, requester, eventID string) error {
	// Follow the persisted event's complete ancestry through the exact/prose
	// admission indexes, including any attribution added during insertion.
	readable, err := tx.ReadableValues(WithRequestAccessScope(ctx, AccessScope{ActorID: requester}), "event:"+eventID)
	if err != nil {
		return err
	}
	if !readable {
		return ErrNotFound
	}
	return nil
}
