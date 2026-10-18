package bridge

import (
	"debug/buildinfo"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Process identity is kernel-observed argv, executable, UID and start time.
// Never infer ownership from a label prefix, process name or shell source text.
type Process struct {
	PID        int      `json:"pid"`
	PPID       int      `json:"ppid"`
	UID        int      `json:"uid"`
	Started    string   `json:"started"`
	Executable string   `json:"executable"`
	Argv       []string `json:"argv"`
	CWD        string   `json:"cwd"`
}
type LegacyUnit struct {
	Loaded      bool              `json:"loaded"`
	Enabled     bool              `json:"enabled"`
	Mode        uint32            `json:"mode"`
	Unresolved  string            `json:"unresolved,omitempty"`
	CWD         string            `json:"cwd,omitempty"`
	Path        string            `json:"path"`
	Label       string            `json:"label"`
	Kind        string            `json:"kind"`
	PID         int               `json:"pid"`
	Argv        []string          `json:"argv"`
	Environment map[string]string `json:"environment"`
	Content     []byte            `json:"content"`
}
type LegacyInvocation struct {
	Unit         string `json:"unit"`
	PID          int    `json:"pid"`
	Agent        string `json:"agent"`
	Workspace    string `json:"workspace"`
	BaseURL      string `json:"base_url"`
	ConfigDir    string `json:"config_dir"`
	Policy       Policy `json:"policy"`
	CWD          string `json:"cwd"`
	PythonConfig string `json:"python_config,omitempty"`
}
type MigrationPlan struct {
	Units       []LegacyUnit       `json:"units"`
	Processes   []Process          `json:"processes"`
	Invocations []LegacyInvocation `json:"invocations"`
	Blockers    []string           `json:"blockers"`
}

func sameExecutable(a, b string) bool {
	x, e := filepath.EvalSymlinks(a)
	if e != nil {
		return false
	}
	y, e := filepath.EvalSymlinks(b)
	return e == nil && x == y
}
func IsConsumer(argv []string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "bridge" && argv[i+1] == "run" {
			return true
		}
	}
	for _, a := range argv {
		if filepath.Base(a) == "anx-agent-bridge" {
			return true
		}
	}
	return false
}
func ParseLegacy(argv []string, env map[string]string, cwd, home string) (LegacyInvocation, error) {
	v := LegacyInvocation{Agent: env["ANX_AS"], BaseURL: env["ANX_BASE_URL"], ConfigDir: env["ANX_CONFIG_DIR"], CWD: cwd, Policy: Policy{5, 30 * time.Minute}}
	if v.ConfigDir == "" {
		v.ConfigDir = filepath.Join(home, ".config", "anx")
	}
	if !filepath.IsAbs(v.ConfigDir) {
		return v, fmt.Errorf("legacy config root is not absolute")
	}
	for index, a := range argv {
		if filepath.Base(a) == "anx-agent-bridge" {
			for i := index + 1; i < len(argv); i++ {
				switch argv[i] {
				case "run":
				case "--config":
					if i+1 >= len(argv) {
						return v, fmt.Errorf("legacy Python config flag lacks value")
					}
					i++
					v.PythonConfig = argv[i]
				default:
					return v, fmt.Errorf("unsupported legacy Python argument")
				}
			}
			if v.PythonConfig == "" {
				return v, fmt.Errorf("legacy Python consumer lacks explicit config")
			}
			if !filepath.IsAbs(v.PythonConfig) {
				v.PythonConfig = filepath.Join(cwd, v.PythonConfig)
			}
			return v, nil
		}
	}
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "bridge" || arg == "run" {
			continue
		}
		name, value, eq := strings.Cut(arg, "=")
		if !eq {
			if i+1 >= len(argv) {
				return v, fmt.Errorf("legacy flag lacks a value")
			}
			i++
			value = argv[i]
		}
		switch name {
		case "--as":
			v.Agent = value
		case "--workspace":
			v.Workspace = value
		case "--base-url":
			v.BaseURL = value
		case "--config-dir":
			v.ConfigDir = value
		case "--command-timeout":
			d, e := time.ParseDuration(value)
			if e != nil {
				return v, e
			}
			v.Policy.Timeout = d
		case "--max-attempts":
			n, e := strconv.Atoi(value)
			if e != nil {
				return v, e
			}
			v.Policy.MaxAttempts = n
		default:
			return v, fmt.Errorf("unsupported legacy flag %s", name)
		}
	}
	if v.Agent == "" || v.Policy.MaxAttempts < 1 || v.Policy.MaxAttempts > 20 || v.Policy.Timeout <= 0 {
		return v, fmt.Errorf("legacy consumer has unresolved agent or policy")
	}
	return v, nil
}
func PlanMigration(home, exe string, uid int, units []LegacyUnit, processes []Process) MigrationPlan {
	plan := MigrationPlan{Units: []LegacyUnit{}, Processes: []Process{}, Invocations: []LegacyInvocation{}, Blockers: []string{}}
	byPID := map[int]Process{}
	for _, p := range processes {
		byPID[p.PID] = p
	}
	covered := map[int]bool{}
	descendant := func(pid, ancestor int) bool {
		seen := map[int]bool{}
		for pid > 1 && !seen[pid] {
			if pid == ancestor {
				return true
			}
			seen[pid] = true
			p, ok := byPID[pid]
			if !ok {
				return false
			}
			pid = p.PPID
		}
		return false
	}
	for _, u := range units {
		if u.Unresolved == "" && (u.Label == Label || u.Label == Unit) {
			continue
		}
		candidates := []Process{}
		for _, p := range processes {
			if IsConsumer(p.Argv) && descendant(p.PID, u.PID) {
				candidates = append(candidates, p)
			}
		}
		direct := IsConsumer(u.Argv)
		if u.Unresolved != "" {
			if len(candidates) > 0 || possibleBridgeUnit(u.Path, u.Content) {
				plan.Blockers = append(plan.Blockers, "unresolved bridge unit: "+u.Label)
			}
			continue
		}
		if len(candidates) == 0 && !direct {
			continue
		}
		if u.CWD == "%h" {
			u.CWD = home
		}
		if u.CWD != "" && !filepath.IsAbs(u.CWD) {
			plan.Blockers = append(plan.Blockers, "unresolved unit working directory: "+u.Label)
			continue
		}
		if u.Environment["UNSUPPORTED_ENV"] != "" {
			plan.Blockers = append(plan.Blockers, "unsupported identity environment: "+u.Label)
			continue
		}
		if u.Environment["HOME"] != "" && u.Environment["HOME"] != home {
			plan.Blockers = append(plan.Blockers, "different service HOME: "+u.Label)
			continue
		}
		for _, p := range candidates {
			if p.UID != uid || p.Started == "" || !verifiedLegacyExecutable(p, exe) {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("unproven executable/owner for PID %d", p.PID))
				continue
			}
			covered[p.PID] = true
			v, e := ParseLegacy(p.Argv, u.Environment, p.CWD, home)
			if e != nil {
				plan.Blockers = append(plan.Blockers, u.Label+": "+e.Error())
				continue
			}
			v.Unit = u.Label
			v.PID = p.PID
			plan.Invocations = append(plan.Invocations, v)
			plan.Processes = append(plan.Processes, p)
		}
		if len(candidates) == 0 && direct {
			if u.PID > 0 {
				plan.Blockers = append(plan.Blockers, "cannot verify loaded consumer: "+u.Label)
				continue
			}
			if !sameExecutable(u.Argv[0], exe) && !anxExecutable(u.Argv[0]) && !verifiedStoppedPython(u.Argv[0]) {
				plan.Blockers = append(plan.Blockers, "unproven unit executable: "+u.Label)
				continue
			}
			cwd := u.CWD
			if cwd == "" {
				cwd = home
			}
			v, e := ParseLegacy(u.Argv, u.Environment, cwd, home)
			if e != nil {
				plan.Blockers = append(plan.Blockers, u.Label+": "+e.Error())
				continue
			}
			v.Unit = u.Label
			plan.Invocations = append(plan.Invocations, v)
		}
		plan.Units = append(plan.Units, u)
	}
	newSupervisors := 0
	for _, p := range processes {
		if IsConsumer(p.Argv) && !covered[p.PID] { // The new foreground singleton is permitted only with no agent flags.
			if p.UID == uid && len(p.Argv) == 3 && p.Argv[1] == "bridge" && p.Argv[2] == "run" && sameExecutable(p.Executable, exe) {
				newSupervisors++
				if newSupervisors > 1 {
					plan.Blockers = append(plan.Blockers, "multiple host supervisors already running")
				}
				continue
			}
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("unowned bridge consumer PID %d", p.PID))
		}
	}
	return plan
}

// XML decoding does not execute a plist or its wrapper, nor inspect env files.
func plistValue(d *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "string", "key", "integer", "real", "data", "date":
		var s string
		e := d.DecodeElement(&s, &start)
		return s, e
	case "true", "false":
		e := d.Skip()
		return start.Name.Local == "true", e
	case "array":
		out := []any{}
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch v := t.(type) {
			case xml.StartElement:
				x, e := plistValue(d, v)
				if e != nil {
					return nil, e
				}
				out = append(out, x)
			case xml.EndElement:
				return out, nil
			}
		}
	case "dict":
		out := map[string]any{}
		key := ""
		for {
			t, e := d.Token()
			if e != nil {
				return nil, e
			}
			switch v := t.(type) {
			case xml.StartElement:
				x, e := plistValue(d, v)
				if e != nil {
					return nil, e
				}
				if v.Name.Local == "key" {
					key, _ = x.(string)
				} else {
					if key == "" {
						return nil, fmt.Errorf("invalid plist dict")
					}
					if _, exists := out[key]; exists {
						return nil, fmt.Errorf("duplicate plist key")
					}
					out[key] = x
					key = ""
				}
			case xml.EndElement:
				return out, nil
			}
		}
	default:
		return nil, fmt.Errorf("unsupported plist value %s", start.Name.Local)
	}
}

var plistHeaders = regexp.MustCompile(`(?s)<\?xml\b.*?\?>|<plist\b[^>]*>`)

// On parse failure, conservatively retain any ANX marker in the filename or
// bytes (including wrappers/labels), or bridge plus run separated by XML tags
// or whitespace. These are suspicion, not ownership proof: unresolved matches
// block cutover even when stopped. Unrelated malformed units are ignored; live
// consumers are still independently discovered through kernel argv/ancestry.
func possibleBridgeUnit(path string, raw []byte) bool {
	text := strings.ToLower(filepath.Base(path) + "\n" + string(raw))
	return strings.Contains(text, "anx") || strings.Contains(text, "io.agent-nexus") ||
		strings.Contains(text, "bridge") && strings.Contains(text, "run")
}

func ParseUnit(path, kind string, raw []byte) (LegacyUnit, error) {
	u := LegacyUnit{Path: path, Kind: kind, Content: raw, Environment: map[string]string{}}
	if len(raw) > 1<<20 {
		return u, fmt.Errorf("unit too large")
	}
	if kind == "launchd" {
		// Some launchd-accepted plists escape attribute quotes as if embedded in
		// a string. Normalize only the XML/plist headers, never command values;
		// Content retains the exact original bytes for rollback.
		xmlText := plistHeaders.ReplaceAllStringFunc(string(raw), func(header string) string {
			return strings.ReplaceAll(header, `\"`, `"`)
		})
		d := xml.NewDecoder(strings.NewReader(xmlText))
		for {
			t, e := d.Token()
			if e != nil {
				return u, e
			}
			if v, ok := t.(xml.StartElement); ok && v.Name.Local == "dict" {
				value, e := plistValue(d, v)
				if e != nil {
					return u, e
				}
				m := value.(map[string]any)
				u.Label, _ = m["Label"].(string)
				u.CWD, _ = m["WorkingDirectory"].(string)
				argv, _ := m["ProgramArguments"].([]any)
				for _, v := range argv {
					s, ok := v.(string)
					if !ok {
						return u, fmt.Errorf("invalid unit argv")
					}
					u.Argv = append(u.Argv, s)
				}
				env, _ := m["EnvironmentVariables"].(map[string]any)
				for k, v := range env {
					if strings.HasPrefix(k, "ANX_") && k != "ANX_BASE_URL" && k != "ANX_CONFIG_DIR" && k != "ANX_AS" {
						u.Environment["UNSUPPORTED_ENV"] = k
						continue
					}
					if k == "HOME" || strings.HasPrefix(k, "ANX_") {
						s, ok := v.(string)
						if !ok {
							return u, fmt.Errorf("invalid unit environment")
						}
						u.Environment[k] = s
					}
				}
				return u, nil
			}
		}
	}
	u.Label = filepath.Base(path)
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		if section != "[Service]" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ExecStart":
			if u.Argv != nil {
				return u, fmt.Errorf("multiple ExecStart commands")
			}
			args, e := SplitUnitWords(value)
			if e != nil {
				return u, e
			}
			u.Argv = args
		case "Environment":
			args, e := SplitUnitWords(value)
			if e != nil {
				return u, e
			}
			for _, v := range args {
				k, val, _ := strings.Cut(v, "=")
				if strings.HasPrefix(k, "ANX_") && k != "ANX_BASE_URL" && k != "ANX_CONFIG_DIR" && k != "ANX_AS" {
					u.Environment["UNSUPPORTED_ENV"] = k
					continue
				}
				if k == "HOME" || strings.HasPrefix(k, "ANX_") {
					u.Environment[k] = val
				}
			}
		case "WorkingDirectory":
			if value == "%h" || value == `"%h"` {
				u.CWD = "%h"
				continue
			}
			words, e := SplitUnitWords(value)
			if e != nil || len(words) != 1 {
				return u, fmt.Errorf("unsupported working directory")
			}
			u.CWD = words[0]
		case "EnvironmentFile":
			u.Environment["UNRESOLVED_ENV_FILE"] = value
		}
	}
	return u, nil
}

// Parse unit-file quoting only, not shell syntax/expansion. Ambiguous escapes,
// variable/specifier substitutions and continuation commands require repair.
func SplitUnitWords(s string) ([]string, error) {
	out := []string{}
	var word strings.Builder
	quote := rune(0)
	active := false
	escaped := false
	for _, r := range s {
		if escaped {
			if r != '\\' && r != '"' && r != '\'' && r != ' ' {
				return nil, fmt.Errorf("unsupported unit escape")
			}
			word.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			active = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			active = true
			continue
		}
		if r == ' ' || r == '\t' {
			if active {
				out = append(out, word.String())
				word.Reset()
				active = false
			}
			continue
		}
		active = true
		word.WriteRune(r)
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated unit quoting")
	}
	if active {
		out = append(out, word.String())
	}
	for i, v := range out {
		if strings.Contains(v, "$") || strings.Contains(strings.ReplaceAll(v, "%%", ""), "%") {
			return nil, fmt.Errorf("unresolved unit expansion")
		}
		out[i] = strings.ReplaceAll(v, "%%", "%")
	}
	return out, nil
}
func ReadUnits(home, goos string) ([]LegacyUnit, error) {
	dir := filepath.Join(home, "Library", "LaunchAgents")
	kind := "launchd"
	suffix := ".plist"
	if goos == "linux" {
		dir = filepath.Join(home, ".config", "systemd", "user")
		kind = "systemd-user"
		suffix = ".service"
	}
	files, e := os.ReadDir(dir)
	if errors.Is(e, os.ErrNotExist) {
		return []LegacyUnit{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []LegacyUnit{}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), suffix) {
			continue
		}
		path := filepath.Join(dir, f.Name())
		info, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || !unitOwned(info) || info.Size() > 1<<20 {
			out = append(out, LegacyUnit{Path: path, Label: strings.TrimSuffix(f.Name(), ".plist"), Kind: kind, Unresolved: "unit ownership/type cannot be verified"})
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		u, e := ParseUnit(path, kind, raw)
		if e != nil {
			if !possibleBridgeUnit(path, raw) {
				continue
			}
			u = LegacyUnit{Path: path, Label: strings.TrimSuffix(f.Name(), suffix), Kind: kind, Content: raw, Unresolved: "unit cannot be parsed"}
		}
		u.Mode = uint32(info.Mode().Perm())
		out = append(out, u)
	}
	return out, nil
}

// Python console scripts require an owned executable file and a matching
// absolute shebang interpreter observed by the kernel. A name or label alone
// is not executable identity; wrappers are proven separately by unit ancestry.
func verifiedLegacyExecutable(p Process, exe string) bool {
	if sameExecutable(p.Executable, exe) {
		return true
	}
	executable := p.Executable
	if runtime.GOOS == "linux" && strings.HasSuffix(executable, " (deleted)") {
		executable = fmt.Sprintf("/proc/%d/exe", p.PID)
	}
	if anxExecutable(executable) {
		return true
	}
	for i, arg := range p.Argv {
		if i > 1 {
			break
		}
		if filepath.Base(arg) != "anx-agent-bridge" || !filepath.IsAbs(arg) {
			continue
		}
		st, e := os.Lstat(arg)
		if e != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 || !unitOwned(st) {
			return false
		}
		f, e := os.Open(arg)
		if e != nil {
			return false
		}
		raw := make([]byte, 512)
		n, _ := f.Read(raw)
		f.Close()
		line, _, _ := strings.Cut(string(raw[:n]), "\n")
		if !strings.HasPrefix(line, "#!") {
			return false
		}
		fields := strings.Fields(strings.TrimPrefix(line, "#!"))
		return len(fields) == 1 && filepath.IsAbs(fields[0]) && sameExecutable(p.Executable, fields[0])
	}
	return false
}

func anxExecutable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, e := buildinfo.ReadFile(path)
	return e == nil && info.Main.Path == "agent-nexus-cli" && info.Path == "agent-nexus-cli/cmd/anx"
}

func verifiedStoppedPython(path string) bool {
	if !filepath.IsAbs(path) || filepath.Base(path) != "anx-agent-bridge" {
		return false
	}
	st, e := os.Lstat(path)
	if e != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 || !unitOwned(st) {
		return false
	}
	f, e := os.Open(path)
	if e != nil {
		return false
	}
	defer f.Close()
	raw := make([]byte, 512)
	n, _ := f.Read(raw)
	line, _, _ := strings.Cut(string(raw[:n]), "\n")
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) != 1 || !filepath.IsAbs(fields[0]) {
		return false
	}
	interpreter, e := os.Stat(fields[0])
	return e == nil && interpreter.Mode().IsRegular() && interpreter.Mode()&0111 != 0
}
