package primitives

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// Access-equivalence dumps are a deterministic, versioned JSONL stream. The
// harness deliberately reuses the production policy builders below instead of
// copying their SQL: candidateVisibleSQL, admissionLedgerCTE, deniedRootSQL /
// accessCTEs, ownershipClosure and candidateValuesDenied.
//
// Sections:
//
//	p  policy/capability state (ledger selection is comparable; expected
//	   migration/fence/epoch/counter rows are marked ignored for compare)
//	i  persisted decision inputs (owners, spellings, lifecycle positions);
//	   a section above the row limit is emitted as a sorted digest + count
//	sc scope table (numeric ids keep per-decision rows small)
//	d  per-scope resource decisions: a summary (candidate count + SHA-256 over
//	   the full sorted (kind,id,candidate,canonical) stream) followed only by
//	   the exceptional denied/hidden (kind,id) rows
//	t  deterministic text-corpus decisions: a summary digest followed only by
//	   the denied probes
//	a  admin/non-admin audit-repository decisions
//	g  storage diagnostics (hashes; never permission identity)
const accessEquivalenceFormat = 2

// accessEquivalenceInputRowLimit keeps persisted-input sections compact. A
// section above the limit is represented by a sorted digest and row count; a
// digest mismatch is always a compare failure, never a silent pass.
var accessEquivalenceInputRowLimit int64 = 100000

type AccessEquivalenceDumpStats struct {
	Format    int           `json:"format"`
	Records   int64         `json:"records"`
	Decisions int64         `json:"decisions"`
	Denied    int64         `json:"denied"`
	Bytes     int64         `json:"bytes"`
	Duration  time.Duration `json:"duration_ns"`
	Scopes    int           `json:"scopes"`
	Resources int           `json:"resources"`
}

type AccessEquivalenceCompareResult struct {
	Compared   int64 `json:"compared"`
	Allowed    int64 `json:"allowed_card_history"`
	Mismatches int64 `json:"mismatches"`
}

type accessEquivalenceWriter struct {
	buf     *bufio.Writer
	records int64
	bytes   int64
}

func (w *accessEquivalenceWriter) write(value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err = w.buf.Write(line); err != nil {
		return err
	}
	if err = w.buf.WriteByte('\n'); err != nil {
		return err
	}
	w.records++
	w.bytes += int64(len(line) + 1)
	return nil
}

func (w *accessEquivalenceWriter) flush() error { return w.buf.Flush() }

type accessPolicyRecord struct {
	Section string `json:"s"`
	Key     string `json:"key"`
	Name    string `json:"n"`
	Value   any    `json:"v"`
	Compare string `json:"cmp,omitempty"`
}

type accessInputRecord struct {
	Section string `json:"s"`
	Key     string `json:"key"`
	Name    string `json:"n"`
	Value   any    `json:"v"`
}

type accessScopeRecord struct {
	Section string `json:"s"`
	ID      int    `json:"z"`
	Actor   string `json:"a"`
	PM      string `json:"p"`
	Admin   bool   `json:"admin"`
	Class   string `json:"class"`
}

type accessDecisionSummary struct {
	Section string `json:"s"`
	Group   int    `json:"g"`
	Scope   int    `json:"z"`
	Count   int64  `json:"cnt"`
	Digest  string `json:"h"`
}

type accessDecisionRecord struct {
	Section string `json:"s"`
	Group   int    `json:"g"`
	Scope   int    `json:"z"`
	Kind    string `json:"k"`
	ID      string `json:"i"`
	Cand    int    `json:"c"`
	Canon   int    `json:"w"`
}

type accessTextSummary struct {
	Section string `json:"s"`
	Group   int    `json:"g"`
	Scope   int    `json:"z"`
	Count   int64  `json:"cnt"`
	Digest  string `json:"h"`
}

type accessTextRecord struct {
	Section string `json:"s"`
	Group   int    `json:"g"`
	Scope   int    `json:"z"`
	Probe   string `json:"q"`
	Cand    int    `json:"c"`
	Canon   int    `json:"w"`
}

type accessAuditRecord struct {
	Section string `json:"s"`
	Scope   int    `json:"z"`
	Admin   bool   `json:"admin"`
	Visible int    `json:"vis"`
}

type accessDiagnosticRecord struct {
	Section string `json:"s"`
	Name    string `json:"n"`
	Value   any    `json:"v"`
}

// DumpAccessEquivalence writes a read-only snapshot dump. It opens the database
// with mode=ro and query_only and never migrates it or touches the serving
// workspace.
func DumpAccessEquivalence(ctx context.Context, databasePath, outputPath string) (stats AccessEquivalenceDumpStats, err error) {
	started := time.Now()
	stats.Format = accessEquivalenceFormat
	db, err := openAccessEquivalenceReadOnly(ctx, databasePath)
	if err != nil {
		return stats, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return stats, fmt.Errorf("begin read snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := checkAccessEquivalenceDiskSpace(databasePath, outputPath); err != nil {
		return stats, err
	}
	file, err := os.Create(outputPath)
	if err != nil {
		return stats, err
	}
	writer := &accessEquivalenceWriter{buf: bufio.NewWriterSize(file, 1<<20)}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = file.Close()
			_ = os.Remove(outputPath)
		}
	}()

	tables, err := accessEquivalenceTables(ctx, tx)
	if err != nil {
		return stats, err
	}
	if err := dumpAccessPolicy(ctx, tx, writer, tables); err != nil {
		return stats, err
	}
	if err := dumpAccessInputs(ctx, tx, writer, tables); err != nil {
		return stats, err
	}

	decisionScopes, err := accessEquivalenceDecisionScopes(ctx, tx)
	if err != nil {
		return stats, err
	}
	for i, scope := range decisionScopes {
		if err := writer.write(accessScopeRecord{Section: "sc", ID: i, Actor: scope.ActorID, PM: scope.PMActorID, Admin: scope.AuthAdmin, Class: accessScopeClass(scope)}); err != nil {
			return stats, err
		}
	}
	stats.Scopes = len(decisionScopes)

	candidateSQL, err := accessEquivalenceCandidateSQL(tables)
	if err != nil {
		return stats, err
	}
	specialActors, err := accessEquivalenceSpecialActors(ctx, tx, tables)
	if err != nil {
		return stats, err
	}
	decisions, resources, denied, err := dumpAccessDecisionsByGroup(ctx, tx, writer, decisionScopes, specialActors, candidateSQL)
	if err != nil {
		return stats, err
	}
	stats.Decisions = decisions
	stats.Resources = resources
	stats.Denied = denied
	textDenied, err := dumpAccessTextProbesByGroup(ctx, tx, writer, decisionScopes, specialActors)
	if err != nil {
		return stats, err
	}
	stats.Denied += textDenied
	auditScopes, err := accessEquivalenceAuditScopes(ctx, tx)
	if err != nil {
		return stats, err
	}
	for i, scope := range auditScopes {
		visible, err := accessAuditRepositoryVisible(ctx, tx, scope, tables)
		if err != nil {
			return stats, fmt.Errorf("audit repository scope %d: %w", i, err)
		}
		value := 0
		if visible {
			value = 1
		}
		if err := writer.write(accessAuditRecord{Section: "a", Scope: i, Admin: scope.AuthAdmin, Visible: value}); err != nil {
			return stats, err
		}
	}
	if err := dumpAccessDiagnostics(ctx, tx, writer, tables); err != nil {
		return stats, err
	}
	if err := writer.flush(); err != nil {
		return stats, err
	}
	if err := file.Sync(); err != nil {
		return stats, err
	}
	if err := file.Close(); err != nil {
		return stats, err
	}
	succeeded = true
	stats.Records = writer.records
	stats.Bytes = writer.bytes
	stats.Duration = time.Since(started)
	return stats, nil
}

func openAccessEquivalenceReadOnly(ctx context.Context, databasePath string) (*sql.DB, error) {
	absolute, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, err
	}
	dsn := &url.URL{Scheme: "file", Path: absolute}
	query := dsn.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open read-only database: %w", err)
	}
	for _, pragma := range []string{"PRAGMA query_only=ON", "PRAGMA temp_store=FILE"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure read-only database: %w", err)
		}
	}
	return db, nil
}

// checkAccessEquivalenceDiskSpace refuses to start a dump that could fill the
// output filesystem. The database file size is a conservative upper bound for
// the compact dump, so requiring 2x that plus 5 GiB leaves room for the output
// and the bounded denied-list spools.
func checkAccessEquivalenceDiskSpace(databasePath, outputPath string) error {
	info, err := os.Stat(databasePath)
	if err != nil {
		return fmt.Errorf("stat database for disk estimate: %w", err)
	}
	estimate := uint64(info.Size())
	required := estimate*2 + 5*1024*1024*1024
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return fmt.Errorf("stat output filesystem: %w", err)
	}
	free := stat.Bavail * uint64(stat.Bsize)
	if free < required {
		return fmt.Errorf("insufficient free disk space for dump: need %d bytes (2x estimated %d + 5 GiB), have %d", required, estimate, free)
	}
	return nil
}

func accessEquivalenceTables(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type IN ('table','view')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tables := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		tables[name] = true
	}
	return tables, rows.Err()
}

func accessEquivalenceDecisionScopes(ctx context.Context, tx *sql.Tx) ([]AccessScope, error) {
	ledger := admissionLedgerCTE(AccessScope{})
	actors, err := collectAccessEquivalenceActors(ctx, tx, ledger)
	if err != nil {
		return nil, err
	}
	seen := map[string]AccessScope{}
	add := func(scope AccessScope) { seen[accessEquivalenceScopeKey(scope)] = scope }
	add(AccessScope{})
	for _, actor := range actors {
		if actor == "" {
			continue
		}
		add(AccessScope{ActorID: actor})
		add(AccessScope{ActorID: actor, PMActorID: actor})
	}
	scopes := make([]AccessScope, 0, len(seen))
	for _, scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		return accessEquivalenceScopeKey(scopes[i]) < accessEquivalenceScopeKey(scopes[j])
	})
	return scopes, nil
}

func accessEquivalenceAuditScopes(ctx context.Context, tx *sql.Tx) ([]AccessScope, error) {
	ledger := admissionLedgerCTE(AccessScope{})
	actors, err := collectAccessEquivalenceActors(ctx, tx, ledger)
	if err != nil {
		return nil, err
	}
	seen := map[string]AccessScope{}
	add := func(scope AccessScope) { seen[accessEquivalenceScopeKey(scope)] = scope }
	add(AccessScope{})
	add(AccessScope{AuthAdmin: true})
	for _, actor := range actors {
		add(AccessScope{ActorID: actor})
		add(AccessScope{ActorID: actor, AuthAdmin: true})
	}
	scopes := make([]AccessScope, 0, len(seen))
	for _, scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Slice(scopes, func(i, j int) bool {
		return accessEquivalenceScopeKey(scopes[i]) < accessEquivalenceScopeKey(scopes[j])
	})
	return scopes, nil
}

func collectAccessEquivalenceActors(ctx context.Context, tx *sql.Tx, ledger string) ([]string, error) {
	tables, err := accessEquivalenceTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	parts := []string{}
	if tables["agents"] {
		parts = append(parts, `SELECT CAST(actor_id AS TEXT) AS actor FROM main.agents`)
	}
	if tables["resource_access_admission_root_owners"] {
		parts = append(parts, `SELECT CAST(owner AS TEXT) AS actor FROM main.resource_access_admission_root_owners`)
	}
	if tables["resource_access_admission_owners"] {
		parts = append(parts, `SELECT CAST(owner AS TEXT) AS actor FROM main.resource_access_admission_owners WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)`)
	}
	if tables["threads"] {
		parts = append(parts, `SELECT CAST(json_extract(body_json,'$.pm_actor_id') AS TEXT) AS actor FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>''`)
	}
	if len(parts) == 0 {
		return nil, nil
	}
	q := `WITH RECURSIVE ` + ledger + ` SELECT DISTINCT actor FROM (` + strings.Join(parts, " UNION ALL ") + `) WHERE COALESCE(actor,'')<>'' ORDER BY actor`
	rows, err := tx.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var actors []string
	for rows.Next() {
		var actor string
		if err = rows.Scan(&actor); err != nil {
			return nil, err
		}
		actors = append(actors, actor)
	}
	return actors, rows.Err()
}

func accessEquivalenceScopeKey(scope AccessScope) string {
	encoded, _ := json.Marshal(scope)
	return string(encoded)
}

func accessScopeClass(scope AccessScope) string {
	switch {
	case scope.ActorID == "":
		return "anonymous"
	case scope.PMActorID == scope.ActorID:
		return "self-pm"
	default:
		return "ordinary"
	}
}

// accessEquivalenceCandidateSQL builds the candidate universe from persisted
// ownership vertices first, then canonical relations so public ownerless rows
// cannot disappear, then revisions, tombstones, live identities and virtual
// history vertices.
func accessEquivalenceCandidateSQL(tables map[string]bool) (string, error) {
	parts := []string{}
	add := func(kind, table, id string) {
		if tables[table] {
			parts = append(parts, `SELECT '`+kind+`' AS kind,`+id+` AS id FROM main.`+table)
		}
	}
	if tables["resource_access_admission_owners"] {
		parts = append(parts, `SELECT kind,id FROM main.resource_access_admission_owners WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)`)
	}
	if tables["resource_access_admission_roots"] {
		parts = append(parts, `SELECT kind,id FROM main.resource_access_admission_roots`)
	}
	if tables["resource_access_admission_spellings"] {
		parts = append(parts, `SELECT kind,id FROM main.resource_access_admission_spellings`)
	}
	for _, source := range resourceaccess.OwnershipSources {
		add(source.Kind, source.Table, source.ID)
	}
	for _, source := range resourceaccess.FilterOwnershipSources() {
		add(source.Kind, source.Table, source.ID)
	}
	if tables["resource_access_tombstones"] {
		parts = append(parts, `SELECT kind,id FROM main.resource_access_tombstones`)
	}
	if tables["resource_access_identities"] {
		parts = append(parts, `SELECT kind,resource_id AS id FROM main.resource_access_identities`)
	}
	if tables["resource_access_admission_graph"] {
		parts = append(parts, `SELECT parent_kind AS kind,parent_id AS id FROM main.resource_access_admission_graph WHERE parent_kind LIKE '%\_history' ESCAPE '\'`)
		parts = append(parts, `SELECT child_kind AS kind,child_id AS id FROM main.resource_access_admission_graph WHERE child_kind LIKE '%\_history' ESCAPE '\'`)
	}
	if len(parts) == 0 {
		return "", errors.New("no candidate resource relations found")
	}
	return strings.Join(parts, " UNION "), nil
}

func accessDecisionQuery(scope AccessScope, candidateSQL string) string {
	graph := accessCTEs(scope, `SELECT 1 FROM _anx_denied`)
	return `WITH RECURSIVE ` + admissionLedgerCTE(scope) + `, ` + graph +
		`, _anx_candidates(kind,id) AS MATERIALIZED (` + candidateSQL + `) ` +
		`SELECT c.kind,c.id,` + candidateVisibleSQL(scope, "c.kind", "c.id") +
		`,CASE WHEN NOT EXISTS(SELECT 1 FROM _anx_denied d WHERE d.kind=c.kind AND d.id=c.id) THEN 1 ELSE 0 END ` +
		`FROM _anx_candidates c ORDER BY c.kind,c.id`
}

var accessEquivalenceCorpus = []struct {
	ID    string
	Value string
}{
	{"bare", "alpha"},
	{"bare-upper", "ALPHA"},
	{"empty", ""},
	{"exact-artifact", "artifact:alpha"},
	{"exact-board", "board:alpha"},
	{"exact-card", "card:alpha"},
	{"exact-card-revision", "card_revision:alpha"},
	{"exact-doc", "doc:alpha"},
	{"exact-document", "document:alpha"},
	{"exact-event", "event:alpha"},
	{"exact-external", "external_key:alpha"},
	{"exact-inbox", "inbox:alpha"},
	{"exact-plan", "plan:alpha"},
	{"exact-run", "run:alpha"},
	{"exact-thread", "thread:alpha"},
	{"exact-topic", "topic:alpha"},
	{"exact-wakeup", "wakeup:alpha"},
	{"json-shaped", `{"ref":"card:alpha"}`},
	{"prose-newline", "line one\ncard:alpha\nline two"},
	{"prose-prefix", "see card:alpha for details"},
	{"prose-punct", "please review (card:alpha), thanks"},
	{"prose-suffix", "card:alpha is referenced"},
	{"prose-unicode", "see card:café now"},
	{"punct-colon", "card:alpha:beta"},
	{"punct-dash", "card:alpha-beta"},
	{"punct-dot", "card:alpha.beta"},
	{"punct-slash", "card:alpha/beta"},
	{"punct-space", "card:alpha beta"},
	{"unicode-accent", "card:café"},
	{"unicode-cjk", "card:東京"},
	{"unicode-emoji", "card:😀"},
	{"wrapper-angle", "<card:alpha>"},
	{"wrapper-backtick", "`card:alpha`"},
	{"wrapper-bracket", "[card:alpha]"},
	{"wrapper-markdown", "**card:alpha**"},
	{"wrapper-paren", "(card:alpha)"},
	{"wrapper-quote", `"card:alpha"`},
}

func canonicalAccessTextDenied(ctx context.Context, tx *sql.Tx, scope AccessScope, value string) (bool, error) {
	graph := accessCTEs(scope, `SELECT 1 FROM _anx_denied_atoms`)
	query := `WITH RECURSIVE ` + graph + ` SELECT EXISTS(
 SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j CROSS JOIN _anx_denied_atoms d WHERE j.value=d.ref COLLATE NOCASE
 UNION ALL
 SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j CROSS JOIN _anx_denied_atoms d WHERE ` + resourceaccess.TextReferenceCandidateSQL("j.value") + ` AND d.typed AND ` + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + `)`
	var denied bool
	if err := tx.QueryRowContext(ctx, query, value, value).Scan(&denied); err != nil {
		return false, err
	}
	return denied, nil
}

func accessAuditRepositoryVisible(ctx context.Context, tx *sql.Tx, scope AccessScope, tables map[string]bool) (bool, error) {
	if !tables["auth_audit_events"] {
		return false, nil
	}
	graph := accessCTEs(scope, `SELECT 1 FROM auth_audit_events`)
	var visible bool
	if err := tx.QueryRowContext(ctx, `WITH RECURSIVE `+graph+` SELECT EXISTS(SELECT 1 FROM auth_audit_events)`).Scan(&visible); err != nil {
		return false, err
	}
	return visible, nil
}

// Hex is the only binary representation in a dump. SQLite typeof is retained
// for owner values so numeric legacy owners never silently become text.
type accessTypedValue struct {
	Type  string `json:"t"`
	Value any    `json:"v"`
}

func accessEncodeValue(value any) accessTypedValue {
	switch typed := value.(type) {
	case nil:
		return accessTypedValue{Type: "null"}
	case int64:
		return accessTypedValue{Type: "integer", Value: typed}
	case int:
		return accessTypedValue{Type: "integer", Value: int64(typed)}
	case float64:
		return accessTypedValue{Type: "real", Value: typed}
	case bool:
		encoded := int64(0)
		if typed {
			encoded = 1
		}
		return accessTypedValue{Type: "integer", Value: encoded}
	case string:
		return accessTypedValue{Type: "text", Value: typed}
	case []byte:
		return accessTypedValue{Type: "blob", Value: hex.EncodeToString(typed)}
	default:
		return accessTypedValue{Type: "text", Value: fmt.Sprint(typed)}
	}
}

func accessHexBlob(value string) accessTypedValue {
	return accessTypedValue{Type: "blob", Value: strings.ToLower(value)}
}
