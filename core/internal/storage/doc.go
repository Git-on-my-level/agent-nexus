// Package storage owns core's SQLite persistence: the workspace layout and
// process lease, the migration sequence, and the maintenance programs that
// keep derived tables (scoped access admission, inbox lifecycle, comment and
// document search, delivery queues) in step with canonical records.
//
// Schema changes ship as migrations with rebuild coverage and must preserve
// the canonical-versus-derived boundary. New or changed columns need a privacy
// classification in testdata/resource_access_storage.json, refreshed with
// make access-inventory; see core/AGENTS.md.
package storage
