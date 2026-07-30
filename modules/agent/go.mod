module redpanda/agent

go 1.25.5

require (
	github.com/dop251/goja v0.0.0-20260723142020-b4aef50fa347
	redpanda/ipc v0.0.0
	redpanda/mcpkit v0.0.0-00010101000000-000000000000
	redpanda/protocol v0.0.0
)

require (
	github.com/dlclark/regexp2/v2 v2.5.2 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mark3labs/mcp-go v0.52.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/text v0.40.0 // indirect
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	golang.org/x/net v0.57.0
	golang.org/x/sys v0.47.0 // indirect
)

replace (
	redpanda/ipc => ../ipc
	redpanda/mcpkit => ../mcpkit
	redpanda/protocol => ../protocol
)
