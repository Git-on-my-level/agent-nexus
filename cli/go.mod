module agent-nexus-cli

go 1.23.0

require (
	github.com/pmezard/go-difflib v1.0.0
	agent-nexus-contracts-go-client v0.0.0
)

replace agent-nexus-contracts-go-client => ../contracts/gen/go
