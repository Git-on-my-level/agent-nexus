// Package stream registers core's long-lived HTTP endpoints (SSE, chunked
// streams) under a single /stream/ prefix so authentication and the route
// inventory classify them uniformly, and wraps each connection with its own
// authorization scope.
//
// Register every WebSocket, SSE or chunked route through Mount; direct mux
// additions bypass the access-classifier checks. See core/AGENTS.md.
package stream
