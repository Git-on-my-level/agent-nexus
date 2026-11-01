// Package server exposes core's HTTP API: NewHandler assembles the mux,
// authentication, request limits and the handlers for the canonical and
// derived collections, and the package hosts the in-process maintainers
// (projection rebuild, answer wakes, card leases, PM runtime).
//
// Every route must be classified in testdata/resource_access_routes.json;
// TestResourceAccessRouteInventory fails on new, stale or unclassified entries.
// Long-lived streaming endpoints register through internal/server/stream.Mount.
// The wire contract is contracts/anx-openapi.yaml (described in
// core/docs/http-api.md).
package server
