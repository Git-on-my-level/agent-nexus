// Package primitives is core's canonical domain layer: the Store that owns
// events, topics, cards, boards, documents, artifacts and their evidence-backed
// state transitions, together with the typed errors, limits and access-scope
// policy (WithAccessScope, CanAccessResource) that the rest of core layers on.
//
// Canonical records are append-only with patch-semantics updates, and derived
// views (inboxes, briefs, scoped projections) must stay rebuildable from them.
// All database access goes through internal/resourceaccess scoped handles.
// See core/AGENTS.md ("Resource authorization") and core/docs/anx-core-spec.md.
package primitives
