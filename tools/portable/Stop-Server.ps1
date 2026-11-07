[CmdletBinding(SupportsShouldProcess)]
param([switch]$DryRun)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$packageRoot = [IO.Path]::GetFullPath($PSScriptRoot)
$serverPath = Join-Path $packageRoot 'kairi-server.exe'
$statePath = Join-Path $packageRoot '_local\data\server-state.json'

if (-not (Test-Path -LiteralPath $statePath -PathType Leaf)) {
    [ordered]@{ schema_version = 1; status = 'NOT_RUNNING' } | ConvertTo-Json
    return
}

$state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
if ($null -eq $state.process_id -or [int]$state.process_id -le 0) {
    [ordered]@{ schema_version = 1; status = 'NOT_RUNNING' } | ConvertTo-Json
    return
}

$process = Get-Process -Id ([int]$state.process_id) -ErrorAction SilentlyContinue
if ($null -eq $process) {
    if (-not $DryRun -and $PSCmdlet.ShouldProcess($statePath, 'Record already stopped portable server')) {
        $state.status = 'STOPPED'
        $state | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $statePath -Encoding UTF8
    }
    [ordered]@{ schema_version = 1; status = 'ALREADY_STOPPED'; process_id = [int]$state.process_id } |
        ConvertTo-Json
    return
}

$actualPath = $null
try { $actualPath = [IO.Path]::GetFullPath($process.Path) } catch {}
if ($null -eq $actualPath -or
    -not $actualPath.Equals($serverPath, [StringComparison]::OrdinalIgnoreCase)) {
    throw "PID $($state.process_id) does not belong to this package. No process was stopped."
}

if ($DryRun -or -not $PSCmdlet.ShouldProcess("PID $($state.process_id)", 'Request graceful portable server shutdown')) {
    [ordered]@{ schema_version = 1; status = 'UNCHANGED'; process_id = [int]$state.process_id } |
        ConvertTo-Json
    return
}

if ($state.PSObject.Properties.Name -notcontains 'shutdown_event' -or
    -not ([string]$state.shutdown_event).StartsWith('Local\Kairisei-', [StringComparison]::Ordinal)) {
    throw 'This older server has no graceful shutdown channel. It was not forcefully terminated.'
}
$shutdown = [Threading.EventWaitHandle]::OpenExisting([string]$state.shutdown_event)
try { [void]$shutdown.Set() } finally { $shutdown.Dispose() }
$deadline = [DateTime]::UtcNow.AddSeconds(20)
while ([DateTime]::UtcNow -lt $deadline -and
    $null -ne (Get-Process -Id ([int]$state.process_id) -ErrorAction SilentlyContinue)) {
    Start-Sleep -Milliseconds 200
}
if ($null -ne (Get-Process -Id ([int]$state.process_id) -ErrorAction SilentlyContinue)) {
    throw "Server PID $($state.process_id) is still draining operations after 20 seconds. It was not forcefully terminated."
}
$state.status = 'STOPPED'
$state | Add-Member -NotePropertyName stopped_utc -NotePropertyValue ([DateTime]::UtcNow.ToString('o')) -Force
$state | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $statePath -Encoding UTF8

[ordered]@{ schema_version = 1; status = 'STOPPED'; process_id = [int]$state.process_id } |
    ConvertTo-Json
