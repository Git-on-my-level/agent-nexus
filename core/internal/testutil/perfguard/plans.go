package perfguard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PlanException grants one SQL shape one specific plan finding, never a route
// or table-wide exemption. Reasons belong in a reviewed, checked-in manifest.
type PlanException struct {
	SQLHash  string   `json:"sql_sha256"`
	PlanHash string   `json:"plan_sha256"`
	Findings []string `json:"findings"`
	Reason   string   `json:"reason"`
	Issue    string   `json:"issue"`
	IssueURL string   `json:"issue_url"`
}

func SQLHash(q string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(q))))
}

// PlanSQLHash preserves the submitted SQL exactly except for the two epoch
// values emitted by the authorization compiler. Those values are snapshot data,
// not query structure, and change with fixture writes. Business literals and
// quoted/commented text remain part of the fingerprint.
func PlanSQLHash(q string) string {
	if !strings.HasPrefix(strings.TrimSpace(q), "WITH RECURSIVE _anx_fresh_denied(") {
		return SQLHash(q)
	}
	mask := unquotedSQL(q)
	re := regexp.MustCompile(`COALESCE\(\(SELECT version FROM main\.resource_access_epoch WHERE singleton=1\),-1\)(?:<>|=)([0-9]+)\b`)
	matches := re.FindAllSubmatchIndex(mask, -1)
	// The compiler emits exactly a cold <> gate and a cached = gate with the
	// same epoch. Extra occurrences (including business predicates) stay exact.
	if len(matches) != 2 || !strings.HasSuffix(string(mask[matches[0][0]:matches[0][2]]), "<>") || !strings.HasSuffix(string(mask[matches[1][0]:matches[1][2]]), "=") || q[matches[0][2]:matches[0][3]] != q[matches[1][2]:matches[1][3]] {
		return SQLHash(q)
	}
	var b strings.Builder
	start := 0
	for _, m := range matches {
		b.WriteString(q[start:m[2]])
		b.WriteByte('?')
		start = m[3]
	}
	b.WriteString(q[start:])
	return SQLHash(b.String())
}

func unquotedSQL(q string) []byte {
	mask := []byte(q)
	for i := 0; i < len(mask); {
		start := i
		switch {
		case q[i] == '\'' || q[i] == '"' || q[i] == '`' || q[i] == '[':
			end := q[i]
			if end == '[' {
				end = ']'
			}
			i++
			for i < len(q) {
				if q[i] == end {
					i++
					if i < len(q) && q[i] == end && end != ']' {
						i++
						continue
					}
					break
				}
				i++
			}
		case i+1 < len(q) && q[i:i+2] == "--":
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case i+1 < len(q) && q[i:i+2] == "/*":
			i += 2
			for i+1 < len(q) && q[i:i+2] != "*/" {
				i++
			}
			i = min(i+2, len(q))
		default:
			i++
			continue
		}
		for j := start; j < i; j++ {
			mask[j] = ' '
		}
	}
	return mask
}

// PlanMemoKey keeps every typed argument variant: this SQLite build enables
// STAT4, so distribution-sensitive equality/range predicates as well as LIKE
// can choose different plans. Byte strings and text must not collide.
func PlanMemoKey(s Statement) string { return PlanSQLHash(s.SQL) + ArgsHash(s.Args) }
func ArgsHash(args []any) string {
	h := sha256.New()
	number := func(n uint64) { var b [8]byte; binary.LittleEndian.PutUint64(b[:], n); h.Write(b[:]) }
	bytes := func(b []byte) { number(uint64(len(b))); h.Write(b) }
	number(uint64(len(args)))
	for _, v := range args {
		if named, ok := v.(sql.NamedArg); ok {
			h.Write([]byte{'N'})
			bytes([]byte(named.Name))
			v = named.Value
		} else {
			h.Write([]byte{'V'})
		}
		switch value := v.(type) {
		case nil:
			h.Write([]byte{'0'})
		case string:
			h.Write([]byte{'s'})
			bytes([]byte(value))
		case []byte:
			h.Write([]byte{'b'})
			if value == nil {
				h.Write([]byte{0})
			} else {
				h.Write([]byte{1})
			}
			bytes(value)
		case int64:
			h.Write([]byte{'i'})
			number(uint64(value))
		case float64:
			h.Write([]byte{'f'})
			number(math.Float64bits(value))
		case bool:
			h.Write([]byte{'t'})
			if value {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
		case time.Time:
			h.Write([]byte{'d'})
			bytes([]byte(value.String()))
		default:
			h.Write([]byte{'x'})
			bytes([]byte(fmt.Sprintf("%T:%#v", v, v)))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// PlanHash retains ordering and duplicate nodes. A second scan with the same
// alias must not inherit an exception intended for the first one.
func PlanHash(details []string) string {
	b, _ := json.Marshal(details)
	return SQLHash(string(b))
}

// sqlTokens retains quoted identifiers and skips literals/comments. It is a
// conservative lexer, not a query rewriter; SQLite still owns the actual plan.
func sqlTokens(q string) []string {
	var out []string
	for i := 0; i < len(q); {
		c := q[i]
		if c == '\'' {
			i++
			for i < len(q) {
				if q[i] == '\'' {
					i++
					if i < len(q) && q[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "--" {
			for i < len(q) && q[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(q) && q[i:i+2] == "/*" {
			i += 2
			for i+1 < len(q) && q[i:i+2] != "*/" {
				i++
			}
			i += 2
			continue
		}
		if c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			i++
			var b strings.Builder
			for i < len(q) {
				if q[i] == end {
					i++
					if i < len(q) && q[i] == end && end != ']' {
						b.WriteByte(end)
						i++
						continue
					}
					break
				}
				b.WriteByte(q[i])
				i++
			}
			out = append(out, "\x00"+strings.ToLower(b.String()))
			continue
		}
		if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128 {
			start := i
			i++
			for i < len(q) {
				c = q[i]
				if !(c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 128) {
					break
				}
				i++
			}
			out = append(out, strings.ToLower(q[start:i]))
			continue
		}
		if strings.ContainsRune("().,", rune(c)) {
			out = append(out, string(c))
		}
		i++
	}
	return out
}

func identifier(token string) string { return strings.TrimPrefix(token, "\x00") }

func customPredicate(tokens []string, custom map[string]bool) bool {
	depth := 0
	predicates := map[int]bool{}
	for i, token := range tokens {
		switch token {
		case "(":
			depth++
		case ")":
			delete(predicates, depth)
			depth--
		case "where":
			predicates[depth] = true
		case "order", "group", "limit", "union", "returning":
			delete(predicates, depth)
		}
		if i+1 < len(tokens) && tokens[i+1] == "(" && (custom[identifier(token)] || strings.HasPrefix(identifier(token), "anx_")) {
			for _, active := range predicates {
				if active {
					return true
				}
			}
		}
	}
	return false
}

// CustomFunctions uses SQLite's registry, so a newly registered scalar cannot
// escape the gate merely by choosing a name outside the anx_* convention.
func CustomFunctions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_function_list WHERE builtin=0")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

// AggregateFunctions discovers reducing and window functions from the actual
// SQLite connection, including extension/custom functions and future builtins.
// A function name may have both scalar and aggregate overloads (MIN/MAX); the
// classifier conservatively reviews either call rather than guessing its arity.
func AggregateFunctions(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM pragma_function_list WHERE type IN ('a','w')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[strings.ToLower(name)] = true
	}
	return out, rows.Err()
}

// The fallback keeps direct classifier callers conservative. The scale gate
// supplies AggregateFunctions so its coverage follows the executing driver.
var builtinAggregateFunctions = map[string]bool{
	"avg": true, "count": true, "group_concat": true, "string_agg": true,
	"sum": true, "total": true, "min": true, "max": true,
	"json_group_array": true, "json_group_object": true,
	"jsonb_group_array": true, "jsonb_group_object": true,
	"row_number": true, "rank": true, "dense_rank": true, "percent_rank": true,
	"cume_dist": true, "ntile": true, "lag": true, "lead": true,
	"first_value": true, "last_value": true, "nth_value": true,
}

// ViewDefinitions reads current view SQL from every attached SQLite schema.
// Keys are lowercase schema-qualified names; quoted names remain literal names.
// Definitions are analyzed, never executed or rewritten by the classifier.
func ViewDefinitions(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	var schemas []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			rows.Close()
			return nil, err
		}
		schemas = append(schemas, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, schema := range schemas {
		quoted := `"` + strings.ReplaceAll(schema, `"`, `""`) + `"`
		rows, err := db.QueryContext(ctx, "SELECT name,sql FROM "+quoted+".sqlite_schema WHERE type='view' AND sql IS NOT NULL")
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name, definition string
			if err := rows.Scan(&name, &definition); err != nil {
				rows.Close()
				return nil, err
			}
			out[strings.ToLower(schema+"."+name)] = definition
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AnalysisSQL returns the submitted SQL plus the current definitions of its
// referenced views, recursively. The appended definitions are analysis evidence,
// not executable rewritten SQL. Unreferenced definitions do not affect output.
func AnalysisSQL(q string, views map[string]string) string { return viewAnalysisSQL(q, views) }

// PlanSQLHashWithViews binds an exact-plan exception to referenced view bodies
// as well as the submitted statement. Changing a view's input bounds can keep
// EXPLAIN identical; its definition must still invalidate the old exception.
// Statements without referenced views retain their existing PlanSQLHash.
func PlanSQLHashWithViews(q string, views map[string]string) string {
	return PlanSQLHash(AnalysisSQL(q, views))
}

// viewAnalysisSQL includes only definitions referenced as FROM/JOIN relations
// (including comma joins), recursively. Query plans still describe the actual
// submitted statement. Cycle detection also bounds invalid recursive views.
func viewAnalysisSQL(q string, views map[string]string) string {
	if len(views) == 0 {
		return q
	}
	normalized := make(map[string]string, len(views))
	for key, definition := range views {
		normalized[strings.ToLower(key)] = definition
	}
	seen := map[string]bool{}
	var out strings.Builder
	out.WriteString(q)
	var visit func(string, string, bool)
	visit = func(sqlText, defaultSchema string, allowTemp bool) {
		tokens := sqlTokens(sqlText)
		reference := func(index int) {
			for index < len(tokens) && tokens[index] == "(" {
				index++
			}
			if index >= len(tokens) || tokens[index] == "select" || tokens[index] == "with" || tokens[index] == "values" {
				return
			}
			name := identifier(tokens[index])
			schema := ""
			if index+2 < len(tokens) && tokens[index+1] == "." {
				schema, name = name, identifier(tokens[index+2])
			}
			var keys []string
			if schema != "" {
				keys = []string{schema + "." + name}
			} else if allowTemp || defaultSchema == "temp" {
				// Root queries and temp-view bodies resolve unqualified names through
				// temp, main, then attached databases. A map cannot encode attachment
				// order, so include every matching attached view if temp/main miss.
				// This deliberately requires exact review for ambiguous attached names.
				for _, key := range []string{"temp." + name, "main." + name, name} {
					if _, exists := normalized[key]; exists {
						keys = []string{key}
						break
					}
				}
				if len(keys) == 0 {
					for key := range normalized {
						if !strings.HasPrefix(key, "main.") && !strings.HasPrefix(key, "temp.") && strings.HasSuffix(key, "."+name) {
							keys = append(keys, key)
						}
					}
					sort.Strings(keys)
				}
			} else {
				// Persistent views bind their unqualified relations to their schema.
				for _, key := range []string{defaultSchema + "." + name, name} {
					if _, exists := normalized[key]; exists {
						keys = []string{key}
						break
					}
				}
			}
			for _, key := range keys {
				definition, exists := normalized[key]
				if !exists {
					continue
				}
				if !seen[key] {
					seen[key] = true
					out.WriteString(";\n")
					out.WriteString(definition)
					owningSchema, _, qualified := strings.Cut(key, ".")
					if !qualified {
						owningSchema = defaultSchema
					}
					visit(definition, owningSchema, false)
				}
			}
		}
		depth := 0
		from := map[int]bool{}
		for i, token := range tokens {
			switch token {
			case "(":
				depth++
			case ")":
				delete(from, depth)
				depth--
			case "from":
				from[depth] = true
				reference(i + 1)
			case "join":
				reference(i + 1)
			case ",":
				if from[depth] {
					reference(i + 1)
				}
			case "where", "group", "order", "having", "limit", "union", "intersect", "except", "window", "returning":
				delete(from, depth)
			}
		}
	}
	visit(q, "main", true)
	return out.String()
}

// Findings uses both the actual plan and aliases in submitted SQL. SQLite
// reports aliases ("SCAN e"), not necessarily table names. Indexed full scans
// also need review; an indexed SEARCH can still aggregate unbounded input.
// Optional function maps are CustomFunctions followed by AggregateFunctions.
func Findings(q string, details []string, large map[string]bool, functionsByKind ...map[string]bool) []string {
	return FindingsWithViews(q, details, large, nil, functionsByKind...)
}

// FindingsWithViews also analyzes referenced current view definitions, so an
// aggregate hidden behind a view cannot escape the large-SEARCH review rule.
func FindingsWithViews(q string, details []string, large map[string]bool, views map[string]string, functionsByKind ...map[string]bool) []string {
	q = viewAnalysisSQL(q, views)
	aliases := map[string]string{}
	for table := range large {
		aliases[table] = table
	}
	tokens := sqlTokens(q)
	// Resolve aliases for quoted/schema-qualified and comma-joined relations.
	// Seeing a large relation anywhere is conservative for nested CTEs; a
	// false positive needs an exact reviewed exception, not silent exemption.
	for i, token := range tokens {
		if !large[identifier(token)] {
			continue
		}
		next := i + 1
		if next < len(tokens) && tokens[next] == "as" {
			next++
		}
		if next < len(tokens) && tokens[next] != "(" && tokens[next] != "." && tokens[next] != "," && tokens[next] != ")" {
			aliases[identifier(tokens[next])] = identifier(token)
		}
	}
	functions := map[string]bool{}
	if len(functionsByKind) > 0 {
		functions = functionsByKind[0]
	}
	seen := map[string]bool{}
	add := func(s string) { seen[s] = true }
	largeRead := false
	// LIMIT on an aggregate limits its result rows, not its input. Equality on
	// a nonunique index can also visit every row. Review every large SEARCH used
	// with a reducing/window aggregate: unique lookups and inner bounded pages
	// need exact reviewed exceptions. This stays conservative across nested
	// SELECTs. MIN/MAX(expression) and JSON aggregates can read every row too;
	// optimized MIN/MAX point lookups therefore also need exact plan review.
	aggregateRead := false
	aggregateFunctions := builtinAggregateFunctions
	if len(functionsByKind) > 1 {
		aggregateFunctions = functionsByKind[1]
	}
	for i, token := range tokens {
		if i+1 < len(tokens) && tokens[i+1] == "(" && aggregateFunctions[identifier(token)] {
			aggregateRead = true
			break
		}
	}
	for _, d := range details {
		upper := strings.ToUpper(d)
		if strings.Contains(upper, "AUTOMATIC") {
			add(d)
		}
		operation, relation, ok := strings.Cut(d, " ")
		if !ok || (operation != "SCAN" && operation != "SEARCH") {
			continue
		}
		relation = strings.ToLower(relation)
		// A known quoted alias can contain dots and spaces: match it before
		// treating a prefix as a database name. Then accept any attached schema,
		// not merely main/temp. Ignore index names and bounds after the relation.
		matched := false
		for alias := range aliases {
			if relation == alias || strings.HasPrefix(relation, alias+" ") {
				matched = true
				break
			}
		}
		if !matched {
			relationName := relation
			for _, separator := range []string{" using ", " ("} {
				if end := strings.Index(relationName, separator); end >= 0 {
					relationName = relationName[:end]
				}
			}
			for alias := range aliases {
				if strings.HasSuffix(relationName, "."+alias) {
					matched = true
					break
				}
			}
		}
		if matched {
			largeRead = true
			if operation == "SCAN" || aggregateRead {
				add(d)
			}
		}
	}
	// Deliberately conservative: nested WHERE predicates can be correlated with
	// a large outer relation even when the function's own alias is small.
	if largeRead && customPredicate(tokens, functions) {
		add("custom SQL function in WHERE over a large relation")
	}
	out := make([]string, 0, len(seen))
	for _, d := range details {
		if seen[d] {
			out = append(out, d)
			delete(seen, d)
		}
	}
	if seen["custom SQL function in WHERE over a large relation"] {
		out = append(out, "custom SQL function in WHERE over a large relation")
	}
	return out
}

func Explain(ctx context.Context, db *sql.DB, s Statement) ([]string, error) {
	rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+s.SQL, s.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, unused int
		var d string
		if err := rows.Scan(&id, &parent, &unused, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// LargeTables discovers size instead of assuming new tables are small.
func LargeTables(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, name := range names {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM "`+strings.ReplaceAll(name, `"`, `""`)+`" LIMIT 1024)`).Scan(&count); err != nil {
			return nil, err
		}
		if count >= 1024 {
			out[strings.ToLower(name)] = true
		}
	}
	return out, nil
}
