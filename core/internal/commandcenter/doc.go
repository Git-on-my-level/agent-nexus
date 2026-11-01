// Package commandcenter records host-agent runs, observations and presence,
// and serves the bounded roster projection for collection panels. Attribution
// context keeps derived recipient-facing records independent of the run that
// authored their private audit.
//
// The freshness constants bound how stale a signal may be before panels stop
// treating it as live.
package commandcenter
