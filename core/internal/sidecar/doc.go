// Package sidecar hosts privileged in-process services inside anx-core: a
// generic lifecycle shell with explicit dependencies, readiness that fails
// closed for enabled services, and small structured ops-health snapshots.
//
// It is not a home for unrelated background work; see internal/sidecar/AGENTS.md.
package sidecar
