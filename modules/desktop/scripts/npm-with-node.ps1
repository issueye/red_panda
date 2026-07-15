param(
  [Parameter(Mandatory = $true)]
  [ValidateSet('install', 'run')]
  [string]$Command,

  [string]$Script = '',

  [int]$Port = 0
)

$ErrorActionPreference = 'Stop'

function Test-ViteNodeVersion([version]$Version) {
  return (($Version.Major -eq 20 -and $Version -ge [version]'20.19.0') -or
    $Version -ge [version]'22.12.0')
}

function Get-CurrentNodeVersion {
  try {
    $raw = (& node --version 2>$null).Trim().TrimStart('v')
    return [version]$raw
  } catch {
    return $null
  }
}

$currentVersion = Get-CurrentNodeVersion
[string[]]$npmArgs = @(
  if ($Command -eq 'install') {
    'install'
  } elseif ($Script -eq 'dev') {
    if ($Port -le 0) { throw 'A positive -Port is required for the dev script.' }
    'run', 'dev', '--', '--port', [string]$Port, '--strictPort'
  } elseif ($Script) {
    'run', $Script, '-q'
  } else {
    throw '-Script is required when -Command is run.'
  }
)

if ($currentVersion -and (Test-ViteNodeVersion $currentVersion)) {
  & npm @npmArgs
  exit $LASTEXITCODE
}

$versionsRoot = Join-Path $env:USERPROFILE '.nvmd\versions'
$candidates = @()
if (Test-Path -LiteralPath $versionsRoot) {
  $candidates = Get-ChildItem -LiteralPath $versionsRoot -Directory | ForEach-Object {
    try {
      [PSCustomObject]@{ Directory = $_; Version = [version]$_.Name }
    } catch {
      $null
    }
  } | Sort-Object Version -Descending
}

$selected = $candidates | Where-Object { Test-ViteNodeVersion $_.Version } | Select-Object -First 1
if (-not $selected) {
  # Node 22.3+ runs the current toolchain, although Vite reports it as unsupported.
  $selected = $candidates | Where-Object {
    $_.Version.Major -eq 22 -and $_.Version -ge [version]'22.3.0'
  } | Select-Object -First 1
}

if (-not $selected) {
  throw 'Vite requires Node 20.19+ or 22.12+. Install a supported Node version with NVM Desktop.'
}

$node = Join-Path $selected.Directory.FullName 'node.exe'
$npmCli = Join-Path $selected.Directory.FullName 'node_modules\npm\bin\npm-cli.js'
if (-not (Test-Path -LiteralPath $node) -or -not (Test-Path -LiteralPath $npmCli)) {
  throw "Node or npm CLI was not found for Node $($selected.Version)."
}

Write-Host "Using Node $($selected.Version) for frontend tasks (current: $currentVersion)."
$env:Path = "$($selected.Directory.FullName);$env:Path"
& $node $npmCli @npmArgs
exit $LASTEXITCODE
