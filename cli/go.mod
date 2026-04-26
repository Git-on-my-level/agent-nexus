module agent-nexus-cli

go 1.23.0

require (
	agent-nexus-contracts-go-client v0.0.0
	github.com/pmezard/go-difflib v1.0.0
)

require github.com/pelletier/go-toml/v2 v2.2.3 // indirect

replace agent-nexus-contracts-go-client => ../contracts/gen/go
