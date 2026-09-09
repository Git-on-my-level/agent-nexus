package pm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
func recordPage[T any](ctx context.Context, s *Service, p Principal, kind string, limit int, cursor string, allowed func(T) bool) (Page[T], error) {
	out := Page[T]{Items: make([]T, 0)}
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return out, err
	}
	if limit < 1 || limit > 200 {
		return out, ErrInvalid
	}
	scope := stableID(kind, p.WorkspaceID, p.ActorID)
	var after int64
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return out, ErrInvalid
		}
		parts := strings.Split(string(raw), ":")
		if len(parts) != 2 || parts[0] != scope {
			return out, ErrInvalid
		}
		after, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || after < 0 {
			return out, ErrInvalid
		}
	}
	owner := p.ActorID
	if kind == "decision" || kind == "action" {
		owner = ""
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT rowid,body FROM pm_records WHERE kind=? AND workspace_id=? AND (?='' OR actor_id=?) AND rowid>? ORDER BY rowid LIMIT ?`, kind, p.WorkspaceID, owner, owner, after, limit+1)
	if err != nil {
		return out, err
	}
	// Close SQL rows before permission callbacks, which may query the same DB.
	type record struct {
		row   int64
		value T
	}
	records := make([]record, 0, limit+1)
	for rows.Next() {
		var v record
		var b []byte
		if err = rows.Scan(&v.row, &b); err != nil {
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
	out.HasMore = len(records) > limit
	if out.HasMore {
		records = records[:limit]
	}
	for _, v := range records {
		if allowed(v.value) {
			out.Items = append(out.Items, v.value)
		}
		after = v.row
	}
	if out.HasMore {
		out.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(scope + ":" + strconv.FormatInt(after, 10)))
	}
	return out, nil
}
func (s *Service) ConversationPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Conversation], error) {
	return recordPage(ctx, s, p, "conversation", limit, cursor, func(c Conversation) bool { return s.authorize(ctx, p, "pm.read", c.WorkRef) == nil })
}
func (s *Service) DecisionPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Decision], error) {
	return recordPage(ctx, s, p, "decision", limit, cursor, func(d Decision) bool { return s.authorize(ctx, p, "pm.read", d.WorkRef) == nil })
}
func (s *Service) ActionPage(ctx context.Context, p Principal, limit int, cursor string) (Page[Action], error) {
	return recordPage(ctx, s, p, "action", limit, cursor, func(a Action) bool { return s.authorize(ctx, p, "pm.read", a.WorkRef) == nil })
}
func (s *Service) BindingPage(ctx context.Context, p Principal) (Page[Binding], error) {
	if err := s.authorize(ctx, p, "pm.bind", ""); err != nil {
		return Page[Binding]{}, err
	}
	items, err := listRecords[Binding](ctx, s.store, "binding", p.WorkspaceID, "", "")
	if err != nil {
		return Page[Binding]{}, err
	}
	return Page[Binding]{Items: items}, nil
}

// ConversationHistory reads newest history by default, in chronological display
// order, with a cursor for older turns. A long-lived chat cannot lose visibility
// of its newest reply when it passes the bounded history window.
func (s *Service) ConversationHistory(ctx context.Context, p Principal, id string, limit int, cursor string) (ConversationDetail, error) {
	c, err := s.conversation(ctx, p, id)
	if err != nil {
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
	rows, err := s.store.db.QueryContext(ctx, `SELECT rowid,body FROM pm_records WHERE kind='turn' AND workspace_id=? AND actor_id=? AND parent_id=? AND rowid<? ORDER BY rowid DESC LIMIT ?`, p.WorkspaceID, p.ActorID, id, before, limit+1)
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
