package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Pins belong to the authenticated actor, including agents. No request can name
// another principal. The relation also enforces this boundary on every read.
func (s *Store) DashboardPins(ctx context.Context) (map[string]any, error) {
	scope, ok := accessScopeFrom(ctx)
	if !ok || scope.ActorID == "" {
		return nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT document_id FROM dashboard_pins WHERE principal_id=? ORDER BY position LIMIT 12`, scope.ActorID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	source := "personal"
	if len(ids) == 0 {
		source = "workspace_default"
		var id sql.NullString
		err = s.db.QueryRowContext(ctx, `SELECT document_id FROM workspace_dashboard WHERE singleton=1`).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if id.Valid && id.String != "" {
			ids = append(ids, id.String)
		}
	}
	refs := []string{}
	if len(ids) > 0 {
		raw, _ := json.Marshal(ids)
		rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(d.handle,''),d.id),COALESCE(a.content_hash,'') FROM json_each(?) j JOIN documents d ON d.id=j.value JOIN document_revisions dr ON dr.revision_id=d.head_revision_id JOIN artifacts a ON a.id=dr.artifact_id WHERE COALESCE(d.archived_at,'')='' AND COALESCE(d.trashed_at,'')='' AND `+backingThreadLifecycleSQL(ctx, `d.thread_id`, false)+` ORDER BY CAST(j.key AS INTEGER)`, string(raw))
		if err != nil {
			return nil, err
		}
		type head struct{ ref, hash string }
		heads := []head{}
		for rows.Next() {
			var h head
			if err = rows.Scan(&h.ref, &h.hash); err != nil {
				rows.Close()
				return nil, err
			}
			heads = append(heads, h)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, h := range heads {
			if s.blob == nil {
				return nil, fmt.Errorf("dashboard content unavailable")
			}
			content, err := s.blob.Read(ctx, h.hash)
			if err != nil {
				return nil, err
			}
			if ParseDashboardReport(string(content)) != nil {
				refs = append(refs, "document:"+h.ref)
			}
		}
	}

	return map[string]any{"document_refs": refs, "source": source}, nil
}

func (s *Store) SetDashboardPins(ctx context.Context, refs []string) error {
	scope, ok := accessScopeFrom(ctx)
	if !ok || scope.ActorID == "" {
		return ErrForbidden
	}
	if refs == nil || len(refs) > 12 {
		return workInvalid("document_refs must be an array of at most 12 document refs")
	}
	return s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		ids := []string{}
		seen := map[string]bool{}
		for _, ref := range refs {
			if !strings.HasPrefix(ref, "document:") || len(ref) > 256 {
				return workInvalid("pins must be document refs")
			}
			resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "document", Ref: ref})
			if err != nil {
				return err
			}
			if seen[resolved.ID] {
				return workInvalid("duplicate dashboard pin")
			}
			seen[resolved.ID] = true
			doc, revision, err := s.GetDocument(ctx, resolved.ID)
			if err != nil {
				return err
			}
			if doc["state"] != "active" || ParseDashboardReport(revision["content"]) == nil {
				return workInvalid("dashboard must be an active visual-report document")
			}
			ids = append(ids, resolved.ID)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM dashboard_pins WHERE principal_id=?`, scope.ActorID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO dashboard_pins(principal_id,position,document_id) VALUES(?,?,?)`, scope.ActorID, i, id); err != nil {
				return err
			}
		}
		return nil
	})
}
