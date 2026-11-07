[CmdletBinding(SupportsShouldProcess)]
param(
    [ValidateNotNullOrEmpty()]
    [string]$AdvertiseHost = '10.0.2.2',

    [ValidateNotNullOrEmpty()]
    [string]$ListenHost = '0.0.0.0',

    [ValidateRange(1, 65533)]
    [int]$Port = 26040,

    [ValidateRange(5, 120)]
    [int]$ReadinessTimeoutSeconds = 60,

    [switch]$DryRun,

    [switch]$IncludeAndroid,

    [switch]$ValidationOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Assert-AdvertiseHost {
    param([string]$Address)

    $Address = $Address.Trim().ToLowerInvariant()
    if ($Address.Length -lt 1 -or $Address.Length -gt 253) { throw 'Invalid server host length.' }
    if ($Address -notmatch '^[0-9.]+$') {
        foreach ($label in $Address.Split('.')) {
            if ($label -notmatch '^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$') { throw 'Server host must be an IPv4 address or DNS name without URL syntax.' }
        }
        return $Address
    }
    $parsed = $null
    if ($Address -notmatch '^(?:0|[1-9][0-9]{0,2})(?:\.(?:0|[1-9][0-9]{0,2})){3}$') { throw 'Invalid IPv4 address.' }
    if (-not [Net.IPAddress]::TryParse($Address, [ref]$parsed) -or
        $parsed.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) {
        throw "AdvertiseHost must be an IPv4 address: $Address"
    }
    $bytes = $parsed.GetAddressBytes()
    if ($bytes[0] -eq 0 -or $bytes[0] -ge 224) {
        throw "AdvertiseHost must be a unicast IPv4 address: $Address"
    }
    return $parsed.ToString()
}

function Assert-RequiredPath {
    param([string]$LiteralPath, [string]$PathType = 'Leaf')
    if (-not (Test-Path -LiteralPath $LiteralPath -PathType $PathType)) {
        throw "Required portable-server path is missing: $LiteralPath"
    }
}

function Quote-NativeArgument {
    param([string]$Value)
    if ($Value.Contains('"')) {
        throw 'Portable-server paths and arguments must not contain a double quote.'
    }
    return '"' + $Value + '"'
}

function Add-NativeArgument {
    param(
        [Collections.Generic.List[string]]$List,
        [string]$Name,
        [string]$Value
    )
    $List.Add($Name)
    $List.Add((Quote-NativeArgument -Value $Value))
}

function Test-PortAvailable {
    param([int]$CandidatePort)
    $listener = $null
    try {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $CandidatePort)
        $listener.Start()
        return $true
    }
    catch {
        return $false
    }
    finally {
        if ($null -ne $listener) { $listener.Stop() }
    }
}

$packageRoot = [IO.Path]::GetFullPath($PSScriptRoot)
function Resolve-PackagePath {
    param([string]$RelativePath)
    if (-not $RelativePath -or [IO.Path]::IsPathRooted($RelativePath) -or
        $RelativePath.Contains(':') -or $RelativePath.Contains('\') -or
        @($RelativePath.Split('/') | Where-Object { $_ -in @('', '.', '..') }).Count) {
        throw "Invalid package-relative path: $RelativePath"
    }
    $path = [IO.Path]::GetFullPath((Join-Path $packageRoot $RelativePath))
    if (-not $path.StartsWith($packageRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Path leaves this package.' }
    Assert-RequiredPath -LiteralPath $path
    return $path
}
$serverPath = Resolve-PackagePath 'kairi-server.exe'
$deploymentPath = Resolve-PackagePath 'deployment.json'
$deployment = Get-Content -LiteralPath $deploymentPath -Raw -Encoding UTF8 | ConvertFrom-Json
if (-not $PSBoundParameters.ContainsKey('AdvertiseHost')) { $AdvertiseHost = [string]$deployment.advertise_host }
if (-not $PSBoundParameters.ContainsKey('Port')) { $Port = [int]$deployment.port }
$resourceSetPath = Resolve-PackagePath 'resource-set/resource-set.json'
if (Test-Path -LiteralPath (Join-Path $packageRoot 'Clean-RetiredResources.ps1')) {
    $normalization = & (Resolve-PackagePath 'Clean-RetiredResources.ps1') -DryRun:$DryRun -WhatIf:$WhatIfPreference -PassThru
    if (($DryRun -or $WhatIfPreference) -and $normalization.Found -gt 0) {
        Write-Host '预览完成；正常启动时将先移除上述退役文件，再校验资源并启动。未启动服务。'
        return
    }
}
. (Resolve-PackagePath 'Read-ResourceSet.ps1')
$resourceSet = Read-PortableResourceSet -ManifestPath $resourceSetPath
if ($resourceSet.Manifest.policy.publishable -ne $true -or $deployment.validation_only) {
    if (-not $ValidationOnly) { throw 'This test package requires -ValidationOnly; use the packaged launcher.' }
}
$resourceSetSHA256 = (Get-FileHash -LiteralPath $resourceSetPath -Algorithm SHA256).Hash.ToLowerInvariant()
$platformContents = @($deployment.platform_contents | ForEach-Object { Resolve-PackagePath ([string]$_) })
if ($IncludeAndroid) {
    $androidProfilePath = Resolve-PackagePath 'platform/android/delivery.json'
    if ($platformContents -notcontains $androidProfilePath) { $platformContents += $androidProfilePath }
}
if ($platformContents.Count -lt 1 -or $platformContents.Count -gt 2) { throw 'Select one or two PC/Android platform deliveries.' }
$platformHashes = @{}
foreach ($profilePath in $platformContents) {
    $platformProfile = Get-Content -LiteralPath $profilePath -Raw -Encoding UTF8 | ConvertFrom-Json
    $platform = [string]$platformProfile.platform
    if ($platformProfile.format -ne 1 -or $platform -cnotin @('StandaloneWindows64', 'Android')) { throw 'Expected a PC or Android platform delivery.' }
    if ($platformHashes.ContainsKey($platform)) { throw "Repeated platform delivery: $platform" }
    $platformHashes[$platform] = (Get-FileHash -LiteralPath $profilePath -Algorithm SHA256).Hash.ToLowerInvariant()
}
# Sort by platform identity so deployment list order does not affect restarts.
$platformIdentity = ($platformHashes.Keys | Sort-Object | ForEach-Object { "${_}:$($platformHashes[$_])" }) -join "`n"
$profileHash = [Security.Cryptography.SHA256]::Create()
try {
    $platformContentsSHA256 = [BitConverter]::ToString($profileHash.ComputeHash([Text.Encoding]::UTF8.GetBytes($platformIdentity))).Replace('-', '').ToLowerInvariant()
}
finally { $profileHash.Dispose() }
$resourceInputs = $resourceSet.Entrypoints
$bindings = @{
    'cn-save-seed'='saveSeedPath'; 'cn-asset-map'='assetMapPath'; 'cn-card-master'='cardMasterPath'
    'cn-explore-master'='exploreMasterPath'; 'cn-story-master'='storyMasterPath'; 'cn-battle-master'='battleMasterPath'
    'cn-combat-card-master'='combatCardPath'; 'cn-combat-master-root'='combatMasterRoot'; 'cn-navi-master'='naviMasterPath'
    'cn-item-master'='itemMasterPath'; 'cn-avatar-master'='avatarMasterPath'; 'cn-gacha-banner'='gachaBannerPath'
    'cn-five-star-gacha-banner'='fiveStarBannerPath'; 'cn-home-banner'='homeBannerPath'; 'cn-stamp-master'='stampMasterPath'
    'cn-honor-master'='honorMasterPath'; 'cn-pvp-master'='pvpMasterPath'; 'cn-player-progression'='playerProgressionPath'
    'cn-login-bonus'='loginBonusPath'; 'cn-cpk-root'='cpkRoot'; 'cn-cpk-aliases'='cpkAliasesPath'; 'cn-image-root'='imageRoot'
}
foreach ($name in $bindings.Keys) {
    if (-not $resourceInputs.ContainsKey($name)) { throw "Missing resource entrypoint: $name" }
    Set-Variable -Name $bindings[$name] -Value $resourceInputs[$name]
}
$dataRoot = Join-Path $packageRoot '_local/data'
$runRoot = Join-Path $packageRoot ('_local/runtime/server-runs/' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssfffZ'))
$statePath = Join-Path $dataRoot 'server-state.json'
$savePath = Join-Path $dataRoot 'cn602-save.json'
$saveDatabasePath = Join-Path $dataRoot 'cn602-save-state.sqlite3'
$requestLogPath = Join-Path $runRoot 'requests.jsonl'
$stdoutPath = Join-Path $runRoot 'stdout.log'
$stderrPath = Join-Path $runRoot 'stderr.log'
if ($Port -lt 1 -or $Port -gt 65533) {
    throw "Port must be 1 through 65533: $Port"
}
$battlePort = $Port + 1
$adminPort = $Port + 2
$healthURL = "http://127.0.0.1:$Port/healthz"
$adminHealthURL = "http://127.0.0.1:$adminPort/api/health"

$AdvertiseHost = Assert-AdvertiseHost -Address $AdvertiseHost
if (Test-Path -LiteralPath $statePath -PathType Leaf) {
    $oldState = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    if ($null -ne $oldState.process_id -and [int]$oldState.process_id -gt 0) {
        $oldProcess = Get-Process -Id ([int]$oldState.process_id) -ErrorAction SilentlyContinue
        if ($null -ne $oldProcess) {
            $oldProcessPath = $null
            try { $oldProcessPath = [IO.Path]::GetFullPath($oldProcess.Path) } catch {}
            if ($null -ne $oldProcessPath -and
                $oldProcessPath.Equals($serverPath, [StringComparison]::OrdinalIgnoreCase)) {
                if ($oldState.port -ne $Port -or $oldState.listen_host -ne $ListenHost -or
                    $oldState.advertise_host -ne $AdvertiseHost -or
                    ($null -ne $resourceInputs -and
                     ($oldState.PSObject.Properties.Name -notcontains 'resource_set_sha256' -or
                      $oldState.resource_set_sha256 -ne $resourceSetSHA256)) -or
                    ($oldState.PSObject.Properties.Name -notcontains 'platform_contents_sha256') -or
                    ([string]$oldState.platform_contents_sha256 -ne $platformContentsSHA256)) {
                    throw 'Running package uses different resources or network settings; stop it before restarting.'
                }
                [ordered]@{
                    schema_version = 1
                    status = 'ALREADY_RUNNING'
                    process_id = [int]$oldState.process_id
                    health_url = $healthURL
                    admin_url = "http://127.0.0.1:$adminPort/"
                } | ConvertTo-Json -Depth 4
                return
            }
            throw "Recorded PID $($oldState.process_id) belongs to another executable; refusing to stop or replace it."
        }
    }
}


$arguments = [Collections.Generic.List[string]]::new()
$cdnConfigPath = Join-Path $packageRoot 'cdn.json'
if (Test-Path -LiteralPath $cdnConfigPath -PathType Leaf) {
    Add-NativeArgument -List $arguments -Name '-cdn-config' -Value $cdnConfigPath
}
Add-NativeArgument -List $arguments -Name '-cdn-sync-config' -Value (Join-Path $packageRoot 'cdn-sync.json')
Add-NativeArgument -List $arguments -Name '-client-profile' -Value 'cn602-bootstrap'
Add-NativeArgument -List $arguments -Name '-request-log' -Value $requestLogPath
Add-NativeArgument -List $arguments -Name '-admin-log-file' -Value $stdoutPath
Add-NativeArgument -List $arguments -Name '-cn-save-path' -Value $savePath
Add-NativeArgument -List $arguments -Name '-cn-save-seed' -Value $saveSeedPath
Add-NativeArgument -List $arguments -Name '-cn-asset-map' -Value $assetMapPath
Add-NativeArgument -List $arguments -Name '-cn-card-master' -Value $cardMasterPath
Add-NativeArgument -List $arguments -Name '-cn-explore-master' -Value $exploreMasterPath
Add-NativeArgument -List $arguments -Name '-cn-story-master' -Value $storyMasterPath
Add-NativeArgument -List $arguments -Name '-cn-battle-master' -Value $battleMasterPath
Add-NativeArgument -List $arguments -Name '-cn-combat-card-master' -Value $combatCardPath
Add-NativeArgument -List $arguments -Name '-cn-combat-master-root' -Value $combatMasterRoot
Add-NativeArgument -List $arguments -Name '-cn-navi-master' -Value $naviMasterPath
Add-NativeArgument -List $arguments -Name '-cn-item-master' -Value $itemMasterPath
Add-NativeArgument -List $arguments -Name '-cn-avatar-master' -Value $avatarMasterPath
Add-NativeArgument -List $arguments -Name '-cn-gacha-banner' -Value $gachaBannerPath
Add-NativeArgument -List $arguments -Name '-cn-five-star-gacha-banner' -Value $fiveStarBannerPath
Add-NativeArgument -List $arguments -Name '-cn-home-banner' -Value $homeBannerPath
Add-NativeArgument -List $arguments -Name '-cn-stamp-master' -Value $stampMasterPath
Add-NativeArgument -List $arguments -Name '-cn-honor-master' -Value $honorMasterPath
Add-NativeArgument -List $arguments -Name '-cn-pvp-master' -Value $pvpMasterPath
Add-NativeArgument -List $arguments -Name '-cn-player-progression' -Value $playerProgressionPath
Add-NativeArgument -List $arguments -Name '-cn-login-bonus' -Value $loginBonusPath
Add-NativeArgument -List $arguments -Name '-cn-cpk-root' -Value $cpkRoot
Add-NativeArgument -List $arguments -Name '-cn-cpk-aliases' -Value $cpkAliasesPath
if ($null -ne $imageRoot) {
    Add-NativeArgument -List $arguments -Name '-cn-image-root' -Value $imageRoot
}
foreach ($profilePath in $platformContents) {
    Add-NativeArgument -List $arguments -Name '-platform-content' -Value $profilePath
}
Add-NativeArgument -List $arguments -Name '-listen-host' -Value $ListenHost
Add-NativeArgument -List $arguments -Name '-advertise-host' -Value $AdvertiseHost
Add-NativeArgument -List $arguments -Name '-port' -Value ([string]$Port)
Add-NativeArgument -List $arguments -Name '-battle-port' -Value ([string]$battlePort)
Add-NativeArgument -List $arguments -Name '-admin-listen-host' -Value '127.0.0.1'
Add-NativeArgument -List $arguments -Name '-admin-port' -Value ([string]$adminPort)
$shutdownEvent = 'Local\Kairisei-' + [Guid]::NewGuid().ToString('N')
Add-NativeArgument -List $arguments -Name '-shutdown-event' -Value $shutdownEvent

$preview = [ordered]@{
    schema_version = 1
    action = 'START_PORTABLE_CN602_SERVER'
    executable = $serverPath
    resource_set_sha256 = $resourceSetSHA256
    platform_contents_sha256 = $platformContentsSHA256
    advertise_host = $AdvertiseHost
    listen_host = $ListenHost
    port = $Port
    battle_port = $battlePort
    admin_port = $adminPort
    save_path = $saveDatabasePath
    arguments = $arguments.ToArray()
    fresh_save_created_on_first_run = -not (Test-Path -LiteralPath $saveDatabasePath -PathType Leaf)
}
if ($DryRun -or -not $PSCmdlet.ShouldProcess($serverPath, 'Start portable local server')) {
    $preview | ConvertTo-Json -Depth 4
    return
}

foreach ($candidatePort in @($Port, $battlePort, $adminPort)) {
    if (-not (Test-PortAvailable -CandidatePort $candidatePort)) {
        throw "TCP port $candidatePort is already in use. No existing process was stopped."
    }
}

New-Item -ItemType Directory -Path $dataRoot -Force | Out-Null
New-Item -ItemType Directory -Path $runRoot -Force | Out-Null
# The server initializes a missing save through the normal onboarding factory.
New-Item -ItemType File -Path $requestLogPath -Force | Out-Null

$process = Start-Process `
    -FilePath $serverPath `
    -ArgumentList $arguments.ToArray() `
    -WorkingDirectory $packageRoot `
    -WindowStyle Hidden `
    -RedirectStandardOutput $stdoutPath `
    -RedirectStandardError $stderrPath `
    -PassThru

# Keep the control channel available even while initialization is pending.
[ordered]@{
    schema_version = 1; status = 'STARTING'; process_id = [int]$process.Id
    executable = $serverPath; shutdown_event = $shutdownEvent
    listen_host = $ListenHost; resource_set_sha256 = $resourceSetSHA256; platform_contents_sha256 = $platformContentsSHA256
    port = $Port; battle_port = $battlePort; admin_port = $adminPort; advertise_host = $AdvertiseHost
    stdout_path = $stdoutPath; stderr_path = $stderrPath
} | ConvertTo-Json | Set-Content -LiteralPath $statePath -Encoding UTF8

$deadline = [DateTime]::UtcNow.AddSeconds($ReadinessTimeoutSeconds)
$health = $null
$adminHealth = $null
while ([DateTime]::UtcNow -lt $deadline) {
    if ($null -eq (Get-Process -Id $process.Id -ErrorAction SilentlyContinue)) { break }
    try {
        $healthResponse = Invoke-WebRequest -Uri $healthURL -UseBasicParsing -TimeoutSec 2
        if ($healthResponse.StatusCode -eq 200) { $health = $healthResponse.Content | ConvertFrom-Json }
    } catch {}
    try {
        $adminResponse = Invoke-WebRequest -Uri $adminHealthURL -UseBasicParsing -TimeoutSec 2
        if ($adminResponse.StatusCode -eq 200) { $adminHealth = $adminResponse.Content | ConvertFrom-Json }
    } catch {}
    if ($null -ne $health -and [string]$health.state -eq 'PASS' -and
        $null -ne $adminHealth -and [string]$adminHealth.state -eq 'PASS') {
        break
    }
    Start-Sleep -Milliseconds 250
}

$ready = ($null -ne $health -and [string]$health.state -eq 'PASS' -and
    $null -ne $adminHealth -and [string]$adminHealth.state -eq 'PASS' -and
    $null -ne (Get-Process -Id $process.Id -ErrorAction SilentlyContinue))
if (-not $ready) {
    $exitedBeforeReady = ($null -eq (Get-Process -Id $process.Id -ErrorAction SilentlyContinue))
    if (-not $exitedBeforeReady) {
        $shutdown = [Threading.EventWaitHandle]::OpenExisting($shutdownEvent)
        try { [void]$shutdown.Set() } finally { $shutdown.Dispose() }
        [void]$process.WaitForExit(10000)
    }
    $stderr = if (Test-Path -LiteralPath $stderrPath -PathType Leaf) {
        Get-Content -LiteralPath $stderrPath -Raw -Encoding UTF8
    } else { '' }
    $stdout = if (Test-Path -LiteralPath $stdoutPath -PathType Leaf) {
        Get-Content -LiteralPath $stdoutPath -Raw -Encoding UTF8
    } else { '' }
    if ($exitedBeforeReady) {
        throw "Portable server exited before becoming ready. STDERR: $stderr STDOUT: $stdout"
    }
    throw "Portable server readiness timed out after $ReadinessTimeoutSeconds seconds. STDERR: $stderr STDOUT: $stdout"
}

$state = [ordered]@{
    schema_version = 1
    status = 'RUNNING'
    process_id = [int]$process.Id
    shutdown_event = $shutdownEvent
    executable = $serverPath
    resource_set_sha256 = $resourceSetSHA256
    platform_contents_sha256 = $platformContentsSHA256
    executable_sha256 = (Get-FileHash -LiteralPath $serverPath -Algorithm SHA256).Hash.ToLowerInvariant()
    started_utc = [DateTime]::UtcNow.ToString('o')
    advertise_host = $AdvertiseHost
    listen_host = $ListenHost
    port = $Port
    battle_port = $battlePort
    admin_port = $adminPort
    health_url = $healthURL
    admin_url = "http://127.0.0.1:$adminPort/"
    save_path = $saveDatabasePath
    stdout_path = $stdoutPath
    stderr_path = $stderrPath
    request_log_path = $requestLogPath
    health = $health
    admin_health = $adminHealth
}
$state | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $statePath -Encoding UTF8
$state | ConvertTo-Json -Depth 6
