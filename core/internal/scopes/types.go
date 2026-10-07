// Package scopes defines the bounded, internal contract for the scope migration.
// It does not replace legacy authorization until semantic cutover is approved.
package scopes

const MaxScopes = 64
const MaxStreamsPerScope = 4
const MaxStreams = MaxScopes * MaxStreamsPerScope
const MaxPage = 100
const MaxValueBytes = 64 * 1024
const MaxComputationOps = 256
const MaxDerivedValues = 64

type ID string
type Role string

const (
	Reader Role = "reader"
	Writer Role = "writer"
	Owner  Role = "owner"
	Admin  Role = "admin"
)

func (r Role) CanRead() bool    { return r == Reader || r == Writer || r == Owner || r == Admin }
func (r Role) CanWrite() bool   { return r == Writer || r == Owner || r == Admin }
func (r Role) CanPublish() bool { return r == Owner || r == Admin }

// Immutable sentinels cannot be replaced with an implementation carrying SQL
// or another capability from outside the reviewed scope dependency.
type scopeError string

func (e scopeError) Error() string { return string(e) }

const (
	ErrDenied     scopeError = "scope unavailable" // Missing and unauthorized are indistinguishable.
	ErrUpdating   scopeError = "scope updating"
	ErrBudget     scopeError = "scope request exceeds budget"
	ErrDerivation scopeError = "cross-scope derivation requires publication"
	ErrClosed     scopeError = "scope capability expired"
)

// Binding contains only IDs the caller is authorized to enumerate. A directory
// slot remains present when transitioning; it is not silently filtered out.
type Binding struct {
	ID        ID
	Available bool
}
type DirectoryPage struct {
	Bindings   []Binding
	After      ID
	MoreScopes bool
}
type Stream struct {
	Scope    ID
	Family   string
	Audience string
}

func ValidateStreams(streams []Stream) error {
	if len(streams) > MaxStreams {
		return ErrBudget
	}
	counts := map[ID]int{}
	seen := map[Stream]bool{}
	for _, s := range streams {
		if s.Scope == "" || s.Family == "" || s.Audience == "" || seen[s] {
			return ErrBudget
		}
		seen[s] = true
		counts[s.Scope]++
		if counts[s.Scope] > MaxStreamsPerScope || len(counts) > MaxScopes {
			return ErrBudget
		}
	}
	return nil
}
