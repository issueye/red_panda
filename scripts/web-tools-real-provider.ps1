param(
  [string]$Addr = "127.0.0.1:17922",
  [string]$ApiKey = "",
  [string]$BaseUrl = "https://api.stepfun.com/step_plan/v1",
  [string]$Model = "step-3.7-flash"
)

$ErrorActionPreference = "Stop"
if (-not $ApiKey) { $ApiKey = $env:STEPFUN_KEY }
if (-not $ApiKey) { throw "StepFun API key is required (param -ApiKey or `$env:STEPFUN_KEY)" }

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$Workspace = Join-Path $Root "tmp\web-tools-workspace"
$Database = Join-Path $Root "web-tools-real.db"
$GatewayExe = Join-Path $Root "bin\red-panda-gateway.exe"

function Assert-True($condition, [string]$message) { if (-not $condition) { throw $message } }

function Invoke-Api($method, $path, $payload = $null) {
  $params = @{ Uri = "http://$Addr$path"; Method = $method; UseBasicParsing = $true; TimeoutSec = 60 }
  if ($null -ne $payload) { $params.ContentType = "application/json"; $params.Body = ($payload | ConvertTo-Json -Depth 30 -Compress) }
  $response = Invoke-WebRequest @params
  $body = $response.Content | ConvertFrom-Json
  Assert-True $body.ok "$method $path failed"
  return $body.data
}

function New-WsClient {
  $ws = [System.Net.WebSockets.ClientWebSocket]::new()
  $ws.ConnectAsync([Uri]"ws://$Addr/api/v1/ws", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $ws
}
function Send-WsJson($ws, $obj) {
  $bytes = [Text.Encoding]::UTF8.GetBytes(($obj | ConvertTo-Json -Depth 30 -Compress))
  $ws.SendAsync([ArraySegment[byte]]::new($bytes), [System.Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
}
function Read-WsMessage($ws, [int]$timeoutMs = 120000) {
  $buffer = New-Object byte[] 65536; $segment = [ArraySegment[byte]]::new($buffer); $builder = New-Object System.Text.StringBuilder
  do {
    $task = $ws.ReceiveAsync($segment, [Threading.CancellationToken]::None)
    if (-not $task.Wait($timeoutMs)) { throw "websocket receive timed out" }
    $result = $task.GetAwaiter().GetResult()
    if ($result.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) { throw "websocket closed" }
    [void]$builder.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
  } while (-not $result.EndOfMessage)
  return ($builder.ToString() | ConvertFrom-Json)
}
function Wait-RunFinish($ws, [ref]$events, [int]$timeoutMs = 180000) {
  $deadline = [DateTime]::UtcNow.AddMilliseconds($timeoutMs)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws $timeoutMs
    if ($msg.type -ne "event" -or $msg.method -ne "run.event") { continue }
    $events.Value += $msg.payload
    if ($msg.payload.type -eq "permission_required") {
      Send-WsJson $ws @{ id = "perm_$($msg.payload.payload.permission_id)"; type = "request"; method = "permission.resolve"
        payload = @{ permission_id = $msg.payload.payload.permission_id; run_id = $msg.payload.root_run_id; decision = "approve"; reason = "web-tools" } }
    }
    if ($msg.payload.type -eq "finish") { return $msg.payload }
  }
  throw "run did not finish"
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

  $profile = Invoke-Api "POST" "/api/v1/provider-profiles" @{ name="web-tools"; provider="openai_compatible"; base_url=$BaseUrl; model=$Model; api_key=$ApiKey; is_default=$true }
  $session = Invoke-Api "POST" "/api/v1/sessions" @{ name="web-tools-real"; workspace_root=$Workspace }
  Write-Host "provider: $($profile.id)  session: $($session.id)"

  # ===== TEST 1: web.search =====
  Write-Host "`n=== TEST 1: web.search ==="
  $ws = New-WsClient
  try {
    Send-WsJson $ws @{ id="search_run"; type="request"; method="run.start"; payload=@{
      session_id=$session.id
      input=@{ text="Use the web__search tool to search for 'Go programming language official site' and report the top result title and URL." }
      options=@{ working_dir=$Workspace; provider_profile_id=$profile.id; tool_policy="allow_all"; permission_mode="trusted"; web_search_max_results=5 }
      subscribe=$true
    }}
    $events = @()
    $finish = Wait-RunFinish $ws ([ref]$events)
    Write-Host "search run status: $($finish.payload.status)"
    $toolOut = @($events | Where-Object { $_.type -eq "tool_output" })
    if ($toolOut.Count -gt 0) { Write-Host "tool_output excerpt: $($toolOut[-1].payload.delta.Substring(0,[Math]::Min(300,$toolOut[-1].payload.delta.Length)))" }
    $assistantMsg = (@($events | Where-Object { $_.type -eq "message_delta" -and $_.agent.role -ne "subagent" }) | ForEach-Object { $_.payload.delta }) -join ""
    Write-Host "assistant excerpt: $($assistantMsg.Substring(0,[Math]::Min(300,$assistantMsg.Length)))"
  } finally { $ws.Dispose() }

  # ===== TEST 2: web.fetch =====
  Write-Host "`n=== TEST 2: web.fetch ==="
  $ws2 = New-WsClient
  try {
    Send-WsJson $ws2 @{ id="fetch_run"; type="request"; method="run.start"; payload=@{
      session_id=$session.id
      input=@{ text="Use the web__fetch tool to fetch https://example.com and describe what the page says in one sentence." }
      options=@{ working_dir=$Workspace; provider_profile_id=$profile.id; tool_policy="allow_all"; permission_mode="trusted"; web_fetch_max_bytes=1048576 }
      subscribe=$true
    }}
    $events2 = @()
    $finish2 = Wait-RunFinish $ws2 ([ref]$events2)
    Write-Host "fetch run status: $($finish2.payload.status)"
    $toolOut2 = @($events2 | Where-Object { $_.type -eq "tool_output" })
    if ($toolOut2.Count -gt 0) { Write-Host "tool_output excerpt: $($toolOut2[-1].payload.delta.Substring(0,[Math]::Min(300,$toolOut2[-1].payload.delta.Length)))" }
    $assistantMsg2 = (@($events2 | Where-Object { $_.type -eq "message_delta" -and $_.agent.role -ne "subagent" }) | ForEach-Object { $_.payload.delta }) -join ""
    Write-Host "assistant excerpt: $($assistantMsg2.Substring(0,[Math]::Min(300,$assistantMsg2.Length)))"
  } finally { $ws2.Dispose() }

  Write-Host "`n=== RESULT ==="
  [pscustomobject]@{ ok=$true; search_status=$finish.payload.status; fetch_status=$finish2.payload.status } | ConvertTo-Json -Compress
} finally {
  if ($gateway -and -not $gateway.HasExited) { Stop-Process -Id $gateway.Id -Force; $gateway.WaitForExit() }
  foreach ($p in @($Database, "$Database-shm", "$Database-wal")) { Remove-Item -LiteralPath $p -ErrorAction SilentlyContinue }
  Remove-Item -Recurse -Force $Workspace -ErrorAction SilentlyContinue
}
