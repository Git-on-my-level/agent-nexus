package app

import (
	"fmt"
	"strconv"
	"strings"
)

// orientUpkeepView reports whether `anx orient --upkeep` asked for every open
// suggestion. The default shows an agent only what concerns its own work.
func orientUpkeepView(args []string) bool {
	for _, arg := range args {
		if arg == "--upkeep" || arg == "--upkeep=true" {
			return true
		}
	}
	return false
}

// orientSuggestions prints the upkeep an agent should do. By default that is
// the suggestions about its own asks and cards, each with the two commands
// that apply or dismiss it, and one line counting the rest. Every agent used
// to be shown every suggestion, which made each one nobody's job.
func orientSuggestions(brief map[string]any) string {
	var b strings.Builder
	all := brief["upkeep_view"] == true
	others := 0
	for _, raw := range asSlice(brief["curation_suggestions"]) {
		item := asMap(raw)
		if !all && item["yours"] == false {
			others++
			continue
		}
		fmt.Fprintf(&b, "suggestion: %s %s %q\n", anyString(item["key"]), anyString(item["action"]), anyString(item["title"]))
		for _, verb := range []string{"apply", "dismiss"} {
			argv := stringList(item[verb+"_argv"])
			// Quote the JSON argument for copy/paste into a shell.
			if len(argv) > 0 {
				for i, arg := range argv {
					if strings.ContainsAny(arg, " \"'{}") {
						argv[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
					}
				}
				fmt.Fprintf(&b, "next %s\n", strings.Join(argv, " "))
			}
		}
	}
	if others > 0 {
		more := ""
		if asBool(asMap(brief["truncated"])["curation_suggestions"]) {
			more = "+"
		}
		fmt.Fprintf(&b, "upkeep: %d%s suggestions about other owners' work · anx orient --upkeep\n", others, more)
	}
	if !all {
		return b.String()
	}
	// The upkeep view also lists what a human is being asked right now, so a
	// stale or duplicated question can be retired; an empty view says so.
	if len(asSlice(brief["curation_suggestions"])) == 0 {
		b.WriteString("upkeep: no open suggestions\n")
	}
	asks := asSlice(brief["open_asks"])
	if len(asks) == 0 {
		b.WriteString("asked of humans:  none by other agents\n")
	}
	for _, raw := range asks {
		item := asMap(raw)
		by := asMap(item["requested_by"])
		line := fmt.Sprintf("asked of humans:  %s %q by %s", anyString(item["event_ref"]), anyString(item["title"]), firstNonEmpty(anyString(by["display"]), anyString(by["id"]), "someone"))
		if age := briefSeconds(item["age_seconds"]); age > 0 {
			line += " · " + compactDuration(age)
		}
		if subject := anyString(item["subject_ref"]); subject != "" {
			line += " · on " + subject
		}
		b.WriteString(line + "\n")
	}
	if len(asks) > 0 {
		b.WriteString("next anx ask retire <ask> --reason \"<why it no longer needs an answer>\" --evidence <url|ref>\n")
	}
	return b.String()
}

// orientReturned prints the asks someone else closed for this agent, with the
// reason, so it learns that a question was retired and why.
func orientReturned(brief map[string]any) string {
	var b strings.Builder
	for _, raw := range asSlice(brief["returned_to_you"]) {
		item := asMap(raw)
		by := asMap(item["returned_by"])
		line := fmt.Sprintf("returned to you:  %s %q by %s", anyString(item["event_ref"]), anyString(item["title"]), firstNonEmpty(anyString(by["display"]), anyString(by["id"]), "someone"))
		if reason := strings.TrimSpace(anyString(item["reason"])); reason != "" {
			line += ": " + truncateRunes(reason, 200)
		}
		b.WriteString(line + "\n")
		if argv := stringList(item["undo_argv"]); len(argv) > 0 {
			fmt.Fprintf(&b, "next %s\n", strings.Join(argv, " "))
		}
	}
	return b.String()
}

// orientMeasure appends the number an initiative is judged by, as
// " · Monthly churn 12.4% → 10% (was 13.1%)", or says that nothing reads it.
func orientMeasure(item map[string]any) string {
	measure := asMap(item["measure"])
	if measure == nil {
		return ""
	}
	unit := anyString(measure["unit"])
	number := func(v any) string {
		f, ok := v.(float64)
		if !ok {
			return ""
		}
		text := strconv.FormatFloat(f, 'f', -1, 64)
		if unit == "%" || len(unit) == 1 {
			return text + unit
		}
		if unit != "" {
			return text + " " + unit
		}
		return text
	}
	label, target := anyString(measure["label"]), number(measure["target"])
	latest := number(measure["latest"])
	if latest == "" {
		// Say the one command that would produce a reading.
		hint := "link a series: anx work measure " + anyString(item["ref"]) + " --series series:<name>"
		if series := strings.TrimPrefix(anyString(measure["series_ref"]), "series:"); series != "" {
			hint = "publish it: anx series push " + strings.SplitN(series, "?", 2)[0] + " <value>"
		}
		return fmt.Sprintf(" · %s: no reading (target %s) · %s", label, target, hint)
	}
	out := fmt.Sprintf(" · %s %s → %s", label, latest, target)
	if previous := number(measure["previous"]); previous != "" && previous != latest {
		out += " (was " + previous + ")"
	}
	if age := briefSeconds(measure["age_seconds"]); age > 0 {
		out += " · read " + compactDuration(age) + " ago"
	}
	return out
}
