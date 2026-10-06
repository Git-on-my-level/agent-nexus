//go:build integration

// Test-only legacy fixture writer. Use the production SQLite driver and
// reference functions so raw fixture writes retain atomic ownership triggers.
package main

import (
	"database/sql"
	"io"
	"log"
	"os"

	_ "agent-nexus-core/internal/resourceaccess"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: test-sql-fixture DATABASE < fixture.sql")
	}
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", os.Args[1]+"?_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(string(body)); err != nil {
		log.Fatal(err)
	}
}
