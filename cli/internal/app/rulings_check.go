package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const bareRulingID = `R-[0-9]{8}-[0-9]{2}|R[0-9]+(?:\.[0-9]+)?`
const rulingStatusAmbiguous = "ambiguous ruling"

var rulingTag = regexp.MustCompile(`ruling["']?[ \t]*[:=][ \t]*["']?`)
var legacyRuling = regexp.MustCompile(`^R[0-9]+$`)
var lineBoundaries = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\v", "\n", "\f", "\n", "\x1c", "\n", "\x1d", "\n", "\x1e", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")

type rulingReference struct {
	token, name string
	tagged      bool
}

type rulingReferenceScanner struct {
	names *regexp.Regexp
	bare  *regexp.Regexp
}

func newRulingReferenceScanner(names map[string]map[string]any) rulingReferenceScanner {
	aliases := []string{}
	for name, row := range names {
		if row["kind"] == "operating" {
			aliases = append(aliases, name)
		}
	}
	sort.Slice(aliases, func(i, j int) bool {
		if len(aliases[i]) == len(aliases[j]) {
			return aliases[i] < aliases[j]
		}
		return len(aliases[i]) > len(aliases[j])
	})
	for i := range aliases {
		aliases[i] = regexp.QuoteMeta(aliases[i])
	}
	// Special legacy spellings come from the index. Keep them ahead of the
	// generic R-number grammar so a longer alias wins over its prefix.
	bare := make([]string, 0, len(aliases)+1)
	for _, alias := range aliases {
		if !legacyRuling.MatchString(alias) && !rulingDateID.MatchString(alias) && strings.ContainsAny(alias, "0123456789") {
			bare = append(bare, alias)
		}
	}
	bare = append(bare, bareRulingID)
	aliases = append(aliases, rulingDateID.String()[1:len(rulingDateID.String())-1])
	return rulingReferenceScanner{names: regexp.MustCompile("^(?:" + strings.Join(aliases, "|") + ")"), bare: regexp.MustCompile(strings.Join(bare, "|"))}
}

func (s rulingReferenceScanner) references(line string) []rulingReference {
	byStart := map[int]rulingReference{}
	var taggedSpans [][2]int
	for _, tag := range rulingTag.FindAllStringIndex(line, -1) {
		start := tag[1]
		token := s.names.FindString(line[start:])
		if token == "" {
			continue
		}
		end := start + len(token)
		if next, _ := utf8.DecodeRuneInString(line[end:]); rulingWord(next) {
			continue
		}
		// Preserve the clause token where bare scanning would report it.
		if end < len(line) && line[end] == '.' && legacyRuling.MatchString(token) {
			if clause := s.bare.FindString(line[start:]); strings.HasPrefix(clause, token+".") {
				token, end = clause, start+len(clause)
			}
		}
		name, _, _ := strings.Cut(token, ".")
		if !legacyRuling.MatchString(name) {
			name = token
		}
		taggedSpans = append(taggedSpans, [2]int{start, end})
		byStart[start] = rulingReference{token: token, name: name, tagged: true}
	}
	for _, span := range s.bare.FindAllStringIndex(line, -1) {
		start, end := span[0], span[1]
		covered := false
		for _, tag := range taggedSpans {
			if start < tag[1] && end > tag[0] {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		token := line[start:end]
		name, _, _ := strings.Cut(token, ".")
		prev, _ := utf8.DecodeLastRuneInString(line[:start])
		next, _ := utf8.DecodeRuneInString(line[end:])
		if legacyRuling.MatchString(name) {
			suffix, priority := strings.CutPrefix(line[end:], " P")
			digit, _ := utf8.DecodeRuneInString(suffix)
			if rulingWord(prev) || prev == '-' || prev == '/' || prev == '$' || rulingWord(next) || (priority && unicode.IsDigit(digit)) {
				continue
			}
		} else {
			name = token
			if rulingDateID.MatchString(name) && (rulingWord(prev) || prev == '-' || unicode.IsDigit(next)) {
				continue
			}
		}
		byStart[start] = rulingReference{token: token, name: name}
	}
	starts := make([]int, 0, len(byStart))
	for start := range byStart {
		starts = append(starts, start)
	}
	sort.Ints(starts)
	out := make([]rulingReference, 0, len(starts))
	for _, start := range starts {
		out = append(out, byStart[start])
	}
	return out
}

func rulingWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

type rulingFinding struct {
	Path         string `json:"path"`
	Line         int    `json:"line"`
	Token        string `json:"token"`
	ID           string `json:"id"`
	Status       string `json:"status"`
	SupersededBy string `json:"superseded_by"`
}

func (f rulingFinding) String() string {
	return fmt.Sprintf("%s:%d: %s -> %s (%s) superseded_by %s", f.Path, f.Line, f.Token, f.ID, f.Status, firstNonEmpty(f.SupersededBy, "none"))
}

func (s rulingReferenceScanner) scan(file, text string, names map[string]map[string]any, strict bool) []rulingFinding {
	out := []rulingFinding{}
	for n, line := range strings.Split(lineBoundaries.Replace(text), "\n") {
		refs := s.references(line)
		cited := make(map[string]bool, len(refs))
		for _, ref := range refs {
			if id := anyString(names[ref.name]["ruling_id"]); id != "" {
				cited[id] = true
			}
		}
		for _, ref := range refs {
			row := names[ref.name]
			id := anyString(row["ruling_id"])
			status := anyString(row["status"])
			next := anyString(row["superseded_by"])
			switch {
			case status == rulingStatusAmbiguous:
				id = "?"
			case row == nil && rulingDateID.MatchString(ref.name):
				id, status = "?", "unknown ruling ID"
			case id == "":
				continue
			case status == "revoked" && row["status_hint"] == "spent" && row["decided_at"] != nil:
				status = "spent"
			case status == "proposed" && row["status_hint"] == "historical":
				continue
			case status == "proposed" || status == "superseded" || status == "revoked" || status == "spent":
			case status == "active" && strict && !ref.tagged && legacyRuling.MatchString(ref.name):
				status = "legacy citation"
			default:
				continue
			}
			if next == "" || !cited[next] {
				out = append(out, rulingFinding{Path: file, Line: n + 1, Token: ref.token, ID: id, Status: status, SupersededBy: next})
			}
		}
	}
	return out
}

type rulingSurfaces struct {
	Include     []string `json:"include"`
	Exclude     []string `json:"exclude"`
	LiveInclude []string `json:"live_include"`
}

type rulingAllowance struct {
	Path   string `json:"path"`
	Match  string `json:"match"`
	Reason string `json:"reason"`
	Issue  string `json:"issue"`
}

type rulingFile struct{ name, path string }

func rulingGlob(pattern, name string) (bool, error) {
	// Python fnmatch, used by Phase 0, lets * and ? match path separators.
	return path.Match(strings.ReplaceAll(strings.ReplaceAll(pattern, "[!", "[^"), "/", "\x00"), strings.ReplaceAll(name, "/", "\x00"))
}

func anyRulingGlob(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if ok, _ := rulingGlob(pattern, name); ok {
			return true
		}
	}
	return false
}

func readRulingJSON(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func rulingScanRoot(ctx context.Context, cwd string) string {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	if root, err := cmd.Output(); err == nil {
		return strings.TrimSpace(string(root))
	}
	return cwd
}

func rulingCheckFiles(ctx context.Context, cwd, root string, args rulingsArgs) ([]rulingFile, []rulingAllowance, error) {
	files := []rulingFile{}
	allow := []rulingAllowance{}
	var surfaces rulingSurfaces
	surfacesPath := args.surfaces
	if surfacesPath == "" && len(args.paths) == 0 {
		surfacesPath = filepath.Join(root, "rulings", "surfaces.json")
	} else if surfacesPath != "" && !filepath.IsAbs(surfacesPath) {
		surfacesPath = filepath.Join(cwd, surfacesPath)
	}
	if surfacesPath != "" {
		if err := readRulingJSON(surfacesPath, &surfaces); err != nil {
			return nil, nil, err
		}
		if len(surfaces.Include) == 0 {
			return nil, nil, fmt.Errorf("%s must specify a non-empty include array", surfacesPath)
		}
		for _, patterns := range [][]string{surfaces.Include, surfaces.Exclude, surfaces.LiveInclude} {
			for _, pattern := range patterns {
				if _, err := rulingGlob(pattern, ""); err != nil {
					return nil, nil, fmt.Errorf("invalid surface glob %q", pattern)
				}
			}
		}
	}
	allowPath := filepath.Join(root, "rulings", "allowlist.json")
	if err := readRulingJSON(allowPath, &allow); err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	for _, a := range allow {
		if _, err := rulingGlob(a.Path, ""); err != nil || a.Path == "" || a.Match == "" || a.Reason == "" || a.Issue == "" {
			return nil, nil, fmt.Errorf("invalid allowlist entry in %s", allowPath)
		}
	}
	if len(args.paths) > 0 {
		for _, raw := range args.paths {
			target := raw
			if !filepath.IsAbs(target) {
				target = filepath.Join(cwd, target)
			}
			info, err := os.Stat(target)
			if err != nil {
				return nil, nil, err
			}
			if !info.IsDir() {
				if !info.Mode().IsRegular() {
					return nil, nil, fmt.Errorf("%s is not a regular file or directory", raw)
				}
				files = append(files, rulingFile{filepath.Clean(raw), target})
				continue
			}
			err = filepath.WalkDir(target, func(file string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && d.Name() == ".git" {
					return filepath.SkipDir
				}
				if !d.IsDir() {
					stat, err := os.Stat(file)
					if err == nil && stat.Mode().IsRegular() {
						rel, err := filepath.Rel(target, file)
						if err != nil {
							return err
						}
						files = append(files, rulingFile{filepath.Join(raw, rel), file})
					}
				}
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
		}
		return files, allow, nil
	}
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z")
	cmd.Dir = root
	tracked, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("cannot list tracked enforcement files: %w", err)
	}
	for _, name := range strings.Split(string(tracked), "\x00") {
		if name == "" || (!anyRulingGlob(surfaces.LiveInclude, name) && (!anyRulingGlob(surfaces.Include, name) || anyRulingGlob(surfaces.Exclude, name))) {
			continue
		}
		file := filepath.Join(root, filepath.FromSlash(name))
		if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
			files = append(files, rulingFile{name, file})
		}
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("enforcement globs in %s match no tracked regular files", surfacesPath)
	}
	return files, allow, nil
}

func (a *App) checkRulings(ctx context.Context, cfg config.Resolved, args rulingsArgs, snapshot rulingCollection) (*commandResult, error) {
	cwd, err := a.workspaceCWD()
	if err != nil {
		return nil, errnorm.Usage("rulings_path_unavailable", err.Error())
	}
	files, allow, err := rulingCheckFiles(ctx, cwd, rulingScanRoot(ctx, cwd), args)
	if err != nil {
		return nil, errnorm.Usage("invalid_rulings_surfaces", err.Error())
	}
	scanner := newRulingReferenceScanner(snapshot.names)
	output := []string{}
	findings := []rulingFinding{}
	unused := []rulingAllowance{}
	used := make(map[int]bool, len(allow))
	for _, file := range files {
		raw, err := os.ReadFile(file.path)
		if err != nil {
			if len(args.paths) > 0 || !os.IsNotExist(err) {
				return nil, errnorm.Usage("rulings_input_unreadable", err.Error())
			}
			continue
		}
		if bytes.IndexByte(raw[:min(len(raw), 4096)], 0) >= 0 {
			continue
		}
		text := strings.ToValidUTF8(string(raw), "\ufffd")
		lines := strings.Split(lineBoundaries.Replace(text), "\n")
		for _, f := range scanner.scan(file.name, text, snapshot.names, args.strictLegacy) {
			hit := false
			for i, entry := range allow {
				if yes, _ := rulingGlob(entry.Path, file.name); yes && strings.Contains(lines[f.Line-1], entry.Match) {
					used[i], hit = true, true
					break
				}
			}
			if !hit {
				findings = append(findings, f)
				output = append(output, f.String())
			}
		}
	}
	if len(args.paths) == 0 {
		for i, entry := range allow {
			if !used[i] {
				unused = append(unused, entry)
				output = append(output, fmt.Sprintf("%s: allowlist entry no longer matches a finding: '%s' (%s)", entry.Path, entry.Match, entry.Issue))
			}
		}
	}
	count := len(findings) + len(unused)
	summary := fmt.Sprintf("anx rulings check: %d finding(s) in %d file(s)", count, len(files))
	data := map[string]any{"findings": findings, "unused_allowlist": unused, "finding_count": count, "file_count": len(files)}
	if cfg.JSON && count > 0 {
		err := errnorm.Local("rulings_check_findings", summary)
		if len(snapshot.warnings) > 0 {
			data["projection_warnings"] = snapshot.warnings
		}
		err.Details = data
		return nil, err
	}
	output = append(output, summary)
	result := a.rulingsOutput(cfg, data, output, snapshot)
	if count > 0 {
		result.ExitCode = 1
	}
	return result, nil
}
