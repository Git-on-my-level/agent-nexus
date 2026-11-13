// anx-sqlite-compact writes a verified, compacted copy of a stopped workspace
// database. The output uses auto_vacuum=INCREMENTAL so later deletes can be
// reclaimed online. It refuses to run against a live core; stop the service
// first.
package main
