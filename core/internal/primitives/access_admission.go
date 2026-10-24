package primitives

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

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
	key := fmt.Sprint(proof.pointToken) + `,CAST(` + kind + ` AS BLOB),CAST(` + id + ` AS BLOB)`
	return `CASE WHEN ` + candidateProofGuardSQL(proof) + ` THEN COALESCE(anx_candidate_lookup(` + key + `),anx_candidate_remember(` + key + `,` + canonical + `,CAST('[]' AS BLOB))) ELSE ` + canonical + ` END`
}

func candidateProofGuardSQL(proof *denialSnapshot) string {
	return `COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=` + fmt.Sprint(proof.epoch) + ` AND total_changes()=` + fmt.Sprint(proof.candidateChanges)
}

func candidateTextProofSQL(scopeSQL, mode, value string, proofs ...*denialSnapshot) string {
	if len(proofs) == 0 || proofs[0] == nil || (proofs[0].inventory && proofs[0].candidateComplete == nil) || proofs[0].pointToken == 0 || proofs[0].epoch < 0 {
		return scopeSQL
	}
	proof := proofs[0]
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
	sqlite.MustRegisterScalarFunction("anx_candidate_remember", 5, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		allowed, valid := args[3].(int64)
		if !valid || allowed != 0 && allowed != 1 {
			return int64(0), nil
		}
		snapshot, target, ok := candidateProofTarget(args)
		if !ok {
			return allowed, nil
		} // The SQL-computed result remains canonical.
		var ancestors []denialTarget
		if raw, ok := args[4].([]byte); ok && allowed == 1 && len(raw) <= 1<<20 && utf8.Valid(raw) {
			var rows [][]any
			if json.Unmarshal(raw, &rows) == nil {
				for _, row := range rows {
					if len(row) != 2 {
						ancestors = nil
						break
					}
					kind, kOK := row[0].(string)
					id, iOK := row[1].(string)
					if !kOK || !iOK {
						ancestors = nil
						break
					}
					ancestors = append(ancestors, denialTarget{kind, id})
				}
			}
		}
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
		// If this candidate has no denied ancestor, every visited ancestor is
		// itself visible. Reuse those SQL proofs across the page's joins.
		for _, target := range ancestors {
			remember(target, true)
		}
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

func candidateValueCheckSQL(scope AccessScope) string {
	return `WITH RECURSIVE ` + admissionLedgerCTE(scope) + ` SELECT CASE WHEN (SELECT active FROM _anx_admission_ledger)=0 THEN 0 ELSE EXISTS(SELECT 1 FROM json_each(?) j WHERE NOT (` + candidateCleanAtomSQL(scope, "j.value") + `)) END`
}

// Full inventories already seek the root index in bulk. Avoid filling a fresh
// per-page proof map for every inventory hydration batch.
type inventoryAdmissionKey struct{}

func WithInventoryAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, inventoryAdmissionKey{}, true)
}
