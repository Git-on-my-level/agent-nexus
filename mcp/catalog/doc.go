// Package catalog builds the MCP tool catalog from the CLI's embedded contract
// commands plus the tool policy: it maps command IDs to tool names, applies
// the policy classifications (exposed_read, exposed_write, gated_*) and keeps
// the surfaced tool set in step with the contract. See mcp/docs/tool-coverage.md.
package catalog
