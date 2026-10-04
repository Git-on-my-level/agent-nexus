// conformance runs the production validator over the browser's shared corpus.
package main

import (
	visualreport "agent-nexus-visualreport"
	"encoding/json"
	"os"
)

func main() {
	var inputs []string
	if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
		panic(err)
	}
	results := make([]struct {
		Recognized bool `json:"recognized"`
		Valid      bool `json:"valid"`
	}, len(inputs))
	for i, content := range inputs {
		result := visualreport.Validate([]byte(content))
		results[i].Recognized = result.Recognized
		results[i].Valid = result.Valid
	}
	if err := json.NewEncoder(os.Stdout).Encode(results); err != nil {
		panic(err)
	}
}
