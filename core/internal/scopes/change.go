package scopes

// RequestSelection is dispatcher input, never proof of authority. The dispatcher
// takes Principal from authenticated context, not request JSON. Repositories
// authorize every selected scope in their current transaction.
type RequestSelection struct {
	Principal string
	ScopeIDs  []ID
}

// Projection is a bounded canonical mutation delta for feed/search maintenance.
// It is internal data, not an author-ingress DTO or permission to publish.
type Projection struct {
	Title       string
	Text        string
	ContainerID string
	Status      string
	Rank        string
	Timestamp   int64
	Deleted     bool
	// SourceTruncated means Title/Text omit canonical source bytes. Search must
	// retain this bit even if its own normalization fits the index budget.
	SourceTruncated bool
}

// Change is consumed by B/C in the canonical mutation transaction. Integration
// owns dispatch; workers must not persist caller-invented scope/provenance.
type Change struct {
	ScopeID          ID
	Kind             string
	ResourceID       string
	CanonicalVersion int64
	Family           string
	Audience         string
	Before           *Projection
	After            *Projection
}

func (c Change) Validate() error {
	if c.ScopeID == "" || c.Kind == "" || len(c.Kind) > 32 || c.ResourceID == "" || len(c.ResourceID) > 512 || c.CanonicalVersion < 1 || c.Family == "" || len(c.Family) > 64 || c.Audience == "" || len(c.Audience) > 256 {
		return ErrBudget
	}
	if c.Before == nil && c.After == nil {
		return ErrBudget
	}
	for _, p := range []*Projection{c.Before, c.After} {
		if p == nil {
			continue
		}
		if len(p.Title)+len(p.Text)+len(p.ContainerID)+len(p.Status)+len(p.Rank) > MaxValueBytes {
			return ErrBudget
		}
	}
	return nil
}
