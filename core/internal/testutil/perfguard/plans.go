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

// Findings uses both the actual plan and aliases in submitted SQL. SQLite
// reports aliases ("SCAN e"), not necessarily table names. Indexed full scans
// also need review; a SEARCH is the bounded lookup we want on large tables.
func Findings(q string, details []string, large map[string]bool, custom ...map[string]bool) []string {
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
	if len(custom) > 0 {
		functions = custom[0]
	}
	seen := map[string]bool{}
	add := func(s string) { seen[s] = true }
	largeRead := false
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
		// SQLite prefixes unaliased attached/schema-qualified tables in EXPLAIN.
		// Match both the bare name and schema-qualified name before an index suffix.
		if strings.HasPrefix(relation, "main.") || strings.HasPrefix(relation, "temp.") {
			_, relation, _ = strings.Cut(relation, ".")
		}
		// EXPLAIN prints a quoted alias verbatim, including embedded spaces.
		// Compare whole known names, rather than splitting the alias into words.
		for alias := range aliases {
			if relation == alias || strings.HasPrefix(relation, alias+" ") {
				largeRead = true
				if operation == "SCAN" {
					add(d)
				}
				break
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
