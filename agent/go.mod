module probe/agent

go 1.26.9

require (
	github.com/coder/websocket v1.8.14
	github.com/prometheus-community/pro-bing v0.9.1
	golang.org/x/sys v0.47.0
	gopkg.in/yaml.v3 v3.0.1
	probe/common v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/net v0.56.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
)

replace probe/common => ../common
