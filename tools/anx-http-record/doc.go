// anx-http-record is a local-development reverse proxy that records
// CLI-to-anx-core HTTP exchanges into a single interleaved JSONL log with
// completion-ordered seq values, bounded body capture, and default filtering
// of probes and SSE streams. Local dev only; see
// tools/anx-http-record/README.md.
package main
