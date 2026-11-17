package primitives

import (
	"database/sql"
	"errors"
)

// ScopeInboxInvalidationSchemaProposal is disabled DDL, not a migration. Only
// the trusted schema installer may register it. Directory coverage is a receipt
// from complete canonical enumeration, never a consequence of installation.
const ScopeInboxInvalidationSchemaProposal = `CREATE TABLE IF NOT EXISTS scope_inbox_source_clock (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 source_revision INTEGER NOT NULL CHECK(typeof(source_revision)='integer' AND source_revision>0),
 authority_revision INTEGER NOT NULL CHECK(typeof(authority_revision)='integer' AND authority_revision>0),
 directory_revision INTEGER NOT NULL CHECK(typeof(directory_revision)='integer' AND directory_revision>0),
 registry_hash TEXT NOT NULL CHECK(length(registry_hash)=64),
 installation_complete INTEGER NOT NULL CHECK(installation_complete IN (0,1)),
 installed_schema_version INTEGER NOT NULL,
 directory_coverage_revision INTEGER
);`

var ErrScopeInboxInvalidationIncomplete = errors.New("scope inbox source invalidation incomplete")

type ScopeInboxInvalidationInstallation struct {
	Complete       bool
	MissingSources []string
	RegistryHash   string
}

// ScopeInboxSourceSnapshot comes exclusively from the source transaction. A
// verifier must bind all three revisions and separately prove directory coverage.
type ScopeInboxSourceSnapshot struct {
	SourceRevision, AuthorityRevision, DirectoryRevision int64
	DirectoryCoverageRevision                            sql.NullInt64
	RegistryHash                                         string
}
