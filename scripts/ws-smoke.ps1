param(
  [string]$Addr = "127.0.0.1:17901"
)

$ErrorActionPreference = "Stop"

$Root = Resolve-Path (Join-Path $PSScriptRoot "..")
$GatewayExe = Join-Path $Root "bin\red-panda-gateway.exe"
$Database = Join-Path $Root "smoke-red-panda.db"
$EditFile = Join-Path $Root "tmp\smoke-edit.txt"
$DiffFile = Join-Path $Root "tmp\smoke-patch.txt"
$PatchFile = Join-Path $Root "tmp\smoke-apply-patch.txt"
$ProviderAddr = "127.0.0.1:17902"

function Remove-SmokeDatabase {
  foreach ($path in @($Database, "$Database-shm", "$Database-wal")) {
    Remove-Item -LiteralPath $path -ErrorAction SilentlyContinue
  }
  foreach ($path in @($EditFile, $DiffFile, $PatchFile)) {
    Remove-Item -LiteralPath $path -ErrorAction SilentlyContinue
  }
}

function Read-WsMessage($ws) {
  $buffer = New-Object byte[] 65536
  $segment = [ArraySegment[byte]]::new($buffer)
  $builder = New-Object System.Text.StringBuilder
  do {
    $result = $ws.ReceiveAsync($segment, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
    if ($result.MessageType -eq [System.Net.WebSockets.WebSocketMessageType]::Close) {
      throw "websocket closed"
    }
    [void]$builder.Append([Text.Encoding]::UTF8.GetString($buffer, 0, $result.Count))
  } while (-not $result.EndOfMessage)
  return ($builder.ToString() | ConvertFrom-Json)
}

function Send-WsJson($ws, $obj) {
  $json = $obj | ConvertTo-Json -Depth 30 -Compress
  $bytes = [Text.Encoding]::UTF8.GetBytes($json)
  $task = $ws.SendAsync(
    [ArraySegment[byte]]::new($bytes),
    [System.Net.WebSockets.WebSocketMessageType]::Text,
    $true,
    [Threading.CancellationToken]::None
  )
  $task.GetAwaiter().GetResult() | Out-Null
}

function New-WsClient {
  $ws = [System.Net.WebSockets.ClientWebSocket]::new()
  $ws.ConnectAsync([Uri]"ws://$Addr/api/v1/ws", [Threading.CancellationToken]::None).GetAwaiter().GetResult() | Out-Null
  return $ws
}

function Start-Run($ws, $text, $sessionID, $options = @{}) {
  Send-WsJson $ws @{
    id = "start_$sessionID"
    type = "request"
    method = "run.start"
    payload = @{
      session_id = $sessionID
      input = @{ text = $text }
      options = $options
      subscribe = $true
    }
  }
}

function Wait-RunEvents($ws, [scriptblock]$onEvent, [string]$name = "run") {
  $events = @()
  $deadline = [DateTime]::UtcNow.AddSeconds(12)
  while ([DateTime]::UtcNow -lt $deadline) {
    $msg = Read-WsMessage $ws
    if ($msg.type -ne "event" -or $msg.method -ne "run.event") {
      continue
    }
    $event = $msg.payload
    $events += $event
    & $onEvent $event
    if ($event.type -eq "finish") {
      return $events
    }
  }
  throw "$name did not finish before timeout"
}

function Assert-ContainsSequence($name, $events, [string[]]$expected) {
  $actual = @($events | ForEach-Object { $_.type })
  $cursor = 0
  foreach ($item in $actual) {
    if ($cursor -lt $expected.Count -and $item -eq $expected[$cursor]) {
      $cursor++
    }
  }
  if ($cursor -ne $expected.Count) {
    throw "$name expected sequence '$($expected -join ",")', actual '$($actual -join ",")'"
  }
}

function Assert-StrictlyIncreasingRootSeq($name, $events) {
  $last = 0
  foreach ($event in $events) {
    if ([uint64]$event.root_seq -le $last) {
      throw "$name root_seq is not strictly increasing"
    }
    $last = [uint64]$event.root_seq
  }
}

function Get-ApiData($path) {
  $response = Invoke-WebRequest -Uri "http://$Addr$path" -UseBasicParsing -TimeoutSec 5
  $body = $response.Content | ConvertFrom-Json
  if (-not $body.ok) {
    throw "GET $path failed: $($body.error.message)"
  }
  return $body.data
}

function Invoke-ApiData($method, $path, $payload = $null) {
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
  try {
    $response = Invoke-WebRequest @params
  } catch {
    $status = ""
    $content = ""
    if ($_.Exception.Response) {
      $status = [int]$_.Exception.Response.StatusCode
      try {
        $stream = $_.Exception.Response.GetResponseStream()
        if ($stream) {
          $reader = [IO.StreamReader]::new($stream)
          $content = $reader.ReadToEnd()
        }
      } catch {
        $content = ""
      }
    }
    throw "$method $path failed: status=$status body=$content"
  }
  if ($response.Content -eq "") {
    return $null
  }
  $body = $response.Content | ConvertFrom-Json
  if (-not $body.ok) {
    throw "$method $path failed: $($body.error.message)"
  }
  return $body.data
}

function Wait-ApiCondition($name, [scriptblock]$check) {
  $script:pendingDiag = ""
  $deadline = [DateTime]::UtcNow.AddSeconds(2)
  do {
    if (& $check) {
      return
    }
    Start-Sleep -Milliseconds 100
  } while ([DateTime]::UtcNow -lt $deadline)
  if ($script:pendingDiag) {
    throw "$name; $script:pendingDiag"
  }
  throw $name
}

function Try-ApiData($path) {
  try {
    return Get-ApiData $path
  } catch {
    return $null
  }
}

function Test-JsonProperty($obj, [string]$name) {
  return $null -ne $obj -and $null -ne $obj.PSObject.Properties[$name]
}

function Assert-ProviderProfileSafe($name, $profile, [string]$rawApiKey) {
  if (-not (Test-JsonProperty $profile "api_key_set") -or -not $profile.api_key_set) {
    throw "$name expected api_key_set=true"
  }
  if (Test-JsonProperty $profile "api_key" -and $profile.api_key -eq $rawApiKey) {
    throw "$name exposed raw api_key"
  }
  if (-not (Test-JsonProperty $profile "api_key_masked") -and -not (Test-JsonProperty $profile "masked_api_key") -and -not (Test-JsonProperty $profile "api_key_preview")) {
    throw "$name expected masked api key state"
  }
}

function Get-ProviderProfileItems($data) {
  if (Test-JsonProperty $data "items") {
    return @($data.items)
  }
  return @($data)
}

function Test-ProviderProfileInactive($profile) {
  if (Test-JsonProperty $profile "deleted" -and $profile.deleted) {
    return $true
  }
  if (Test-JsonProperty $profile "active" -and -not $profile.active) {
    return $true
  }
  if (Test-JsonProperty $profile "is_active" -and -not $profile.is_active) {
    return $true
  }
  return $false
}

function Start-SmokeProvider {
  $job = Start-Job -ScriptBlock {
    param($prefix)
    $listener = [System.Net.HttpListener]::new()
    $listener.Prefixes.Add($prefix)
    $listener.Start()
    try {
      while ($listener.IsListening) {
        $ctx = $listener.GetContext()
        $body = '{"choices":[{"message":{"content":"provider profile ok"}}]}'
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

Remove-SmokeDatabase
$env:RED_PANDA_GATEWAY_ADDR = $Addr
$env:RED_PANDA_DATABASE = $Database
$gateway = Start-Process -FilePath $GatewayExe -WorkingDirectory $Root -PassThru -WindowStyle Hidden

try {
  $ready = $false
  for ($i = 0; $i -lt 40; $i++) {
    try {
      $response = Invoke-WebRequest -Uri "http://$Addr/healthz" -UseBasicParsing -TimeoutSec 1
      if ($response.StatusCode -eq 200) {
        $ready = $true
        break
      }
    } catch {
      Start-Sleep -Milliseconds 200
    }
  }
  if (-not $ready) {
    throw "gateway did not become healthy"
  }

  $providerJob = Start-SmokeProvider
  $providerProfileKey = "sk-smoke-provider-profile-raw-key"
  $providerProfile = Invoke-ApiData "POST" "/api/v1/provider-profiles" @{
    name = "smoke-openai-compatible"
    provider = "openai_compatible"
    base_url = "http://$ProviderAddr"
    model = "smoke-model-create"
    api_key = $providerProfileKey
    is_default = $true
  }
  if (-not (Test-JsonProperty $providerProfile "id") -or $providerProfile.id -eq "") {
    throw "provider profile create response did not include id"
  }
  Assert-ProviderProfileSafe "provider profile create" $providerProfile $providerProfileKey
  $providerProfileID = $providerProfile.id

  $providerProfileList = Get-ProviderProfileItems (Invoke-ApiData "GET" "/api/v1/provider-profiles")
  $providerProfileFromList = @($providerProfileList | Where-Object { $_.id -eq $providerProfileID })[0]
  if (-not $providerProfileFromList) {
    throw "provider profile list did not include created profile '$providerProfileID'"
  }
  Assert-ProviderProfileSafe "provider profile list" $providerProfileFromList $providerProfileKey

  $providerProfileByID = Invoke-ApiData "GET" "/api/v1/provider-profiles/$providerProfileID"
  if ($providerProfileByID.id -ne $providerProfileID) {
    throw "provider profile get by id returned '$($providerProfileByID.id)', expected '$providerProfileID'"
  }
  Assert-ProviderProfileSafe "provider profile get" $providerProfileByID $providerProfileKey

  $providerProfileUpdated = Invoke-ApiData "PUT" "/api/v1/provider-profiles/$providerProfileID" @{
    name = "smoke-openai-compatible"
    provider = "openai_compatible"
    base_url = "http://$ProviderAddr"
    model = "smoke-model-updated"
    is_default = $true
    is_active = $true
  }
  if ($providerProfileUpdated.id -ne $providerProfileID -or $providerProfileUpdated.model -ne "smoke-model-updated") {
    throw "provider profile update did not preserve id and update model"
  }
  Assert-ProviderProfileSafe "provider profile update" $providerProfileUpdated $providerProfileKey

  $providerRunWs = New-WsClient
  try {
    Start-Run $providerRunWs "/read README.md" "smoke_provider_profile_run" @{
      working_dir = $Root.Path
      permission_mode = "strict"
      provider_profile_id = $providerProfileID
    }
    $providerRunEvents = Wait-RunEvents $providerRunWs { param($event) } "provider profile run"
    Assert-ContainsSequence "provider profile run" $providerRunEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "provider profile run" $providerRunEvents
    $providerRunID = $providerRunEvents[0].root_run_id
    $providerRun = $null
    Wait-ApiCondition "provider profile run projection expected completed status" {
      $script:providerRun = Try-ApiData "/api/v1/runs/$providerRunID"
      $script:pendingDiag = "run=$($script:providerRun | ConvertTo-Json -Compress -Depth 10)"
      return ($null -ne $script:providerRun -and $script:providerRun.status -eq "completed")
    }
  } finally {
    $providerRunWs.Dispose()
  }

  $providerProfileDelete = Invoke-ApiData "DELETE" "/api/v1/provider-profiles/$providerProfileID"
  $providerProfileAfterDelete = Try-ApiData "/api/v1/provider-profiles/$providerProfileID"
  if ($providerProfileAfterDelete) {
    Assert-ProviderProfileSafe "provider profile delete get" $providerProfileAfterDelete $providerProfileKey
    if (-not (Test-ProviderProfileInactive $providerProfileAfterDelete)) {
      throw "provider profile delete did not remove or deactivate profile '$providerProfileID'"
    }
  } else {
    $providerProfileAfterList = @((Get-ProviderProfileItems (Invoke-ApiData "GET" "/api/v1/provider-profiles")) | Where-Object { $_.id -eq $providerProfileID })[0]
    if ($providerProfileAfterList) {
      Assert-ProviderProfileSafe "provider profile delete list" $providerProfileAfterList $providerProfileKey
      if (-not (Test-ProviderProfileInactive $providerProfileAfterList)) {
        throw "provider profile delete list still included active profile '$providerProfileID'"
      }
    }
  }

  $readWs = New-WsClient
  try {
    Start-Run $readWs "/read README.md" "smoke_read" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $readEvents = Wait-RunEvents $readWs { param($event) } "read"
    Assert-ContainsSequence "read tool" $readEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "read tool" $readEvents
    $readRunID = $readEvents[0].root_run_id
    $readRun = $null
    Wait-ApiCondition "run projection expected completed read run" {
      $script:readRun = Try-ApiData "/api/v1/runs/$readRunID"
      $script:pendingDiag = "run=$($script:readRun | ConvertTo-Json -Compress -Depth 10)"
      return ($null -ne $script:readRun -and $script:readRun.status -eq "completed" -and [uint64]$script:readRun.last_root_seq -ge 1)
    }
    $sessionRuns = @(Get-ApiData "/api/v1/sessions/smoke_read/runs")
    if ($sessionRuns.Count -lt 1 -or $sessionRuns[0].id -ne $readRunID) {
      throw "session run projection did not include read run"
    }
    $runTools = @(Get-ApiData "/api/v1/runs/$readRunID/tools")
    if ($runTools.Count -lt 1 -or $runTools[0].tool_name -ne "workspace.read_file" -or $runTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed read tool"
    }
    $readRunEvents = @(Get-ApiData "/api/v1/runs/$readRunID/events")
    if ($readRunEvents.Count -lt 1 -or $readRunEvents[0].root_seq -lt 1 -or $readRunEvents[0].agent_role -eq "") {
      throw "run events API did not include event timeline for read run"
    }
    $sessionTools = @(Get-ApiData "/api/v1/sessions/smoke_read/tools")
    if ($sessionTools.Count -lt 1 -or $sessionTools[0].root_run_id -ne $readRunID) {
      throw "session tool audit projection did not include read run"
    }
  } finally {
    $readWs.Dispose()
  }

  $perRunWs = New-WsClient
  try {
    Start-Run $perRunWs "/read README.md" "smoke_per_run" @{
      working_dir = $Root.Path
      permission_mode = "strict"
      runtime_mode = "per_run_process"
    }
    $perRunEvents = Wait-RunEvents $perRunWs { param($event) } "per-run process"
    Assert-ContainsSequence "per-run process" $perRunEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "per-run process" $perRunEvents
    $perRunID = $perRunEvents[0].root_run_id
    $perRun = Get-ApiData "/api/v1/runs/$perRunID"
    if ($perRun.status -ne "completed" -or $perRun.runtime_mode -ne "per_run_process") {
      throw "per-run process projection expected completed per_run_process run, got '$($perRun | ConvertTo-Json -Compress -Depth 10)'"
    }
    $perRunTools = @(Get-ApiData "/api/v1/runs/$perRunID/tools")
    if ($perRunTools.Count -lt 1 -or $perRunTools[0].tool_name -ne "workspace.read_file" -or $perRunTools[0].status -ne "completed") {
      throw "per-run process tool audit projection did not include completed read tool"
    }
  } finally {
    $perRunWs.Dispose()
  }

  $listWs = New-WsClient
  try {
    Start-Run $listWs "/list scripts" "smoke_list" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $listEvents = Wait-RunEvents $listWs { param($event) } "list"
    Assert-ContainsSequence "list tool" $listEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "list tool" $listEvents
    $listOutput = @($listEvents | Where-Object { $_.type -eq "tool_output" })[0]
    if ($listOutput.payload.delta -notlike "*ws-smoke.ps1*") {
      throw "list tool expected scripts/ws-smoke.ps1 in tool output"
    }
    $listRunID = $listEvents[0].root_run_id
    $listTools = @(Get-ApiData "/api/v1/runs/$listRunID/tools")
    if ($listTools.Count -lt 1 -or $listTools[0].tool_name -ne "workspace.list" -or $listTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed list tool"
    }
  } finally {
    $listWs.Dispose()
  }

  $grepWs = New-WsClient
  try {
    Start-Run $grepWs "/grep red_panda README.md" "smoke_grep" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $grepEvents = Wait-RunEvents $grepWs { param($event) } "grep"
    Assert-ContainsSequence "grep tool" $grepEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "grep tool" $grepEvents
    $grepOutput = @($grepEvents | Where-Object { $_.type -eq "tool_output" })[0]
    if ($grepOutput.payload.delta -notlike "*red_panda*") {
      throw "grep tool expected red_panda match in tool output"
    }
    $grepRunID = $grepEvents[0].root_run_id
    $grepTools = @(Get-ApiData "/api/v1/runs/$grepRunID/tools")
    if ($grepTools.Count -lt 1 -or $grepTools[0].tool_name -ne "workspace.grep" -or $grepTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed grep tool"
    }
  } finally {
    $grepWs.Dispose()
  }

  $shellWs = New-WsClient
  try {
    $shellPermissionID = ""
    Start-Run $shellWs "/shell echo rp-smoke" "smoke_shell" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $shellEvents = Wait-RunEvents $shellWs {
      param($event)
      if ($event.type -eq "permission_required") {
        $script:shellPermissionID = $event.payload.permission_id
        Wait-ApiCondition "pending permission API did not include shell permission" {
          $permission = Try-ApiData "/api/v1/permissions/$($script:shellPermissionID)"
          $script:pendingDiag = "id=$($script:shellPermissionID); permission=$($permission | ConvertTo-Json -Compress -Depth 10)"
          if ($null -eq $permission -or $permission.status -ne "pending") {
            return $false
          }
          $pendingPermissions = @(Get-ApiData "/api/v1/permissions/pending")
          $pendingIDs = @($pendingPermissions | ForEach-Object { $_.id })
          $script:pendingDiag = "id=$($script:shellPermissionID); pending_ids=$($pendingIDs -join ','); permission=$($permission | ConvertTo-Json -Compress -Depth 10)"
          return ($pendingIDs -contains $script:shellPermissionID)
        }
        Send-WsJson $shellWs @{
          id = "perm_shell"
          type = "request"
          method = "permission.resolve"
          payload = @{
            permission_id = $event.payload.permission_id
            run_id = $event.root_run_id
            decision = "approve"
          }
        }
      }
    } "shell"
    Assert-ContainsSequence "shell tool" $shellEvents @("tool_started", "permission_required", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "shell tool" $shellEvents
    $shellPermission = Get-ApiData "/api/v1/permissions/$($script:shellPermissionID)"
    if ($shellPermission.status -ne "resolved" -or $shellPermission.decision -ne "approve") {
      throw "permission projection expected resolved approve status"
    }
    $shellRunPermissions = @(Get-ApiData "/api/v1/runs/$($shellEvents[0].root_run_id)/permissions")
    if ($shellRunPermissions.Count -lt 1 -or $shellRunPermissions[0].id -ne $script:shellPermissionID) {
      throw "run permissions API did not include shell permission"
    }
  } finally {
    $shellWs.Dispose()
  }

  New-Item -ItemType Directory -Force -Path (Split-Path $EditFile) | Out-Null
  Set-Content -LiteralPath $EditFile -Value "alpha" -NoNewline
  $editWs = New-WsClient
  try {
    $editPermissionID = ""
    Start-Run $editWs "/edit tmp/smoke-edit.txt alpha => beta" "smoke_edit" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $editEvents = Wait-RunEvents $editWs {
      param($event)
      if ($event.type -eq "permission_required") {
        $script:editPermissionID = $event.payload.permission_id
        Send-WsJson $editWs @{
          id = "perm_edit"
          type = "request"
          method = "permission.resolve"
          payload = @{
            permission_id = $event.payload.permission_id
            run_id = $event.root_run_id
            decision = "approve"
          }
        }
      }
    } "edit"
    Assert-ContainsSequence "edit tool" $editEvents @("tool_started", "permission_required", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "edit tool" $editEvents
    if ((Get-Content -LiteralPath $EditFile -Raw) -ne "beta") {
      throw "edit tool expected file content to be beta"
    }
    $editPermission = Get-ApiData "/api/v1/permissions/$($script:editPermissionID)"
    if ($editPermission.status -ne "resolved" -or $editPermission.decision -ne "approve") {
      throw "edit permission projection expected resolved approve status"
    }
    $editRunID = $editEvents[0].root_run_id
    $editTools = @(Get-ApiData "/api/v1/runs/$editRunID/tools")
    if ($editTools.Count -lt 1 -or $editTools[0].tool_name -ne "workspace.edit_file" -or $editTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed edit tool"
    }
  } finally {
    $editWs.Dispose()
  }

  New-Item -ItemType Directory -Force -Path (Split-Path $DiffFile) | Out-Null
  Set-Content -LiteralPath $DiffFile -Value "alpha" -NoNewline
  $diffWs = New-WsClient
  try {
    Start-Run $diffWs "/diff tmp/smoke-patch.txt alpha => beta" "smoke_diff" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $diffEvents = Wait-RunEvents $diffWs { param($event) } "diff"
    Assert-ContainsSequence "diff tool" $diffEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "diff tool" $diffEvents
    $diffOutput = @($diffEvents | Where-Object { $_.type -eq "tool_output" })[0]
    if ($diffOutput.payload.delta -notlike "*beta*") {
      throw "diff tool expected beta in tool output"
    }
    $diffRunID = $diffEvents[0].root_run_id
    $diffTools = @(Get-ApiData "/api/v1/runs/$diffRunID/tools")
    if ($diffTools.Count -lt 1 -or $diffTools[0].tool_name -ne "workspace.diff_file" -or $diffTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed diff tool"
    }
  } finally {
    $diffWs.Dispose()
  }

  New-Item -ItemType Directory -Force -Path (Split-Path $PatchFile) | Out-Null
  [IO.File]::WriteAllText($PatchFile, "alpha`n", [Text.UTF8Encoding]::new($false))
  $patchText = @"
--- a/tmp/smoke-apply-patch.txt
+++ b/tmp/smoke-apply-patch.txt
@@ -1 +1 @@
-alpha
+beta
"@
  $patchWs = New-WsClient
  try {
    $patchPermissionID = ""
    Start-Run $patchWs "/patch $patchText" "smoke_patch" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $patchEvents = Wait-RunEvents $patchWs {
      param($event)
      if ($event.type -eq "permission_required") {
        $script:patchPermissionID = $event.payload.permission_id
        Send-WsJson $patchWs @{
          id = "perm_patch"
          type = "request"
          method = "permission.resolve"
          payload = @{
            permission_id = $event.payload.permission_id
            run_id = $event.root_run_id
            decision = "approve"
          }
        }
      }
    } "patch"
    Assert-ContainsSequence "patch tool" $patchEvents @("tool_started", "permission_required", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "patch tool" $patchEvents
    if ((Get-Content -LiteralPath $PatchFile -Raw) -ne "beta`n") {
      throw "patch tool expected file content to be beta"
    }
    $patchPermission = Get-ApiData "/api/v1/permissions/$($script:patchPermissionID)"
    if ($patchPermission.status -ne "resolved" -or $patchPermission.decision -ne "approve") {
      throw "patch permission projection expected resolved approve status"
    }
    $patchRunID = $patchEvents[0].root_run_id
    $patchTools = @(Get-ApiData "/api/v1/runs/$patchRunID/tools")
    if ($patchTools.Count -lt 1 -or $patchTools[0].tool_name -ne "workspace.apply_patch" -or $patchTools[0].status -ne "completed") {
      throw "run tool audit projection did not include completed patch tool"
    }
  } finally {
    $patchWs.Dispose()
  }

  $modelToolWs = New-WsClient
  try {
    Start-Run $modelToolWs "read file README.md" "smoke_model_tool" @{
      working_dir = $Root.Path
      permission_mode = "strict"
    }
    $modelToolEvents = Wait-RunEvents $modelToolWs { param($event) } "model tool"
    Assert-ContainsSequence "model tool loop" $modelToolEvents @("tool_started", "tool_output", "tool_finished", "message_delta", "finish")
    Assert-StrictlyIncreasingRootSeq "model tool loop" $modelToolEvents
    $modelToolOutput = @($modelToolEvents | Where-Object { $_.type -eq "tool_output" })[0]
    if ($modelToolOutput.payload.delta -notlike "*red_panda*") {
      throw "model tool loop expected README content in tool output"
    }
  } finally {
    $modelToolWs.Dispose()
  }

  $subAgentWs = New-WsClient
  try {
    $subAgentID = ""
    $subAgentRunID = ""
    $subAgentList = $null
    $subAgentCancel = $null
    $subAgentCancelled = $null
    $subAgentFinish = $null
    $sentSubAgentRequests = $false

    Start-Run $subAgentWs "spawn planner /subagent" "smoke_subagent" @{
      working_dir = $Root.Path
      permission_mode = "strict"
      spawn_subagents = $true
    }

    $subAgentEvents = @()
    $deadline = [DateTime]::UtcNow.AddSeconds(12)
    while ([DateTime]::UtcNow -lt $deadline) {
      $msg = Read-WsMessage $subAgentWs
      if ($msg.type -eq "error") {
        throw "subagent websocket request '$($msg.id)' failed: $($msg.error.message)"
      }
      if ($msg.type -eq "response") {
        switch ($msg.id) {
          "subagents_list_smoke" { $subAgentList = $msg.payload }
          "subagent_cancel_smoke" { $subAgentCancel = $msg.payload }
        }
        if ($subAgentFinish -and $subAgentList -and $subAgentCancel -and $subAgentCancelled) {
          break
        }
        continue
      }
      if ($msg.type -ne "event" -or $msg.method -ne "run.event") {
        continue
      }

      $event = $msg.payload
      $subAgentEvents += $event
      if ($event.type -eq "subagent_update" -and $event.payload.status -eq "running" -and -not $sentSubAgentRequests) {
        $subAgentID = $event.payload.subagent_id
        $subAgentRunID = $event.root_run_id
        if ($subAgentID -eq "" -or $subAgentRunID -eq "") {
          throw "subagent running update did not include subagent_id/root_run_id"
        }
        Send-WsJson $subAgentWs @{
          id = "subagents_list_smoke"
          type = "request"
          method = "subagents.list"
          payload = @{
            run_id = $subAgentRunID
          }
        }
        Send-WsJson $subAgentWs @{
          id = "subagent_cancel_smoke"
          type = "request"
          method = "subagent.cancel"
          payload = @{
            run_id = $subAgentRunID
            subagent_id = $subAgentID
            reason = "smoke"
          }
        }
        $sentSubAgentRequests = $true
      } elseif ($event.type -eq "subagent_update" -and $event.payload.status -eq "cancelled") {
        $subAgentCancelled = $event
      } elseif ($event.type -eq "finish") {
        $subAgentFinish = $event
      }

      if ($subAgentFinish -and $subAgentList -and $subAgentCancel -and $subAgentCancelled) {
        break
      }
    }

    if (-not $sentSubAgentRequests) {
      throw "subagent smoke did not receive running subagent_update before timeout"
    }
    if (-not $subAgentList) {
      throw "subagents.list response was not received"
    }
    $runningRecords = @($subAgentList.items | Where-Object {
      $_.subagent_id -eq $subAgentID -and $_.root_run_id -eq $subAgentRunID -and $_.status -eq "running"
    })
    if ($runningRecords.Count -ne 1) {
      throw "subagents.list expected one running record for '$subAgentID', got '$($subAgentList | ConvertTo-Json -Compress -Depth 10)'"
    }
    if (-not $subAgentCancel -or -not $subAgentCancel.accepted -or -not $subAgentCancel.cancelled -or $subAgentCancel.run_id -ne $subAgentRunID -or $subAgentCancel.subagent_id -ne $subAgentID) {
      throw "subagent.cancel expected accepted cancelled response, got '$($subAgentCancel | ConvertTo-Json -Compress -Depth 10)'"
    }
    if (-not $subAgentCancelled -or $subAgentCancelled.payload.subagent_id -ne $subAgentID) {
      throw "subagent smoke expected cancelled subagent_update for '$subAgentID'"
    }
    if (-not $subAgentFinish -or $subAgentFinish.payload.status -ne "completed") {
      throw "subagent smoke expected run finish status completed, got '$($subAgentFinish.payload.status)'"
    }
    Assert-ContainsSequence "subagent cancel" $subAgentEvents @("subagent_update", "subagent_update", "finish")
    Assert-StrictlyIncreasingRootSeq "subagent cancel" $subAgentEvents
    $subAgentRun = Get-ApiData "/api/v1/runs/$subAgentRunID"
    if ($subAgentRun.status -ne "completed") {
      throw "subagent run projection expected completed status"
    }
  } finally {
    $subAgentWs.Dispose()
  }

  $processSubAgentWs = New-WsClient
  try {
    Start-Run $processSubAgentWs "spawn runtime process planner /subagent" "smoke_process_subagent" @{
      working_dir = $Root.Path
      permission_mode = "strict"
      spawn_subagents = $true
      subagent_backend = "runtime_process"
    }
    $processSubAgentEvents = Wait-RunEvents $processSubAgentWs { param($event) } "runtime process subagent"
    Assert-ContainsSequence "runtime process subagent" $processSubAgentEvents @("subagent_update", "message_delta", "subagent_update", "finish")
    Assert-StrictlyIncreasingRootSeq "runtime process subagent" $processSubAgentEvents
    $processRunning = @($processSubAgentEvents | Where-Object {
      $_.type -eq "subagent_update" -and $_.payload.status -eq "running" -and $_.payload.backend -eq "runtime_process"
    })[0]
    if (-not $processRunning) {
      throw "runtime process subagent expected running update with backend runtime_process"
    }
    $processMessage = @($processSubAgentEvents | Where-Object {
      $_.type -eq "message_delta" -and $_.agent.role -eq "subagent" -and $_.payload.backend -eq "runtime_process"
    })[0]
    if (-not $processMessage -or $processMessage.parent_run_id -ne $processMessage.root_run_id) {
      throw "runtime process subagent expected bridged subagent message on root run channel"
    }
    $processCompleted = @($processSubAgentEvents | Where-Object {
      $_.type -eq "subagent_update" -and $_.payload.status -eq "completed" -and $_.payload.backend -eq "runtime_process"
    })[-1]
    if (-not $processCompleted) {
      throw "runtime process subagent expected completed update with backend runtime_process"
    }
    $processRunID = $processSubAgentEvents[0].root_run_id
    $processSubAgentRun = Get-ApiData "/api/v1/runs/$processRunID"
    if ($processSubAgentRun.status -ne "completed") {
      throw "runtime process subagent run projection expected completed status"
    }
  } finally {
    $processSubAgentWs.Dispose()
  }

  $poolSubAgentWs = New-WsClient
  try {
    Start-Run $poolSubAgentWs "spawn pooled planner /subagent" "smoke_pool_subagent" @{
      working_dir = $Root.Path
      permission_mode = "strict"
      spawn_subagents = $true
      subagent_backend = "process_pool"
    }
    $poolSubAgentEvents = Wait-RunEvents $poolSubAgentWs { param($event) } "process pool subagent"
    Assert-ContainsSequence "process pool subagent" $poolSubAgentEvents @("subagent_update", "message_delta", "subagent_update", "finish")
    Assert-StrictlyIncreasingRootSeq "process pool subagent" $poolSubAgentEvents
    $poolRunning = @($poolSubAgentEvents | Where-Object {
      $_.type -eq "subagent_update" -and $_.payload.status -eq "running" -and $_.payload.backend -eq "process_pool"
    })[0]
    if (-not $poolRunning) {
      throw "process pool subagent expected running update with backend process_pool"
    }
    $poolMessage = @($poolSubAgentEvents | Where-Object {
      $_.type -eq "message_delta" -and $_.agent.role -eq "subagent" -and $_.payload.backend -eq "process_pool"
    })[0]
    if (-not $poolMessage -or $poolMessage.parent_run_id -ne $poolMessage.root_run_id) {
      throw "process pool subagent expected bridged subagent message on root run channel"
    }
    $poolCompleted = @($poolSubAgentEvents | Where-Object {
      $_.type -eq "subagent_update" -and $_.payload.status -eq "completed" -and $_.payload.backend -eq "process_pool"
    })[-1]
    if (-not $poolCompleted) {
      throw "process pool subagent expected completed update with backend process_pool"
    }
    $poolRunID = $poolSubAgentEvents[0].root_run_id
    $poolSubAgentRun = Get-ApiData "/api/v1/runs/$poolRunID"
    if ($poolSubAgentRun.status -ne "completed") {
      throw "process pool subagent run projection expected completed status"
    }
  } finally {
    $poolSubAgentWs.Dispose()
  }

  $denyWs = New-WsClient
  try {
    Start-Run $denyWs "/shell echo should-not-run" "smoke_deny" @{
      working_dir = $Root.Path
      permission_mode = "allow_all"
      tool_policy = "allow_all"
      tool_denylist = @("shell.exec")
    }
    $denyEvents = Wait-RunEvents $denyWs { param($event) } "deny"
    Assert-ContainsSequence "denylist" $denyEvents @("tool_started", "tool_failed", "error", "finish")
    $denyFinish = @($denyEvents | Where-Object { $_.type -eq "finish" })[-1]
    if ($denyFinish.payload.status -ne "denied") {
      throw "denylist expected finish status denied, got '$($denyFinish.payload.status)'"
    }
    $denyRunID = $denyEvents[0].root_run_id
    $denyRun = Get-ApiData "/api/v1/runs/$denyRunID"
    if ($denyRun.status -ne "denied" -or $denyRun.error -eq "") {
      throw "denylist run projection expected denied status and error"
    }
  } finally {
    $denyWs.Dispose()
  }

  $cancelWs = New-WsClient
  try {
    $cancelPermissionID = ""
    Start-Run $cancelWs "cancel me /permission" "smoke_cancel" @{
      working_dir = $Root.Path
      require_permission = $true
      permission_mode = "strict"
    }
    $cancelEvents = Wait-RunEvents $cancelWs {
      param($event)
      if ($event.type -eq "permission_required") {
        $script:cancelPermissionID = $event.payload.permission_id
        Send-WsJson $cancelWs @{
          id = "cancel_run"
          type = "request"
          method = "run.cancel"
          payload = @{
            run_id = $event.root_run_id
            reason = "smoke"
          }
        }
      }
    } "cancel"
    Assert-ContainsSequence "cancel run" $cancelEvents @("permission_required", "finish")
    $finish = @($cancelEvents | Where-Object { $_.type -eq "finish" })[-1]
    if ($finish.payload.status -ne "cancelled") {
      throw "cancel run expected finish status cancelled, got '$($finish.payload.status)'"
    }
    Assert-StrictlyIncreasingRootSeq "cancel run" $cancelEvents
    $cancelRunID = $cancelEvents[0].root_run_id
    $cancelRun = Get-ApiData "/api/v1/runs/$cancelRunID"
    if ($cancelRun.status -ne "cancelled") {
      throw "cancel run projection expected cancelled status"
    }
    $cancelPermission = Get-ApiData "/api/v1/permissions/$($script:cancelPermissionID)"
    if ($cancelPermission.status -ne "closed") {
      throw "cancel permission projection expected closed status"
    }
  } finally {
    $cancelWs.Dispose()
  }

  [pscustomobject]@{
    ok = $true
    read_events = @($readEvents | ForEach-Object { $_.type }) -join ","
    read_run_status = "$($readRun.status):tools=$($readRun.tool_count)"
    read_run_events = $readRunEvents.Count
    read_tool_audit = "$($runTools[0].tool_name):$($runTools[0].status)"
    per_run_events = @($perRunEvents | ForEach-Object { $_.type }) -join ","
    per_run_status = "$($perRun.status):$($perRun.runtime_mode)"
    per_run_tool_audit = "$($perRunTools[0].tool_name):$($perRunTools[0].status)"
    list_events = @($listEvents | ForEach-Object { $_.type }) -join ","
    list_tool_audit = "$($listTools[0].tool_name):$($listTools[0].status)"
    grep_events = @($grepEvents | ForEach-Object { $_.type }) -join ","
    grep_tool_audit = "$($grepTools[0].tool_name):$($grepTools[0].status)"
    shell_events = @($shellEvents | ForEach-Object { $_.type }) -join ","
    shell_permission = "$($shellPermission.status):$($shellPermission.decision)"
    edit_events = @($editEvents | ForEach-Object { $_.type }) -join ","
    edit_permission = "$($editPermission.status):$($editPermission.decision)"
    edit_tool_audit = "$($editTools[0].tool_name):$($editTools[0].status)"
    diff_events = @($diffEvents | ForEach-Object { $_.type }) -join ","
    diff_tool_audit = "$($diffTools[0].tool_name):$($diffTools[0].status)"
    patch_events = @($patchEvents | ForEach-Object { $_.type }) -join ","
    patch_permission = "$($patchPermission.status):$($patchPermission.decision)"
    patch_tool_audit = "$($patchTools[0].tool_name):$($patchTools[0].status)"
    model_tool_events = @($modelToolEvents | ForEach-Object { $_.type }) -join ","
    subagent_events = @($subAgentEvents | ForEach-Object { "$($_.type):$($_.payload.status)" }) -join ","
    subagent_record = "$($subAgentID):$($runningRecords[0].status)"
    subagent_cancel = "$($subAgentCancel.accepted):$($subAgentCancel.cancelled)"
    subagent_run_status = $subAgentRun.status
    process_subagent_events = @($processSubAgentEvents | ForEach-Object { "$($_.type):$($_.payload.status)" }) -join ","
    process_subagent_backend = $processRunning.payload.backend
    process_subagent_run_status = $processSubAgentRun.status
    pool_subagent_events = @($poolSubAgentEvents | ForEach-Object { "$($_.type):$($_.payload.status)" }) -join ","
    pool_subagent_backend = $poolRunning.payload.backend
    pool_subagent_run_status = $poolSubAgentRun.status
    provider_profile = "$($providerProfileID):$($providerProfileUpdated.model)"
    provider_profile_run = $providerRun.status
    provider_profile_delete = if ($providerProfileAfterDelete) { "deactivated" } else { "removed" }
    deny_events = @($denyEvents | ForEach-Object { "$($_.type):$($_.payload.status)" }) -join ","
    cancel_events = @($cancelEvents | ForEach-Object { "$($_.type):$($_.payload.status)" }) -join ","
    cancel_run_status = $cancelRun.status
    cancel_permission = $cancelPermission.status
  } | ConvertTo-Json -Compress
} finally {
  Stop-Process -Id $gateway.Id -Force -ErrorAction SilentlyContinue
  if ($providerJob) {
    Stop-Job $providerJob -ErrorAction SilentlyContinue
    Remove-Job $providerJob -Force -ErrorAction SilentlyContinue
  }
  Remove-SmokeDatabase
}
