// Package importer plans and applies imports of external inventory into core:
// it builds an import plan from inventoried records, groups exact and probable
// duplicates, and applies creations through a caller-supplied create function.
package importer
