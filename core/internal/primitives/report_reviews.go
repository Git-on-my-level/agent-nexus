package primitives

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"
)

// AddReportReviewReminder is a bounded check-on-read projection. The singleton
// pin and current revision are checked in the insert, including concurrent edits.
// Inbox identity is durable and independent of topic projection rebuilds.
func (s *Store) AddReportReviewReminder(ctx context.Context, documentID, revisionID, panelID, title, recipient, due string) error {
	id := fmt.Sprintf("report-review:%x", sha256.Sum256([]byte(documentID+"\x00"+revisionID+"\x00"+panelID)))
	data := map[string]any{"kind": "report_review", "response_proposals": []any{}, "subtype": "report_review_due", "title": title, "summary": title, "recipient_actor_id": recipient, "subject_ref": "document:" + documentID, "related_refs": []string{"document:" + documentID}, "panel_id": panelID, "revision_ref": "document_revision:" + revisionID}
	encoded, hash, err := marshalDerivedJSON(data, "")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,due_at,has_due_at,generated_at,data_json,source_hash)
 SELECT ?,d.thread_id,'exception',?,?,1,?,?,? FROM documents d JOIN workspace_dashboard wd ON wd.document_id=d.id AND wd.singleton=1
 WHERE d.id=? AND d.head_revision_id=? AND COALESCE(d.archived_at,'')='' AND COALESCE(d.trashed_at,'')=''
 ON CONFLICT(id) DO NOTHING`, id, due, due, time.Now().UTC().Format(time.RFC3339Nano), encoded, hash, documentID, revisionID)
	return err
}

// Obsolete reminders remain as dedupe receipts but leave every inbox read as
// soon as the report is revised, archived, trashed or unpinned.
const currentReportReviewSQL = `(COALESCE(json_extract(data_json,'$.subtype'),'')<>'report_review_due' OR EXISTS (
 SELECT 1 FROM documents rd JOIN workspace_dashboard wd ON wd.document_id=rd.id AND wd.singleton=1
 WHERE json_extract(data_json,'$.subject_ref')='document:'||rd.id
 AND json_extract(data_json,'$.revision_ref')='document_revision:'||rd.head_revision_id
 AND COALESCE(rd.archived_at,'')='' AND COALESCE(rd.trashed_at,'')=''))`
