package scopesearch

import (
	"context"
	"strconv"

	"agent-nexus-core/internal/scopes"
)

// ApplyCanonical consumes the frozen foundation delta within the canonical
// transaction. Only scope-visible heads and comments belong in this index.
// Audience-restricted inbox/profile/event projections never enter it. The hook
// must prepare a bounded prefix before forming Change, and carry whether it
// omitted source text. The separate trusted argument is retained for existing
// callers and conservatively combined with the shared SourceTruncated bit.
func ApplyCanonical(ctx context.Context, w Writer, delta scopes.Change, sourceTruncated bool) error {
	if err := delta.Validate(); err != nil {
		return err
	}
	if delta.Audience != "all" || (delta.Kind != "document" && delta.Kind != "comment") {
		return ErrProjection
	}
	c := Change{Scope: string(delta.ScopeID), Kind: delta.Kind, RID: delta.ResourceID, Version: strconv.FormatInt(delta.CanonicalVersion, 10)}
	if delta.After == nil || delta.After.Deleted {
		c.Delete = true
	} else {
		c.Parent = delta.After.ContainerID
		c.Recency = delta.After.Timestamp
		c.Content = Prepare(c.Scope, delta.After.Title+"\n"+delta.After.Text)
		c.Content.truncated = c.Content.truncated || sourceTruncated || delta.After.SourceTruncated
	}
	return Apply(ctx, w, c)
}
