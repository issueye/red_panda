package internal

// Workspace tool shared constants — copied from tools/workspace_const.go
// and tools/coding.go to keep values identical.
//
// Internal lowercase names are used by the implementation functions in
// this package; Default* constants are exported for the workspace
// package to reference them in Definition Parameters.
const maxToolOutputBytes = 64 * 1024
const maxGrepFileBytes = 2 * 1024 * 1024
const maxListEntries = 500
const maxPatchBytes = 256 * 1024
const WorkerSummaryTurns = 16

const DefaultListDepth = 3
const DefaultGrepMatches = 100
const DefaultFindFiles = 200
const DefaultReadFiles = 12
