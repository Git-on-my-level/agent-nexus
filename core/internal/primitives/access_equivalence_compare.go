package primitives

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

type accessCompareRecord struct {
	Section string `json:"s"`
	Key     string `json:"key"`
	Group   int    `json:"g"`
	Scope   int    `json:"z"`
	Actor   string `json:"a"`
	PM      string `json:"p"`
	Admin   bool   `json:"admin"`
	Kind    string `json:"k"`
	ID      string `json:"i"`
	Probe   string `json:"q"`
	Cand    int    `json:"c"`
	Canon   int    `json:"w"`
	Visible int    `json:"vis"`
	Count   int64  `json:"cnt"`
	Digest  string `json:"h"`
	Compare string `json:"cmp"`
}

// CompareAccessEquivalence compares two dumps. Policy, persisted-input, scope
// and audit records must be identical after expected migration/fence/epoch
// rows are excluded. Diagnostics (section g) are never permission identity and
// are ignored.
//
// Resource and text decisions are compact: each scope has a summary digest over
// the full sorted decision stream plus only its denied rows. A digest mismatch
// with an identical denied list is always a failure. With
// allowCardHistoryUnion, only a computed visible->hidden change for an older
// card revision or its exclusively owned artifact is accepted, and only when a
// target in the card's union-minus-own set was already denied to that same
// scope in the before database. Every other difference fails.
func CompareAccessEquivalence(ctx context.Context, beforePath, afterPath string, allowCardHistoryUnion bool, beforeDBPath string) (AccessEquivalenceCompareResult, error) {
	var result AccessEquivalenceCompareResult
	var allowlist *cardHistoryAllowlist
	if allowCardHistoryUnion {
		if strings.TrimSpace(beforeDBPath) == "" {
			return result, errors.New("--allow-card-history-union requires --before-db")
		}
		var err error
		allowlist, err = newCardHistoryAllowlist(ctx, beforeDBPath)
		if err != nil {
			return result, err
		}
		defer allowlist.Close()
	}

	beforeScopes, compared, err := compareAccessSimpleSections(beforePath, afterPath)
	if err != nil {
		return result, err
	}
	result.Compared = compared
	for _, section := range []string{"d", "t"} {
		sectionCompared, allowed, err := compareAccessGroupedSection(ctx, beforePath, afterPath, section, allowlist, beforeScopes)
		if err != nil {
			return result, err
		}
		result.Compared += sectionCompared
		result.Allowed += allowed
	}
	return result, nil
}

func isAccessGroupedSection(section string) bool {
	return section == "d" || section == "t"
}

func accessSectionRank(section string) int {
	switch section {
	case "p":
		return 1
	case "i":
		return 2
	case "sc":
		return 3
	case "d":
		return 4
	case "t":
		return 5
	case "a":
		return 6
	case "g":
		return 7
	}
	return 0
}

func compareAccessSimpleSections(beforePath, afterPath string) (map[int]AccessScope, int64, error) {
	beforeFile, err := os.Open(beforePath)
	if err != nil {
		return nil, 0, err
	}
	defer beforeFile.Close()
	afterFile, err := os.Open(afterPath)
	if err != nil {
		return nil, 0, err
	}
	defer afterFile.Close()
	beforeScanner := newAccessCompareScanner(beforeFile)
	afterScanner := newAccessCompareScanner(afterFile)
	beforeScopes := map[int]AccessScope{}
	var compared int64
	for {
		beforeRecord, beforeRaw, err := nextAccessCompareRecord(beforeScanner)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		afterRecord, afterRaw, err := nextAccessCompareRecord(afterScanner)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		for beforeRecord != nil && isAccessGroupedSection(beforeRecord.Section) {
			beforeRecord, beforeRaw, err = nextAccessCompareRecord(beforeScanner)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, 0, err
			}
		}
		for afterRecord != nil && isAccessGroupedSection(afterRecord.Section) {
			afterRecord, afterRaw, err = nextAccessCompareRecord(afterScanner)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, 0, err
			}
		}
		if beforeRecord == nil && afterRecord == nil {
			break
		}
		if beforeRecord == nil {
			return nil, 0, accessCompareMismatch("after has an extra record", afterRecord)
		}
		if afterRecord == nil {
			return nil, 0, accessCompareMismatch("before has an extra record", beforeRecord)
		}
		if beforeRecord.Key != afterRecord.Key {
			if beforeRecord.Key < afterRecord.Key {
				return nil, 0, accessCompareMismatch("before has an extra record", beforeRecord)
			}
			return nil, 0, accessCompareMismatch("after has an extra record", afterRecord)
		}
		compared++
		if !bytes.Equal(beforeRaw, afterRaw) {
			return nil, 0, accessCompareMismatch("record differs", beforeRecord)
		}
		if beforeRecord.Section == "sc" {
			beforeScopes[beforeRecord.Scope] = AccessScope{ActorID: beforeRecord.Actor, PMActorID: beforeRecord.PM, AuthAdmin: beforeRecord.Admin}
		}
	}
	return beforeScopes, compared, nil
}

type accessGroup struct {
	scope  int
	count  int64
	digest string
	denied map[string]accessCompareRecord
}

type accessGroupReader struct {
	file       *os.File
	scanner    *bufio.Scanner
	section    string
	pending    *accessCompareRecord
	pendingRaw []byte
}

func newAccessGroupReader(path, section string) (*accessGroupReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &accessGroupReader{file: file, scanner: newAccessCompareScanner(file), section: section}, nil
}

func (r *accessGroupReader) Close() {
	if r.file != nil {
		_ = r.file.Close()
	}
}

func (r *accessGroupReader) nextRecord() (*accessCompareRecord, []byte, error) {
	if r.pending != nil {
		record, raw := r.pending, r.pendingRaw
		r.pending, r.pendingRaw = nil, nil
		return record, raw, nil
	}
	return nextAccessCompareRecord(r.scanner)
}

func (r *accessGroupReader) next() (*accessGroup, error) {
	var first *accessCompareRecord
	for {
		record, raw, err := r.nextRecord()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			return nil, err
		}
		if record == nil {
			return nil, nil
		}
		rank, target := accessSectionRank(record.Section), accessSectionRank(r.section)
		if rank < target {
			continue
		}
		if rank > target {
			r.pending, r.pendingRaw = record, raw
			return nil, nil
		}
		first = record
		break
	}
	if first.Kind != "" || first.Probe != "" {
		return nil, fmt.Errorf("section %s scope %d missing summary", r.section, first.Scope)
	}
	group := &accessGroup{scope: first.Scope, count: first.Count, digest: first.Digest, denied: map[string]accessCompareRecord{}}
	for {
		record, raw, err := r.nextRecord()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if record == nil {
			break
		}
		if record.Section != r.section || record.Scope != group.scope {
			r.pending, r.pendingRaw = record, raw
			break
		}
		group.denied[accessDeniedKey(*record)] = *record
	}
	return group, nil
}

func accessDeniedKey(record accessCompareRecord) string {
	if record.Section == "t" {
		return record.Probe
	}
	return record.Kind + "\x00" + record.ID
}

func compareAccessGroupedSection(ctx context.Context, beforePath, afterPath, section string, allowlist *cardHistoryAllowlist, beforeScopes map[int]AccessScope) (int64, int64, error) {
	beforeReader, err := newAccessGroupReader(beforePath, section)
	if err != nil {
		return 0, 0, err
	}
	defer beforeReader.Close()
	afterReader, err := newAccessGroupReader(afterPath, section)
	if err != nil {
		return 0, 0, err
	}
	defer afterReader.Close()
	var compared, allowed int64
	for {
		beforeGroup, err := beforeReader.next()
		if err != nil {
			return compared, allowed, err
		}
		afterGroup, err := afterReader.next()
		if err != nil {
			return compared, allowed, err
		}
		if beforeGroup == nil && afterGroup == nil {
			break
		}
		if beforeGroup == nil {
			return compared, allowed, accessCompareGroupMismatch("after has an extra scope", section, afterGroup.scope)
		}
		if afterGroup == nil {
			return compared, allowed, accessCompareGroupMismatch("before has an extra scope", section, beforeGroup.scope)
		}
		if beforeGroup.scope != afterGroup.scope {
			return compared, allowed, accessCompareGroupMismatch("scope differs", section, beforeGroup.scope)
		}
		compared++
		groupAllowed, err := compareAccessGroup(ctx, beforeGroup, afterGroup, section, allowlist, beforeScopes)
		if err != nil {
			return compared, allowed, err
		}
		allowed += groupAllowed
	}
	return compared, allowed, nil
}

func compareAccessGroup(ctx context.Context, before, after *accessGroup, section string, allowlist *cardHistoryAllowlist, beforeScopes map[int]AccessScope) (int64, error) {
	if before.count != after.count {
		return 0, accessCompareGroupMismatch("decision count differs", section, before.scope)
	}
	keys := make([]string, 0, len(before.denied)+len(after.denied))
	seen := map[string]struct{}{}
	for key := range before.denied {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	for key := range after.denied {
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var allowed int64
	var differences int
	for _, key := range keys {
		beforeRecord, beforeOK := before.denied[key]
		afterRecord, afterOK := after.denied[key]
		if beforeOK && afterOK {
			if beforeRecord.Cand == afterRecord.Cand && beforeRecord.Canon == afterRecord.Canon {
				continue
			}
			differences++
			if section == "d" && allowlist != nil {
				scope := beforeScopes[before.scope]
				ok, err := allowlist.permits(ctx, scope, beforeRecord.Kind, beforeRecord.ID, beforeRecord.Cand, beforeRecord.Canon, afterRecord.Cand, afterRecord.Canon)
				if err != nil {
					return allowed, err
				}
				if ok {
					allowed++
					continue
				}
			}
			return allowed, accessCompareMismatch("denied decision differs", &beforeRecord)
		}
		if beforeOK && !afterOK {
			differences++
			return allowed, accessCompareMismatch("denied decision disappeared", &beforeRecord)
		}
		if !beforeOK && afterOK {
			differences++
			if section == "d" && allowlist != nil {
				scope := beforeScopes[before.scope]
				ok, err := allowlist.permits(ctx, scope, afterRecord.Kind, afterRecord.ID, 1, 1, afterRecord.Cand, afterRecord.Canon)
				if err != nil {
					return allowed, err
				}
				if ok {
					allowed++
					continue
				}
			}
			return allowed, accessCompareMismatch("new denied decision", &afterRecord)
		}
	}
	if before.digest == after.digest {
		if differences != 0 {
			return allowed, accessCompareGroupMismatch("denied list differs with identical digest", section, before.scope)
		}
		return allowed, nil
	}
	if differences == 0 {
		return allowed, accessCompareGroupMismatch("digest differs with identical denied list", section, before.scope)
	}
	return allowed, nil
}

func accessCompareGroupMismatch(message, section string, scope int) error {
	key := fmt.Sprintf("%s|%08d", section, scope)
	digest := sha256.Sum256([]byte(key))
	return fmt.Errorf("access equivalence mismatch: %s (section=%s scope=%d key_sha256=%x)", message, section, scope, digest[:8])
}

func newAccessCompareScanner(file *os.File) *bufio.Scanner {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return scanner
}

func nextAccessCompareRecord(scanner *bufio.Scanner) (*accessCompareRecord, []byte, error) {
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		var record accessCompareRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, nil, fmt.Errorf("decode dump record: %w", err)
		}
		if record.Section == "g" || record.Compare == "x" {
			continue
		}
		record.Key = accessCompareRecordKey(record)
		return &record, raw, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return nil, nil, io.EOF
}

func accessCompareRecordKey(record accessCompareRecord) string {
	if record.Key != "" {
		return record.Key
	}
	switch record.Section {
	case "sc":
		return fmt.Sprintf("3|%08d", record.Scope)
	case "d":
		return fmt.Sprintf("4|%08d|%08d|%s\x00%s", record.Group, record.Scope, record.Kind, record.ID)
	case "t":
		return fmt.Sprintf("5|%08d|%08d|%s", record.Group, record.Scope, record.Probe)
	case "a":
		return fmt.Sprintf("6|%08d|%t", record.Scope, record.Admin)
	}
	return ""
}

func accessCompareMismatch(message string, record *accessCompareRecord) error {
	if record == nil {
		return errors.New("access equivalence mismatch: " + message)
	}
	keyDigest := sha256.Sum256([]byte(record.Key))
	return fmt.Errorf("access equivalence mismatch: %s (section=%s key_sha256=%x)", message, record.Section, keyDigest[:8])
}

// cardHistoryAllowlist is computed from the before database, never from a
// blanket "revision may hide" rule.
type cardHistoryAllowlist struct {
	db               *sql.DB
	tx               *sql.Tx
	revisions        map[string]cardHistoryRevision
	artifactRevision map[string]string
	revisionOwn      map[string]map[string]struct{}
	artifactOwn      map[string]map[string]struct{}
	cardUnion        map[string]map[string]struct{}
	denied           map[string]map[string]struct{}
}

type cardHistoryRevision struct {
	cardID   string
	artifact string
	older    bool
}

func newCardHistoryAllowlist(ctx context.Context, databasePath string) (*cardHistoryAllowlist, error) {
	db, err := openAccessEquivalenceReadOnly(ctx, databasePath)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		db.Close()
		return nil, err
	}
	allowlist := &cardHistoryAllowlist{
		db: db, tx: tx,
		revisions:        map[string]cardHistoryRevision{},
		artifactRevision: map[string]string{},
		revisionOwn:      map[string]map[string]struct{}{},
		artifactOwn:      map[string]map[string]struct{}{},
		cardUnion:        map[string]map[string]struct{}{},
		denied:           map[string]map[string]struct{}{},
	}
	if err = allowlist.load(ctx); err != nil {
		allowlist.Close()
		return nil, err
	}
	return allowlist, nil
}

func (a *cardHistoryAllowlist) Close() {
	if a == nil {
		return
	}
	if a.tx != nil {
		_ = a.tx.Rollback()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *cardHistoryAllowlist) load(ctx context.Context) error {
	rows, err := a.tx.QueryContext(ctx, `SELECT r.revision_id,r.card_id,r.artifact_id,COALESCE(c.head_revision_id,'')
 FROM main.card_revisions r LEFT JOIN main.cards c ON c.id=r.card_id ORDER BY r.revision_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var revisionID, cardID, artifactID, headRevisionID string
		if err = rows.Scan(&revisionID, &cardID, &artifactID, &headRevisionID); err != nil {
			rows.Close()
			return err
		}
		a.revisions[revisionID] = cardHistoryRevision{cardID: cardID, artifact: artifactID, older: headRevisionID == "" || revisionID != headRevisionID}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	cardCounts := map[string]int{}
	rows, err = a.tx.QueryContext(ctx, `SELECT artifact_id,count(*) FROM main.card_revisions GROUP BY artifact_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var artifactID string
		var count int
		if err = rows.Scan(&artifactID, &count); err != nil {
			rows.Close()
			return err
		}
		cardCounts[artifactID] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	documentCounts := map[string]int{}
	rows, err = a.tx.QueryContext(ctx, `SELECT artifact_id,count(*) FROM main.document_revisions GROUP BY artifact_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var artifactID string
		var count int
		if err = rows.Scan(&artifactID, &count); err != nil {
			rows.Close()
			return err
		}
		documentCounts[artifactID] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for revisionID, revision := range a.revisions {
		if cardCounts[revision.artifact] == 1 && documentCounts[revision.artifact] == 0 {
			a.artifactRevision[revision.artifact] = revisionID
		}
	}

	rows, err = a.tx.QueryContext(ctx, `SELECT source_kind,source_id,target_ref FROM main.resource_access_edges WHERE source_kind IN ('card_revision','artifact')`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sourceKind, sourceID, targetRef string
		if err = rows.Scan(&sourceKind, &sourceID, &targetRef); err != nil {
			rows.Close()
			return err
		}
		atom := resourceaccess.AtomKey(targetRef)
		switch sourceKind {
		case "card_revision":
			if _, ok := a.revisions[sourceID]; ok {
				addAccessTarget(a.revisionOwn, sourceID, atom)
			}
		case "artifact":
			addAccessTarget(a.artifactOwn, sourceID, atom)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for revisionID, revision := range a.revisions {
		union := a.cardUnion[revision.cardID]
		if union == nil {
			union = map[string]struct{}{}
			a.cardUnion[revision.cardID] = union
		}
		for target := range a.revisionOwn[revisionID] {
			union[target] = struct{}{}
		}
		for target := range a.artifactOwn[revision.artifact] {
			union[target] = struct{}{}
		}
	}
	return nil
}

func addAccessTarget(index map[string]map[string]struct{}, key, target string) {
	targets := index[key]
	if targets == nil {
		targets = map[string]struct{}{}
		index[key] = targets
	}
	targets[target] = struct{}{}
}

func (a *cardHistoryAllowlist) permits(ctx context.Context, scope AccessScope, kind, id string, beforeCand, beforeCanon, afterCand, afterCanon int) (bool, error) {
	if beforeCand != 1 || beforeCanon != 1 || afterCand != 0 || afterCanon != 0 {
		return false, nil
	}
	var cardID string
	var own, union map[string]struct{}
	switch kind {
	case "card_revision":
		revision, ok := a.revisions[id]
		if !ok || !revision.older {
			return false, nil
		}
		cardID = revision.cardID
		own = a.revisionOwn[id]
		union = a.cardUnion[cardID]
	case "artifact":
		revisionID, ok := a.artifactRevision[id]
		if !ok {
			return false, nil
		}
		revision := a.revisions[revisionID]
		if !revision.older {
			return false, nil
		}
		cardID = revision.cardID
		own = a.artifactOwn[id]
		union = a.cardUnion[cardID]
	default:
		return false, nil
	}
	if len(union) == 0 {
		return false, nil
	}
	denied, err := a.deniedAtoms(ctx, scope)
	if err != nil {
		return false, err
	}
	for target := range union {
		if _, inOwn := own[target]; inOwn {
			continue
		}
		if _, alreadyDenied := denied[target]; alreadyDenied {
			return true, nil
		}
	}
	return false, nil
}

func (a *cardHistoryAllowlist) deniedAtoms(ctx context.Context, scope AccessScope) (map[string]struct{}, error) {
	key := accessEquivalenceScopeKey(scope)
	if cached, ok := a.denied[key]; ok {
		return cached, nil
	}
	graph := accessCTEs(scope, `SELECT 1 FROM _anx_denied_atoms`)
	rows, err := a.tx.QueryContext(ctx, `WITH RECURSIVE `+graph+` SELECT ref FROM _anx_denied_atoms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	denied := map[string]struct{}{}
	for rows.Next() {
		var ref string
		if err = rows.Scan(&ref); err != nil {
			return nil, err
		}
		denied[resourceaccess.AtomKey(ref)] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	a.denied[key] = denied
	return denied, nil
}
