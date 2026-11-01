// Package sqliteutil opens the bounded maintenance transactions used by
// background rebuild programs: BEGIN IMMEDIATE chunks that fail fast on
// contention so long maintenance work yields the database between slices
// instead of inheriting the serving busy handler.
package sqliteutil
