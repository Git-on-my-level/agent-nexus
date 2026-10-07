package scopestream

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"agent-nexus-core/internal/scopes"
)

const (
	MaxScopes          = scopes.MaxScopes
	MaxStreams         = scopes.MaxStreams
	MaxStreamsPerScope = scopes.MaxStreamsPerScope
	MaxPageSize        = scopes.MaxPage
	MaxPayloadBytes    = 16 * 1024
	MaxEnvelopeBytes   = 8 * 1024
	MaxTokenBytes      = 256 * 1024
	MaxOutputBytes     = 1024 * 1024
)

var (
	ErrBudget      = errors.New("stream request exceeds scope, audience or byte budget")
	ErrCursor      = errors.New("invalid stream continuation")
	ErrRestart     = errors.New("stream authorization changed; reset required")
	ErrResync      = errors.New("stream retention changed; bounded resync required")
	ErrUnavailable = errors.New("scope stream unavailable")
	ErrProjection  = errors.New("invalid scope stream projection")
	ErrDerivation  = errors.New("stream projection must stay in its source scope")
)

type Stream = scopes.Stream

// The repository validates ALL finite scope/family/audience bindings before
// any change range is read. Principal-specific authority generations and
// binding versions exclude unrelated activity and other members' grants.
type Binding struct {
	Principal           string
	AuthorityGeneration int64
	Streams             []StreamGeneration
}
type StreamGeneration struct {
	Stream              Stream
	ScopeGeneration     int64
	BindingGeneration   int64
	AudienceGeneration  int64
	RetentionGeneration int64
	AfterCompacted      int64
}

type Head struct {
	Sequence     int64
	RID          string
	Version      string
	PayloadBytes int
}

type Key struct {
	Stream Stream
	Head   Head
}

type Record struct {
	Key     Key
	Payload json.RawMessage
}

type Snapshot interface {
	Binding() Binding
	// Heads returns bounded metadata only, with an exact prefix and seq seek.
	Heads(context.Context, Stream, int64, int) ([]Head, error)
	// Records hydrates one bounded batch in this authorized snapshot.
	Records(context.Context, []Key) ([]Record, error)
}
type Repository interface {
	ReadStreams(context.Context, string, []Stream, func(Snapshot) error) error
}

type Request struct {
	Principal    string
	Streams      []Stream
	Limit        int
	OutputBytes  int
	Continuation string
}
type Item struct {
	Record Record
	// Each record acknowledges only its emitted prefix. Reusing the final
	// page token on every SSE event would drop data on mid-page disconnect.
	Continuation string
}
type Page struct {
	Items         []Item
	Continuation  string
	ExaminedHeads int
	OutputBytes   int
}
type position struct {
	After   int64 `json:"a"`
	Pending *Head `json:"h,omitempty"`
}
type tickCursor struct {
	Binding   string     `json:"b"`
	Selection string     `json:"s"`
	Retention []int64    `json:"r"`
	Positions []position `json:"p"`
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func streamLess(a, b Stream) bool {
	if a.Scope != b.Scope {
		return a.Scope < b.Scope
	}
	if a.Family != b.Family {
		return a.Family < b.Family
	}
	return a.Audience < b.Audience
}

func validate(req Request) ([]Stream, error) {
	if req.Principal == "" || len(req.Streams) == 0 || len(req.Streams) > MaxStreams || req.Limit < 1 || req.Limit > MaxPageSize || req.OutputBytes < MaxTokenBytes+MaxPayloadBytes+MaxEnvelopeBytes || req.OutputBytes > MaxOutputBytes {
		return nil, ErrBudget
	}
	streams := append([]Stream(nil), req.Streams...)
	sort.Slice(streams, func(i, j int) bool { return streamLess(streams[i], streams[j]) })
	scopes := map[scopes.ID]int{}
	for i, s := range streams {
		if s.Scope == "" || s.Family == "" || s.Audience == "" || len(s.Scope) > 256 || len(s.Family) > 64 || len(s.Audience) > 256 || (i > 0 && streams[i-1] == s) {
			return nil, ErrBudget
		}
		scopes[s.Scope]++
		if scopes[s.Scope] > MaxStreamsPerScope {
			return nil, ErrBudget
		}
	}
	if len(scopes) > MaxScopes {
		return nil, ErrBudget
	}
	return streams, nil
}

// Tick reauthorizes in a fresh repository transaction each call. Unemitted
// heads and emitted stream-local positions are encrypted together. Idle ticks
// reuse the identical token: there is no hidden global sequence to advance.
func Tick(ctx context.Context, repo Repository, tokens *Tokens, req Request) (Page, error) {
	page := Page{Items: []Item{}}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.OutputBytes == 0 {
		req.OutputBytes = MaxOutputBytes
	}
	streams, err := validate(req)
	if err != nil {
		return page, err
	}
	if repo == nil || tokens == nil {
		return page, ErrUnavailable
	}
	selection := fingerprint(struct {
		Principal string
		Streams   []Stream
	}{req.Principal, streams})
	var cursor tickCursor
	if req.Continuation != "" {
		if tokens.open(req.Continuation, &cursor) != nil || cursor.Selection != selection || len(cursor.Positions) != len(streams) || len(cursor.Retention) != len(streams) {
			return page, ErrCursor
		}
	}
	err = repo.ReadStreams(ctx, req.Principal, streams, func(snap Snapshot) error {
		binding := snap.Binding()
		binding.Streams = append([]StreamGeneration(nil), binding.Streams...)
		if binding.Principal != req.Principal || binding.AuthorityGeneration < 1 || len(binding.Streams) != len(streams) {
			return ErrUnavailable
		}
		sort.Slice(binding.Streams, func(i, j int) bool { return streamLess(binding.Streams[i].Stream, binding.Streams[j].Stream) })
		retention := make([]int64, len(streams))
		// Compaction is checked separately from security generations.
		bound := binding
		bound.Streams = append([]StreamGeneration(nil), binding.Streams...)
		for i, g := range binding.Streams {
			if g.Stream != streams[i] || g.ScopeGeneration < 1 || g.BindingGeneration < 1 || g.AudienceGeneration < 1 || g.RetentionGeneration < 1 || g.AfterCompacted < 0 {
				return ErrUnavailable
			}
			retention[i] = g.RetentionGeneration
			bound.Streams[i].RetentionGeneration = 0
			bound.Streams[i].AfterCompacted = 0
		}
		bindID := fingerprint(bound)
		if req.Continuation != "" {
			if cursor.Binding != bindID {
				return ErrRestart
			}
			for i, p := range cursor.Positions {
				if cursor.Retention[i] != retention[i] || p.After < binding.Streams[i].AfterCompacted {
					return ErrResync
				}
				if p.After < 0 || (p.Pending != nil && !validHead(*p.Pending, p.After)) {
					return ErrCursor
				}
			}
		} else {
			cursor = tickCursor{Binding: bindID, Selection: selection, Retention: retention, Positions: make([]position, len(streams))}
			for _, g := range binding.Streams {
				if g.AfterCompacted > 0 {
					return ErrResync
				}
			}
		}
		batches := make([][]Head, len(streams))
		h := &headHeap{}
		for i, s := range streams {
			after := cursor.Positions[i].After
			limit := req.Limit + 1
			if pending := cursor.Positions[i].Pending; pending != nil {
				batches[i] = append(batches[i], *pending)
				after = pending.Sequence
				limit--
			}
			rows, err := snap.Heads(ctx, s, after, limit)
			if err != nil {
				return err
			}
			if len(rows) > limit {
				return ErrProjection
			}
			for _, head := range rows {
				if !validHead(head, after) {
					return ErrProjection
				}
				after = head.Sequence
			}
			page.ExaminedHeads += len(rows)
			batches[i] = append(batches[i], rows...)
			if len(batches[i]) > 0 {
				heap.Push(h, headRef{batches[i][0], i, 0})
			}
		}
		var selected []headRef
		var continuations []string
		// Every prefix token must retain all known unemitted stream heads.
		for i, heads := range batches {
			if len(heads) > 0 {
				head := heads[0]
				cursor.Positions[i].Pending = &head
			}
		}
		bytes := 0
		for h.Len() > 0 && len(selected) < req.Limit {
			head := heap.Pop(h).(headRef)
			pos := &cursor.Positions[head.stream]
			old := *pos
			pos.After = head.head.Sequence
			pos.Pending = nil
			if next := head.index + 1; next < len(batches[head.stream]) {
				pending := batches[head.stream][next]
				pos.Pending = &pending
			}
			token, err := tokens.seal(cursor)
			if err != nil {
				return err
			}
			charge := head.head.PayloadBytes + MaxEnvelopeBytes + len(token)
			if bytes+charge > req.OutputBytes {
				*pos = old
				heap.Push(h, head)
				break
			}
			bytes += charge
			selected = append(selected, head)
			continuations = append(continuations, token)
			if next := head.index + 1; next < len(batches[head.stream]) {
				heap.Push(h, headRef{batches[head.stream][next], head.stream, next})
			}
		}
		keys := make([]Key, len(selected))
		for i, head := range selected {
			keys[i] = Key{streams[head.stream], head.head}
		}
		records, err := snap.Records(ctx, keys)
		if err != nil {
			return err
		}
		if len(records) != len(keys) {
			return ErrProjection
		}
		for i, record := range records {
			if record.Key != keys[i] || len(record.Payload) != record.Key.Head.PayloadBytes || !json.Valid(record.Payload) {
				return ErrProjection
			}
			token := continuations[i]
			page.Items = append(page.Items, Item{record, token})
			page.Continuation = token
		}
		page.OutputBytes = bytes
		if len(records) == 0 {
			page.Continuation = req.Continuation
			if page.Continuation == "" {
				page.Continuation, err = tokens.seal(cursor)
			}
			return err
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return page, nil
}
func validHead(h Head, after int64) bool {
	return h.Sequence > after && h.RID != "" && h.Version != "" && len(h.RID) <= 512 && len(h.Version) <= 256 && h.PayloadBytes >= 1 && h.PayloadBytes <= MaxPayloadBytes
}

type headRef struct {
	head          Head
	stream, index int
}
type headHeap []headRef

func (h headHeap) Len() int { return len(h) }
func (h headHeap) Less(i, j int) bool {
	if h[i].head.Sequence != h[j].head.Sequence {
		return h[i].head.Sequence < h[j].head.Sequence
	}
	return h[i].stream < h[j].stream
}
func (h headHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *headHeap) Push(v any)   { *h = append(*h, v.(headRef)) }
func (h *headHeap) Pop() any     { a := *h; v := a[len(a)-1]; *h = a[:len(a)-1]; return v }
