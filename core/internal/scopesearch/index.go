// Package scopesearch implements the bounded search executor and transactional
// posting deltas. It has no database factory: trusted scoped repositories own
// selection, snapshots and the reviewed SQL templates.
package scopesearch

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxTextBytes         = 64 * 1024
	MaxTerms             = 4096
	MaxQueryTerms        = 8
	MaxScopes            = 64
	MaxPageSize          = 100
	MaxCandidates        = 4096
	MaxVerificationBytes = 4 * 1024 * 1024
	MaxSnippetBytes      = 512
)

var (
	ErrBudget      = errors.New("search request exceeds budget")
	ErrCursor      = errors.New("invalid search continuation")
	ErrRestart     = errors.New("search generation changed; restart required")
	ErrDerivation  = errors.New("search projection must stay in its source scope")
	ErrProjection  = errors.New("invalid search projection")
	ErrUnavailable = errors.New("scope search unavailable")
)

// Text is a prepared scope-pinned value. Preparing values is a trusted mutation
// hook operation, never a business computation's way to copy between scopes.
type Text struct {
	scope     string
	text      string
	terms     []string
	truncated bool
}

func (t Text) ScopeID() string    { return t.scope }
func (t Text) Normalized() string { return t.text }
func (t Text) Terms() []string    { return append([]string(nil), t.terms...) }
func (t Text) IndexedBytes() int  { return len(t.text) }
func (t Text) Truncated() bool    { return t.truncated }

// Prepare bounds both byte coverage and distinct postings. It stops at the
// first term which would exceed either cap; the stored text and postings always
// describe the same prefix. Binary resources use empty input at the caller.
func Prepare(scope, input string) Text {
	t := Text{scope: scope}
	seen := make(map[string]bool)
	var out strings.Builder
	var word strings.Builder
	flush := func() bool {
		if word.Len() == 0 {
			return true
		}
		term := word.String()
		n := len(term)
		if out.Len() > 0 {
			n++
		}
		if out.Len()+n > MaxTextBytes || (!seen[term] && len(seen) == MaxTerms) {
			return false
		}
		if out.Len() > 0 {
			out.WriteByte(' ')
		}
		out.WriteString(term)
		if !seen[term] {
			seen[term] = true
			t.terms = append(t.terms, term)
		}
		word.Reset()
		return true
	}
	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			r = unicode.ToLower(r)
			remaining := MaxTextBytes - out.Len()
			if out.Len() > 0 {
				remaining--
			}
			if word.Len()+utf8.RuneLen(r) > remaining {
				// Include the bounded portion of a single oversized term; matches
				// extending past this prefix are explicitly outside coverage.
				flush()
				t.truncated = true
				break
			}
			word.WriteRune(r)
		} else if !flush() {
			t.truncated = true
			break
		}
	}
	if !flush() {
		t.truncated = true
	}
	t.text = out.String()
	return t
}

// Change is the bounded search portion of the transaction-local canonical
// change. A's hook maps the canonical delta to it; no history loader is needed.
type Change struct {
	Scope, Kind, RID, Version, Parent string
	Recency                           int64
	Content                           Text
	Delete                            bool
}

// Writer is pinned to one scope and one canonical write transaction. Replace
// removes at most MaxTerms old postings, writes at most MaxTerms new postings,
// and bumps that scope's search generation before the transaction commits.
type Writer interface {
	ScopeID() string
	ReplaceSearch(context.Context, Change) error
}

func Apply(ctx context.Context, w Writer, c Change) error {
	if w == nil || c.Scope == "" || c.Scope != w.ScopeID() || (!c.Delete && c.Content.scope != c.Scope) {
		return ErrDerivation
	}
	if (c.Kind != "document" && c.Kind != "comment") || c.RID == "" || c.Version == "" || c.Recency < 0 || len(c.RID) > 512 || len(c.Version) > 256 || len(c.Parent) > 512 {
		return ErrProjection
	}
	return w.ReplaceSearch(ctx, c)
}
