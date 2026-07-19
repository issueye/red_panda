// Package ipc provides a cross-platform local IPC transport used by default
// for newline-delimited JSON-RPC between processes
// (for example gateway ↔ agent, or parent agent ↔ worker process).
//
// Transport backends:
//
//   - Windows: named pipes via github.com/Microsoft/go-winio
//   - Unix (Linux/macOS): AF_UNIX domain sockets
//
// Typical parent/child handshake:
//
//  1. Parent calls ListenTemp (or Listen) and gets a Listener + Addr.
//  2. Parent starts the child with ChildEnv(addr) so RED_PANDA_IPC_ADDR is set.
//  3. Parent Accepts the connection (with context deadline as needed).
//  4. Child calls DialFromEnv (or Dial) and uses the Conn for bidirectional NDJSON.
//
// Framing helpers in Stream keep the existing "one JSON-RPC message per line"
// wire format, so higher layers can swap stdio for IPC without changing the
// JSON-RPC protocol itself.
package ipc
