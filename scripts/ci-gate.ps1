# red_panda local CI gate (docs/36 track D6).
# Runs the minimum correctness suite that every PR should pass:
#   protocol + agent + gateway unit tests, then desktop unit tests.
# Optional: -WithProtocolCompat to exercise scripts/protocol-compat.ps1
#           (starts a local gateway; slower, needs free ports).

param(
  [switch]$WithProtocolCompat,
  [switch]$SkipDesktop
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location $Root

function Write-Step([string]$message) {
  Write-Host ""
  Write-Host "==> $message" -ForegroundColor Cyan
}

function Invoke-Checked([string]$label, [scriptblock]$action) {
  Write-Step $label
  & $action
  if ($LASTEXITCODE -ne 0 -and $null -ne $LASTEXITCODE) {
    throw "ci-gate failed: $label (exit $LASTEXITCODE)"
  }
}

$env:CGO_ENABLED = "0"

Invoke-Checked "go test ./modules/protocol/..." {
  go test ./modules/protocol/...
}

Invoke-Checked "go test ./modules/agent/..." {
  go test ./modules/agent/...
}

Invoke-Checked "go test ./modules/gateway/..." {
  go test ./modules/gateway/...
}

if (-not $SkipDesktop) {
  $frontend = Join-Path $Root "modules\desktop\frontend"
  if (-not (Test-Path (Join-Path $frontend "package.json"))) {
    throw "desktop frontend package.json not found at $frontend"
  }
  Invoke-Checked "desktop unit tests (npm test -- --run)" {
    Push-Location $frontend
    try {
      # package.json uses node --test; "--run" is accepted as a passthrough no-op
      # by some runners and kept for docs/36 compatibility.
      npm test -- --run
    } finally {
      Pop-Location
    }
  }
}

if ($WithProtocolCompat) {
  $compat = Join-Path $Root "scripts\protocol-compat.ps1"
  if (-not (Test-Path $compat)) {
    throw "protocol-compat script missing: $compat"
  }
  Invoke-Checked "protocol-compat.ps1" {
    powershell -NoProfile -File $compat
  }
}

Write-Host ""
Write-Host "ci-gate OK" -ForegroundColor Green
