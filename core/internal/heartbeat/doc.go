// Package heartbeat publishes signed workspace heartbeat snapshots to the
// generic publisher configured through the ANX_HEARTBEAT_* environment
// variables.
//
// The publisher is deployment config only: control-plane-specific imports,
// URLs or type coupling must not enter this package or core (core/AGENTS.md).
package heartbeat
