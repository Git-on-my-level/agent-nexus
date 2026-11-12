package resourceaccess

import "context"

type authAdminKey struct{}

// WithAuthAdmin records the authenticated principal's administrative authority
// for repository reads that are not resource-scoped. The request scope installer
// sets it from the same predicate the HTTP handler uses. Absence is non-admin
// and must fail closed.
func WithAuthAdmin(ctx context.Context, admin bool) context.Context {
	return context.WithValue(ctx, authAdminKey{}, admin)
}

// IsAuthAdmin reports whether the context carries an explicit auth-admin
// principal. A missing marker is not privileged.
func IsAuthAdmin(ctx context.Context) bool {
	admin, _ := ctx.Value(authAdminKey{}).(bool)
	return admin
}
