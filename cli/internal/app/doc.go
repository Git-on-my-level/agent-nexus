// Package app implements CLI commands. Large pieces live in adjacent files:
// resource_transport.go (invokeTypedJSON, headers, commandSpecByID),
// resource_topics_cards.go (topics and cards list wiring),
// text_output.go (default non-JSON summaries for typed commands),
// resource_streaming.go (tail stream loop),
// event_reference_preflight.go (embedded contract rules for events create),
// draft_commands.go (local command drafts).
package app
