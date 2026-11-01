// Core primitives Store: constructor, shared sentinel errors, cross-resource
// list-filter types, and the package-wide helper functions.

package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/commentindex"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/secrets"
)

var ErrNotFound = errors.New("not found")

var ErrConflict = errors.New("conflict")

var ErrNotTrashed = errors.New("entity is not trashed")

var ErrNotArchived = errors.New("entity is not archived")

var ErrAlreadyTrashed = errors.New("entity is trashed")

var ErrArtifactInUse = errors.New("artifact is referenced by document revisions")

var ErrOwnedArtifactLifecycle = errors.New("artifact lifecycle is owned by its parent resource")

var ErrInvalidArtifactID = errors.New("invalid artifact id")

var ErrInvalidDocumentRequest = errors.New("invalid document request")

var ErrForbidden = errors.New("forbidden")

var ErrInvalidCursor = errors.New("invalid cursor")

type DocumentListFilter struct {
	ids          []string
	metadataOnly bool
	States       []string

	ThreadID  string
	Query     string
	Tag       string
	Knowledge bool
	Limit     *int
	Cursor    string
}

type TopicListFilter struct {
	States []string

	Query  string
	Limit  *int
	Cursor string
}

type Store struct {
	trackingLead         func() string
	askWebhookEncryption *secrets.Encryptor
	askWebhookAllowHosts []string
	askNextActorOrder    []string
	scopeInbox           *scopeInboxRuntime
	streamDB             *sql.DB // Used only by the read-only commit-version observer.
	db                   *accessDB
	blob                 blob.Backend
	blobRoot             string
	dbPath               string
	quota                WorkspaceQuota
	quotaMu              sync.Mutex
	contentUsageMu       sync.Mutex
	contentUsageBytes    int64
	contentUsageAt       time.Time
	canonicalUsageMu     sync.Mutex
	canonicalUsageCache  *canonicalUsageSnapshot
	askLifecycleMu       sync.Mutex
	askCloseCursor       string
	askCloseBackoff      map[string]askBackoffState
	// blobCollectHook is a test seam that runs after a purge commits and before
	// it collects unreferenced blobs.
	blobCollectHook func(hashes []string)
	// blobCollectLockedHook is a test seam that runs inside a blob collection
	// transaction, after its recount and before its delete.
	blobCollectLockedHook func(hash string)
	// blobPreloadedHook is a test seam that runs after a create preloads its
	// blobs and before its write transaction begins.
	blobPreloadedHook func(hashes []string)
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewStore(db *sql.DB, blobBackend blob.Backend, blobRoot string, options ...Option) *Store {
	var scopedDB *accessDB
	if db != nil {
		scopedDB = resourceaccess.NewDB(db)
		scopedDB.SetWriteHook(commentindex.AfterWrite)
	}
	store := &Store{db: scopedDB, streamDB: db, blob: blobBackend, blobRoot: blobRoot}
	for _, option := range options {
		option(store)
	}
	return store
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func anyStringSlice(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text := anyStringValue(value); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func splitTypedRef(ref string) (string, string, bool) {
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return "", "", false
	}
	prefix := strings.TrimSpace(ref[:idx])
	value := strings.TrimSpace(ref[idx+1:])
	if prefix == "" || value == "" {
		return "", "", false
	}
	return prefix, value, true
}

func normalizeStringSlice(raw any) ([]string, error) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, len(values))
		copy(out, values)
		return out, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("must contain only strings")
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be a list of strings")
	}
}

func uniqueNormalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func encodeCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	cursor := fmt.Sprintf("offset:%d", offset)
	return base64.StdEncoding.EncodeToString([]byte(cursor))
}

func decodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 2 || parts[0] != "offset" {
		return 0, fmt.Errorf("invalid cursor format")
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, fmt.Errorf("invalid cursor offset: %w", err)
	}
	if offset <= 0 {
		return 0, fmt.Errorf("invalid cursor offset: must be greater than zero")
	}
	return offset, nil
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func computeRevisionHash(contentHash, prevRevisionHash, documentID string, revisionNumber int, createdAt, createdBy string) string {
	h := sha256.New()
	fmt.Fprintf(h, "content_hash:%s\n", contentHash)
	fmt.Fprintf(h, "prev_revision_hash:%s\n", prevRevisionHash)
	fmt.Fprintf(h, "document_id:%s\n", documentID)
	fmt.Fprintf(h, "revision_number:%d\n", revisionNumber)
	fmt.Fprintf(h, "created_at:%s\n", createdAt)
	fmt.Fprintf(h, "created_by:%s\n", createdBy)
	return hex.EncodeToString(h.Sum(nil))
}

// mutationTxBegin is a seam only so tests can cancel between the moment a
// mutation's transaction takes the workspace writer and its first statement;
// production always begins directly.
var mutationTxBegin = func(ctx context.Context, db *accessDB) (*accessTx, error) {
	return db.BeginTx(ctx, nil)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
