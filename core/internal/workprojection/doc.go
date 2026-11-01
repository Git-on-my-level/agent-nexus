// Package workprojection builds the indexed SQL fragments that evaluate a
// card's effective work fields: adapter authority may override the canonical
// value from observation facts or metadata, where a JSON null is a present
// override distinct from a missing key.
package workprojection
