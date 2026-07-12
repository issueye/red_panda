module redpanda/agent

go 1.25.0

require (
	redpanda/ipc v0.0.0
	redpanda/protocol v0.0.0
)

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	golang.org/x/net v0.57.0
	golang.org/x/sys v0.47.0 // indirect
)

replace (
	redpanda/ipc => ../ipc
	redpanda/protocol => ../protocol
)
