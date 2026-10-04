package primitives

import (
	"agent-nexus-core/internal/plans"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"agent-nexus-visualreport"
)

// HiddenSubjectRefs includes aliases and backing threads so archived subjects
// cannot leak into projections through a secondary event or inbox ref.
func (s *Store) HiddenSubjectRefs(ctx context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	queries := []struct{ kind, query string }{
		{"board", `SELECT id, COALESCE(handle,''), thread_id FROM boards WHERE COALESCE(archived_at,'')<>'' OR COALESCE(trashed_at,'')<>''`},
		{"document", `SELECT id, COALESCE(handle,''), COALESCE(thread_id,'') FROM documents WHERE COALESCE(archived_at,'')<>'' OR COALESCE(trashed_at,'')<>''`},
		{"thread", `SELECT id, '', id FROM threads WHERE COALESCE(archived_at,'')<>'' OR COALESCE(trashed_at,'')<>''`},
		{"topic", `SELECT id, COALESCE(handle,''), COALESCE(thread_id,'') FROM topics WHERE COALESCE(archived_at,'')<>'' OR COALESCE(trashed_at,'')<>''`},
		{"card", `SELECT c.id, COALESCE(c.handle,''), c.thread_id FROM cards c ` + cardVisibilityJoins + ` WHERE NOT ` + cardLifecycleWhere([]string{"active"})},
	}
	for _, q := range queries {
		rows, err := s.db.QueryContext(ctx, q.query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, handle, thread string
			if err = rows.Scan(&id, &handle, &thread); err != nil {
				rows.Close()
				return nil, err
			}
			out[q.kind+":"+id] = true
			if handle != "" {
				out[q.kind+":"+handle] = true
			}
			if thread != "" {
				out["thread:"+thread] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func refsHidden(refs []string, hidden map[string]bool) bool {
	for _, ref := range refs {
		if hidden[ref] {
			return true
		}
	}
	return false
}

func homeImportance(events []map[string]any) int {
	rank := 0
	for _, e := range events {
		kind := anyStringValue(e["type"])
		p, _ := e["payload"].(map[string]any)
		if kind == "human_attention_requested" || kind == "human_attention_responded" {
			return 3
		}
		if kind == "card_resolved" || (kind == "card_moved" && (anyStringValue(p["column_key"]) == "done" || anyStringValue(p["column_key"]) == "blocked")) {
			rank = 2
		}
	}
	return rank
}

// Pin acceptance, fallback selection and CLI publishing use the same full
// validator. Web conformance tests cover the renderer's presentation validator.
func ParseDashboardReport(content any) map[string]any {
	var raw []byte
	if text, ok := content.(string); ok {
		raw = []byte(text)
	} else {
		var err error
		raw, err = json.Marshal(content)
		if err != nil {
			return nil
		}
	}
	validation := visualreport.Validate(raw)
	if !validation.Valid {
		return nil
	}
	report, _ := validation.Report.(map[string]any)
	return report
}

// DashboardReports defers selector-only document reads until requested.
func (s *Store) DashboardReports(ctx context.Context) (map[string]any, error) {
	return s.dashboard(ctx, true)
}

func (s *Store) dashboard(ctx context.Context, all bool) (map[string]any, error) {
	var pin sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT document_id FROM workspace_dashboard WHERE singleton=1`).Scan(&pin)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	result := map[string]any{"status": "ok", "pinned_ref": nil, "reports": []map[string]any{}, "has_more": false}
	if pin.Valid && pin.String != "" {
		result["pinned_ref"] = "document:" + pin.String
	}
	// One metadata read, with the pin first; only read candidate blobs until the
	// selected valid report is found. No per-document GetDocument queries.
	rows, err := s.db.QueryContext(ctx, `SELECT d.id, COALESCE(NULLIF(d.handle,''),d.id), d.title, d.updated_at,
        COALESCE(a.content_hash,''), COALESCE(d.archived_at,''), COALESCE(d.trashed_at,''), d.head_revision_number
        FROM documents d LEFT JOIN document_revisions dr ON dr.revision_id=d.head_revision_id
        LEFT JOIN artifacts a ON a.id=dr.artifact_id
        WHERE (COALESCE(d.archived_at,'')='' AND COALESCE(d.trashed_at,'')='') OR d.id=?
        ORDER BY (d.id=?) DESC, d.updated_at DESC, d.id ASC`, pin.String, pin.String)
	if err != nil {
		return nil, err
	}
	type head struct {
		id, handle, title, updated, hash, archived, trashed string
		revisionNumber                                      int
	}
	heads := []head{}
	for rows.Next() {
		var h head
		if err = rows.Scan(&h.id, &h.handle, &h.title, &h.updated, &h.hash, &h.archived, &h.trashed, &h.revisionNumber); err != nil {
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
	reports := []map[string]any{}
	failures := 0
	for i, h := range heads {
		if h.id == pin.String {
			result["pinned_ref"] = "document:" + h.handle
		}
		if h.archived != "" || h.trashed != "" || strings.HasPrefix(h.id, "agentreg.") {
			continue
		}
		if s.blob == nil {
			failures++
			continue
		}
		raw, err := s.blob.Read(ctx, h.hash)
		if err != nil {
			failures++
			continue
		}
		validation := visualreport.Validate(raw)
		if !validation.Valid {
			continue
		}
		reports = append(reports, map[string]any{"id": h.id, "segment": h.handle, "title": h.title, "updated_at": h.updated, "revision_ref": "document_revision:" + revisionHandle(h.handle, h.revisionNumber), "report": validation.Report, "ref": "document:" + h.handle})
		if !all {
			result["has_more"] = i+1 < len(heads)
			break
		}
	}
	result["reports"] = reports
	if failures > 0 {
		result["warning"] = "Some documents could not be read."
		if len(reports) == 0 {
			result["status"] = "unavailable"
			result["message"] = "Dashboard could not be loaded."
		}
	}
	return result, nil
}

func (s *Store) SetWorkspaceDashboard(ctx context.Context, actor, id string) error {
	if id != "" {
		d, rev, err := s.GetDocument(ctx, id)
		if err != nil {
			return err
		}
		if d["state"] != "active" || ParseDashboardReport(rev["content"]) == nil {
			return fmt.Errorf("%w: dashboard must be an active visual-report document", ErrInvalidWorkRequest)
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO workspace_dashboard(singleton,document_id,updated_at,updated_by) VALUES(1,?,?,?) ON CONFLICT(singleton) DO UPDATE SET document_id=excluded.document_id,updated_at=excluded.updated_at,updated_by=excluded.updated_by`, id, time.Now().UTC().Format(time.RFC3339Nano), actor)
	return err
}

func (s *Store) Overview(ctx context.Context, humanIDs map[string]bool, agentNames map[string]bool) (map[string]any, error) {
	result, err := s.OverviewVisible(ctx, humanIDs, agentNames, nil, time.Now().UTC(), 0)
	if result != nil {
		delete(result, "_visit_work")
	}
	return result, err
}

// overviewWork shares the bounded report projection and its joined privacy context.
func (s *Store) overviewWork(ctx context.Context, visible func(string, string) bool, now time.Time, threshold time.Duration, includeClosed bool) ([]map[string]any, bool, error) {
	page, err := s.ListReportWork(ctx, ReportWorkFilter{Limit: 2000, IncludeClosed: includeClosed})
	if err != nil {
		return nil, false, err
	}
	work := []map[string]any{}
	for _, w := range page.Work {
		b := page.Boards[anyStringValue(w["board_ref"])]
		thread := firstNonEmptyString(anyStringValue(w["thread_id"]), anyStringValue(w["parent_thread_id"]))
		if visible == nil || (visible(b.ThreadID, b.PrivateOwner) && visible(thread, page.PrivateOwners[anyStringValue(w["id"])])) {
			work = append(work, w)
		}
	}
	sort.Slice(work, func(i, j int) bool {
		a, b := anyStringValue(work[i]["updated_at"]), anyStringValue(work[j]["updated_at"])
		if a == b {
			return anyStringValue(work[i]["id"]) > anyStringValue(work[j]["id"])
		}
		return a > b
	})
	if err = s.EnrichCardPlans(ctx, work, visible, now, threshold); err != nil {
		return nil, false, err
	}
	return work, page.Truncated, nil
}

func (s *Store) OverviewVisible(ctx context.Context, humanIDs, agentNames map[string]bool, visible func(string, string) bool, now time.Time, threshold time.Duration) (map[string]any, error) {
	result := map[string]any{"generated_at": now.Format(time.RFC3339Nano)}
	work, truncated, err := s.overviewWork(ctx, visible, now, threshold, true)
	if err != nil {
		return nil, err
	}
	result["_visit_work"] = work
	activeWork := []map[string]any{}
	initiatives := []map[string]any{}
	needs := []map[string]any{}
	humanCount := 0
	for _, w := range work {
		next := strings.TrimSpace(anyStringValue(w["next_actor"]))
		human := humanIDs[strings.TrimPrefix(next, "actor:")] || strings.EqualFold(next, "human") || strings.HasPrefix(strings.ToLower(next), "human:")
		phase := anyStringValue(w["phase"])
		if phase == "done" || phase == "cancelled" {
			continue
		}
		activeWork = append(activeWork, w)
		summary, progress, summaryNeeds := visualreport.Summary(anyStringValue(w["summary"]))
		needsHuman := []string{}
		for _, line := range summaryNeeds {
			name := strings.TrimSpace(strings.SplitN(line[len("Needs"):], ":", 2)[0])
			if !strings.EqualFold(name, "agent") && !agentNames[strings.ToLower(name)] {
				needsHuman = append(needsHuman, line)
			}
		}
		if len(needsHuman) == 0 && human && anyStringValue(w["next_action"]) != "" {
			needsHuman = append(needsHuman, anyStringValue(w["next_action"]))
		}
		ask := strings.Join(needsHuman, " · ")
		ref := anyStringValue(w["ref"])
		href := "/tasks/" + url.PathEscape(strings.TrimPrefix(ref, "card:"))
		initiative := map[string]any{"ref": ref, "title": w["title"], "summary": summary, "progress": progress, "priority": firstNonEmptyString(anyStringValue(w["priority"]), "none"), "needs": needsHuman, "phase": phase, "board_ref": w["board_ref"], "updated_at": w["updated_at"], "plan_state": nil, "geometry": nil, "health": initiativeHealth(w)}
		if state, ok := w["plan_state"].(plans.State); ok {
			initiative["plan_state"] = state
			initiative["progress"] = state.Progress
			initiative["geometry"] = plans.TileGeometry(w["plan"].(plans.Plan), state)
		}
		initiatives = append(initiatives, initiative)
		if human {
			humanCount++
		}
		if human || phase == "blocked" {
			needs = append(needs, map[string]any{"id": "task:" + ref, "title": w["title"], "source": firstNonEmptyString(ask, anyStringValue(w["next_action"]), "Waiting on you"), "href": href, "badge": map[string]any{"label": firstNonEmptyString(ask, "Needs you"), "tone": "warn"}})
		}
	}
	result["work"] = map[string]any{"status": "ok", "total": len(activeWork), "human_count": humanCount, "items": activeWork, "truncated": truncated}
	result["initiatives"] = map[string]any{"status": "ok", "count": len(initiatives), "items": initiatives, "truncated": truncated}
	result["needs_you"] = map[string]any{"status": "ok", "count": len(needs), "rows": needs, "href": "/inbox?mailbox=needs-you"}
	dashboard, err := s.dashboard(ctx, false)
	if err != nil {
		dashboard = map[string]any{"status": "unavailable", "message": "Dashboard could not be loaded."}
	}
	result["dashboard"] = dashboard
	return result, nil
}

func initiativeHealth(w map[string]any) map[string]any {
	status := "on_track"
	if state, ok := w["plan_state"].(plans.State); ok {
		status = state.Health
	} else if w["phase"] == "blocked" {
		status = "blocked"
	}
	reason := "Work is progressing."
	switch status {
	case "blocked":
		reason = "A step on a critical path is blocked."
		if w["plan_state"] == nil {
			reason = "Initiative is blocked."
		}
	case "stalled":
		reason = "No movement within the configured stall threshold."
	}
	return map[string]any{"status": status, "reason": reason}
}
