param(
  [string]$Addr = "127.0.0.1:17921",
  [string]$ApiKey = "",            # pass StepFun key, or set $env:STEPFUN_KEY
  [string]$BaseUrl = "https://api.stepfun.com/step_plan/v1",
  [string]$Model = "step-3.7-flash"
)

$ErrorActionPreference = "Stop"

if (-not $ApiKey) { $ApiKey = $env:STEPFUN_KEY }
if (-not $ApiKey) { throw "StepFun API key is required (param -ApiKey or `$env:STEPFUN_KEY)" }

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Workspace = Join-Path $Root "tmp\skills-real-workspace"
$Database = Join-Path $Root "skills-real-red-panda.db"
$GatewayExe = Join-Path $Root "bin\red-panda-gateway.exe"
$AgentExe = Join-Path $Root "bin\red-panda-agent.exe"

function Assert-True($condition, [string]$message) {
  if (-not $condition) { throw $message }
}

function Test-JsonProperty($obj, [string]$name) {
  return $null -ne $obj -and $null -ne $obj.PSObject.Properties[$name]
}

function Invoke-Api($method, $path, $payload = $null) {
  $params = @{ Uri = "http://$Addr$path"; Method = $method; UseBasicParsing = $true; TimeoutSec = 60 }
  if ($null -ne $payload) {
    $params.ContentType = "application/json"
    $params.Body = ($payload | ConvertTo-Json -Depth 30 -Compress)
  }
  $response = Invoke-WebRequest @params
  $body = $response.Content | ConvertFrom-Json
  Assert-True $body.ok "$method $path did not return ok envelope"
  return $body.data
}

function New-WsClient {
  $ws = [System.Net.WebSockets.ClientWebSocket]::new()
  $ws.ConnectAsync([Uri]"ws://$Addr/api/v1/ws", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $ws
}

function Send-WsJson($ws, $obj) {
  $json = $obj | ConvertTo-Json -Depth 30 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($json)
  $ws.SendAsync([ArraySegment[byte]]::new($bytes), [System.Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}

function Read-WsMessage($ws, [int]$timeoutMs = 120000) {
  $buffer = New-Object byte[] 65536
  $segment = [ArraySegment[byte]]::new($buffer)
  $builder = New-Object System.Text.StringBuilder
  do {
    $task = $ws.ReceiveAsync($segment, [Threading.CancellationToken]::None)
    if (-not $task.Wait($timeoutMs)) { throw "websocket receive timed out" }
    $result = $task.GetAwaiter().GetResult()
    if ($result.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) { throw "websocket closed" }
    [void]$builder.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
  } while (-not $result.EndOfMessage)
  return ($builder.ToString() | ConvertFrom-Json)
}

function Wait-WsResponseWithEvents($ws, [string]$id, [ref]$events, [int]$timeoutMs = 120000) {
  $deadline = [DateTime]::UtcNow.AddMilliseconds($timeoutMs)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws $timeoutMs
    if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
      $events.Value += $msg.payload
      # auto-approve any permission request so the run is not blocked
      if ($msg.payload.type -eq "permission_required") {
        Send-WsJson $ws @{
          id = "auto_perm_$($msg.payload.payload.permission_id)"
          type = "request"
          method = "permission.resolve"
          payload = @{
            permission_id = $msg.payload.payload.permission_id
            run_id = $msg.payload.root_run_id
            decision = "approve"
            reason = "skills-real auto approve"
          }
        }
      }
      continue
    }
    if ($msg.id -eq $id) { return $msg }
  }
  throw "websocket response $id not received"
}

function Wait-RunFinish($ws, [ref]$events, [int]$timeoutMs = 180000) {
  $deadline = [DateTime]::UtcNow.AddMilliseconds($timeoutMs)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws $timeoutMs
    if ($msg.type -ne "event" -or $msg.method -ne "run.event") { continue }
    $events.Value += $msg.payload
    if ($msg.payload.type -eq "permission_required") {
      Send-WsJson $ws @{
        id = "auto_perm2_$($msg.payload.payload.permission_id)"
        type = "request"; method = "permission.resolve"
        payload = @{
          permission_id = $msg.payload.payload.permission_id
          run_id = $msg.payload.root_run_id
          decision = "approve"; reason = "skills-real auto approve"
        }
      }
    }
    if ($msg.payload.type -eq "finish") { return $msg.payload }
  }
  throw "run did not finish within $timeoutMs ms"
}

# --- fresh workspace + database ---
if (Test-Path $Workspace) { Remove-Item -Recurse -Force $Workspace }
New-Item -ItemType Directory -Force -Path $Workspace | Out-Null
foreach ($p in @($Database, "$Database-shm", "$Database-wal")) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }

$env:RED_PANDA_GATEWAY_ADDR = $Addr
$env:RED_PANDA_DATABASE = $Database

$gateway = Start-Process -FilePath $GatewayExe -WorkingDirectory $Root -PassThru -WindowStyle Hidden
try {
  $ready = $false
  for ($i = 0; $i -lt 40; $i++) {
    try {
      $r = Invoke-WebRequest -Uri "http://$Addr/readyz" -UseBasicParsing -TimeoutSec 1
      if ($r.StatusCode -eq 200) { $ready = $true; break }
    } catch { Start-Sleep -Milliseconds 150 }
  }
  Assert-True $ready "gateway did not become ready"

  # provider profile with the real StepFun key
  $profile = Invoke-Api "POST" "/api/v1/provider-profiles" @{
    name = "stepfun-skills-real"
    provider = "openai_compatible"
    base_url = $BaseUrl
    model = $Model
    api_key = $ApiKey
    is_default = $true
  }
  Assert-True ($profile.id -ne "") "provider profile create missing id"
  Write-Host "provider profile: $($profile.id) (key set=$($profile.api_key_set))"

  $session = Invoke-Api "POST" "/api/v1/sessions" @{ name = "skills-real"; workspace_root = $Workspace }
  Assert-True ($session.id -ne "") "session create missing id"

  # ===== TEST 1: skill CREATE via real provider =====
  Write-Host "`n=== TEST 1: skill.create via real provider ($Model) ==="
  $ws = New-WsClient
  try {
    Send-WsJson $ws @{
      id = "create_run"; type = "request"; method = "run.start"
      payload = @{
        session_id = $session.id
        input = @{ text = "Create a managed skill named 'summarizer' with description 'Summarize text concisely.' and instructions '# Summarizer`n`nRead the provided text and write a 2-sentence summary.' Use the skill__create tool." }
        options = @{
          working_dir = $Workspace
          provider_profile_id = $profile.id
          tool_policy = "allow_all"
          permission_mode = "trusted"
        }
        subscribe = $true
      }
    }
    $createEvents = @()
    $createResp = Wait-WsResponseWithEvents $ws "create_run" ([ref]$createEvents)
    Assert-True ($createResp.type -eq "response" -and $createResp.payload.accepted) "create run.start mismatch"
    $createFinish = Wait-RunFinish $ws ([ref]$createEvents)
    Write-Host "create run status: $($createFinish.payload.status)"

    $skillPath = Join-Path $Workspace ".codex\skills\summarizer\SKILL.md"
    Assert-True (Test-Path $skillPath) "SKILL.md was NOT created at $skillPath"
    $skillContent = Get-Content -Raw $skillPath
    Write-Host "SKILL.md created ($($skillContent.Length) bytes)"
    Write-Host "--- SKILL.md ---"; Write-Host $skillContent

    $skillList = Invoke-Api "GET" "/api/v1/skills?workspace_root=$([uri]::EscapeDataString($Workspace))"
    Assert-True (@($skillList.items | Where-Object { $_.name -eq "summarizer" }).Count -eq 1) "created skill not listed by gateway"
    Write-Host "gateway lists skill: $($skillList.items[0].name) - $($skillList.items[0].description)"
  } finally { $ws.Dispose() }

  # ===== TEST 2: skill RUN via real provider =====
  Write-Host "`n=== TEST 2: skill.run via real provider ($Model) ==="
  $ws2 = New-WsClient
  try {
    Send-WsJson $ws2 @{
      id = "run_run"; type = "request"; method = "run.start"
      payload = @{
        session_id = $session.id
        input = @{ text = "Run the managed skill 'summarizer' on this task: summarize the sentence 'The quick brown fox jumps over the lazy dog while a distant bell rings twice.' Use the skill__run tool with name=summarizer." }
        options = @{
          working_dir = $Workspace
          provider_profile_id = $profile.id
          tool_policy = "allow_all"
          permission_mode = "trusted"
        }
        subscribe = $true
      }
    }
    $runEvents = @()
    $runResp = Wait-WsResponseWithEvents $ws2 "run_run" ([ref]$runEvents)
    Assert-True ($runResp.type -eq "response" -and $runResp.payload.accepted) "skill.run start mismatch"
    $runFinish = Wait-RunFinish $ws2 ([ref]$runEvents)
    Write-Host "skill.run status: $($runFinish.payload.status)"

    $subagentEvents = @($runEvents | Where-Object { $_.type -eq "subagent_update" })
    Write-Host "subagent_update events: $($subagentEvents.Count)"
    foreach ($e in $subagentEvents) { Write-Host "  - $($e.payload.status): $($e.payload.summary)" }

    # collect assistant message text (the final root result includes skill output)
    $deltas = @($runEvents | Where-Object { $_.type -eq "message_delta" -and $_.agent.role -ne "subagent" })
    $finalText = ($deltas | ForEach-Object { $_.payload.delta }) -join ""
    Write-Host "`nroot final message (skill result excerpt):"
    Write-Host $finalText.Substring(0, [Math]::Min(400, $finalText.Length))
  } finally { $ws2.Dispose() }

  Write-Host "`n=== RESULT ==="
  [pscustomobject]@{
    ok = $true
    profile = $profile.id
    session = $session.id
    skill_created = (Test-Path $skillPath)
    create_status = $createFinish.payload.status
    run_status = $runFinish.payload.status
    subagent_updates = $subagentEvents.Count
  } | ConvertTo-Json -Compress
} finally {
  if ($gateway -and -not $gateway.HasExited) {
    Stop-Process -Id $gateway.Id -Force
    $gateway.WaitForExit()
  }
  foreach ($p in @($Database, "$Database-shm", "$Database-wal")) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }
  # leave the workspace .codex/skills for inspection but print its location
  Write-Host "`nWorkspace left at: $Workspace"
}
