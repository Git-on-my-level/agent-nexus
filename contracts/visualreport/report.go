// Package visualreport validates the data-only report documents rendered by the
// ANX web UI. Keep this validator in conformance with web-ui/src/lib/visualReports.js.
package visualreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	Kind      = "anx.visual-report"
	Version   = 1
	MaxBytes  = 128 * 1024
	MaxErrors = 20
)

var panelTypes = []string{
	"explanation", "evidence-table", "milestone-timeline", "dependency-diagram",
	"metric-chart", "artifact-preview", "chart", "metric-strip", "callout", "comparison",
	"live-initiatives", "live-asks", "live-work-mix", "live-activity",
}

// PanelTypes returns the panel types accepted by the shared visual-report contract.
func PanelTypes() []string { return append([]string(nil), panelTypes...) }

const jsSpacePattern = `[\x09-\x0d\x20\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	timestampPattern  = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$`)
	reportKindPattern = regexp.MustCompile(`"kind"` + jsSpacePattern + `*:` + jsSpacePattern + `*"anx\.visual-report[^"\r\n]*"`)
)

type Result struct {
	Recognized bool     `json:"recognized"`
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors"`
	Report     any      `json:"report,omitempty"`
}

// Match String.slice's UTF-16 bound when recognizing malformed/oversized input.
func jsPrefix(content []byte, units int) []byte {
	count := 0
	for i, r := range string(content) {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if count+width > units {
			return content[:i]
		}
		count += width
	}
	return content
}

// Validate parses raw document content using the same recognition behavior as
// parseVisualReport, then validates the report's bounded schema.
func Validate(content []byte) Result {
	looksLike := bytes.HasPrefix(bytes.TrimLeftFunc(content, jsWhitespace), []byte("{")) && reportKindPattern.Match(jsPrefix(content, MaxBytes))
	if len(content) > MaxBytes {
		if looksLike {
			return Result{Recognized: true, Errors: []string{"report: exceeds the 128 KiB size limit"}}
		}
		return Result{Errors: []string{}}
	}
	var report any
	if err := json.Unmarshal(content, &report); err != nil {
		if looksLike {
			return Result{Recognized: true, Errors: []string{"report: invalid JSON"}}
		}
		return Result{Errors: []string{}}
	}
	root, ok := report.(map[string]any)
	if !ok {
		return Result{Errors: []string{}}
	}
	kind, ok := root["kind"].(string)
	if !ok || !strings.HasPrefix(kind, Kind) {
		return Result{Errors: []string{}}
	}
	v := validator{}
	v.report(root)
	return Result{Recognized: true, Valid: len(v.errors) == 0, Errors: v.errors, Report: report}
}

type validator struct{ errors []string }

func (v *validator) add(path, message string) {
	if len(v.errors) < MaxErrors {
		v.errors = append(v.errors, path+": "+message)
	}
}

func object(value any, path string, required, optional []string, add func(string, string)) (map[string]any, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		add(path, "must be an object")
		return nil, false
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
	}
	for _, key := range optional {
		allowed[key] = true
	}
	missing := false
	for _, key := range required {
		if _, exists := m[key]; !exists {
			missing = true
			break
		}
	}
	unsupported := false
	for key := range m {
		if !allowed[key] {
			unsupported = true
			break
		}
	}
	if missing {
		add(path, "required fields are missing")
	}
	if unsupported {
		add(path, "contains unsupported fields")
	}
	return m, true
}

func strLen(s string) int { return len(utf16.Encode([]rune(s))) }
func (v *validator) text(value any, path string, maxLen int, allowEmpty bool) {
	s, ok := value.(string)
	if !ok || strLen(s) > maxLen || (!allowEmpty && strings.TrimFunc(s, jsWhitespace) == "") {
		kind := "a nonempty"
		if allowEmpty {
			kind = "a"
		}
		v.add(path, fmt.Sprintf("must be %s string of at most %d characters", kind, maxLen))
	}
}
func (v *validator) id(value any, path string) bool {
	s, ok := value.(string)
	if !ok || !identifierPattern.MatchString(s) {
		v.add(path, "must be an identifier of 1–80 letters, digits, dots, underscores, colons, or hyphens")
		return false
	}
	return true
}
func (v *validator) enum(value any, path string, allowed ...string) {
	s, _ := value.(string)
	for _, candidate := range allowed {
		if s == candidate {
			return
		}
	}
	v.add(path, "must be one of "+strings.Join(allowed, ", "))
}
func (v *validator) arr(value any, path string, max, min int) []any {
	items, ok := value.([]any)
	if !ok || len(items) < min || len(items) > max {
		v.add(path, fmt.Sprintf("must be an array with %d–%d items", min, max))
		return []any{}
	}
	return items
}
func (v *validator) timestamp(value any, path string, nullable bool) {
	if nullable && value == nil {
		return
	}
	s, ok := value.(string)
	if !ok || !validTimestamp(s) {
		extra := ""
		if nullable {
			extra = " or null"
		}
		v.add(path, "must be an ISO 8601 timestamp with a timezone"+extra)
	}
}
func validTimestamp(value string) bool {
	m := timestampPattern.FindStringSubmatch(value)
	if m == nil {
		return false
	}
	zone := m[7]
	if zone != "Z" && (num(zone[1:3]) > 23 || num(zone[4:]) > 59) {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}
func num(s string) int {
	var n int
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
func (v *validator) unique(items []any, path string) map[string]bool {
	ids := map[string]bool{}
	for i, item := range items {
		m, _ := item.(map[string]any)
		if !v.id(m["id"], fmt.Sprintf("%s[%d].id", path, i)) {
			continue
		}
		id := m["id"].(string)
		if ids[id] {
			v.add(fmt.Sprintf("%s[%d].id", path, i), "must be unique")
		}
		ids[id] = true
	}
	return ids
}

// jsWhitespace matches ECMAScript WhiteSpace + LineTerminator, including FEFF
// and excluding NEL (0085). Go's unicode.TrimSpace has a different set.
func jsWhitespace(r rune) bool {
	return r == 0xFEFF || r == 0x09 || r == 0x0A || r == 0x0B || r == 0x0C || r == 0x0D || r == 0x20 || r == 0xA0 || r == 0x1680 || (r >= 0x2000 && r <= 0x200A) || r == 0x2028 || r == 0x2029 || r == 0x202F || r == 0x205F || r == 0x3000
}
func safeURL(value any) bool {
	s, ok := value.(string)
	if !ok || strLen(s) > 2048 || (!strings.HasPrefix(strings.ToLower(s), "http://") && !strings.HasPrefix(strings.ToLower(s), "https://")) {
		return false
	}
	for _, r := range s {
		if jsWhitespace(r) || r < 0x20 || r == 0x7f || r == '\\' {
			return false
		}
	}
	// Check escapes in the entire URL, including query and fragment.
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) {
				return false
			}
			if _, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err != nil {
				return false
			}
			i += 2
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n > 65535 {
			return false
		}
	}
	host := u.Hostname()
	if strings.ContainsAny(host, "%<>^|{}\"`") {
		return false
	}
	// net/url before Go 1.27 accepts bracketed non-IP hosts. Validate the
	// brackets ourselves so all supported toolchains follow the same contract.
	if strings.HasPrefix(u.Host, "[") {
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is6() {
			return false
		}
		return true
	} else if strings.ContainsAny(host, "[]:") {
		return false
	}

	// Numeric host suffixes are IPv4 in WHATWG URL. Require canonical dotted
	// decimal so Go and the browser cannot interpret the same hostname differently.
	tail := host[strings.LastIndex(host, ".")+1:]
	if _, err := strconv.ParseUint(tail, 0, 64); err == nil || regexp.MustCompile(`^[0-9]+$`).MatchString(tail) {
		addr, err := netip.ParseAddr(host)
		if err != nil || !addr.Is4() {
			return false
		}
	}
	return true
}
func (v *validator) url(value any, path string) {
	if !safeURL(value) {
		v.add(path, "must be an absolute HTTP(S) URL without credentials")
	}
}
func numeric(value any) (float64, bool) {
	n, ok := value.(float64)
	return n, ok && !math.IsNaN(n) && !math.IsInf(n, 0)
}

func (v *validator) report(r map[string]any) {
	root, ok := object(r, "report", []string{"kind", "schema_version", "title", "summary", "generated_at", "projects", "sources", "panels"}, []string{"layout"}, v.add)
	if !ok {
		return
	}
	if root["kind"] != Kind {
		v.add("report.kind", "unsupported visual report kind")
	}
	if root["schema_version"] != float64(Version) {
		v.add("report.schema_version", "unsupported version; expected 1")
	}
	v.text(root["title"], "report.title", 200, false)
	v.text(root["summary"], "report.summary", 12000, false)
	v.timestamp(root["generated_at"], "report.generated_at", false)

	projects := v.arr(root["projects"], "projects", 16, 1)
	projectIDs := v.unique(projects, "projects")
	if projectIDs["all"] {
		v.add("projects", "the project identifier all is reserved for filtering")
	}
	for i, raw := range projects {
		p := fmt.Sprintf("projects[%d]", i)
		item, ok := object(raw, p, []string{"id", "title", "summary", "outcome"}, nil, v.add)
		if !ok {
			continue
		}
		v.text(item["title"], p+".title", 200, false)
		v.text(item["summary"], p+".summary", 12000, false)
		v.text(item["outcome"], p+".outcome", 2000, false)
	}
	sources := v.arr(root["sources"], "sources", 64, 0)
	sourceIDs := v.unique(sources, "sources")
	for i, raw := range sources {
		p := fmt.Sprintf("sources[%d]", i)
		item, ok := object(raw, p, []string{"id", "label", "url", "observed_at", "kind"}, nil, v.add)
		if !ok {
			continue
		}
		v.text(item["label"], p+".label", 200, false)
		v.text(item["kind"], p+".kind", 80, false)
		v.url(item["url"], p+".url")
		v.timestamp(item["observed_at"], p+".observed_at", true)
	}
	reference := func(raw any, path string, panelSources map[string]bool) {
		seen := map[string]bool{}
		for i, id := range v.arr(raw, path, 64, 0) {
			at := fmt.Sprintf("%s[%d]", path, i)
			if !v.id(id, at) {
				continue
			}
			key := id.(string)
			if !sourceIDs[key] {
				v.add(at, "references a missing source")
			}
			if panelSources != nil && !panelSources[key] {
				v.add(at, "must also appear in panel source_ids")
			}
			if seen[key] {
				v.add(at, "must be unique")
			}
			seen[key] = true
		}
	}
	panels := v.arr(root["panels"], "panels", 32, 1)
	panelIDs := v.unique(panels, "panels")
	if raw, exists := root["layout"]; exists {
		v.layout(raw, panelIDs)
	}
	for i, raw := range panels {
		p := fmt.Sprintf("panels[%d]", i)
		item, ok := object(raw, p, []string{"id", "project_id", "type", "title", "author", "provenance", "observed_at", "freshness", "source_ids", "data"}, []string{"appearance", "density"}, v.add)
		if !ok {
			continue
		}
		if id, ok := item["project_id"].(string); !ok || !projectIDs[id] {
			v.add(p+".project_id", "references a missing project")
		}
		if value, exists := item["appearance"]; exists {
			v.enum(value, p+".appearance", "plain", "soft", "outlined")
		}
		if value, exists := item["density"]; exists {
			v.enum(value, p+".density", "compact", "comfortable")
		}
		v.text(item["title"], p+".title", 200, false)
		v.text(item["author"], p+".author", 200, false)
		v.enum(item["type"], p+".type", panelTypes...)
		v.enum(item["provenance"], p+".provenance", "reported", "verified", "illustrative")
		v.enum(item["freshness"], p+".freshness", "current", "stale", "unknown", "unavailable")
		v.timestamp(item["observed_at"], p+".observed_at", true)
		if item["observed_at"] == nil && (item["freshness"] == "current" || item["freshness"] == "stale") {
			v.add(p+".freshness", "requires an observation timestamp")
		}
		reference(item["source_ids"], p+".source_ids", nil)
		panelSources := map[string]bool{}
		if ids, ok := item["source_ids"].([]any); ok {
			for _, id := range ids {
				if s, ok := id.(string); ok {
					panelSources[s] = true
				}
			}
		}
		if item["provenance"] == "verified" {
			if ids, ok := item["source_ids"].([]any); !ok || len(ids) == 0 {
				v.add(p+".source_ids", "verified panels require evidence sources")
			}
		}
		v.panelData(item, p, panelSources, reference)
	}
}

func (v *validator) panelData(panel map[string]any, path string, panelSources map[string]bool, reference func(any, string, map[string]bool)) {
	data, dp := panel["data"], path+".data"
	switch panel["type"] {
	case "live-initiatives", "live-asks", "live-work-mix", "live-activity":
		raw, _ := json.Marshal(data)
		if _, err := ParseQuery(panel["type"].(string), raw); err != nil {
			v.add(dp, err.Error())
		}
	case "chart":
		v.chart(data, dp)
	case "callout":
		m, ok := object(data, dp, []string{"tone", "text"}, []string{"label"}, v.add)
		if !ok {
			return
		}
		v.enum(m["tone"], dp+".tone", "info", "success", "warning", "critical")
		v.text(m["text"], dp+".text", 12000, false)
		if x, ok := m["label"]; ok {
			v.text(x, dp+".label", 200, false)
		}
	case "metric-strip":
		m, ok := object(data, dp, []string{"items"}, nil, v.add)
		if !ok {
			return
		}
		for i, raw := range v.arr(m["items"], dp+".items", 6, 1) {
			p := fmt.Sprintf("%s.items[%d]", dp, i)
			it, ok := object(raw, p, []string{"label", "value", "detail"}, []string{"trend", "trend_label", "tone"}, v.add)
			if !ok {
				continue
			}
			v.text(it["label"], p+".label", 200, false)
			v.text(it["value"], p+".value", 80, false)
			v.text(it["detail"], p+".detail", 2000, true)
			if x, ok := it["tone"]; ok {
				v.enum(x, p+".tone", "neutral", "positive", "negative")
			}
			if x, ok := it["trend"]; ok {
				v.arr(x, p+".trend", 50, 2)
				if vals, ok := x.([]any); ok {
					for _, value := range vals {
						n, good := numeric(value)
						if !good || math.Abs(n) > 1e12 {
							v.add(p+".trend", "must contain finite numbers within ±1e12")
							break
						}
					}
				}
				v.text(it["trend_label"], p+".trend_label", 200, false)
			} else if _, ok := it["trend_label"]; ok {
				v.add(p, "trend_label requires trend values")
			}
		}
	case "comparison":
		m, ok := object(data, dp, []string{"items"}, nil, v.add)
		if !ok {
			return
		}
		for i, raw := range v.arr(m["items"], dp+".items", 4, 2) {
			p := fmt.Sprintf("%s.items[%d]", dp, i)
			it, ok := object(raw, p, []string{"title", "summary", "verdict", "attributes"}, nil, v.add)
			if !ok {
				continue
			}
			v.text(it["title"], p+".title", 200, false)
			v.text(it["summary"], p+".summary", 2000, false)
			v.enum(it["verdict"], p+".verdict", "recommended", "neutral", "caution")
			for j, a := range v.arr(it["attributes"], p+".attributes", 10, 1) {
				q := fmt.Sprintf("%s.attributes[%d]", p, j)
				at, ok := object(a, q, []string{"label", "value"}, nil, v.add)
				if ok {
					v.text(at["label"], q+".label", 200, false)
					v.text(at["value"], q+".value", 2000, false)
				}
			}
		}
	case "explanation":
		m, ok := object(data, dp, []string{"text"}, nil, v.add)
		if ok {
			v.text(m["text"], dp+".text", 12000, false)
		}
	case "evidence-table":
		m, ok := object(data, dp, []string{"columns", "rows"}, nil, v.add)
		if !ok {
			return
		}
		cols := v.arr(m["columns"], dp+".columns", 12, 1)
		for i, x := range cols {
			v.text(x, fmt.Sprintf("%s.columns[%d]", dp, i), 200, false)
		}
		for i, raw := range v.arr(m["rows"], dp+".rows", 200, 0) {
			p := fmt.Sprintf("%s.rows[%d]", dp, i)
			row, ok := object(raw, p, []string{"cells", "source_ids"}, nil, v.add)
			if !ok {
				continue
			}
			cells := v.arr(row["cells"], p+".cells", 12, 0)
			if len(cells) != len(cols) {
				v.add(p+".cells", "must match the column count")
			}
			for j, x := range cells {
				v.text(x, fmt.Sprintf("%s.cells[%d]", p, j), 2000, true)
			}
			reference(row["source_ids"], p+".source_ids", panelSources)
		}
	case "milestone-timeline":
		m, ok := object(data, dp, []string{"items"}, nil, v.add)
		if !ok {
			return
		}
		for i, raw := range v.arr(m["items"], dp+".items", 100, 0) {
			p := fmt.Sprintf("%s.items[%d]", dp, i)
			it, ok := object(raw, p, []string{"label", "date", "status", "detail", "source_ids"}, nil, v.add)
			if !ok {
				continue
			}
			v.text(it["label"], p+".label", 200, false)
			v.timestamp(it["date"], p+".date", true)
			v.enum(it["status"], p+".status", "complete", "pending", "unknown")
			v.text(it["detail"], p+".detail", 2000, false)
			reference(it["source_ids"], p+".source_ids", panelSources)
		}
	case "dependency-diagram":
		v.dependency(data, dp)
	case "metric-chart":
		m, ok := object(data, dp, []string{"unit", "label", "points", "illustrative"}, nil, v.add)
		if !ok {
			return
		}
		v.text(m["unit"], dp+".unit", 80, false)
		v.text(m["label"], dp+".label", 200, false)
		illustrative, _ := m["illustrative"].(bool)
		if _, ok := m["illustrative"].(bool); !ok {
			v.add(dp+".illustrative", "must be a boolean")
		}
		if illustrative != (panel["provenance"] == "illustrative") {
			v.add(dp+".illustrative", "must agree with panel provenance")
		}
		for i, raw := range v.arr(m["points"], dp+".points", 200, 0) {
			p := fmt.Sprintf("%s.points[%d]", dp, i)
			it, ok := object(raw, p, []string{"label", "value"}, nil, v.add)
			if !ok {
				continue
			}
			v.text(it["label"], p+".label", 200, false)
			if it["value"] != nil {
				n, ok := numeric(it["value"])
				if !ok || n < 0 || n > 1e12 {
					v.add(p+".value", "must be null or a finite number between 0 and 1e12")
				}
			}
		}
	case "artifact-preview":
		m, ok := object(data, dp, []string{"label", "media_type", "excerpt"}, []string{"url"}, v.add)
		if !ok {
			return
		}
		v.text(m["label"], dp+".label", 200, false)
		v.enum(m["media_type"], dp+".media_type", "text/plain", "text/markdown", "application/json")
		v.text(m["excerpt"], dp+".excerpt", 12000, false)
		if x, ok := m["url"]; ok {
			v.url(x, dp+".url")
		}
	}
}

func (v *validator) dependency(raw any, path string) {
	m, ok := object(raw, path, []string{"nodes", "edges"}, nil, v.add)
	if !ok {
		return
	}
	nodes := v.arr(m["nodes"], path+".nodes", 40, 0)
	ids := v.unique(nodes, path+".nodes")
	for i, raw := range nodes {
		p := fmt.Sprintf("%s.nodes[%d]", path, i)
		n, ok := object(raw, p, []string{"id", "label", "status"}, nil, v.add)
		if ok {
			v.text(n["label"], p+".label", 200, false)
			v.enum(n["status"], p+".status", "complete", "pending", "unknown")
		}
	}
	seen := map[string]bool{}
	for i, raw := range v.arr(m["edges"], path+".edges", 80, 0) {
		p := fmt.Sprintf("%s.edges[%d]", path, i)
		e, ok := object(raw, p, []string{"from", "to", "label"}, nil, v.add)
		if !ok {
			continue
		}
		fromOK := v.id(e["from"], p+".from")
		toOK := v.id(e["to"], p+".to")
		from, _ := e["from"].(string)
		to, _ := e["to"].(string)
		if fromOK && toOK {
			if !ids[from] || !ids[to] {
				v.add(p, "references a missing node")
			}
			if from == to {
				v.add(p, "must connect distinct nodes")
			}
			key := from + "/" + to
			if seen[key] {
				v.add(p, "must be unique")
			}
			seen[key] = true
		}
		v.text(e["label"], p+".label", 200, true)
	}
}

// MarshalReport serializes arbitrary structured document content for renderers.
func MarshalReport(value any) ([]byte, error) { return json.Marshal(value) }
