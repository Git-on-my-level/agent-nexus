// Package executor runs catalog tool calls against the workspace HTTP API as a
// derived agent, obtaining the short-lived token through the anx CLI so host
// key handling stays out of this server, and redacts secret values from
// results before they leave the executor.
package executor
