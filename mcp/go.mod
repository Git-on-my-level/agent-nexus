module github.com/Git-on-my-level/agent-nexus/mcp

go 1.23.0

require (
	agent-nexus-cli v0.0.0
	agent-nexus-contracts-go-client v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace agent-nexus-contracts-go-client => ../contracts/gen/go

replace agent-nexus-cli => ../cli

// Replacements from the locally shared CLI module are not inherited by Go.
replace agent-nexus-markdownhygiene => ../contracts/markdownhygiene

replace agent-nexus-visualreport => ../contracts/visualreport
