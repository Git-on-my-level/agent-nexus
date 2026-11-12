package primitives

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"sort"
)

// Policy grouping is an exact optimization, not a relaxation: two scopes share
// a group only when every persisted policy input compares the same way. Ordinary
// actors outside the finite owner/PM/tombstone set differ only by a literal that
// appears in no predicate, so their decisions are identical. Every self-PM scope
// skips owner comparisons and has only the unconditional unknown-artifact root.
func accessEquivalenceSpecialActors(ctx context.Context, tx *sql.Tx, tables map[string]bool) (map[string]bool, error) {
	special := map[string]bool{"": true}
	addQuery := func(query string) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var actor sql.NullString
			if err = rows.Scan(&actor); err != nil {
				return err
			}
			if actor.Valid {
				special[actor.String] = true
			}
		}
		return rows.Err()
	}
	ledger := admissionLedgerCTE(AccessScope{})
	if tables["resource_access_admission_owners"] {
		if err := addQuery(`WITH RECURSIVE ` + ledger + ` SELECT DISTINCT CAST(owner AS TEXT) FROM main.resource_access_admission_owners WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)`); err != nil {
			return nil, err
		}
	}
	if tables["resource_access_admission_root_owners"] {
		if err := addQuery(`SELECT DISTINCT CAST(owner AS TEXT) FROM main.resource_access_admission_root_owners`); err != nil {
			return nil, err
		}
	}
	if tables["threads"] {
		if err := addQuery(`SELECT DISTINCT CAST(json_extract(body_json,'$.pm_actor_id') AS TEXT) FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>''`); err != nil {
			return nil, err
		}
	}
	if tables["resource_access_tombstones"] {
		if err := addQuery(`SELECT DISTINCT CAST(owner AS TEXT) FROM main.resource_access_tombstones`); err != nil {
			return nil, err
		}
	}
	return special, nil
}

func accessEquivalencePolicyGroup(scope AccessScope, special map[string]bool) string {
	if scope.ActorID != "" && scope.ActorID == scope.PMActorID {
		return "self-pm"
	}
	if special[scope.ActorID] {
		return "actor:" + scope.ActorID
	}
	return "other"
}

func accessEquivalenceGroupScopes(scopes []AccessScope, special map[string]bool) ([]string, map[string][]int) {
	groups := map[string][]int{}
	for i, scope := range scopes {
		key := accessEquivalencePolicyGroup(scope, special)
		groups[key] = append(groups[key], i)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, groups
}

type accessGroupDecision struct {
	kind  string
	id    string
	cand  int
	canon int
}

func writeAccessDecisionDigest(digest hash.Hash, kind, id string, cand, canon int) {
	writeAccessHashValue(digest, kind)
	writeAccessHashValue(digest, id)
	writeAccessHashValue(digest, int64(cand))
	writeAccessHashValue(digest, int64(canon))
}

func writeAccessTextDigest(digest hash.Hash, probe string, cand, canon int) {
	writeAccessHashValue(digest, probe)
	writeAccessHashValue(digest, int64(cand))
	writeAccessHashValue(digest, int64(canon))
}

type accessDecisionGroupResult struct {
	rank   int
	count  int64
	digest string
	spool  *os.File
}

func (r *accessDecisionGroupResult) close() {
	if r == nil || r.spool == nil {
		return
	}
	name := r.spool.Name()
	_ = r.spool.Close()
	_ = os.Remove(name)
	r.spool = nil
}

// dumpAccessDecisionsByGroup evaluates each policy group once, spools only its
// denied rows, then writes per-scope summaries and denied rows in scope order.
// The full decision stream is represented by the summary digest, so a visible
// set change is still detected even when the denied list is unchanged.
func dumpAccessDecisionsByGroup(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, scopes []AccessScope, special map[string]bool, candidateSQL string) (int64, int, int64, error) {
	keys, groups := accessEquivalenceGroupScopes(scopes, special)
	results := make([]*accessDecisionGroupResult, len(keys))
	defer func() {
		for _, result := range results {
			result.close()
		}
	}()
	rankByKey := make(map[string]int, len(keys))
	var resources int
	for rank, key := range keys {
		indices := groups[key]
		representative := scopes[indices[0]]
		result, err := spoolAccessCompactDecisions(ctx, tx, representative, candidateSQL)
		if err != nil {
			return 0, 0, 0, err
		}
		result.rank = rank
		results[rank] = result
		rankByKey[key] = rank
		if rank == 0 {
			resources = int(result.count)
		}
	}

	var decisions, denied int64
	for scopeID, scope := range scopes {
		rank := rankByKey[accessEquivalencePolicyGroup(scope, special)]
		result := results[rank]
		if err := writer.write(accessDecisionSummary{
			Section: "d", Group: rank, Scope: scopeID, Count: result.count, Digest: result.digest,
		}); err != nil {
			return decisions, resources, denied, err
		}
		if _, err := result.spool.Seek(0, io.SeekStart); err != nil {
			return decisions, resources, denied, err
		}
		reader := bufio.NewReaderSize(result.spool, 1<<20)
		for {
			row, err := readAccessGroupDecision(reader)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return decisions, resources, denied, err
			}
			if err = writer.write(accessDecisionRecord{
				Section: "d", Group: rank, Scope: scopeID,
				Kind: row.kind, ID: row.id, Cand: row.cand, Canon: row.canon,
			}); err != nil {
				return decisions, resources, denied, err
			}
			denied++
		}
		decisions += result.count
	}
	return decisions, resources, denied, nil
}

func spoolAccessCompactDecisions(ctx context.Context, tx *sql.Tx, scope AccessScope, candidateSQL string) (*accessDecisionGroupResult, error) {
	spool, err := os.CreateTemp("", "anx-access-equivalence-denied-*")
	if err != nil {
		return nil, err
	}
	result := &accessDecisionGroupResult{spool: spool}
	rows, err := tx.QueryContext(ctx, accessDecisionQuery(scope, candidateSQL))
	if err != nil {
		result.close()
		return nil, err
	}
	defer rows.Close()
	writer := bufio.NewWriterSize(spool, 1<<20)
	digest := sha256.New()
	for rows.Next() {
		var kind, id string
		var cand, canon int
		if err = rows.Scan(&kind, &id, &cand, &canon); err != nil {
			result.close()
			return nil, err
		}
		writeAccessDecisionDigest(digest, kind, id, cand, canon)
		result.count++
		if cand == 0 || canon == 0 {
			if err = writeAccessGroupDecision(writer, accessGroupDecision{kind: kind, id: id, cand: cand, canon: canon}); err != nil {
				result.close()
				return nil, err
			}
		}
	}
	if err = rows.Err(); err != nil {
		result.close()
		return nil, err
	}
	if err = writer.Flush(); err != nil {
		result.close()
		return nil, err
	}
	result.digest = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

func writeAccessGroupDecision(writer io.Writer, row accessGroupDecision) error {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(row.kind)))
	if _, err := writer.Write(length[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, row.kind); err != nil {
		return err
	}
	binary.BigEndian.PutUint32(length[:], uint32(len(row.id)))
	if _, err := writer.Write(length[:]); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, row.id); err != nil {
		return err
	}
	if _, err := writer.Write([]byte{byte(row.cand), byte(row.canon)}); err != nil {
		return err
	}
	return nil
}

func readAccessGroupDecision(reader io.Reader) (accessGroupDecision, error) {
	var row accessGroupDecision
	var length [4]byte
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return row, err
	}
	kind := make([]byte, binary.BigEndian.Uint32(length[:]))
	if _, err := io.ReadFull(reader, kind); err != nil {
		return row, err
	}
	if _, err := io.ReadFull(reader, length[:]); err != nil {
		return row, err
	}
	id := make([]byte, binary.BigEndian.Uint32(length[:]))
	if _, err := io.ReadFull(reader, id); err != nil {
		return row, err
	}
	flags := make([]byte, 2)
	if _, err := io.ReadFull(reader, flags); err != nil {
		return row, err
	}
	row.kind, row.id, row.cand, row.canon = string(kind), string(id), int(flags[0]), int(flags[1])
	return row, nil
}

type accessTextResult struct {
	probe string
	cand  int
	canon int
}

type accessTextGroupResult struct {
	rank   int
	count  int64
	digest string
	denied []accessTextResult
}

func dumpAccessTextProbesByGroup(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, scopes []AccessScope, special map[string]bool) (int64, error) {
	keys, groups := accessEquivalenceGroupScopes(scopes, special)
	results := make(map[string]*accessTextGroupResult, len(keys))
	rankByKey := make(map[string]int, len(keys))
	for rank, key := range keys {
		indices := groups[key]
		result, err := computeAccessTextProbes(ctx, tx, scopes[indices[0]])
		if err != nil {
			return 0, err
		}
		result.rank = rank
		results[key] = result
		rankByKey[key] = rank
	}

	var denied int64
	for scopeID, scope := range scopes {
		key := accessEquivalencePolicyGroup(scope, special)
		result := results[key]
		if err := writer.write(accessTextSummary{
			Section: "t", Group: result.rank, Scope: scopeID, Count: result.count, Digest: result.digest,
		}); err != nil {
			return denied, err
		}
		for _, row := range result.denied {
			if err := writer.write(accessTextRecord{
				Section: "t", Group: result.rank, Scope: scopeID,
				Probe: row.probe, Cand: row.cand, Canon: row.canon,
			}); err != nil {
				return denied, err
			}
			denied++
		}
	}
	return denied, nil
}

func computeAccessTextProbes(ctx context.Context, tx *sql.Tx, scope AccessScope) (*accessTextGroupResult, error) {
	result := &accessTextGroupResult{count: int64(len(accessEquivalenceCorpus))}
	digest := sha256.New()
	for _, probe := range accessEquivalenceCorpus {
		atoms, err := json.Marshal([]string{probe.Value})
		if err != nil {
			return nil, err
		}
		denied, err := candidateValuesDenied(ctx, tx, scope, string(atoms))
		if err != nil {
			return nil, err
		}
		canonical, err := canonicalAccessTextDenied(ctx, tx, scope, probe.Value)
		if err != nil {
			return nil, err
		}
		cand, canon := 0, 0
		if !denied {
			cand = 1
		}
		if !canonical {
			canon = 1
		}
		writeAccessTextDigest(digest, probe.ID, cand, canon)
		if cand == 0 || canon == 0 {
			result.denied = append(result.denied, accessTextResult{probe: probe.ID, cand: cand, canon: canon})
		}
	}
	result.digest = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}
