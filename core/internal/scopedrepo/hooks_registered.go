package scopedrepo

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopes"
)

// ApplyRegisteredCanonicalHooks is the SQL-sealed alternative to the disabled
// generic testing boundary ApplyCanonicalHooks. Unknown hook implementations
// are rejected before any callback; each proxy admits only the exact reviewed
// identity read and six single-row delta statements. An ignored SQL rejection,
// panic or adapter failure rolls back the existing source transaction.
//
// This does NOT establish canonical provenance or computation isolation:
// ReadModelCanonicalHook.Capture is still a trusted adapter callback. Live
// registration remains forbidden until real capture and derivation tests pass.
func ApplyRegisteredCanonicalHooks(ctx context.Context, tx *resourceaccess.Tx, mutation scopes.CanonicalMutation, hooks ...CanonicalHook) error {
	return applyCanonicalHooks(ctx, tx, mutation, true, hooks...)
}
