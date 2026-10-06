package storage_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"agent-nexus-core/internal/observation"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

type accessInventory struct {
	Tables                  map[string]map[string]string `json:"tables"`
	Writers                 map[string]string            `json:"writers"`
	ExternalKeyPublications map[string][]string          `json:"external_key_publications"`
}

func validStorageClassification(policy string) bool {
	for _, source := range resourceaccess.OwnershipSources {
		if policy == "ownership:"+source.Kind || policy == "parent:"+source.Kind {
			return true
		}
	}
	// Named, reviewed non-resource policies; arbitrary rationales are not exemptions.
	switch policy {
	case "configuration:operator-investigation",
		"configuration:series",
		"derived:document-and-comments",
		"identity",
		"internal:auth-provenance",
		"internal:authorization",
		"internal:canonical-accounting",
		"internal:delivery",
		"internal:rate-budget",
		"internal:reader-cursor",
		"internal:reader-snapshot",
		"internal:schema",
		"opaque:credential",
		"scope",
		"scope:board-cache",
		"scope:claim",
		"scope:edge-endpoints",
		"scope:inbox",
		"scope:pm-closure",
		"scope:presence",
		"scope:progress",
		"scope:replay",
		"scope:request-event",
		"scope:resource-alias",
		"scope:series-stream",
		"scope:thread-error",
		"scope:topic-cache":
		return true
	}
	return false
}

// Enumerate every column, not just names ending in _json: arbitrary BLOB bodies,
// TEXT labels, new sibling tables and newly added fields must all be reviewed.
func liveAccessColumns(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_schema WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' AND name NOT GLOB 'document_fts_*' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := map[string][]string{}
	for _, table := range tables {
		rows, err := db.Query(`SELECT name FROM pragma_table_xinfo(?) WHERE hidden IN (0,2,3) ORDER BY name`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatal(err)
			}
			out[table] = append(out[table], col)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return out
}

var storageMutation = regexp.MustCompile(`(?is)\b(INSERT\s+(?:OR\s+\w+\s+)?INTO|UPDATE|DELETE\s+FROM|CREATE\s+(?:VIRTUAL\s+)?TABLE|ALTER\s+TABLE)\b`)

// Trace callers of persistence helpers as well as direct SQL/blob writers.
// Matching call names conservatively across receiver types can overclassify a
// reader, but cannot let a new caller of Store.insert/cas evade review.
func accessWriterFingerprints(sources map[string]string) (map[string]string, error) {
	type declaration struct {
		key, name, source string
		calls             []string
		writes            bool
	}
	var declarations []declaration
	for path, source := range sources {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, source, 0)
		if err != nil {
			return nil, err
		}
		for _, node := range file.Decls {
			d := declaration{key: path + ":declaration"}
			if f, ok := node.(*ast.FuncDecl); ok {
				d.name = f.Name.Name
				d.key = path + ":" + d.name
				if f.Recv != nil {
					r := f.Recv.List[0].Type
					d.key = path + ":" + source[set.Position(r.Pos()).Offset:set.Position(r.End()).Offset] + ":" + d.name
				}
			} else if g, ok := node.(*ast.GenDecl); ok && len(g.Specs) > 0 {
				if typ, ok := g.Specs[0].(*ast.TypeSpec); ok {
					d.key = path + ":type:" + typ.Name.Name
				}
				if v, ok := g.Specs[0].(*ast.ValueSpec); ok && len(v.Names) > 0 {
					d.key = path + ":" + v.Names[0].Name
				}
			}
			d.source = source[set.Position(node.Pos()).Offset:set.Position(node.End()).Offset]
			ast.Inspect(node, func(n ast.Node) bool {
				// Generic JSON persistence can acquire a new stored field through a type
				// change alone, without editing its SQL or the caller of json.Marshal.
				if field, ok := n.(*ast.Field); ok && field.Tag != nil && strings.Contains(field.Tag.Value, "json:") {
					d.writes = true
				}
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					value, err := strconv.Unquote(lit.Value)
					if err == nil && storageMutation.MatchString(value) {
						d.writes = true
					}
				}
				if call, ok := n.(*ast.CallExpr); ok {
					name := ""
					switch f := call.Fun.(type) {
					case *ast.SelectorExpr:
						name = f.Sel.Name
					case *ast.Ident:
						name = f.Name
					}
					d.calls = append(d.calls, name)
					switch name {
					case "Exec", "ExecContext", "PrepareContext", "Write", "WriteStream", "Promote":
						d.writes = true
					}
				}
				return true
			})
			declarations = append(declarations, d)
		}
	}
	writerNames := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for i := range declarations {
			d := &declarations[i]
			for _, call := range d.calls {
				if writerNames[call] {
					d.writes = true
				}
			}
			if d.writes && d.name != "" && !writerNames[d.name] {
				writerNames[d.name] = true
				changed = true
			}
		}
	}
	out := map[string]string{}
	for _, d := range declarations {
		if d.writes {
			sum := sha256.Sum256([]byte(d.source))
			out[d.key] = hex.EncodeToString(sum[:])
		}
	}
	return out, nil
}

func liveAccessWriters(t *testing.T) map[string]string {
	t.Helper()
	sources := map[string]string{}
	for _, root := range []string{"..", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			sources[filepath.ToSlash(path)] = string(data)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := accessWriterFingerprints(sources)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestResourceAccessInventoryTracksHelperWriters(t *testing.T) {
	sources := map[string]string{"store.go": `package pm
 func insert(body any){ db.ExecContext(ctx, query, body) }
 func cas(body any){ insert(body) }
 func NewKind(){ store.cas(map[string]any{"evidence": "card:private"}) }
 `}
	writers, err := accessWriterFingerprints(sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(writers) != 3 || writers["store.go:NewKind"] == "" {
		t.Fatalf("transitive writers escaped: %v", writers)
	}
	sources["model.go"] = "package pm\ntype Stored struct { Evidence string `json:\"evidence\"` }"
	sources["store.go"] += `func AddedKind(){ store.insert(newBody) }`
	changed, err := accessWriterFingerprints(sources)
	if err != nil {
		t.Fatal(err)
	}
	if changed["model.go:type:Stored"] == "" {
		t.Fatal("new serialized field escaped inventory")
	}
	if changed["store.go:AddedKind"] == "" {
		t.Fatal("new helper caller escaped inventory")
	}
}

func TestResourceAccessStorageInventory(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err = pm.NewStore(ws.DB()); err != nil {
		t.Fatal(err)
	}
	if _, err = observation.NewInvestigationStore(ws.DB(), func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	columns := liveAccessColumns(t, ws.DB())
	writers := liveAccessWriters(t)
	path := "testdata/resource_access_storage.json"
	var inventory accessInventory
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, &inventory)
	}
	if os.Getenv("ANX_UPDATE_ACCESS_INVENTORY") == "1" {
		if inventory.Tables == nil {
			inventory.Tables = map[string]map[string]string{}
		}
		for table, cols := range columns {
			if inventory.Tables[table] == nil {
				inventory.Tables[table] = map[string]string{}
			}
			for _, col := range cols {
				if inventory.Tables[table][col] == "" {
					inventory.Tables[table][col] = "UNCLASSIFIED"
				}
			}
		}
		inventory.Writers = writers
		inventory.ExternalKeyPublications = resourceaccess.ExternalKeyPublications
		data, err = json.MarshalIndent(inventory, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	wantColumns := map[string][]string{}
	if !reflect.DeepEqual(inventory.ExternalKeyPublications, resourceaccess.ExternalKeyPublications) {
		t.Error("external publication/reference field inventory changed; classify and refresh deliberately")
	}
	for table, fields := range inventory.Tables {
		for col, policy := range fields {
			if !validStorageClassification(policy) {
				t.Errorf("classify %s.%s", table, col)
			}
			wantColumns[table] = append(wantColumns[table], col)
		}
		sort.Strings(wantColumns[table])
	}
	if !reflect.DeepEqual(columns, wantColumns) {
		t.Error("storage schema changed: audit and classify added/removed columns in resource_access_storage.json")
	}
	if !reflect.DeepEqual(writers, inventory.Writers) {
		for key, hash := range writers {
			if inventory.Writers[key] != hash {
				t.Errorf("storage writer needs review: %s", key)
			}
		}
		for key := range inventory.Writers {
			if _, ok := writers[key]; !ok {
				t.Errorf("stale writer: %s", key)
			}
		}
	}
	for _, source := range resourceaccess.OwnershipSources {
		for _, col := range source.Columns {
			if inventory.Tables[source.Table][col] != "ownership:"+source.Kind {
				t.Errorf("%s.%s must match executable ownership policy", source.Table, col)
			}
		}
	}
	for table, cols := range resourceaccess.FilterSources {
		for _, col := range cols {
			if inventory.Tables[table][col] != "scope" {
				t.Errorf("%s.%s must match executable scope policy", table, col)
			}
		}
	}
	// The reverse comparison prevents documenting an ownership policy that no
	// executable trigger implements (or an arbitrary kind/rationale exemption).
	for table, columns := range inventory.Tables {
		for col, policy := range columns {
			if policy == "scope" {
				found := false
				for _, field := range resourceaccess.FilterSources[table] {
					if field == col {
						found = true
					}
				}
				if !found {
					t.Errorf("documented scope has no executable filter: %s.%s", table, col)
				}
			}
			if strings.HasPrefix(policy, "ownership:") {
				found := false
				for _, source := range resourceaccess.OwnershipSources {
					if source.Table == table && policy == "ownership:"+source.Kind {
						for _, field := range source.Columns {
							if field == col {
								found = true
							}
						}
					}
				}
				if !found {
					t.Errorf("documented ownership has no executable source: %s.%s", table, col)
				}
			}
		}
	}
}

func TestResourceAccessInventoryRejectsUnclassifiedSchema(t *testing.T) {
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	before := liveAccessColumns(t, ws.DB())
	// Deliberately use neither a _json suffix nor a TEXT affinity. A future
	// observation/evidence sibling must still change the required inventory.
	if _, err = ws.DB().Exec(`ALTER TABLE work_observations ADD COLUMN opaque_evidence BLOB`); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, liveAccessColumns(t, ws.DB())) {
		t.Fatal("new ref-bearing storage escaped schema inventory")
	}

	before = liveAccessColumns(t, ws.DB())
	if _, err = ws.DB().Exec(`ALTER TABLE work_observations ADD COLUMN generated_evidence TEXT GENERATED ALWAYS AS (body_json) VIRTUAL`); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, liveAccessColumns(t, ws.DB())) {
		t.Fatal("generated column escaped inventory")
	}
	before = liveAccessColumns(t, ws.DB())
	if _, err = ws.DB().Exec(`CREATE VIEW evidence_view AS SELECT body_json AS evidence FROM work_observations`); err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(before, liveAccessColumns(t, ws.DB())) {
		t.Fatal("view escaped inventory")
	}
	if validStorageClassification("independent:some rationale") || validStorageClassification("UNCLASSIFIED") || validStorageClassification("internal:some rationale") {
		t.Fatal("arbitrary exemption accepted")
	}
}
