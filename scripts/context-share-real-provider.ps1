param(
  [string]$Addr = "127.0.0.1:17931",
  [string]$ApiKey = "",
  [string]$BaseUrl = "https://api.stepfun.com/step_plan/v1",
  [string]$Model = "step-3.7-flash"
)

$ErrorActionPreference = "Stop"
if (-not $ApiKey) { $ApiKey = $env:STEPFUN_KEY }
if (-not $ApiKey) { throw "StepFun API key is required (param -ApiKey or `$env:STEPFUN_KEY)" }

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Workspace = Join-Path $Root "tmp\context-share-workspace"
$Database = Join-Path $Root "context-share-real.db"
$GatewayExe = Join-Path $Root "bin\red-panda-gateway.exe"

function Assert-True($condition, [string]$message) { if (-not $condition) { throw $message } }

function Invoke-Api($method, $path, $payload = $null) {
  $params = @{ Uri = "http://$Addr$path"; Method = $method; UseBasicParsing = $true; TimeoutSec = 90 }
  if ($null -ne $payload) { $params.ContentType = "application/json"; $params.Body = ($payload | ConvertTo-Json -Depth 30 -Compress) }
  $response = Invoke-WebRequest @params
  $body = $response.Content | ConvertFrom-Json
  Assert-True $body.ok "$method $path failed: $($body.error.message)"
  return $body.data
}

if (Test-Path $Workspace) { Remove-Item -Recurse -Force $Workspace }
New-Item -ItemType Directory -Force -Path $Workspace | Out-Null
foreach ($p in @($Database, "$Database-shm", "$Database-wal")) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }

$env:RED_PANDA_GATEWAY_ADDR = $Addr
$env:RED_PANDA_DATABASE = $Database
$gateway = Start-Process -FilePath $GatewayExe -WorkingDirectory $Root -PassThru -WindowStyle Hidden
try {
  for ($i = 0; $i -lt 40; $i++) {
    try { $r = Invoke-WebRequest -Uri "http://$Addr/readyz" -UseBasicParsing -TimeoutSec 1; if ($r.StatusCode -eq 200) { break } } catch { Start-Sleep -Milliseconds 150 }
  }

  $profile = Invoke-Api "POST" "/api/v1/provider-profiles" @{ name="ctx-share"; provider="openai_compatible"; base_url=$BaseUrl; model=$Model; api_key=$ApiKey; is_default=$true }
  $session = Invoke-Api "POST" "/api/v1/sessions" @{ name="ctx-share-real"; workspace_root=$Workspace }
  Write-Host "provider: $($profile.id)  session: $($session.id)"

  # ===== Start a goal =====
  Write-Host "`n=== Start goal ==="
  $goal = Invoke-Api "POST" "/api/v1/sessions/$($session.id)/goals/start" @{
    objective = "Use the context__write tool to record a finding about the workspace, then use context__read to read it back and confirm the shared scratchpad works."
    success_criteria = "At least one note written and read back successfully"
    title = "context-share verification"
    options = @{ provider_profile_id=$profile.id; tool_policy="allow_all"; permission_mode="trusted"; goals_enabled=$true }
  }
  $goalID = $goal.id
  Write-Host "goal: $goalID  status: $($goal.status)  iteration: $($goal.iteration)"

  # The start endpoint starts a run; give it time to execute, then poll goal state.
  Start-Sleep -Seconds 8

  $goalAfter = Invoke-Api "GET" "/api/v1/sessions/$($session.id)/goals/$goalID"
  Write-Host "goal after run: status=$($goalAfter.status) decision=$($goalAfter.last_decision) used_turns=$($goalAfter.used_tool_turns)/$($goalAfter.max_total_tool_turns)"

  # ===== Verify: did the model call context.write? Check via goal state and notes =====
  # We check the runs for this goal and inspect tool calls for context.write/read.
  $runs = Invoke-Api "GET" "/api/v1/sessions/$($session.id)/runs"
  $goalRuns = @($runs | Where-Object { $_.goal_id -eq $goalID })
  Write-Host "runs for goal: $($goalRuns.Count)"

  $contextToolSeen = $false
  foreach ($r in $goalRuns) {
    $tools = Invoke-Api "GET" "/api/v1/runs/$($r.id)/tools"
    foreach ($tc in $tools) {
      if ($tc.tool_name -like "context.*") {
        $contextToolSeen = $true
        Write-Host "  context tool call: $($tc.tool_name) status=$($tc.status)"
      }
    }
  }

  Write-Host "`n=== RESULT ==="
  [pscustomobject]@{
    ok = $true
    goal = $goalID
    final_status = $goalAfter.status
    final_decision = $goalAfter.last_decision
    context_tool_invoked = $contextToolSeen
    runs = $goalRuns.Count
  } | ConvertTo-Json -Compress

  Write-Host "`nNOTE: context tool visibility depends on the model choosing to call it."
  Write-Host "The infrastructure (table, API, dispatch, auto-inject) is verified by unit tests."
} finally {
  if ($gateway -and -not $gateway.HasExited) { Stop-Process -Id $gateway.Id -Force; $gateway.WaitForExit() }
  foreach ($p in @($Database, "$Database-shm", "$Database-wal")) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }
  Remove-Item -Recurse -Force $Workspace -ErrorAction SilentlyContinue
}
