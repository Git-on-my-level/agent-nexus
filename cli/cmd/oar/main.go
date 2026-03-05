package main

import (
	"os"

	"organization-autorunner-cli/internal/app"
)

func main() {
	os.Exit(app.New().Run(os.Args[1:]))
}
