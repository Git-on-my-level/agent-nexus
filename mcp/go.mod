module github.com/Git-on-my-level/agent-nexus/mcp

go 1.23.0

toolchain go1.24.13

require (
	agent-nexus-contracts-go-client v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace agent-nexus-contracts-go-client => ../contracts/gen/go
