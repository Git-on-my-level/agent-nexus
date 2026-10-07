package pm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

func pageParams(r *http.Request) (int, string, error) {
	n := 50
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err = strconv.Atoi(raw)
	}
	if err != nil || n < 1 || n > 200 {
		return 0, "", ErrInvalid
	}
	return n, r.URL.Query().Get("cursor"), nil
}
func recordPage[T any](ctx context.Context, s *Service, p Principal, kind string, limit int, cursor string, workRef func(T) string) (Page[T], error) {
	out := Page[T]{Items: make([]T, 0)}
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return out, err
	}
	if limit < 1 || limit > 200 {
		return out, ErrInvalid
	}
	scope := stableID(kind, p.WorkspaceID, p.ActorID)
	// Cursor values are carried forward, never re-read from a mutable/deleted row.
	var before struct {
		Scope   string `json:"scope"`
		Created string `json:"created"`
		Row     int64  `json:"row"`
	}
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &before) != nil || before.Scope != scope || before.Row < 1 {
			return out, ErrInvalid
		}
	}
	owner := p.ActorID
	if kind == "decision" || kind == "action" || kind == "binding" {
		owner = ""
	}
	lastVisible := before
	for {
		// Go stores UTC RFC3339Nano. Removing Z preserves exact fractional-second
		// ordering (including whole seconds) without SQLite's millisecond rounding.
		// Legacy actions had no creation timestamp; use their decision's timestamp.
		created := `rtrim(COALESCE(json_extract(r.body,'$.created_at'),''),'Z')`
		where := `r.kind=? AND r.workspace_id=?`
		args := []any{kind, p.WorkspaceID}
		if owner != "" {
			where += ` AND r.actor_id=?`
			args = append(args, owner)
		}
		boundary := func(expr string) string {
			if before.Row == 0 {
				return ""
			}
			return ` AND (` + expr + `<? OR (` + expr + `=? AND r.rowid<?))`
		}
		bindBoundary := func(args []any) []any {
			if before.Row > 0 {
				args = append(args, before.Created, before.Created, before.Row)
			}
			return args
		}
		query := `SELECT r.rowid,` + created + ` AS created,r.body FROM pm_records r WHERE ` + where + boundary(created) + ` ORDER BY ` + created + ` DESC,r.rowid DESC LIMIT ?`
		args = bindBoundary(args)
		args = append(args, limit+1)
		if kind == "action" {
			// Modern actions use the indexed timestamp directly. Keep legacy fallback
			// in a separate sparse selector so it cannot force a sort of all actions.
			query = `SELECT * FROM (SELECT r.rowid,` + created + ` AS created,r.body FROM pm_records r WHERE ` + where + ` AND json_extract(r.body,'$.created_at') IS NOT NULL` + boundary(created) + ` ORDER BY ` + created + ` DESC,r.rowid DESC LIMIT ?)`
			legacy := `rtrim(COALESCE((SELECT json_extract(d.body,'$.created_at') FROM pm_records d WHERE d.kind='decision' AND d.id=r.parent_id AND d.workspace_id=r.workspace_id),''),'Z')`
			legacyArgs := []any{kind, p.WorkspaceID}
			if owner != "" {
				legacyArgs = append(legacyArgs, owner)
			}
			query += ` UNION ALL SELECT * FROM (SELECT r.rowid,` + legacy + ` AS created,r.body FROM pm_records r WHERE ` + where + ` AND json_extract(r.body,'$.created_at') IS NULL` + boundary(legacy) + ` ORDER BY created DESC,r.rowid DESC LIMIT ?)`
			legacyArgs = bindBoundary(legacyArgs)
			legacyArgs = append(legacyArgs, limit+1)
			args = append(args, legacyArgs...)
			query = `SELECT * FROM (` + query + `) ORDER BY created DESC,rowid DESC LIMIT ?`
			args = append(args, limit+1)
		}
		rows, err := s.store.database().QueryContext(ctx, query, args...)
		if err != nil {
			return out, err
		}
		// Close SQL rows before permission callbacks, which may query the same DB.
		type record struct {
			row     int64
			created string
			value   T
		}
		records := make([]record, 0, limit+1)
		for rows.Next() {
			var v record
			var b []byte
			if err = rows.Scan(&v.row, &v.created, &b); err != nil {
				rows.Close()
				return out, err
			}
			if err = json.Unmarshal(b, &v.value); err != nil {
				rows.Close()
				return out, err
			}
			records = append(records, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return out, err
		}
		readable := map[string]bool{}
		if workRef != nil {
			refs := make([]string, 0, len(records))
			for _, v := range records {
				refs = append(refs, workRef(v.value))
			}
			readable, err = s.authorizeReadBatch(ctx, p, "pm.read", refs)
			if err != nil {
				return out, err
			}
		}
		for _, v := range records {
			before.Scope, before.Created, before.Row = scope, v.created, v.row
			if workRef != nil && !readable[workRef(v.value)] {
				continue
			}
			if len(out.Items) == limit {
				// Only a further visible record proves there is another page.
				out.HasMore = true
				raw, err := json.Marshal(lastVisible)
				if err != nil {
					return out, err
				}
				out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
				return out, nil
			}
			out.Items = append(out.Items, v.value)
			lastVisible = before
		}
		if len(records) < limit+1 {
			return out, nil
		}
		// Continue in bounded batches past hidden rows. SQL rows are closed
		// before callbacks, including callbacks that read from this same store.
	}
}

func (s *Service) ConversationPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Conversation], error) {
	return recordPage(ctx, s, p, "conversation", limit, cursor, func(c Conversation) string { return c.WorkRef })
}
func (s *Service) DecisionPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Decision], error) {
	page, err := recordPage(ctx, s, p, "decision", limit, cursor, func(d Decision) string { return d.WorkRef })
	if err == nil {
		page.Items, err = s.decisionsForReader(ctx, p, page.Items)
	}
	return page, err
}
func (s *Service) ActionPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Action], error) {
	page, err := recordPage(ctx, s, p, "action", limit, cursor, func(a Action) string { return a.WorkRef })
	for i, a := range page.Items {
		page.Items[i] = s.actionForReader(ctx, a)
	}
	return page, err
}
func (s *Service) BindingPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Binding], error) {
	if err := s.authorize(ctx, p, "pm.bind", ""); err != nil {
		return Page[Binding]{}, err
	}
	return recordPage[Binding](ctx, s, p, "binding", limit, cursor, nil)
}

// ConversationHistory reads newest history by default, in chronological display
// order, with a cursor for older turns. A long-lived chat cannot lose visibility
// of its newest reply when it passes the bounded history window.
func (s *Service) ConversationHistory(ctx context.Context, p Principal, id string, limit int, cursor string) (ConversationDetail, error) {
	c, err := s.conversation(ctx, p, id)
	if err != nil {
		return ConversationDetail{}, err
	}
	if err := s.ExpireTurns(ctx, time.Now().UTC()); err != nil {
		return ConversationDetail{}, err
	}
	out := ConversationDetail{Conversation: c, Turns: make([]Turn, 0)}
	if limit < 1 || limit > 200 {
		return out, ErrInvalid
	}
	scope := stableID("history", p.WorkspaceID, p.ActorID, id)
	before := int64(9223372036854775807)
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return out, ErrInvalid
		}
		parts := strings.Split(string(raw), ":")
		if len(parts) != 2 || parts[0] != scope {
			return out, ErrInvalid
		}
		before, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || before < 1 {
			return out, ErrInvalid
		}
	}
	rows, err := s.store.database().QueryContext(ctx, `SELECT rowid,body FROM pm_records WHERE kind='turn' AND workspace_id=? AND actor_id=? AND parent_id=? AND rowid<? ORDER BY rowid DESC LIMIT ?`, p.WorkspaceID, p.ActorID, id, before, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var row int64
		var b []byte
		if err = rows.Scan(&row, &b); err != nil {
			return out, err
		}
		if len(out.Turns) == limit {
			out.HasMore = true
			break
		}
		var turn Turn
		if err = json.Unmarshal(b, &turn); err != nil {
			return out, err
		}
		out.Turns = append(out.Turns, turn)
		before = row
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if out.HasMore {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + ":" + strconv.FormatInt(before, 10)))
	}
	for left, right := 0, len(out.Turns)-1; left < right; left, right = left+1, right-1 {
		out.Turns[left], out.Turns[right] = out.Turns[right], out.Turns[left]
	}
	return out, nil
}
