// anx-core runs the Agent Nexus core service: it opens or initializes the
// SQLite workspace, wires blob storage, auth, primitives and the sidecar
// services, and serves the HTTP API.
//
// Runtime and deployment guidance lives in core/docs/runbook.md.
package main
