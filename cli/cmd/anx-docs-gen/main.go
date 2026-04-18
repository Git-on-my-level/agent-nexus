package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"

	"agent-nexus-cli/internal/app"
)

func main() {
	outDir := flag.String("out-dir", filepath.Join("docs", "generated"), "Directory to write runtime help markdown into")
	flag.Parse()

	writtenPath, err := app.WriteRuntimeHelpDocs(*outDir)
	if err != nil {
		log.Fatalf("write runtime help docs: %v", err)
	}
	fmt.Println(writtenPath)
}
