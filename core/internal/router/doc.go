// Package router watches canonical events and routes agent wake packets to
// registered agent bridges, building the wake artifacts and verifying signed
// bridge check-ins. It runs as an in-process sidecar service of anx-core.
package router
