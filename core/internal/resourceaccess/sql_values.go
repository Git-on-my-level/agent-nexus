package resourceaccess

import (
	"strings"
	"unicode"
)

// SQLValues keeps trusted statement syntax alongside mutation parameters, so an
// encoded JSON column can be distinguished from a scalar ID whose bytes are [].
// Public request decoding never constructs this type.
type SQLValues struct {
	Query string
	Args  []any
}

type sqlValueToken struct {
	text string
	arg  int
}

// AnonymousSQLParameters reports whether prepending an anonymous binding can
// shift every existing parameter uniformly. Explicit numbers/names retain their
// original binding semantics by using the uncached policy rewrite instead.
func AnonymousSQLParameters(q string) bool {
	for _, t := range sqlValueTokens(q) {
		if t.text == "?" && t.arg < 0 || t.text == ":" || t.text == "@" || t.text == "$" {
			return false
		}
	}
	return true
}

func sqlValueTokens(q string) []sqlValueToken {
	var tokens []sqlValueToken
	arg := 0
	for i := 0; i < len(q); {
		c := q[i]
		if c == '\'' || c == '"' || c == '`' || c == '[' {
			end := c
			if c == '[' {
				end = ']'
			}
			start := i
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
			// Quoted identifiers are accepted; string literals never affect binding.
			text := "literal"
			if c != '\'' && i > start+1 {
				text = strings.ToLower(q[start+1 : i-1])
			}
			tokens = append(tokens, sqlValueToken{text, -1})
			continue
		}
		if c == '-' && i+1 < len(q) && q[i+1] == '-' {
			for i < len(q) && q[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(q) && q[i+1] == '*' {
			i += 2
			for i+1 < len(q) && q[i:i+2] != "*/" {
				i++
			}
			i += 2
			continue
		}
		if c == '?' {
			i++
			// Numbered parameters are deliberately unclassified: unknown syntax
			// must remain scalar/conservative, never suppress an identity check.
			numbered := false
			for i < len(q) && q[i] >= '0' && q[i] <= '9' {
				numbered = true
				i++
			}
			a := arg
			if numbered {
				a = -1
			}
			tokens = append(tokens, sqlValueToken{"?", a})
			arg++
			continue
		}
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' {
			start := i
			i++
			for i < len(q) {
				c = q[i]
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
					break
				}
				i++
			}
			tokens = append(tokens, sqlValueToken{strings.ToLower(q[start:i]), -1})
			continue
		}
		tokens = append(tokens, sqlValueToken{string(c), -1})
		i++
	}
	return tokens
}

// StructuredSQLArguments recognizes only explicit INSERT VALUES columns and
// UPDATE SET targets. Other forms stay scalar, which can over-restrict but can
// never bypass authorization. The lexer ignores quoted strings and comments.
func StructuredSQLArguments(query string) map[int]bool {
	tokens := sqlValueTokens(query)
	out := map[int]bool{}
	// Explicit parameter numbers/names can alias earlier positions. Do not infer
	// a positional mapping for any statement containing those forms.
	for _, t := range tokens {
		if t.text == "?" && t.arg < 0 || t.text == ":" || t.text == "@" || t.text == "$" {
			return out
		}
	}
	if len(tokens) == 0 || (tokens[0].text != "insert" && tokens[0].text != "update") {
		return out
	}
	table := ""
	if tokens[0].text == "update" {
		at := 1
		if len(tokens) > 3 && tokens[1].text == "or" {
			at = 3
		}
		if at < len(tokens) {
			table = tokens[at].text
		}
	} else {
		for i, t := range tokens {
			if t.text == "into" && i+1 < len(tokens) {
				table = tokens[i+1].text
				break
			}
		}
	}
	structured := func(col string) bool {
		return strings.HasSuffix(col, "_json") || table == "pm_records" && col == "body" || strings.HasPrefix(table, "series_") && col == "labels"
	}
	mark := func(expr []sqlValueToken, col string) {
		if !structured(col) {
			return
		}
		// Only a complete encoded JSON value may be decoded. json_array(?),
		// json_object(...,?) and json_set(...,?) consume scalar arguments, whose
		// []/{} spelling must survive the authorization check.
		if len(expr) == 1 && expr[0].arg >= 0 {
			out[expr[0].arg] = true
		}
		if len(expr) == 4 && expr[0].text == "json" && expr[1].text == "(" && expr[2].arg >= 0 && expr[3].text == ")" {
			out[expr[2].arg] = true
		}
	}
	for at, t := range tokens {
		if t.text == "values" {
			into := -1
			for i := 0; i < at; i++ {
				if tokens[i].text == "into" {
					into = i
					break
				}
			}
			if into < 0 {
				continue
			}
			open := into + 2
			if open+1 < len(tokens) && tokens[open].text == "." {
				open += 2
			}
			if open >= at || tokens[open].text != "(" {
				continue
			}
			var cols []string
			valid := true
			for i := open + 1; i < at && tokens[i].text != ")"; i++ {
				if tokens[i].text == "," {
					continue
				}
				if i > open+1 && tokens[i-1].text != "," {
					valid = false
				}
				cols = append(cols, tokens[i].text)
			}
			if !valid {
				continue
			}
			for i := at + 1; i < len(tokens) && tokens[i].text == "("; {
				i++
				depth, col, start := 0, 0, i
				for i < len(tokens) {
					token := tokens[i].text
					if (token == "," || token == ")") && depth == 0 {
						if col < len(cols) {
							mark(tokens[start:i], cols[col])
						}
						col++
						start = i + 1
						if token == ")" {
							i++
							break
						}
					}
					if token == "(" {
						depth++
					}
					if token == ")" {
						depth--
					}
					i++
				}
				if i < len(tokens) && tokens[i].text == "," {
					i++
				} else {
					break
				}
			}
			break
		}
		if t.text == "set" && len(tokens) > 0 && tokens[0].text == "update" {
			start, depth := at+1, 0
			for i := start; i <= len(tokens); i++ {
				end := i == len(tokens)
				token := ""
				if !end {
					token = tokens[i].text
				}
				boundary := end || depth == 0 && (token == "," || token == "where" || token == "returning")
				if boundary {
					expr := tokens[start:i]
					if len(expr) > 2 && expr[1].text == "=" {
						mark(expr[2:], expr[0].text)
					}
					if end || token != "," {
						break
					}
					start = i + 1
				}
				if token == "(" {
					depth++
				}
				if token == ")" {
					depth--
				}
			}
			break
		}
	}
	return out
}
