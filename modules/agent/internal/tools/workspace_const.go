package tools

// Workspace tool shared constants.
// Implementation is split across workspace_{path,read,write,slash}.go (docs/41 W5-1).
const maxGrepFileBytes = 2 * 1024 * 1024
const defaultListDepth = 3
const WorkerSummaryTurns = 8
const maxListEntries = 500
const defaultGrepMatches = 100
const maxPatchBytes = 256 * 1024
