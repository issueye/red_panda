param(
  [string]$Addr = "127.0.0.1:17911"
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$GatewayExe = Join-Path $Root "bin\red-panda-gateway.exe"
$Database = Join-Path $Root "protocol-compat-red-panda.db"
$ProviderAddr = "127.0.0.1:17912"
$MissingSubAgentExe = Join-Path $Root "bin\missing-red-panda-subagent.exe"

function Remove-CompatDatabase {
  foreach ($path in @($Database, "$Database-shm", "$Database-wal")) {
    Remove-Item -LiteralPath $path -ErrorAction SilentlyContinue
  }
}

function Assert-True($condition, [string]$message) {
  if (-not $condition) {
    throw $message
  }
}

function Test-JsonProperty($obj, [string]$name) {
  return $null -ne $obj -and $null -ne $obj.PSObject.Properties[$name]
}

function Invoke-Api($method, $path, $payload = $null) {
  $params = @{
    Uri = "http://$Addr$path"
    Method = $method
    UseBasicParsing = $true
    TimeoutSec = 5
  }
  if ($null -ne $payload) {
    $params.ContentType = "application/json"
    $params.Body = ($payload | ConvertTo-Json -Depth 30 -Compress)
  }
  $response = Invoke-WebRequest @params
  $body = $response.Content | ConvertFrom-Json
  Assert-True $body.ok "$method $path did not return ok envelope"
  Assert-True (Test-JsonProperty $body "data") "$method $path did not return data"
  return $body.data
}

function Read-WsMessage($ws, [int]$timeoutMs = 12000) {
  $buffer = New-Object byte[] 65536
  $segment = [ArraySegment[byte]]::new($buffer)
  $builder = New-Object System.Text.StringBuilder
  do {
    $task = $ws.ReceiveAsync($segment, [Threading.CancellationToken]::None)
    if (-not $task.Wait($timeoutMs)) {
      throw "websocket receive timed out"
    }
    $result = $task.GetAwaiter().GetResult()
    if ($result.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) {
      throw "websocket closed"
    }
    [void]$builder.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
  } while (-not $result.EndOfMessage)
  return ($builder.ToString() | ConvertFrom-Json)
}

function Send-WsJson($ws, $obj) {
  $json = $obj | ConvertTo-Json -Depth 30 -Compress
  Send-WsText $ws $json
}

function Send-WsText($ws, [string]$text) {
  $bytes = [Text.Encoding]::UTF8.GetBytes($text)
  $ws.SendAsync(
    [ArraySegment[byte]]::new($bytes),
    [System.Net.WebSockets.WebSocketMessageType]::Text,
    $true,
    [Threading.CancellationToken]::None
  ).GetAwaiter().GetResult() | Out-Null
}

function New-WsClient {
  $ws = [System.Net.WebSockets.ClientWebSocket]::new()
  $ws.ConnectAsync([Uri]"ws://$Addr/api/v1/ws", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $ws
}

function Wait-WsResponse($ws, [string]$id) {
  $events = @()
  return Wait-WsResponseWithEvents $ws $id ([ref]$events)
}

function Wait-WsResponseWithEvents($ws, [string]$id, [ref]$events) {
  $deadline = [DateTime]::UtcNow.AddSeconds(12)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws
    if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
      $events.Value += $msg.payload
      continue
    }
    if ($msg.id -eq $id) {
      return $msg
    }
  }
  throw "websocket response $id not received"
}

function Wait-RunFinish($ws) {
  $events = @()
  return Wait-RunFinishWithInitialEvents $ws $events
}

function Wait-RunFinishWithInitialEvents($ws, $initialEvents) {
  $events = @($initialEvents)
  if ($events.Count -gt 0 -and $events[-1].type -eq "finish") {
    return $events
  }
  $deadline = [DateTime]::UtcNow.AddSeconds(15)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws
    if ($msg.type -ne "event" -or $msg.method -ne "run.event") {
      continue
    }
    $event = $msg.payload
    $events += $event
    if ($event.type -eq "finish") {
      return $events
    }
  }
  throw "run did not finish"
}

function Assert-IncreasingRootSeq($events) {
  $last = 0
  foreach ($event in $events) {
    $seq = [uint64]$event.root_seq
    Assert-True ($seq -gt $last) "root_seq is not strictly increasing"
    $last = $seq
  }
}

function Assert-EventTypes($events, [string[]]$expected) {
  $actual = @($events | ForEach-Object { $_.type })
  foreach ($type in $expected) {
    Assert-True ($actual -contains $type) "expected event type $type, actual=$($actual -join ",")"
  }
}

function Select-EventsByType($events, [string]$type) {
  return @($events | Where-Object { $_.type -eq $type })
}

function Start-CompatProvider {
  $job = Start-Job -ScriptBlock {
    param($prefix)
    $listener = [System.Net.HttpListener]::new()
    $listener.Prefixes.Add($prefix)
    $listener.Start()
    try {
      while ($listener.IsListening) {
        $ctx = $listener.GetContext()
        $body = '{"choices":[{"message":{"content":"provider profile compat ok"}}]}'
        $bytes = [Text.Encoding]::UTF8.GetBytes($body)
        $ctx.Response.ContentType = "application/json"
        $ctx.Response.StatusCode = 200
        $ctx.Response.OutputStream.Write($bytes, 0, $bytes.Length)
        $ctx.Response.Close()
      }
    } catch {
    } finally {
      $listener.Close()
    }
  } -ArgumentList "http://$ProviderAddr/"
  for ($i = 0; $i -lt 20; $i++) {
    try {
      $response = Invoke-WebRequest -Uri "http://$ProviderAddr/healthz" -UseBasicParsing -TimeoutSec 1
      if ($response.StatusCode -eq 200) {
        return $job
      }
    } catch {
      Start-Sleep -Milliseconds 100
    }
  }
  return $job
}

Remove-CompatDatabase
$env:RED_PANDA_GATEWAY_ADDR = $Addr
$env:RED_PANDA_DATABASE = $Database
$previousSubAgentCommand = $env:RED_PANDA_SUBAGENT_COMMAND
$env:RED_PANDA_SUBAGENT_COMMAND = $MissingSubAgentExe

$gateway = Start-Process -FilePath $GatewayExe -WorkingDirectory $Root -PassThru -WindowStyle Hidden
$providerJob = $null

try {
  $ready = $false
  for ($i = 0; $i -lt 40; $i++) {
    try {
      $response = Invoke-WebRequest -Uri "http://$Addr/readyz" -UseBasicParsing -TimeoutSec 1
      if ($response.StatusCode -eq 200) {
        $ready = $true
        break
      }
    } catch {
      Start-Sleep -Milliseconds 150
    }
  }
  Assert-True $ready "gateway did not become ready"

  $health = Invoke-Api "GET" "/healthz"
  Assert-True ($health.service -eq "red-panda-gateway") "healthz service mismatch"
  $readyz = Invoke-Api "GET" "/readyz"
  Assert-True (Test-JsonProperty $readyz "agent_runtime") "readyz missing agent_runtime"
  $bootstrap = Invoke-Api "GET" "/api/v1/app/bootstrap"
  Assert-True ($bootstrap.gateway.api_version -eq "v1") "bootstrap api_version mismatch"
  Assert-True ($bootstrap.gateway.ws_url -eq "/api/v1/ws") "bootstrap ws_url mismatch"

  $session = Invoke-Api "POST" "/api/v1/sessions" @{
    name = "compat"
    workspace_root = "$Root"
  }
  Assert-True ($session.id -ne "") "session create did not return id"

  $otherWorkspaceRoot = Join-Path "$Root" "tmp\compat-other-workspace"
  $otherSession = Invoke-Api "POST" "/api/v1/sessions" @{
    name = "compat other"
    workspace_root = $otherWorkspaceRoot
  }
  Assert-True ($otherSession.id -ne "") "other session create did not return id"

  $projectMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "project"
    kind = "fact"
    title = "Compat project memory"
    content = "Use protocol compat project memory for this workspace."
    confidence = "high"
    workspace_root = "$Root"
    source = "user"
  }
  Assert-True ($projectMemory.id -ne "") "project memory create missing id"
  Assert-True ($projectMemory.scope -eq "project" -and $projectMemory.status -eq "active") "project memory create shape mismatch"

  $sessionMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "session"
    kind = "fact"
    title = "Compat session memory"
    content = "Use protocol compat session memory for this session."
    confidence = "medium"
    session_id = $session.id
    source = "user"
  }
  Assert-True ($sessionMemory.id -ne "") "session memory create missing id"
  Assert-True ($sessionMemory.scope -eq "session" -and $sessionMemory.session_id -eq $session.id) "session memory create shape mismatch"

  $projectMemory = Invoke-Api "PUT" "/api/v1/memory/$($projectMemory.id)" @{
    title = "Compat project memory updated"
    content = "Use protocol compat updated project memory for this workspace."
    confidence = "low"
    status = "active"
    metadata = @{ compat = $true }
  }
  Assert-True ($projectMemory.title -eq "Compat project memory updated") "memory update title mismatch"
  Assert-True ($projectMemory.content -eq "Use protocol compat updated project memory for this workspace.") "memory update content mismatch"
  Assert-True ($projectMemory.confidence -eq "low" -and $projectMemory.status -eq "active") "memory update status/confidence mismatch"
  Assert-True (Test-JsonProperty $projectMemory "metadata") "memory update missing metadata"

  $disabledMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "session"
    kind = "fact"
    title = "Compat disabled memory"
    content = "This disabled memory must not be selected."
    confidence = "medium"
    session_id = $session.id
    source = "user"
  }
  $disabledMemory = Invoke-Api "PUT" "/api/v1/memory/$($disabledMemory.id)" @{
    title = "Compat disabled memory updated"
    content = "This updated disabled memory must not be selected."
    confidence = "high"
    status = "disabled"
  }
  Assert-True ($disabledMemory.status -eq "disabled") "memory status update to disabled mismatch"

  $deletedMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "project"
    kind = "fact"
    title = "Compat deleted memory"
    content = "This deleted memory must not be listed or selected."
    confidence = "high"
    workspace_root = "$Root"
    source = "user"
  }
  Assert-True ($deletedMemory.id -ne "") "deleted memory create missing id"
  $deletedMemory = Invoke-Api "DELETE" "/api/v1/memory/$($deletedMemory.id)"
  Assert-True ($deletedMemory.status -eq "deleted") "memory delete did not return deleted status"

  $otherProjectMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "project"
    kind = "fact"
    title = "Compat other project memory"
    content = "This other project memory must not be selected."
    confidence = "high"
    workspace_root = $otherWorkspaceRoot
    source = "user"
  }
  Assert-True ($otherProjectMemory.id -ne "") "other project memory create missing id"

  $otherSessionMemory = Invoke-Api "POST" "/api/v1/memory" @{
    scope = "session"
    kind = "fact"
    title = "Compat other session memory"
    content = "This other session memory must not be selected."
    confidence = "high"
    session_id = $otherSession.id
    source = "user"
  }
  Assert-True ($otherSessionMemory.id -ne "") "other session memory create missing id"

  $memoryList = @(Invoke-Api "GET" "/api/v1/memory")
  Assert-True ($memoryList.Count -ge 4) "memory default list missing active records"
  Assert-True (@($memoryList | Where-Object { $_.status -ne "active" }).Count -eq 0) "memory default list returned non-active records"
  Assert-True (@($memoryList | Where-Object { $_.id -eq $projectMemory.id }).Count -eq 1) "memory default list missing project memory"
  Assert-True (@($memoryList | Where-Object { $_.id -eq $sessionMemory.id }).Count -eq 1) "memory default list missing session memory"
  Assert-True (@($memoryList | Where-Object { $_.id -eq $deletedMemory.id }).Count -eq 0) "memory default list returned deleted memory"
  Assert-True (@($memoryList | Where-Object { $_.id -eq $disabledMemory.id }).Count -eq 0) "memory default list returned disabled memory"

  $memoryPreview = Invoke-Api "POST" "/api/v1/memory/preview-run" @{
    session_id = $session.id
    workspace_root = "$Root"
    input = "compat memory preview"
  }
  Assert-True (Test-JsonProperty $memoryPreview "items") "memory preview missing items"
  Assert-True (Test-JsonProperty $memoryPreview "context") "memory preview missing context"
  $memoryPreviewItems = @($memoryPreview.items)
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $projectMemory.id }).Count -eq 1) "memory preview missing matching project memory"
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $sessionMemory.id }).Count -eq 1) "memory preview missing matching session memory"
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $disabledMemory.id }).Count -eq 0) "memory preview selected disabled memory"
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $deletedMemory.id }).Count -eq 0) "memory preview selected deleted memory"
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $otherProjectMemory.id }).Count -eq 0) "memory preview selected other workspace project memory"
  Assert-True (@($memoryPreviewItems | Where-Object { $_.id -eq $otherSessionMemory.id }).Count -eq 0) "memory preview selected other session memory"
  Assert-True ($memoryPreview.context -like "*protocol compat updated project memory*" -and $memoryPreview.context -like "*protocol compat session memory*") "memory preview context missing selected memory"
  Assert-True ($memoryPreview.context -notlike "*must not be selected*") "memory preview context included unselected memory"

  $mcpSecret = "compat-mcp-secret"
  $mcpServer = Invoke-Api "POST" "/api/v1/mcp/servers" @{
    name = "compat-filesystem"
    command = "mcp-filesystem"
    args = @("--root", ".")
    env = @{
      LOG_LEVEL = "warn"
      API_TOKEN = $mcpSecret
    }
    cwd = "$Root"
    enabled = $true
    tool_allowlist = @("read_file", "list")
    risk_overrides = @{ read_file = "low" }
  }
  Assert-True ($mcpServer.id -ne "") "MCP server create missing id"
  Assert-True ($mcpServer.env.API_TOKEN -eq "****" -and $mcpServer.env.API_TOKEN -ne $mcpSecret) "MCP server exposed sensitive env"
  Assert-True ($mcpServer.env.LOG_LEVEL -eq "warn") "MCP server redacted non-sensitive env"
  Assert-True ($mcpServer.timeouts.start_ms -eq 10000 -and $mcpServer.timeouts.call_ms -eq 30000) "MCP server default timeouts mismatch"

  $mcpServerGet = Invoke-Api "GET" "/api/v1/mcp/servers/$($mcpServer.id)"
  Assert-True ($mcpServerGet.id -eq $mcpServer.id -and $mcpServerGet.env.API_TOKEN -eq "****") "MCP server get mismatch"
  $mcpServers = Invoke-Api "GET" "/api/v1/mcp/servers"
  Assert-True (@($mcpServers.servers | Where-Object { $_.id -eq $mcpServer.id }).Count -eq 1) "MCP server list missing created config"

  $mcpServer = Invoke-Api "PUT" "/api/v1/mcp/servers/$($mcpServer.id)" @{
    enabled = $false
  }
  Assert-True (-not $mcpServer.enabled) "MCP server update did not disable config"
  Assert-True ($mcpServer.env.API_TOKEN -eq "****") "MCP server update leaked or lost sensitive env"

  $mcpDeleted = Invoke-Api "DELETE" "/api/v1/mcp/servers/$($mcpServer.id)"
  Assert-True ($mcpDeleted.deleted -and $mcpDeleted.id -eq $mcpServer.id) "MCP server delete mismatch"
  $mcpServersAfterDelete = Invoke-Api "GET" "/api/v1/mcp/servers"
  Assert-True (@($mcpServersAfterDelete.servers | Where-Object { $_.id -eq $mcpServer.id }).Count -eq 0) "deleted MCP server remained in list"

  $skillListBefore = @((Invoke-Api "GET" "/api/v1/skills?workspace_root=$([uri]::EscapeDataString($Root))").items)
  Assert-True (@($skillListBefore | Where-Object { $_.name -eq "compat-skill" }).Count -eq 0) "compat skill existed before create"

  $skillCreated = Invoke-Api "POST" "/api/v1/skills" @{
    workspace_root = "$Root"
    name = "compat-skill"
    description = "Compat skill description"
    instructions = "# Compat skill`n`nReview the protocol-compat flow."
  }
  Assert-True ($skillCreated.action -eq "skill.create" -and $skillCreated.name -eq "compat-skill") "skill create result mismatch"
  Assert-True ($skillCreated.path -eq ".codex/skills/compat-skill/SKILL.md") "skill create path mismatch"

  $skillListAfter = @((Invoke-Api "GET" "/api/v1/skills?workspace_root=$([uri]::EscapeDataString($Root))").items)
  $skillSummary = @($skillListAfter | Where-Object { $_.name -eq "compat-skill" })[-1]
  Assert-True ($null -ne $skillSummary) "skill list missing created skill"
  Assert-True ($skillSummary.description -eq "Compat skill description") "skill list description mismatch"
  Assert-True ($skillSummary.has_instructions -eq $true) "skill list has_instructions mismatch"
  Assert-True ($null -eq $skillSummary.instructions) "skill list leaked instructions body"

  $skillDetail = Invoke-Api "GET" "/api/v1/skills/compat-skill?workspace_root=$([uri]::EscapeDataString($Root))&include_instructions=1"
  Assert-True ($skillDetail.skill.name -eq "compat-skill") "skill detail name mismatch"
  Assert-True ($skillDetail.skill.description -eq "Compat skill description") "skill detail description mismatch"
  Assert-True ($skillDetail.skill.instructions -like "*Review the protocol-compat flow*") "skill detail instructions mismatch"
  Assert-True ($skillDetail.skill.size_bytes -gt 0) "skill detail size_bytes mismatch"

  $skillUpdated = Invoke-Api "PUT" "/api/v1/skills/compat-skill" @{
    workspace_root = "$Root"
    description = "Compat skill updated"
    instructions = "# Compat skill updated`n`nVerify update path."
  }
  Assert-True ($skillUpdated.action -eq "skill.update" -and $skillUpdated.name -eq "compat-skill") "skill update result mismatch"
  $skillDetailUpdated = Invoke-Api "GET" "/api/v1/skills/compat-skill?workspace_root=$([uri]::EscapeDataString($Root))&include_instructions=1"
  Assert-True ($skillDetailUpdated.skill.description -eq "Compat skill updated") "skill update description mismatch"
  Assert-True ($skillDetailUpdated.skill.instructions -like "*Verify update path*") "skill update instructions mismatch"

  $skillDeleted = Invoke-Api "DELETE" "/api/v1/skills/compat-skill?workspace_root=$([uri]::EscapeDataString($Root))"
  Assert-True ($skillDeleted.deleted -and $skillDeleted.name -eq "compat-skill") "skill delete result mismatch"
  $skillListFinal = @((Invoke-Api "GET" "/api/v1/skills?workspace_root=$([uri]::EscapeDataString($Root))").items)
  Assert-True (@($skillListFinal | Where-Object { $_.name -eq "compat-skill" }).Count -eq 0) "deleted skill remained in list"

  $providerJob = Start-CompatProvider
  $profileKey = "compat-secret"
  $profile = Invoke-Api "POST" "/api/v1/provider-profiles" @{
    name = "compat-provider"
    provider = "openai_compatible"
    base_url = "http://$ProviderAddr"
    model = "compat-model"
    api_key = $profileKey
    is_default = $true
  }
  Assert-True ($profile.id -ne "") "provider profile create missing id"
  Assert-True ($profile.api_key_set -and $profile.api_key_masked -ne $profileKey) "provider profile exposed raw api key"

  $providerWs = New-WsClient
  try {
    Send-WsJson $providerWs @{
      id = "provider_run"
      type = "request"
      method = "run.start"
      payload = @{
        session_id = $session.id
        input = @{ text = "provider profile compatibility" }
        options = @{
          working_dir = "$Root"
          provider_profile_id = $profile.id
        }
        subscribe = $true
      }
    }
    $providerInitial = @()
    $providerResponse = Wait-WsResponseWithEvents $providerWs "provider_run" ([ref]$providerInitial)
    Assert-True ($providerResponse.type -eq "response" -and $providerResponse.payload.accepted) "provider profile run.start mismatch"
    $providerEvents = Wait-RunFinishWithInitialEvents $providerWs $providerInitial
    Assert-IncreasingRootSeq $providerEvents
    $providerMessage = @($providerEvents | Where-Object { $_.type -eq "message_delta" })[0]
    Assert-True ($providerMessage.payload.delta -like "*provider profile compat ok*") "provider profile selection did not use compat provider"
  } finally {
    $providerWs.Dispose()
  }

  $inactiveProfile = Invoke-Api "POST" "/api/v1/provider-profiles" @{
    name = "compat-provider-inactive"
    provider = "openai_compatible"
    base_url = "http://$ProviderAddr"
    model = "compat-model-inactive"
    api_key = "compat-inactive-secret"
    is_default = $false
  }
  Assert-True ($inactiveProfile.id -ne "") "inactive provider profile create missing id"
  $inactiveProfile = Invoke-Api "PUT" "/api/v1/provider-profiles/$($inactiveProfile.id)" @{
    active = $false
  }
  Assert-True (Test-JsonProperty $inactiveProfile "active") "inactive provider profile missing active"
  Assert-True (-not $inactiveProfile.active) "inactive provider profile was not updated inactive"

  $inactiveProviderWs = New-WsClient
  try {
    Send-WsJson $inactiveProviderWs @{
      id = "inactive_provider_run"
      type = "request"
      method = "run.start"
      payload = @{
        session_id = $session.id
        input = @{ text = "inactive provider profile compatibility" }
        options = @{
          working_dir = "$Root"
          provider_profile_id = $inactiveProfile.id
        }
        subscribe = $true
      }
    }
    $inactiveProviderResponse = Wait-WsResponse $inactiveProviderWs "inactive_provider_run"
    Assert-True ($inactiveProviderResponse.type -eq "error") "inactive provider profile run.start did not return error"
    Assert-True ($inactiveProviderResponse.error.code -eq "run_start_failed") "inactive provider profile error code mismatch"
    Assert-True ($inactiveProviderResponse.error.message -match "(?i)inactive|not active|disabled") "inactive provider profile error message mismatch"

    Send-WsJson $inactiveProviderWs @{ id = "inactive_provider_ping"; type = "ping"; payload = @{ now = 3 } }
    $inactiveProviderPong = Wait-WsResponse $inactiveProviderWs "inactive_provider_ping"
    Assert-True ($inactiveProviderPong.type -eq "pong") "websocket did not remain usable after inactive provider profile error"
  } finally {
    $inactiveProviderWs.Dispose()
  }

  $ws = New-WsClient
  try {
    Send-WsJson $ws @{
      id = "auth_1"
      type = "auth"
      payload = @{
        token = ""
        client = @{ kind = "compat"; name = "protocol-compat"; version = "0.1.0" }
      }
    }
    $auth = Wait-WsResponse $ws "auth_1"
    Assert-True ($auth.type -eq "response" -and $auth.payload.authenticated) "auth response mismatch"

    Send-WsJson $ws @{ id = "ping_1"; type = "ping"; payload = @{ now = 1 } }
    $pong = Wait-WsResponse $ws "ping_1"
    Assert-True ($pong.type -eq "pong") "ping did not return pong"

    Send-WsText $ws "{not-json"
    $invalidJson = Read-WsMessage $ws
    Assert-True ($invalidJson.type -eq "error" -and $invalidJson.error.code -eq "invalid_json") "malformed websocket payload error mismatch"

    Send-WsJson $ws @{ id = "ping_after_bad_json"; type = "ping"; payload = @{ now = 2 } }
    $pongAfterBadJson = Wait-WsResponse $ws "ping_after_bad_json"
    Assert-True ($pongAfterBadJson.type -eq "pong") "websocket did not remain usable after malformed payload"

    Send-WsJson $ws @{ id = "bad_method"; type = "request"; method = "compat.unknown"; payload = @{} }
    $bad = Wait-WsResponse $ws "bad_method"
    Assert-True ($bad.type -eq "error" -and $bad.error.code -eq "method_not_implemented") "unsupported method error mismatch"

    Send-WsJson $ws @{
      id = "status_1"
      type = "request"
      method = "agent.status"
      payload = @{}
    }
    $status = Wait-WsResponse $ws "status_1"
    Assert-True ($status.type -eq "response" -and (Test-JsonProperty $status.payload "available")) "agent.status response mismatch"

    Send-WsJson $ws @{
      id = "run_1"
      type = "request"
      method = "run.start"
      payload = @{
        session_id = $session.id
        input = @{ text = "/read README.md" }
        options = @{
          runtime_mode = "single_core"
          working_dir = "$Root"
          tool_policy = "allow_all"
          permission_mode = "trusted"
        }
        subscribe = $true
      }
    }
    $startEvents = @()
    $runResponse = Wait-WsResponseWithEvents $ws "run_1" ([ref]$startEvents)
    Assert-True ($runResponse.type -eq "response" -and $runResponse.payload.accepted) "run.start response mismatch"
    $runID = $runResponse.payload.run_id
    $events = Wait-RunFinishWithInitialEvents $ws $startEvents
    Assert-True ($events.Count -gt 0) "run emitted no events"
    Assert-IncreasingRootSeq $events
    Assert-EventTypes $events @("tool_started", "tool_finished", "message_delta", "finish")

    $run = Invoke-Api "GET" "/api/v1/runs/$runID"
    Assert-True ($run.status -eq "completed") "run projection status mismatch"
    $timeline = @(Invoke-Api "GET" "/api/v1/runs/$runID/events")
    Assert-True ($timeline.Count -eq $events.Count) "timeline count mismatch"
    Assert-True ($timeline[0].root_seq -eq 1 -and $timeline[0].agent_role -ne "") "timeline event shape mismatch"

    $afterOne = @(Invoke-Api "GET" "/api/v1/runs/$runID/events?after_seq=1&limit=2")
    Assert-True ($afterOne.Count -eq 2 -and $afterOne[0].root_seq -gt 1) "timeline after_seq/limit mismatch"

    $memoryToolWs = New-WsClient
    try {
      Send-WsJson $memoryToolWs @{
        id = "memory_tool_create_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "remember session Protocol compat tool-created memory" }
          options = @{
            runtime_mode = "single_core"
            working_dir = "$Root"
            tool_policy = "risk_based"
            permission_mode = "strict"
          }
          subscribe = $true
        }
      }
      $memoryToolInitial = @()
      $memoryToolResponse = Wait-WsResponseWithEvents $memoryToolWs "memory_tool_create_run" ([ref]$memoryToolInitial)
      Assert-True ($memoryToolResponse.type -eq "response" -and $memoryToolResponse.payload.accepted) "memory tool run.start mismatch"
      $memoryToolRunID = $memoryToolResponse.payload.run_id
      $memoryToolEvents = @($memoryToolInitial)
      $memoryPermissionID = ""
      $memoryPermissionResolve = $null
      $deadline = [DateTime]::UtcNow.AddSeconds(15)
      while ([DateTime]::UtcNow -lt $deadline) {
        foreach ($event in @($memoryToolEvents | Where-Object { $_.type -eq "permission_required" -and $_.payload.permission_id -ne $memoryPermissionID })) {
          $memoryPermissionID = $event.payload.permission_id
          Send-WsJson $memoryToolWs @{
            id = "memory_tool_permission_resolve"
            type = "request"
            method = "permission.resolve"
            payload = @{
              permission_id = $memoryPermissionID
              run_id = $event.root_run_id
              decision = "approve"
              reason = "compat memory approve"
            }
          }
        }
        if ($memoryToolEvents.Count -gt 0 -and $memoryToolEvents[-1].type -eq "finish") {
          break
        }
        $msg = Read-WsMessage $memoryToolWs
        if ($msg.id -eq "memory_tool_permission_resolve") {
          $memoryPermissionResolve = $msg
          continue
        }
        if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
          $memoryToolEvents += $msg.payload
        }
      }
      Assert-True ($memoryPermissionID -ne "") "memory tool permission_required was not emitted"
      Assert-True ($memoryPermissionResolve.type -eq "response" -and $memoryPermissionResolve.payload.accepted) "memory tool permission resolve mismatch"
      Assert-IncreasingRootSeq $memoryToolEvents
      Assert-EventTypes $memoryToolEvents @("tool_started", "permission_required", "tool_finished", "finish")
      $memoryToolStarted = @($memoryToolEvents | Where-Object { $_.type -eq "tool_started" })[-1]
      Assert-True ($memoryToolStarted.payload.tool_name -eq "memory.create" -and $memoryToolStarted.payload.risk -eq "high") "memory tool_started payload mismatch"
      $memoryToolFinished = @($memoryToolEvents | Where-Object { $_.type -eq "tool_finished" })[-1]
      Assert-True ($memoryToolFinished.payload.tool_name -eq "memory.create" -and $memoryToolFinished.payload.status -eq "completed") "memory tool_finished payload mismatch"
      $memoryToolRecords = @(Invoke-Api "GET" "/api/v1/memory?scope=session&session_id=$($session.id)")
      $memoryToolRecord = @($memoryToolRecords | Where-Object { $_.content -eq "Protocol compat tool-created memory" })[-1]
      Assert-True ($null -ne $memoryToolRecord) "memory tool create did not persist record"
      Assert-True ($memoryToolRecord.source -eq "agent" -and $memoryToolRecord.run_id -eq $memoryToolRunID) "memory tool created record trace mismatch"
      $memoryToolCalls = @(Invoke-Api "GET" "/api/v1/runs/$memoryToolRunID/tools")
      Assert-True ($memoryToolCalls.Count -eq 1 -and $memoryToolCalls[0].tool_name -eq "memory.create" -and $memoryToolCalls[0].status -eq "completed") "memory tool projection mismatch"
    } finally {
      $memoryToolWs.Dispose()
    }

    $memoryListWs = New-WsClient
    try {
      Send-WsJson $memoryListWs @{
        id = "memory_tool_list_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "list memory session" }
          options = @{
            runtime_mode = "single_core"
            working_dir = "$Root"
            tool_policy = "risk_based"
            permission_mode = "permissive"
          }
          subscribe = $true
        }
      }
      $memoryListInitial = @()
      $memoryListResponse = Wait-WsResponseWithEvents $memoryListWs "memory_tool_list_run" ([ref]$memoryListInitial)
      Assert-True ($memoryListResponse.type -eq "response" -and $memoryListResponse.payload.accepted) "memory list tool run.start mismatch"
      $memoryListEvents = Wait-RunFinishWithInitialEvents $memoryListWs $memoryListInitial
      Assert-IncreasingRootSeq $memoryListEvents
      Assert-EventTypes $memoryListEvents @("tool_started", "tool_output", "tool_finished", "finish")
      $memoryListOutput = @($memoryListEvents | Where-Object { $_.type -eq "tool_output" })[-1]
      Assert-True ($memoryListOutput.payload.tool_name -eq "memory.list") "memory list tool output tool_name mismatch"
      Assert-True ($memoryListOutput.payload.delta -like "*Protocol compat tool-created memory*") "memory list tool output missing created memory"
    } finally {
      $memoryListWs.Dispose()
    }

    $memoryDenyWs = New-WsClient
    try {
      Send-WsJson $memoryDenyWs @{
        id = "memory_tool_deny_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "remember session Protocol compat denied memory" }
          options = @{
            runtime_mode = "single_core"
            working_dir = "$Root"
            tool_policy = "risk_based"
            permission_mode = "strict"
          }
          subscribe = $true
        }
      }
      $memoryDenyInitial = @()
      $memoryDenyResponse = Wait-WsResponseWithEvents $memoryDenyWs "memory_tool_deny_run" ([ref]$memoryDenyInitial)
      Assert-True ($memoryDenyResponse.type -eq "response" -and $memoryDenyResponse.payload.accepted) "memory deny tool run.start mismatch"
      $memoryDenyRunID = $memoryDenyResponse.payload.run_id
      $memoryDenyEvents = @($memoryDenyInitial)
      $memoryDenyPermissionID = ""
      $memoryDenyResolve = $null
      $deadline = [DateTime]::UtcNow.AddSeconds(15)
      while ([DateTime]::UtcNow -lt $deadline) {
        foreach ($event in @($memoryDenyEvents | Where-Object { $_.type -eq "permission_required" -and $_.payload.permission_id -ne $memoryDenyPermissionID })) {
          $memoryDenyPermissionID = $event.payload.permission_id
          Send-WsJson $memoryDenyWs @{
            id = "memory_tool_deny_resolve"
            type = "request"
            method = "permission.resolve"
            payload = @{
              permission_id = $memoryDenyPermissionID
              run_id = $event.root_run_id
              decision = "deny"
              reason = "compat memory deny"
            }
          }
        }
        if ($memoryDenyEvents.Count -gt 0 -and $memoryDenyEvents[-1].type -eq "finish") {
          break
        }
        $msg = Read-WsMessage $memoryDenyWs
        if ($msg.id -eq "memory_tool_deny_resolve") {
          $memoryDenyResolve = $msg
          continue
        }
        if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
          $memoryDenyEvents += $msg.payload
        }
      }
      Assert-True ($memoryDenyPermissionID -ne "") "memory deny permission_required was not emitted"
      Assert-True ($memoryDenyResolve.type -eq "response" -and $memoryDenyResolve.payload.accepted) "memory deny resolve mismatch"
      Assert-EventTypes $memoryDenyEvents @("tool_started", "permission_required", "tool_failed", "finish")
      $memoryDenyFinish = @($memoryDenyEvents | Where-Object { $_.type -eq "finish" })[-1]
      Assert-True ($memoryDenyFinish.payload.status -eq "completed") "memory deny run finish mismatch"
      $memoryDenyRecords = @(Invoke-Api "GET" "/api/v1/memory?scope=session&session_id=$($session.id)")
      Assert-True (@($memoryDenyRecords | Where-Object { $_.content -eq "Protocol compat denied memory" }).Count -eq 0) "denied memory tool persisted a record"
      $memoryDenyCalls = @(Invoke-Api "GET" "/api/v1/runs/$memoryDenyRunID/tools")
      Assert-True ($memoryDenyCalls.Count -eq 1 -and $memoryDenyCalls[0].tool_name -eq "memory.create" -and $memoryDenyCalls[0].status -eq "denied") "memory deny tool projection mismatch"
    } finally {
      $memoryDenyWs.Dispose()
    }

    $sourceHistory = @(Invoke-Api "GET" "/api/v1/sessions/$($session.id)/history")
    Assert-True ($sourceHistory.Count -gt 0) "source session history missing before fork"
    $forkPointSeq = [uint64]$sourceHistory[-1].seq
    $fork = Invoke-Api "POST" "/api/v1/sessions/$($session.id)/fork" @{
      name = "compat fork"
      fork_point = @{
        message_seq = $forkPointSeq
      }
    }
    Assert-True ($fork.session.parent_id -eq $session.id -and $fork.session.kind -eq "fork") "fork session metadata mismatch"
    Assert-True ($fork.lineage.operation -eq "fork" -and $fork.lineage.fork_point_seq -eq $forkPointSeq) "fork lineage mismatch"
    Assert-True ($fork.copied_messages -eq $sourceHistory.Count) "fork copied message count mismatch"
    $forkHistory = @(Invoke-Api "GET" "/api/v1/sessions/$($fork.session.id)/history")
    Assert-True ($forkHistory.Count -eq $sourceHistory.Count) "fork history count mismatch"

    $compactPreview = Invoke-Api "POST" "/api/v1/sessions/$($session.id)/compact/preview" @{
      keep_tail_messages = 1
    }
    Assert-True ($compactPreview.preview.source_session_id -eq $session.id) "compact preview source mismatch"
    Assert-True (-not [string]::IsNullOrWhiteSpace([string]$compactPreview.preview.summary.summary)) "compact preview summary mismatch"

    $compact = Invoke-Api "POST" "/api/v1/sessions/$($session.id)/compact" @{
      name = "compat compact"
      keep_tail_messages = 1
      summary = $compactPreview.preview.summary
    }
    Assert-True ($compact.session.id -eq $session.id -and $compact.session.kind -eq "normal") "compact session metadata mismatch"
    Assert-True ($compact.compaction.status -eq "applied" -and $compact.compaction.target_session_id -eq $session.id) "compact record mismatch"
    $compactHistory = @(Invoke-Api "GET" "/api/v1/sessions/$($compact.session.id)/history")
    Assert-True ($compactHistory.Count -eq $sourceHistory.Count) "compact mutated source history"
    $compactState = Invoke-Api "GET" "/api/v1/sessions/$($session.id)/compact"
    Assert-True ($compactState.active -and $compactState.compaction.id -eq $compact.compaction.id) "compact state mismatch"
    Assert-True ($compactState.summary.summary -eq $compactPreview.preview.summary.summary) "compact summary state mismatch"

    $resumeWs = New-WsClient
    try {
      Send-WsJson $resumeWs @{
        id = "resume_1"
        type = "request"
        method = "run.resume"
        payload = @{ last_seen = @{ $runID = 2 } }
      }
      $replayed = @()
      $resume = Wait-WsResponseWithEvents $resumeWs "resume_1" ([ref]$replayed)
      Assert-True ($resume.type -eq "response" -and $resume.payload.resumed -eq 1) "run.resume response mismatch"
      while ($replayed.Count -lt ($events.Count - 2)) {
        $msg = Read-WsMessage $resumeWs
        if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
          $replayed += $msg.payload
        }
      }
      Assert-True (($replayed | Select-Object -First 1).root_seq -eq 3) "run.resume replay cursor mismatch"
      Assert-IncreasingRootSeq $replayed
    } finally {
      $resumeWs.Dispose()
    }

    $failedToolWs = New-WsClient
    try {
      Send-WsJson $failedToolWs @{
        id = "failed_tool_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "/shell echo compat-denied-tool" }
          options = @{
            runtime_mode = "single_core"
            working_dir = "$Root"
            tool_policy = "allow_all"
            permission_mode = "trusted"
            tool_denylist = @("shell.exec")
          }
          subscribe = $true
        }
      }
      $failedToolInitial = @()
      $failedToolResponse = Wait-WsResponseWithEvents $failedToolWs "failed_tool_run" ([ref]$failedToolInitial)
      Assert-True ($failedToolResponse.type -eq "response" -and $failedToolResponse.payload.accepted) "failed tool run.start mismatch"
      $failedToolRunID = $failedToolResponse.payload.run_id
      $failedToolEvents = Wait-RunFinishWithInitialEvents $failedToolWs $failedToolInitial
      Assert-IncreasingRootSeq $failedToolEvents
      Assert-EventTypes $failedToolEvents @("tool_started", "tool_failed", "error", "finish")
      $failedToolEvent = @($failedToolEvents | Where-Object { $_.type -eq "tool_failed" })[-1]
      Assert-True ($failedToolEvent.payload.tool_name -eq "shell.exec") "failed tool event tool_name mismatch"
      Assert-True ($failedToolEvent.payload.status -eq "denied") "failed tool event status mismatch"
      Assert-True ($failedToolEvent.payload.error -eq "tool is denied by tool_denylist") "failed tool event error mismatch"
      $failedToolFinish = @($failedToolEvents | Where-Object { $_.type -eq "finish" })[-1]
      Assert-True ($failedToolFinish.payload.status -eq "denied") "failed tool run did not finish denied"

      $failedToolRun = Invoke-Api "GET" "/api/v1/runs/$failedToolRunID"
      Assert-True ($failedToolRun.status -eq "denied" -and $failedToolRun.error -like "*tool_denylist*") "failed tool run projection mismatch"
      $failedToolCalls = @(Invoke-Api "GET" "/api/v1/runs/$failedToolRunID/tools")
      Assert-True ($failedToolCalls.Count -eq 1) "failed tool projection count mismatch"
      Assert-True ($failedToolCalls[0].tool_name -eq "shell.exec" -and $failedToolCalls[0].status -eq "denied") "failed tool projection status mismatch"
      Assert-True ($failedToolCalls[0].policy -eq "deny" -and $failedToolCalls[0].policy_reason -eq "tool is denied by tool_denylist") "failed tool projection policy mismatch"
      $failedToolTimeline = @(Invoke-Api "GET" "/api/v1/runs/$failedToolRunID/events")
      $failedToolTimelineFailures = @(Select-EventsByType $failedToolTimeline "tool_failed")
      Assert-True ($failedToolTimelineFailures.Count -eq 1) "failed tool timeline missing tool_failed, actual=$($failedToolTimeline.type -join ",")"
    } finally {
      $failedToolWs.Dispose()
    }

    $failedSubAgentWs = New-WsClient
    try {
      Send-WsJson $failedSubAgentWs @{
        id = "failed_subagent_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "/subagent compat failed child" }
          options = @{
            runtime_mode = "single_core"
            working_dir = "$Root"
            spawn_subagents = $true
            subagent_backend = "runtime_process"
          }
          subscribe = $true
        }
      }
      $failedSubAgentInitial = @()
      $failedSubAgentResponse = Wait-WsResponseWithEvents $failedSubAgentWs "failed_subagent_run" ([ref]$failedSubAgentInitial)
      Assert-True ($failedSubAgentResponse.type -eq "response" -and $failedSubAgentResponse.payload.accepted) "failed subagent run.start mismatch"
      $failedSubAgentRunID = $failedSubAgentResponse.payload.run_id
      $failedSubAgentEvents = Wait-RunFinishWithInitialEvents $failedSubAgentWs $failedSubAgentInitial
      Assert-IncreasingRootSeq $failedSubAgentEvents
      Assert-EventTypes $failedSubAgentEvents @("subagent_update", "finish")
      $failedSubAgentEvent = @($failedSubAgentEvents | Where-Object { $_.type -eq "subagent_update" -and $_.payload.status -eq "failed" })[-1]
      Assert-True ($null -ne $failedSubAgentEvent) "failed subagent event was not emitted"
      Assert-True ($failedSubAgentEvent.agent.role -eq "subagent") "failed subagent event agent role mismatch"
      Assert-True ($failedSubAgentEvent.payload.backend -eq "runtime_process") "failed subagent backend mismatch"
      Assert-True ($failedSubAgentEvent.payload.summary -eq "runtime_process planner subagent failed") "failed subagent summary mismatch"
      Assert-True ($failedSubAgentEvent.payload.error -like "*missing-red-panda-subagent*") "failed subagent error mismatch"
      $failedSubAgentFinish = @($failedSubAgentEvents | Where-Object { $_.type -eq "finish" })[-1]
      Assert-True ($failedSubAgentFinish.payload.status -eq "completed") "failed subagent root run finish status mismatch"

      $failedSubAgentRun = Invoke-Api "GET" "/api/v1/runs/$failedSubAgentRunID"
      Assert-True ($failedSubAgentRun.status -eq "completed") "failed subagent root run projection mismatch"
      $failedSubAgentTimeline = @(Invoke-Api "GET" "/api/v1/runs/$failedSubAgentRunID/events")
      $failedSubAgentTimelineEvent = @((Select-EventsByType $failedSubAgentTimeline "subagent_update") | Where-Object { $_.agent_role -eq "subagent" -and $_.payload.status -eq "failed" })[-1]
      Assert-True ($null -ne $failedSubAgentTimelineEvent) "failed subagent timeline missing failed update"
      Assert-True ($failedSubAgentTimelineEvent.payload.backend -eq "runtime_process") "failed subagent timeline backend mismatch"
      Assert-True ($failedSubAgentTimelineEvent.payload.error -like "*missing-red-panda-subagent*") "failed subagent timeline error mismatch"
    } finally {
      $failedSubAgentWs.Dispose()
    }

    $permissionWs = New-WsClient
    try {
      Send-WsJson $permissionWs @{
        id = "permission_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "compat permission /permission" }
          options = @{
            working_dir = "$Root"
            require_permission = $true
            permission_mode = "strict"
          }
          subscribe = $true
        }
      }
      $permissionInitial = @()
      $permissionResponse = Wait-WsResponseWithEvents $permissionWs "permission_run" ([ref]$permissionInitial)
      Assert-True ($permissionResponse.type -eq "response" -and $permissionResponse.payload.accepted) "permission run.start mismatch"
      $permissionEvents = @($permissionInitial)
      $permissionResolve = $null
      $permissionID = ""
      $permissionRunID = $permissionResponse.payload.run_id
      $deadline = [DateTime]::UtcNow.AddSeconds(15)
      while ([DateTime]::UtcNow -lt $deadline) {
        foreach ($event in @($permissionEvents | Where-Object { $_.type -eq "permission_required" -and $_.payload.permission_id -ne $permissionID })) {
          $permissionID = $event.payload.permission_id
          Send-WsJson $permissionWs @{
            id = "permission_resolve"
            type = "request"
            method = "permission.resolve"
            payload = @{
              permission_id = $permissionID
              run_id = $event.root_run_id
              decision = "approve"
              reason = "compat"
            }
          }
        }
        if ($permissionEvents.Count -gt 0 -and $permissionEvents[-1].type -eq "finish") {
          break
        }
        $msg = Read-WsMessage $permissionWs
        if ($msg.id -eq "permission_resolve") {
          $permissionResolve = $msg
          continue
        }
        if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
          $permissionEvents += $msg.payload
        }
      }
      Assert-True ($permissionID -ne "") "permission_required event was not emitted"
      Assert-True ($permissionResolve.type -eq "response" -and $permissionResolve.payload.accepted) "permission.resolve response mismatch"
      Assert-EventTypes $permissionEvents @("permission_required", "finish")
      $permissionFinish = @($permissionEvents | Where-Object { $_.type -eq "finish" })[-1]
      Assert-True ($permissionFinish.payload.status -eq "completed") "permission run did not complete after approval"
      $permissionRecord = Invoke-Api "GET" "/api/v1/permissions/$permissionID"
      Assert-True ($permissionRecord.status -eq "resolved" -and $permissionRecord.decision -eq "approve") "permission projection mismatch"
      $permissionRun = Invoke-Api "GET" "/api/v1/runs/$permissionRunID"
      Assert-True ($permissionRun.status -eq "completed") "permission run projection mismatch"
    } finally {
      $permissionWs.Dispose()
    }

    $permissionDenyWs = New-WsClient
    try {
      Send-WsJson $permissionDenyWs @{
        id = "permission_deny_run"
        type = "request"
        method = "run.start"
        payload = @{
          session_id = $session.id
          input = @{ text = "compat permission deny /permission" }
          options = @{
            working_dir = "$Root"
            require_permission = $true
            permission_mode = "strict"
          }
          subscribe = $true
        }
      }
      $permissionDenyInitial = @()
      $permissionDenyResponse = Wait-WsResponseWithEvents $permissionDenyWs "permission_deny_run" ([ref]$permissionDenyInitial)
      Assert-True ($permissionDenyResponse.type -eq "response" -and $permissionDenyResponse.payload.accepted) "permission deny run.start mismatch"
      $permissionDenyEvents = @($permissionDenyInitial)
      $permissionDenyResolve = $null
      $permissionDenyID = ""
      $permissionDenyRunID = $permissionDenyResponse.payload.run_id
      $deadline = [DateTime]::UtcNow.AddSeconds(15)
      while ([DateTime]::UtcNow -lt $deadline) {
        foreach ($event in @($permissionDenyEvents | Where-Object { $_.type -eq "permission_required" -and $_.payload.permission_id -ne $permissionDenyID })) {
          $permissionDenyID = $event.payload.permission_id
          Send-WsJson $permissionDenyWs @{
            id = "permission_deny_resolve"
            type = "request"
            method = "permission.resolve"
            payload = @{
              permission_id = $permissionDenyID
              run_id = $event.root_run_id
              decision = "deny"
              reason = "compat deny"
            }
          }
        }
        if ($permissionDenyEvents.Count -gt 0 -and $permissionDenyEvents[-1].type -eq "finish") {
          break
        }
        $msg = Read-WsMessage $permissionDenyWs
        if ($msg.id -eq "permission_deny_resolve") {
          $permissionDenyResolve = $msg
          continue
        }
        if ($msg.type -eq "event" -and $msg.method -eq "run.event") {
          $permissionDenyEvents += $msg.payload
        }
      }
      Assert-True ($permissionDenyID -ne "") "permission deny permission_required event was not emitted"
      Assert-True ($permissionDenyResolve.type -eq "response" -and $permissionDenyResolve.payload.accepted) "permission deny resolve response mismatch"
      Assert-EventTypes $permissionDenyEvents @("permission_required", "error", "finish")
      $permissionDenyError = @($permissionDenyEvents | Where-Object { $_.type -eq "error" })[-1]
      Assert-True ($permissionDenyError.payload.status -eq "denied" -and $permissionDenyError.payload.permission_id -eq $permissionDenyID) "permission deny error event mismatch"
      $permissionDenyFinish = @($permissionDenyEvents | Where-Object { $_.type -eq "finish" })[-1]
      Assert-True ($permissionDenyFinish.payload.status -eq "denied") "permission deny run did not finish as denied"
      $permissionDenyRecord = Invoke-Api "GET" "/api/v1/permissions/$permissionDenyID"
      Assert-True ($permissionDenyRecord.status -eq "denied" -and $permissionDenyRecord.decision -eq "deny") "permission deny projection mismatch"
      $permissionDenyRun = Invoke-Api "GET" "/api/v1/runs/$permissionDenyRunID"
      Assert-True ($permissionDenyRun.status -eq "denied" -and $permissionDenyRun.error -like "*permission denied*") "permission deny run projection mismatch"
    } finally {
      $permissionDenyWs.Dispose()
    }
  } finally {
    $ws.Dispose()
  }

  [pscustomobject]@{
    ok = $true
    session = $session.id
    run = $runID
    events = $events.Count
    timeline_after_seq = $afterOne.Count
    replayed = $events.Count - 2
    permission = "approved"
    permission_deny = "denied"
    failed_tool = $failedToolRunID
    failed_subagent = $failedSubAgentRunID
    fork = $fork.session.id
    compact = $compact.session.id
    memory_project = $projectMemory.id
    memory_session = $sessionMemory.id
    memory_preview = $memoryPreviewItems.Count
    memory_tool_run = $memoryToolRunID
    memory_tool_record = $memoryToolRecord.id
    memory_tool_denied = $memoryDenyRunID
    mcp_server = $mcpServer.id
    skill_crud = "compat-skill"
    provider_profile = $profile.id
    inactive_provider_profile = $inactiveProfile.id
  } | ConvertTo-Json -Compress
} finally {
  if ($providerJob) {
    Stop-Job $providerJob -ErrorAction SilentlyContinue
    Remove-Job $providerJob -Force -ErrorAction SilentlyContinue
  }
  if ($gateway -and -not $gateway.HasExited) {
    Stop-Process -Id $gateway.Id -Force
    $gateway.WaitForExit()
  }
  Remove-CompatDatabase
  $env:RED_PANDA_SUBAGENT_COMMAND = $previousSubAgentCommand
}
