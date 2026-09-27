package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const SchemaVersion = 2

type NextAction struct {
	Label           string   `json:"label"`
	Argv            []string `json:"argv"`
	Mutates         bool     `json:"mutates"`
	SideEffectClass string   `json:"side_effect_class"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type ErrorPayload struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	Retryable   bool         `json:"retryable"`
	ExitCode    int          `json:"exit_code"`
	Details     any          `json:"details"`
	NextActions []NextAction `json:"next_actions"`
}

type Envelope struct {
	OK            bool          `json:"ok"`
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command,omitempty"`
	Result        any           `json:"result,omitempty"`
	Warnings      []Warning     `json:"warnings"`
	NextActions   []NextAction  `json:"next_actions"`
	Error         *ErrorPayload `json:"error,omitempty"`
}

func (e *Envelope) Init() {
	e.SchemaVersion = SchemaVersion
	if e.OK && e.Result == nil {
		e.Result = map[string]any{}
	}
	if e.Warnings == nil {
		e.Warnings = []Warning{}
	}
	if e.NextActions == nil {
		e.NextActions = []NextAction{}
	}
	if e.Error != nil && e.Error.NextActions == nil {
		e.Error.NextActions = []NextAction{}
	}
}

func WriteEnvelopeJSON(w io.Writer, envelope Envelope) error {
	envelope.Init()
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	if _, err := w.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}
	return nil
}

// WriteEnvelopeText projects the same document used by JSON mode. Nested records
// get dotted keys; lists emit one line per item and never fall back to JSON.
func WriteEnvelopeText(w io.Writer, envelope Envelope) error {
	envelope.Init()
	var lines []string
	if envelope.Error != nil {
		lines = append(lines, "error code="+quote(envelope.Error.Code)+" message="+quote(envelope.Error.Message)+" retryable="+strconv.FormatBool(envelope.Error.Retryable)+" exit_code="+strconv.Itoa(envelope.Error.ExitCode))
		project(&lines, "detail", envelope.Error.Details)
	} else {
		lines = append(lines, "ok command="+quote(envelope.Command))
		if envelope.Command == "cards list" {
			projectCardsList(&lines, envelope.Result)
		} else {
			project(&lines, "result", envelope.Result)
		}
	}
	for _, warning := range envelope.Warnings {
		lines = append(lines, "warning code="+quote(warning.Code)+" message="+quote(warning.Message))
		project(&lines, "warning.detail", warning.Details)
	}
	for _, action := range append(envelope.NextActions, errorActions(envelope.Error)...) {
		argv := make([]string, len(action.Argv))
		for i, arg := range action.Argv {
			argv[i] = shellQuote(arg)
		}
		lines = append(lines, "next "+strings.Join(argv, " ")+" label="+quote(action.Label)+" mutates="+strconv.FormatBool(action.Mutates)+" side_effect_class="+action.SideEffectClass)
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

func projectCardsList(lines *[]string, result any) {
	root, ok := result.(map[string]any)
	if !ok {
		project(lines, "result", result)
		return
	}
	keys := make([]string, 0, len(root))
	for key := range root {
		if key != "cards" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		project(lines, "result."+key, root[key])
	}
	items, _ := root["cards"].([]any)
	for _, raw := range items {
		card, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ref := fmt.Sprint(card["ref"])
		if ref == "<nil>" || ref == "" {
			ref = fmt.Sprint(card["id"])
		}
		line := "card ref=" + quote(ref)
		for _, key := range []string{"title", "column_key", "assignee_refs"} {
			if v, ok := card[key]; ok {
				if key == "assignee_refs" {
					var refs []string
					switch values := v.(type) {
					case []any:
						for _, item := range values {
							refs = append(refs, fmt.Sprint(item))
						}
					case []string:
						refs = append(refs, values...)
					}
					line += " assignees=" + quote(strings.Join(refs, ","))
				} else {
					line += " " + key + "=" + quote(fmt.Sprint(v))
				}
			}
		}
		*lines = append(*lines, line)
	}
}

func errorActions(err *ErrorPayload) []NextAction {
	if err == nil {
		return nil
	}
	return err.NextActions
}

func project(lines *[]string, key string, value any) {
	if value == nil {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			project(lines, key+"."+k, v[k])
		}
	case []any:
		for i, item := range v {
			project(lines, key+"."+strconv.Itoa(i), item)
		}
	case []string:
		for i, item := range v {
			project(lines, key+"."+strconv.Itoa(i), item)
		}
	default:
		encoded, err := json.Marshal(v)
		if err == nil && len(encoded) > 0 && (encoded[0] == '{' || encoded[0] == '[') {
			var normalized any
			if json.Unmarshal(encoded, &normalized) == nil {
				project(lines, key, normalized)
				return
			}
		}
		*lines = append(*lines, "fact "+key+"="+quote(fmt.Sprint(v)))
	}
}

func quote(value string) string {
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || strings.ContainsRune("\"'\\=", r) }) >= 0 {
		return strconv.Quote(value)
	}
	return value
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:@+-", r))
	}) < 0 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
