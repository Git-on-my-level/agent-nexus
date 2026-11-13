package app

import "strings"

// measureBody builds metadata.measure for `anx work measure`: the number an
// initiative is judged by. It needs a label, a direction and a target; the
// series is what a producer publishes so Watch can show the reading.
func measureBody(values map[string]*trackedString, number func(string) (float64, error)) (map[string]any, string) {
	label := strings.TrimSpace(values["label"].value)
	if label == "" || len([]rune(label)) > 120 {
		return nil, "--label is required: the plain name of the number, at most 120 characters"
	}
	direction := values["direction"].value
	if direction != "up" && direction != "down" {
		return nil, "--direction must be up or down"
	}
	if !values["target"].set {
		return nil, "--target is required: the value that means the goal is met"
	}
	m := map[string]any{"label": label, "direction": direction}
	for _, k := range []string{"target", "baseline"} {
		if values[k].set {
			n, err := number(k)
			if err != nil {
				return nil, err.Error()
			}
			m[k] = n
		}
	}
	if values["unit"].set {
		if len([]rune(values["unit"].value)) > 80 {
			return nil, "unit exceeds 80 characters"
		}
		m["unit"] = values["unit"].value
	}
	if values["series"].set {
		ref := values["series"].value
		if !strings.HasPrefix(ref, "series:") || len(ref) > 2048 {
			return nil, "--series must be a series ref"
		}
		m["series_ref"] = ref
	}
	return m, ""
}
