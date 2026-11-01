// Package auth owns principal identity and credentials for core: human passkey
// sessions, agent registrations and tokens, workspace grant exchange, auth
// admins, host bridge check-ins, and the audit trail of those decisions.
//
// Durable authentication and grant invariants may read canonical authority
// records but must not return profile or business content (core/AGENTS.md).
package auth
