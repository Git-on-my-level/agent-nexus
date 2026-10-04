// conformance runs the production validator over the browser's shared corpus.
package main

import (
	visualreport "agent-nexus-visualreport"
	"encoding/json"
	"os"
)

func main() {
	var inputs struct {
		Reports   []string `json:"reports"`
		Summaries []string `json:"summaries"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
		panic(err)
	}
	results := struct {
		Reports []struct {
			Recognized bool `json:"recognized"`
			Valid      bool `json:"valid"`
		} `json:"reports"`
		Summaries []visualreport.Progress `json:"summaries"`
	}{
		Reports: make([]struct {
			Recognized bool `json:"recognized"`
			Valid      bool `json:"valid"`
		}, len(inputs.Reports)),
		Summaries: make([]visualreport.Progress, len(inputs.Summaries)),
	}
	for i, content := range inputs.Reports {
		result := visualreport.Validate([]byte(content))
		results.Reports[i].Recognized = result.Recognized
		results.Reports[i].Valid = result.Valid
	}
	for i, markdown := range inputs.Summaries {
		_, progress, _ := visualreport.Summary(markdown)
		results.Summaries[i] = progress
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}
