package primitives

import "context"

type initiativeAuthorityKey struct{}

// WithInitiativeAuthority records whether this write may make a top-level
// goal by layout (a card created on an initiatives-role board). The HTTP
// boundary withholds it from every agent write: an agent's goal is promoted
// explicitly, with the requesting human named. Internal callers and humans
// carry no marker and keep the authority.
func WithInitiativeAuthority(ctx context.Context, granted bool) context.Context {
	return context.WithValue(ctx, initiativeAuthorityKey{}, granted)
}

// lacksInitiativeAuthority reports that new work must wait in Unsorted even
// where layout (an initiatives-role board) would have made it top level.
func lacksInitiativeAuthority(ctx context.Context) bool {
	granted, marked := ctx.Value(initiativeAuthorityKey{}).(bool)
	return marked && !granted
}
