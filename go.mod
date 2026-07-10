module github.com/router-for-me/model-fallback-chain

go 1.26.0

require (
	github.com/router-for-me/CLIProxyAPI/v7 v7.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/sys v0.38.0 // indirect
)

// Local development: point to the workspace copy of CLIProxyAPI.
// Remove this replace directive when publishing to use the real version tag.
replace github.com/router-for-me/CLIProxyAPI/v7 => ../CLIProxyAPI
