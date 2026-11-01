// Package blob stores artifact content outside SQLite through one Backend
// interface with filesystem and S3-compatible object-store implementations,
// plus staged writes, preload checks and usage accounting.
//
// Writers publish content_refs_json atomically with the blob metadata, and
// manifests are scanned for ownership even when bytes are binary (core/AGENTS.md).
package blob
