package perfguard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
)

// PlanException grants one SQL shape one specific plan finding, never a route
// or table-wide exemption. Reasons belong in a reviewed, checked-in manifest.
type PlanException struct {
	SQLHash string `json:"sql_sha256"`
	Finding string `json:"finding"`
	Reason  string `json:"reason"`
}

func SQLHash(q string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(q))))
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
