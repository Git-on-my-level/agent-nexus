module agent-nexus-cli

go 1.23.0

toolchain go1.24.13

require (
	agent-nexus-contracts-go-client v0.0.0
	github.com/pmezard/go-difflib v1.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/pelletier/go-toml/v2 v2.2.3

replace agent-nexus-contracts-go-client => ../contracts/gen/go
