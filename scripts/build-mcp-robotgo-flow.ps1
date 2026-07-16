# Build the robotgo-flow MCP stdio server into bin/.
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$env:CGO_ENABLED = '0'
$out = Join-Path $root 'bin\mcp-robotgo-flow.exe'
Write-Host "Building $out ..."
go test ./modules/mcp-servers/robotgo-flow/ -count=1
go build -o $out ./modules/mcp-servers/robotgo-flow
Get-Item $out | Format-List Name, Length, LastWriteTime
Write-Host "OK. Register in Gateway MCP with ROBOTGO_FLOW_COMMAND pointing at robotgo-flow.exe"
