package main

import (
	"os"

	"agent-nexus-cli/internal/app"
)

func main() {
	os.Exit(app.New().Run(os.Args[1:]))
}
