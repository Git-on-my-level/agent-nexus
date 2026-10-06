package scopes

// ResourceIdentity joins the public opaque identity to a private storage key.
// RID and CanonicalID are internal only; transports must never serialize them.
type ResourceIdentity struct {
	ScopeID          ID
	Kind             string
	ResourceID       string
	RID              int64  `json:"-"`
	CanonicalID      string `json:"-"`
	CanonicalVersion int64
}

// CanonicalMutation is produced by trusted canonical writers, not request DTOs.
// The previous version is captured before mutation; Identity names the new one.
// Audience changes may carry separate old-only and new-only deltas, up to four.
type CanonicalMutation struct {
	Identity        ResourceIdentity
	PreviousVersion int64
	Changes         []Change
}

func (m CanonicalMutation) Validate() error {
	i := m.Identity
	if i.RID < 1 || i.CanonicalID == "" || len(i.CanonicalID) > 512 || m.PreviousVersion < 0 || i.CanonicalVersion <= m.PreviousVersion || i.CanonicalVersion-m.PreviousVersion != 1 || len(m.Changes) < 1 || len(m.Changes) > MaxStreamsPerScope {
		return ErrBudget
	}
	seen := map[Stream]bool{}
	for _, c := range m.Changes {
		if err := c.Validate(); err != nil {
			return err
		}
		if c.ScopeID != i.ScopeID || c.Kind != i.Kind || c.ResourceID != i.ResourceID || c.CanonicalVersion != i.CanonicalVersion {
			return ErrDerivation
		}
		key := Stream{Scope: c.ScopeID, Family: c.Family, Audience: c.Audience}
		if seen[key] {
			return ErrBudget
		}
		seen[key] = true
	}
	return nil
}
