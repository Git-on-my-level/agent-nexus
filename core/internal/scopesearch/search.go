package scopesearch

import (
	"container/heap"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

type ScopeGeneration struct {
	Scope           string
	Generation      int64
	ScopeGeneration int64
}

// Binding is supplied only by the trusted repository, after all requested
// scopes are authorized in its current transaction snapshot.
type Binding struct {
	Principal           string
	AuthorityGeneration int64
	Scopes              []ScopeGeneration
}

type Key struct {
	Recency int64
	RID     string
	Kind    string
	Scope   string
}

type Candidate struct {
	Scope, Version, Parent string
	Key                    Key
	IndexedBytes           int
	Truncated              bool
}

// Snapshot methods are reviewed typed templates, not arbitrary SQL. Candidates
// returns keys only; Texts hydrates one bounded batch in this same snapshot.
type Snapshot interface {
	Binding() Binding
	Candidates(context.Context, string, string, *Key, int) ([]Candidate, error)
	Texts(context.Context, []Candidate) ([]string, error)
}

type Repository interface {
	ReadSearch(context.Context, string, []string, func(Snapshot) error) error
}

type Request struct {
	Principal         string
	Scopes            []string
	Query             string
	Phrase            bool
	Limit             int
	CandidateBudget   int
	VerificationBytes int
	Continuation      string
}

type Hit struct {
	Scope        string `json:"scope_id"`
	Kind         string `json:"kind"`
	RID          string `json:"rid"`
	Version      string `json:"head_version"`
	Parent       string `json:"parent_rid,omitempty"`
	Snippet      string `json:"snippet"`
	IndexedBytes int    `json:"indexed_bytes"`
	Truncated    bool   `json:"truncated"`
}

type Page struct {
	Items             []Hit    `json:"items"`
	Continuation      string   `json:"continuation,omitempty"`
	CoveredScopes     []string `json:"covered_scopes"`
	CoverageBytes     int      `json:"coverage_bytes_per_resource"`
	Candidates        int      `json:"-"`
	VerificationBytes int      `json:"-"`
}

type searchCursor struct {
	Binding string `json:"b"`
	Query   string `json:"q"`
	After   *Key   `json:"a"`
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Search uses the lexicographically first query term as a fixed driver. AND and
// phrase verification are charged for every candidate, including rejections.
// It never scans until the requested number of hits is filled.
func Search(ctx context.Context, repo Repository, tokens *Tokens, req Request) (Page, error) {
	page := Page{Items: []Hit{}, CoverageBytes: MaxTextBytes}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if req.CandidateBudget == 0 {
		req.CandidateBudget = MaxCandidates
	}
	if req.VerificationBytes == 0 {
		req.VerificationBytes = MaxVerificationBytes
	}
	if repo == nil || tokens == nil || req.Principal == "" || len(req.Scopes) == 0 || len(req.Scopes) > MaxScopes || req.Limit < 1 || req.Limit > MaxPageSize || req.CandidateBudget < 1 || req.CandidateBudget > MaxCandidates || req.VerificationBytes < MaxTextBytes || req.VerificationBytes > MaxVerificationBytes || len(req.Query) > 1024 {
		return page, ErrBudget
	}
	scopes := append([]string(nil), req.Scopes...)
	sort.Strings(scopes)
	for i, s := range scopes {
		if s == "" || len(s) > maxScopeIDBytes || (i > 0 && s == scopes[i-1]) {
			return page, ErrBudget
		}
	}
	q := Prepare("", req.Query)
	terms := q.Terms()
	if len(terms) == 0 || len(terms) > MaxQueryTerms {
		return page, ErrBudget
	}
	sort.Strings(terms)
	queryID := fingerprint(struct {
		Query  string
		Phrase bool
	}{q.Normalized(), req.Phrase})
	var cursor searchCursor
	if req.Continuation != "" {
		if err := tokens.open(req.Continuation, &cursor); err != nil || cursor.Query != queryID {
			return page, ErrCursor
		}
	}
	err := repo.ReadSearch(ctx, req.Principal, scopes, func(snap Snapshot) error {
		binding := snap.Binding()
		binding.Scopes = append([]ScopeGeneration(nil), binding.Scopes...)
		if binding.Principal != req.Principal || binding.AuthorityGeneration < 1 || len(binding.Scopes) != len(scopes) {
			return ErrUnavailable
		}
		sort.Slice(binding.Scopes, func(i, j int) bool { return binding.Scopes[i].Scope < binding.Scopes[j].Scope })
		for i, s := range binding.Scopes {
			if s.Scope != scopes[i] || s.Generation < 1 || s.ScopeGeneration < 1 {
				return ErrUnavailable
			}
		}
		bound := fingerprint(binding)
		if req.Continuation != "" && cursor.Binding != bound {
			return ErrRestart
		}
		cursor.Binding, cursor.Query = bound, queryID
		page.CoveredScopes = scopes
		// B <= 4P per scope; the aggregate budget is enforced before body
		// materialization. Saturation conservatively retains a continuation.
		if req.CandidateBudget < len(scopes) {
			return ErrBudget
		}
		fetch := req.CandidateBudget / len(scopes)
		if fetch > 4*req.Limit {
			fetch = 4 * req.Limit
		}
		possibleMore := false
		streams := make([][]Candidate, len(scopes))
		h := &candidateHeap{}
		for i, s := range scopes {
			rows, err := snap.Candidates(ctx, s, terms[0], cursor.After, fetch)
			if err != nil {
				return err
			}
			if len(rows) > fetch {
				return ErrProjection
			}
			possibleMore = possibleMore || len(rows) == fetch
			for j, c := range rows {
				if c.Scope != s || c.Key.Scope != s || c.IndexedBytes < 0 || c.IndexedBytes > MaxTextBytes || c.Key.RID == "" || len(c.Key.RID) > maxResourceIDBytes || c.Version == "" || len(c.Version) > 256 || len(c.Parent) > 512 || c.Key.Recency < 0 || (c.Key.Kind != "document" && c.Key.Kind != "comment") || (cursor.After != nil && !lessKey(*cursor.After, c.Key)) || (j > 0 && !lessKey(rows[j-1].Key, c.Key)) {
					return ErrProjection
				}
			}
			streams[i] = rows
			if len(rows) > 0 {
				heap.Push(h, candidateHead{rows[0], i, 0})
			}
		}
		var batch []Candidate
		bytes := 0
		for h.Len() > 0 && len(batch) < req.CandidateBudget {
			head := heap.Pop(h).(candidateHead)
			if bytes+head.candidate.IndexedBytes > req.VerificationBytes {
				heap.Push(h, head)
				break
			}
			batch = append(batch, head.candidate)
			bytes += head.candidate.IndexedBytes
			// A saturated stream may contain older candidates which were not
			// fetched. The global key must never advance beyond this frontier.
			if head.index+1 == fetch && len(streams[head.stream]) == fetch {
				break
			}
			if next := head.index + 1; next < len(streams[head.stream]) {
				heap.Push(h, candidateHead{streams[head.stream][next], head.stream, next})
			}
		}
		texts, err := snap.Texts(ctx, batch)
		if err != nil {
			return err
		}
		if len(texts) != len(batch) {
			return ErrProjection
		}
		page.VerificationBytes = bytes
		verified := 0
		for i, c := range batch {
			text := texts[i]
			if len(text) != c.IndexedBytes {
				return ErrProjection
			}
			page.Candidates++
			key := c.Key
			cursor.After = &key
			verified++
			if matches(text, q.Normalized(), terms, req.Phrase) {
				page.Items = append(page.Items, Hit{c.Scope, c.Key.Kind, c.Key.RID, c.Version, c.Parent, snippet(text, terms[0]), len(text), c.Truncated})
				if len(page.Items) == req.Limit {
					break
				}
			}
		}
		if h.Len() > 0 || verified < len(batch) || possibleMore {
			var err error
			page.Continuation, err = tokens.seal(cursor)
			return err
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

// Recency descending, stable opaque identity ascending. Kind distinguishes a
// document and comment even if an importer allocated the same external ID.
func lessKey(a, b Key) bool {
	if a.Recency != b.Recency {
		return a.Recency > b.Recency
	}
	if a.RID != b.RID {
		return a.RID < b.RID
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Scope < b.Scope
}

type candidateHead struct {
	candidate     Candidate
	stream, index int
}
type candidateHeap []candidateHead

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return lessKey(h[i].candidate.Key, h[j].candidate.Key) }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(v any)        { *h = append(*h, v.(candidateHead)) }
func (h *candidateHeap) Pop() any          { a := *h; v := a[len(a)-1]; *h = a[:len(a)-1]; return v }
func matches(text, phrase string, terms []string, isPhrase bool) bool {
	if isPhrase {
		return strings.Contains(" "+text+" ", " "+phrase+" ")
	}
	for _, term := range terms {
		if !strings.Contains(" "+text+" ", " "+term+" ") {
			return false
		}
	}
	return true
}
func snippet(text, term string) string {
	start := strings.Index(text, term)
	if start < 0 {
		start = 0
	}
	end := start + MaxSnippetBytes
	if end > len(text) {
		end = len(text)
	}
	for end > start && !utf8.ValidString(text[start:end]) {
		end--
	}
	return text[start:end]
}
