// Package scopemigrate stages historical metadata. It has no blob dependency
// and cannot enable a reader. Only the integration owner may select a generation
// after the independent parity, privacy and performance gates have passed.
package scopemigrate

import "errors"

const MaxChunk = 64

// Candidate is a scope whose entire audience has been checked against recorded
// legacy authority at the job's source epoch. A matching creator/owner alone is
// not a subset proof (PMs, admins and inherited restrictions also matter).
type Candidate struct {
	ScopeID string
	Subset  bool
}

// Record contains metadata and precomputed authority facts, never historical
// prose or content bytes. Key is the source adapter's stable unique keyset key.
// Unknown ownership/manifest provenance must set Uncertain.
type Record struct {
	Key, Kind, ID                 string
	Version                       int64
	Container, Creator, Admins    Candidate
	RestrictingOwners             []string
	RestrictionOwner              Candidate
	Uncertain, ContentUnavailable bool
}

type Placement struct {
	ScopeID                       string
	Exception, ContentUnavailable bool
}

// Place implements the accepted compatibility decision. SealedScope must have
// zero grants, including implicit grants, and is provisioned by scope authority.
// It is a sink, without a review/recovery queue.
func Place(r Record, sealedScope string) (Placement, error) {
	if sealedScope == "" || r.Key == "" || r.Kind == "" || r.ID == "" || r.Version < 0 {
		return Placement{}, errors.New("invalid migration metadata")
	}
	p := Placement{ContentUnavailable: r.ContentUnavailable}
	candidate := r.Container
	if r.Uncertain || r.ContentUnavailable || candidate.ScopeID == "" {
		candidate = r.Creator
		if candidate.ScopeID == "" {
			candidate = r.Admins
		}
	}
	if candidate.ScopeID != "" && candidate.Subset {
		p.ScopeID = candidate.ScopeID
		return p, nil
	}
	p.Exception = true
	// Deduplicate owners: several references to one owner are one restriction.
	owners := map[string]bool{}
	for _, owner := range r.RestrictingOwners {
		if owner != "" {
			owners[owner] = true
		}
	}
	if len(owners) == 1 && r.RestrictionOwner.ScopeID != "" && r.RestrictionOwner.Subset {
		p.ScopeID = r.RestrictionOwner.ScopeID
	} else {
		p.ScopeID = sealedScope
	}
	return p, nil
}
