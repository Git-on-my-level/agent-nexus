package observation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
)

// Generated transforms are C programs compiled to a host-native executable.
//
// Why C: Seatbelt allows process-exec of one artifact and denies fork, so an
// interpreter (python/node/sh) cannot be exec'd. Linux bubblewrap binds only
// the artifact at /reader with no libc mount, so Linux artifacts must be
// static ELF (`cc -static`). Darwin Mach-O may link /usr/lib and
// /System/Library, which hostMachO already permits for dyld.
const GeneratedLanguage = "c"

// DefaultGenerateHarness is the same omp/glm path the PM runner uses.
var DefaultGenerateHarness = []string{"agentctl", "run", "--prompt-file", "prompt.md", "--prompt-delivery", "argv", "--", "omp", "-p", "--mode", "json", "--model", "zai/glm-5.3", "--auto-approve"}

type GenerateRequest struct {
	Workspace string
	Manifest  Manifest
	Snapshot  json.RawMessage
	Harness   []string
}

type GeneratedArtifact struct {
	Language string
	Source   []byte
	Binary   []byte
	Provider string
	Model    string
}

func (r GenerateRequest) harness() []string {
	if len(r.Harness) == 0 {
		return append([]string{}, DefaultGenerateHarness...)
	}
	return append([]string{}, r.Harness...)
}

func GenerateWorkspace(req GenerateRequest) error {
	if !filepath.IsAbs(req.Workspace) {
		return failure(ErrConfiguration, "generation workspace must be absolute")
	}
	if err := os.MkdirAll(req.Workspace, 0700); err != nil {
		return err
	}
	rawManifest, err := json.MarshalIndent(req.Manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(req.Workspace, "manifest.json"), rawManifest, 0600); err != nil {
		return err
	}
	rawTarget, err := json.MarshalIndent(req.Manifest.Target, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(req.Workspace, "target.json"), rawTarget, 0600); err != nil {
		return err
	}
	snapshot := req.Snapshot
	if len(snapshot) == 0 {
		snapshot = []byte(`{"title":"sample","native_status":"open","facts":{},"evidence":[],"coverage":{"complete":true}}`)
	}
	if err = os.WriteFile(filepath.Join(req.Workspace, "snapshot.json"), snapshot, 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(req.Workspace, "prompt.md"), []byte(generatePrompt()), 0600)
}

func generatePrompt() string {
	return strings.Join([]string{
		"# Generated observation transform",
		"",
		"Write a single self-contained C program. A markdown C fence is allowed.",
		"",
		"The program:",
		"- reads one JSON object from stdin (a source snapshot or a fixture)",
		"- writes one JSON object to stdout, then exits 0",
		"- if the input contains reject:true, write an object with only error=rejected (invalid transform schema) and exit 0",
		"- otherwise write exactly facts, uncertainty, and evidence",
		"- facts must include reader=generated-c-transform and has_snapshot=true",
		"- if the input has a title string, copy it to facts title",
		"- if the input has native_status, copy it to facts native_status",
		"- evidence must be an empty array; do not invent URLs",
		"- no network, files, fork, getenv of secrets, or extra stdout",
		"",
		"Compile-time: ISO C, stdio.h and string.h only, no libraries.",
		"",
		"Files in this workspace (read-only context):",
		"- manifest.json — adapter identity and target",
		"- target.json — the selected source target",
		"- snapshot.json — a sample snapshot",
		"",
	}, "\n")
}

func ExtractGeneratedC(raw []byte) ([]byte, error) {
	if !utf8.Valid(raw) {
		return nil, failure(ErrInvalidOutput, "generated output is not UTF-8")
	}
	text := string(raw)
	if block := firstFence(text, "c"); block != "" {
		return []byte(block), nil
	}
	if block := firstFence(text, ""); block != "" && strings.Contains(block, "int main") {
		return []byte(block), nil
	}
	if i := strings.Index(text, "#include"); i >= 0 {
		rest := text[i:]
		if strings.Contains(rest, "int main") {
			return []byte(strings.TrimSpace(rest)), nil
		}
	}
	return nil, failure(ErrInvalidOutput, "generated output did not contain a C program")
}

func firstFence(text, lang string) string {
	lower := strings.ToLower(text)
	needle := "```"
	if lang != "" {
		needle = "```" + strings.ToLower(lang)
	}
	start := strings.Index(lower, needle)
	if start < 0 {
		return ""
	}
	bodyStart := start + len(needle)
	if bodyStart < len(text) && (text[bodyStart] == '\n' || text[bodyStart] == '\r') {
		if text[bodyStart] == '\r' && bodyStart+1 < len(text) && text[bodyStart+1] == '\n' {
			bodyStart += 2
		} else {
			bodyStart++
		}
	} else if lang == "" && bodyStart < len(text) && text[bodyStart] != '\n' && text[bodyStart] != '\r' {
		// ```json or similar; skip this fence.
		next := strings.Index(text[start+3:], "```")
		if next < 0 {
			return ""
		}
		return firstFence(text[start+3+next+3:], lang)
	}
	endRel := strings.Index(text[bodyStart:], "```")
	if endRel < 0 {
		return ""
	}
	return strings.TrimSpace(text[bodyStart : bodyStart+endRel])
}

func CompileGeneratedC(dir string, source []byte) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", failure(ErrConfiguration, "compile directory must be absolute")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	cc, err := exec.LookPath("cc")
	if err != nil {
		return "", failure(ErrUnavailable, "C compiler is not available")
	}
	cfile := filepath.Join(dir, "transform.c")
	binary := filepath.Join(dir, "transform")
	if err = os.WriteFile(cfile, source, 0600); err != nil {
		return "", err
	}
	args := []string{"-O2", "-o", binary, cfile}
	if runtime.GOOS == "linux" {
		args = []string{"-static", "-O2", "-o", binary, cfile}
	}
	out, err := exec.Command(cc, args...).CombinedOutput()
	if err != nil {
		return "", failure(ErrInvalidOutput, "generated C failed to compile: "+strings.TrimSpace(string(out)))
	}
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func ParseHarnessModel(raw []byte) (provider, model string) {
	re := regexp.MustCompile(`"provider"\s*:\s*"([^"]+)"\s*,\s*"model"\s*:\s*"([^"]+)"`)
	m := re.FindSubmatch(raw)
	if len(m) == 3 {
		return string(m[1]), string(m[2])
	}
	return "", ""
}

func ParseHarnessText(raw []byte) []byte {
	var last []byte
	take := func(s string) {
		if strings.TrimSpace(s) != "" {
			last = []byte(s)
		}
	}
	tryObj := func(line []byte) {
		var msg map[string]any
		if json.Unmarshal(line, &msg) != nil {
			return
		}
		if s, _ := msg["result_content"].(string); s != "" {
			take(s)
		}
		if result, ok := msg["result"].(map[string]any); ok {
			for _, key := range []string{"result_content", "content", "text", "output", "message"} {
				if s, _ := result[key].(string); s != "" {
					take(s)
				}
			}
		}
		if s, _ := msg["content"].(string); s != "" && msg["role"] == "assistant" {
			take(s)
		}
		if message, ok := msg["message"].(map[string]any); ok {
			if message["role"] == "assistant" {
				switch c := message["content"].(type) {
				case string:
					take(c)
				case []any:
					var b strings.Builder
					for _, item := range c {
						m, _ := item.(map[string]any)
						if m != nil {
							if t, _ := m["text"].(string); t != "" {
								b.WriteString(t)
							}
						}
					}
					if b.Len() > 0 {
						take(b.String())
					}
				}
			}
		}
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && raw[0] == '{' {
		tryObj(raw)
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		tryObj(line)
	}
	if len(last) > 0 {
		return last
	}
	return raw
}

func harnessExecutionID(raw []byte) string {
	var wrap struct {
		Result struct {
			ID string `json:"id"`
		} `json:"result"`
		ID string `json:"id"`
	}
	if json.Unmarshal(bytes.TrimSpace(raw), &wrap) == nil {
		if wrap.Result.ID != "" {
			return wrap.Result.ID
		}
		return wrap.ID
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		wrap = struct {
			Result struct {
				ID string `json:"id"`
			} `json:"result"`
			ID string `json:"id"`
		}{}
		if json.Unmarshal(bytes.TrimSpace(line), &wrap) == nil {
			if wrap.Result.ID != "" {
				return wrap.Result.ID
			}
			if wrap.ID != "" {
				return wrap.ID
			}
		}
	}
	return ""
}

func harnessFailed(raw []byte) bool {
	return bytes.Contains(raw, []byte(`"state": "failed"`)) || bytes.Contains(raw, []byte(`"state":"failed"`)) || bytes.Contains(raw, []byte("native_execution_failed"))
}

func RunGenerateHarness(ctx context.Context, req GenerateRequest) (GeneratedArtifact, error) {
	if err := GenerateWorkspace(req); err != nil {
		return GeneratedArtifact{}, err
	}
	args := req.harness()
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return GeneratedArtifact{}, failure(ErrUnavailable, "generation harness executable is not available")
	}
	cmd := exec.CommandContext(ctx, bin, args[1:]...)
	cmd.Dir = req.Workspace
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return GeneratedArtifact{}, failure(ErrUnavailable, "generation harness failed")
	}
	if id := harnessExecutionID(out); id != "" {
		_ = exec.CommandContext(ctx, bin, "await", id).Run()
		result := exec.CommandContext(ctx, bin, "result", id, "--content", "--allow-empty")
		if extra, extraErr := result.CombinedOutput(); extraErr == nil || len(extra) > 0 {
			out = append(out, '\n')
			out = append(out, extra...)
		}
	}
	_ = os.WriteFile(filepath.Join(req.Workspace, "harness.out"), out, 0600)
	if harnessFailed(out) {
		return GeneratedArtifact{}, failure(ErrUnavailable, "generation harness failed")
	}
	provider, model := ParseHarnessModel(out)
	if provider != "" && (provider != "zai" || model != "glm-5.3") {
		return GeneratedArtifact{}, failure(ErrPolicy, "generation harness used an unapproved model")
	}
	source, err := ExtractGeneratedC(ParseHarnessText(out))
	if err != nil {
		return GeneratedArtifact{}, err
	}
	path, err := CompileGeneratedC(req.Workspace, source)
	if err != nil {
		return GeneratedArtifact{}, err
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		return GeneratedArtifact{}, err
	}
	return GeneratedArtifact{Language: GeneratedLanguage, Source: source, Binary: binary, Provider: provider, Model: model}, nil
}
