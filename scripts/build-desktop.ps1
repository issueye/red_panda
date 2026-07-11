# Build red_panda desktop for Windows with embedded app icon + version info.
# Usage (from repo root):
#   pwsh -File scripts/build-desktop.ps1

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$Desktop = Join-Path $Root "modules\desktop"
$Build = Join-Path $Desktop "build"
$Arch = if ($env:GOARCH) { $env:GOARCH } else { "amd64" }

Write-Host "==> frontend"
$node22 = Join-Path $env:USERPROFILE ".nvmd\versions\22.3.0"
if (Test-Path $node22) {
  $env:Path = "$node22;$env:Path"
}
Push-Location (Join-Path $Desktop "frontend")
try {
  npm run build
  if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }
} finally {
  Pop-Location
}

Write-Host "==> icons (appicon.png -> windows/icon.ico)"
Push-Location $Build
try {
  if (-not (Test-Path "appicon.png")) {
    throw "missing build/appicon.png"
  }
  wails3 generate icons -input appicon.png -macfilename darwin/icons.icns -windowsfilename windows/icon.ico
  if ($LASTEXITCODE -ne 0) { throw "icon generation failed" }

  Write-Host "==> syso (embed icon + version into PE resources)"
  $syso = Join-Path $Desktop "wails_windows_$Arch.syso"
  wails3 generate syso -arch $Arch -icon windows/icon.ico -manifest windows/wails.exe.manifest -info windows/info.json -out $syso
  if ($LASTEXITCODE -ne 0) { throw "syso generation failed" }
} finally {
  Pop-Location
}

Write-Host "==> go build"
Push-Location $Desktop
try {
  $env:CGO_ENABLED = "0"
  $env:GOOS = "windows"
  $env:GOARCH = $Arch
  New-Item -ItemType Directory -Force -Path "bin" | Out-Null
  go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o "bin\red_panda.exe" .
  if ($LASTEXITCODE -ne 0) { throw "go build failed" }

  $outRoot = Join-Path $Root "bin"
  New-Item -ItemType Directory -Force -Path $outRoot | Out-Null
  Copy-Item -Force "bin\red_panda.exe" (Join-Path $outRoot "red_panda.exe")
  Write-Host "built: $(Join-Path $outRoot 'red_panda.exe')"
} finally {
  Remove-Item -Force "wails_windows_$Arch.syso" -ErrorAction SilentlyContinue
  Pop-Location
}
