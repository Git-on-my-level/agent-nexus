package primitives

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
	"modernc.org/sqlite"
)

func admissionLedgerCTE(scope AccessScope) string {
	active := `EXISTS(SELECT 1 FROM main.resource_access_admission_root_owners WHERE unconditional=1 LIMIT 1)`
	if scope.ActorID == "" || scope.ActorID != scope.PMActorID {
		actor := `'` + strings.ReplaceAll(scope.ActorID, `'`, `''`) + `'`
		active += ` OR EXISTS(SELECT 1 FROM main.resource_access_admission_root_owners WHERE unconditional=0 AND owner<` + actor + ` LIMIT 1) OR EXISTS(SELECT 1 FROM main.resource_access_admission_root_owners WHERE unconditional=0 AND owner>` + actor + ` LIMIT 1)`
	}
	return `_anx_admission_ledger(ledger,active) AS MATERIALIZED (SELECT CASE WHEN ` + resourceaccess.CompactReaderSQL + ` THEN CASE WHEN EXISTS(SELECT 1 FROM main.sqlite_schema WHERE type='table' AND name='resource_access_compact_edges') THEN 2 ELSE -2 END WHEN NOT (` + resourceaccess.CompactReaderSQL + `) THEN 1 ELSE -1 END,(` + active + `))`
}

// Candidate admission seeks the persisted root set, then current root owners.
// A guarded request-local proof retains the unchanged warm membership budget.
func candidateVisibleSQL(scope AccessScope, kind, id string, proofs ...*denialSnapshot) string {
	base := ` FROM main.resource_access_admission_owners r WHERE r.ledger=(SELECT ledger FROM _anx_admission_ledger) AND r.kind=` + kind + ` AND r.id=CAST(` + id + ` AS TEXT)`
	denied := `EXISTS(SELECT 1` + base + ` AND r.unconditional=1 LIMIT 1)`
	if scope.ActorID == "" || scope.ActorID != scope.PMActorID {
		actor := `'` + strings.ReplaceAll(scope.ActorID, `'`, `''`) + `'`
		denied += ` OR EXISTS(SELECT 1` + base + ` AND r.unconditional=0 AND r.owner<` + actor + ` LIMIT 1) OR EXISTS(SELECT 1` + base + ` AND r.unconditional=0 AND r.owner>` + actor + ` LIMIT 1)`
	}
	canonical := `CASE WHEN (SELECT active FROM _anx_admission_ledger)=0 THEN 1 WHEN (SELECT ledger FROM _anx_admission_ledger)=-2 THEN 0 ELSE NOT (` + denied + `) END`
	if len(proofs) == 0 || proofs[0] == nil || (proofs[0].inventory && (proofs[0].candidateComplete == nil || proofs[0].candidateAllPublic)) || proofs[0].pointToken == 0 || proofs[0].epoch < 0 {
		return canonical
	}
	proof := proofs[0]
	if proof.candidateAllPublic {
		// The epoch/connection guard covers this proof for every identity. Avoid
		// constructing per-row identity arguments for that same empty-root fact.
		return `COALESCE((SELECT valid FROM _anx_candidate_public_guard),` + canonical + `)`
	}
	if proof.candidateSeek {
		// The canonical expression is the same decision the memo returns under
		// its guard. It seeks the owner primary key by (ledger,kind,id) from the
		// candidate row, with no custom function inside the caller's WHERE.
		return canonical
	}
	key := fmt.Sprint(proof.pointToken) + `,CAST(` + kind + ` AS BLOB),CAST(` + id + ` AS BLOB)`
	return `CASE WHEN ` + candidateProofGuardSQL(proof) + ` THEN COALESCE(anx_candidate_lookup(` + key + `),anx_candidate_remember(` + key + `,` + canonical + `)) ELSE ` + canonical + ` END`
}

func candidateProofGuardSQL(proof *denialSnapshot) string {
	return `COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=` + fmt.Sprint(proof.epoch) + ` AND total_changes()=` + fmt.Sprint(proof.candidateChanges)
}

func candidateTextProofSQL(scopeSQL, mode, value string, proofs ...*denialSnapshot) string {
	if len(proofs) == 0 || proofs[0] == nil || (proofs[0].inventory && proofs[0].candidateComplete == nil) || proofs[0].pointToken == 0 || proofs[0].epoch < 0 {
		return scopeSQL
	}
	proof := proofs[0]
	if proof.candidateSeek {
		return scopeSQL
	}
	key := fmt.Sprint(proof.pointToken) + `,CAST('` + mode + `' AS BLOB),CAST(` + value + ` AS BLOB)`
	return `CASE WHEN ` + candidateProofGuardSQL(proof) + ` THEN COALESCE(anx_candidate_text_lookup(` + key + `),anx_candidate_text_remember(` + key + `,` + scopeSQL + `)) ELSE ` + scopeSQL + ` END`
}

func candidateRootConstraintSQL(scope AccessScope, alias string) string {
	where := alias + `.unconditional=1`
	if scope.ActorID == "" || scope.ActorID != scope.PMActorID {
		where += ` OR ` + alias + `.owner<>'` + strings.ReplaceAll(scope.ActorID, `'`, `''`) + `'`
	}
	return where
}

func init() {
	sqlite.MustRegisterScalarFunction("anx_candidate_lookup_public", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		token, ok := args[0].(int64)
		if !ok {
			return nil, nil
		}
		value, found := pointSnapshots.Load(token)
		if !found {
			return nil, nil
		}
		snapshot := value.(*denialSnapshot)
		if !snapshot.candidates || !snapshot.candidateAllPublic {
			return nil, nil
		}
		return int64(1), nil
	})
	sqlite.MustRegisterScalarFunction("anx_candidate_lookup", 3, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		snapshot, target, ok := candidateProofTarget(args)
		if !ok {
			return nil, nil
		}
		snapshot.candidateMu.Lock()
		defer snapshot.candidateMu.Unlock()
		if snapshot.candidateAllPublic {
			return int64(1), nil
		}
		if snapshot.candidateComplete != nil {
			if _, denied := snapshot.candidateComplete[target]; denied {
				return int64(0), nil
			}
			return int64(1), nil
		}
		if allowed, found := snapshot.candidateIndex[target]; found {
			if allowed {
				return int64(1), nil
			}
			return int64(0), nil
		}
		return nil, nil
	})
	sqlite.MustRegisterScalarFunction("anx_candidate_remember", 4, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		allowed, valid := args[3].(int64)
		if !valid || allowed != 0 && allowed != 1 {
			return int64(0), nil
		}
		snapshot, target, ok := candidateProofTarget(args)
		if !ok {
			return allowed, nil
		} // The SQL-computed result remains canonical.
		snapshot.candidateMu.Lock()
		defer snapshot.candidateMu.Unlock()
		remember := func(target denialTarget, allowed bool) {
			if _, found := snapshot.candidateIndex[target]; found {
				return
			}
			bytes := len(target.kind) + len(target.id)
			if len(snapshot.candidateIndex)+len(snapshot.candidateTextIndex) >= 8192 || snapshot.candidateBytes+bytes > 1<<20 {
				return
			}
			snapshot.candidateIndex[target] = allowed
			snapshot.candidateBytes += bytes
		}
		remember(target, allowed == 1)
		return allowed, nil
	})
	sqlite.MustRegisterScalarFunction("anx_candidate_text_lookup", 3, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		snapshot, target, ok := candidateProofTarget(args)
		if !ok {
			return nil, nil
		}
		snapshot.candidateMu.Lock()
		defer snapshot.candidateMu.Unlock()
		if snapshot.candidateAllPublic {
			return int64(1), nil
		}
		allowed, found := snapshot.candidateTextIndex[candidateTextTarget{target.kind, target.id}]
		if !found {
			return nil, nil
		}
		if allowed {
			return int64(1), nil
		}
		return int64(0), nil
	})
	sqlite.MustRegisterScalarFunction("anx_candidate_text_remember", 4, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		allowed, valid := args[3].(int64)
		if !valid || allowed != 0 && allowed != 1 {
			return int64(0), nil
		}
		snapshot, target, ok := candidateProofTarget(args)
		if !ok {
			return allowed, nil
		}
		snapshot.candidateMu.Lock()
		defer snapshot.candidateMu.Unlock()
		key := candidateTextTarget{target.kind, target.id}
		if _, found := snapshot.candidateTextIndex[key]; !found && len(snapshot.candidateIndex)+len(snapshot.candidateTextIndex) < 8192 && snapshot.candidateBytes+len(target.kind)+len(target.id) <= 1<<20 {
			if snapshot.candidateTextIndex == nil {
				snapshot.candidateTextIndex = make(map[candidateTextTarget]bool)
			}
			snapshot.candidateTextIndex[key] = allowed == 1
			snapshot.candidateBytes += len(target.kind) + len(target.id)
		}
		return allowed, nil
	})
}

func candidateProofTarget(args []driver.Value) (*denialSnapshot, denialTarget, bool) {
	token, ok := args[0].(int64)
	if !ok {
		return nil, denialTarget{}, false
	}
	value, found := pointSnapshots.Load(token)
	if !found {
		return nil, denialTarget{}, false
	}
	snapshot := value.(*denialSnapshot)
	kind, kOK := args[1].([]byte)
	id, iOK := args[2].([]byte)
	if !snapshot.candidates || snapshot.candidateIndex == nil || !kOK || !iOK {
		return nil, denialTarget{}, false
	}
	return snapshot, denialTarget{string(kind), string(id)}, true
}

func candidateCleanAtomSQL(scope AccessScope, atom string, proofs ...*denialSnapshot) string {
	exact := `EXISTS(SELECT 1 FROM main.resource_access_admission_spellings k WHERE k.atom=unhex(` + resourceaccess.AtomKeySQL(atom) + `) AND NOT (` + candidateVisibleSQL(scope, "k.kind", "k.id", proofs...) + `))`
	prose := `EXISTS(WITH RECURSIVE _anx_probes(probe) AS (SELECT value FROM json_each(` + resourceaccess.ProseStartsSQL(atom) + `) UNION ALL SELECT ` + resourceaccess.ProseNextSQL(atom, "p.probe") + ` FROM _anx_probes p CROSS JOIN main.resource_access_admission_prose_prefixes n ON n.atom=unhex(json_extract(p.probe,'$.prefix'))) SELECT 1 FROM _anx_probes p CROSS JOIN json_each(p.probe,'$.keys') b CROSS JOIN main.resource_access_admission_spellings k ON k.prose_atom=unhex(b.value) AND k.is_ref=1 WHERE CASE WHEN ` + resourceaccess.TextReferenceMatchSQL(atom, "k.ref") + ` THEN NOT (` + candidateVisibleSQL(scope, "k.kind", "k.id", proofs...) + `) ELSE 0 END)`
	return candidateTextProofSQL(`NOT (`+exact+` OR `+prose+`)`, "atom", atom, proofs...)
}

// Endpoint spellings are binary typed references, whereas JSON atoms also
// match bare IDs case-insensitively. Do not turn a private raw ID such as
// "card:public" into authority over the public card's navigational endpoint.
func candidateCleanRefSQL(scope AccessScope, value string, proofs ...*denialSnapshot) string {
	return candidateTextProofSQL(`NOT EXISTS(SELECT 1 FROM main.resource_access_admission_spellings k WHERE k.ref=`+value+` AND k.is_ref=1 AND NOT (`+candidateVisibleSQL(scope, "k.kind", "k.id", proofs...)+`))`, "ref", value, proofs...)
}

// candidateValuesDenied reports whether any atom in atomsJSON is denied to scope.
// Exact and prose candidates are selected by Go-side walks: each statement is
// indexed on spelling keys and contains no custom function in its WHERE. A
// *sql.DB runs every statement of one check inside a single read transaction,
// so the walk sees one snapshot; an existing transaction is used as is.
func candidateValuesDenied(ctx context.Context, q queryRower, scope AccessScope, atomsJSON string) (bool, error) {
	var atoms []string
	if err := json.Unmarshal([]byte(atomsJSON), &atoms); err != nil {
		return false, err
	}
	rows, ok := q.(candidateRows)
	if !ok {
		return false, errors.New("candidate admission requires a query handle")
	}
	db, ok := q.(*sql.DB)
	if !ok {
		return candidateWalk(ctx, rows, scope, atoms)
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	denied, err := candidateWalk(ctx, tx, scope, atoms)
	if err != nil {
		return false, err
	}
	return denied, tx.Commit()
}

// candidateRows is the query surface of a *sql.DB or *sql.Tx.
type candidateRows interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type candidateProbe struct {
	atom  int
	probe resourceaccess.ProseProbe
}

// candidateWalk evaluates the exact and prose candidates of atoms. Probes are
// expanded in Go and extended only where SQL prefix membership proves a longer
// spelling may exist, so the walk performs one statement per prose level.
func candidateWalk(ctx context.Context, q candidateRows, scope AccessScope, atoms []string) (bool, error) {
	exact := make([]string, 0, len(atoms))
	seenExact := map[string]bool{}
	var probes []candidateProbe
	for i, atom := range atoms {
		key := resourceaccess.AtomKey(atom)
		if !seenExact[key] {
			seenExact[key] = true
			exact = append(exact, key)
		}
		for _, p := range resourceaccess.ProseStarts(atom) {
			probes = append(probes, candidateProbe{atom: i, probe: p})
		}
	}
	exactJSON, err := json.Marshal(exact)
	if err != nil {
		return false, err
	}
	statement := candidateWalkSQL(scope)
	for level := 0; ; level++ {
		keyAtoms := map[string][]int{}
		var keys, prefixes []string
		seenPrefix := map[string]bool{}
		for _, cp := range probes {
			for _, key := range cp.probe.Keys {
				if _, ok := keyAtoms[key]; !ok {
					keys = append(keys, key)
				}
				if !slices.Contains(keyAtoms[key], cp.atom) {
					keyAtoms[key] = append(keyAtoms[key], cp.atom)
				}
			}
			if cp.probe.Prefix != "" && !seenPrefix[cp.probe.Prefix] {
				seenPrefix[cp.probe.Prefix] = true
				prefixes = append(prefixes, cp.probe.Prefix)
			}
		}
		keysJSON, err := json.Marshal(keys)
		if err != nil {
			return false, err
		}
		prefixesJSON, err := json.Marshal(prefixes)
		if err != nil {
			return false, err
		}
		exactArg := "[]"
		if level == 0 {
			exactArg = string(exactJSON)
		}
		denied, present, err := candidateWalkLevel(ctx, q, statement, exactArg, string(keysJSON), string(prefixesJSON), keyAtoms, atoms)
		if err != nil || denied {
			return denied, err
		}
		var next []candidateProbe
		for _, cp := range probes {
			if cp.probe.Prefix == "" || !present[cp.probe.Prefix] {
				continue
			}
			advanced, err := cp.probe.Next()
			if err != nil {
				return false, err
			}
			next = append(next, candidateProbe{atom: cp.atom, probe: advanced})
		}
		if len(next) == 0 {
			return false, nil
		}
		probes = next
	}
}

// candidateWalkLevel runs one statement for a probe level. It reports a denied
// exact or prose spelling, and which probe prefixes exist in the ledger.
func candidateWalkLevel(ctx context.Context, q candidateRows, statement, exactArg, keysArg, prefixesArg string, keyAtoms map[string][]int, atoms []string) (bool, map[string]bool, error) {
	rows, err := q.QueryContext(ctx, statement, exactArg, keysArg, prefixesArg)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	present := map[string]bool{}
	denied := false
	for rows.Next() {
		var tag, a, b string
		if err := rows.Scan(&tag, &a, &b); err != nil {
			return false, nil, err
		}
		switch tag {
		case "x":
			denied = true
		case "p":
			ref, err := hex.DecodeString(b)
			if err != nil {
				return false, nil, err
			}
			for _, i := range keyAtoms[strings.ToLower(a)] {
				if resourceaccess.TextHasReference(atoms[i], string(ref)) {
					denied = true
				}
			}
		case "f":
			present[strings.ToLower(a)] = true
		}
		if denied {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return false, nil, err
	}
	return denied, present, nil
}

// candidateWalkSQL returns denied exact spellings ('x'), prose spellings whose
// key the walk supplied ('p', with its hex key and reference bytes), and probe
// prefixes that exist in the ledger ('f'). Each member is guarded by the same
// inactive-ledger rule as the legacy statement, and visibility is unchanged.
func candidateWalkSQL(scope AccessScope) string {
	active := `COALESCE((SELECT active FROM _anx_admission_ledger)=0,0)=0`
	visible := candidateVisibleSQL(scope, "k.kind", "k.id")
	return `WITH RECURSIVE ` + admissionLedgerCTE(scope) + `
SELECT 'x' AS tag,'' AS a,'' AS b WHERE ` + active + ` AND EXISTS(SELECT 1 FROM json_each(?) e JOIN main.resource_access_admission_spellings k ON k.atom=unhex(e.value) WHERE NOT (` + visible + `))
UNION ALL SELECT 'p',hex(k.prose_atom),hex(k.ref) FROM json_each(?) e JOIN main.resource_access_admission_spellings k ON k.prose_atom=unhex(e.value) WHERE k.is_ref=1 AND ` + active + ` AND NOT (` + visible + `)
UNION ALL SELECT 'f',hex(n.atom),'' FROM main.resource_access_admission_prose_prefixes n WHERE ` + active + ` AND n.atom IN (SELECT unhex(e.value) FROM json_each(?) e)`
}

// Full inventories already seek the root index in bulk. Avoid filling a fresh
// per-page proof map for every inventory hydration batch.
type inventoryAdmissionKey struct{}

func WithInventoryAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, inventoryAdmissionKey{}, true)
}

// Seek admission pins a candidate page whose per-row visibility predicates are
// canonical indexed seeks only. See denialSnapshot.candidateSeek.
type seekAdmissionKey struct{}

func withSeekAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, seekAdmissionKey{}, true)
}
