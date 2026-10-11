module agent-nexus-cli

go 1.23.0

require (
	agent-nexus-contracts-go-client v0.0.0
	github.com/pmezard/go-difflib v1.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/pelletier/go-toml/v2 v2.2.3

replace agent-nexus-contracts-go-client => ../contracts/gen/go

require (
	agent-nexus-visualreport v0.0.0
	golang.org/x/term v0.32.0
)

require (
	github.com/yuin/goldmark v1.7.8 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

replace agent-nexus-visualreport => ../contracts/visualreport
