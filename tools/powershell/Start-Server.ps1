[CmdletBinding(SupportsShouldProcess)]
param(
    [string]$PackageRoot = (Join-Path $PSScriptRoot '../../_local/deployment/kairisei-ma-cn602-server'),
    [string]$AdvertiseHost,
    [ValidateRange(1,65533)][int]$Port,
    [switch]$IncludeAndroid,
    [switch]$ValidationOnly,
    [switch]$DryRun
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$entry = Join-Path ([IO.Path]::GetFullPath($PackageRoot)) 'Start-Server.ps1'
if (-not (Test-Path -LiteralPath $entry -PathType Leaf)) { throw 'Extract the complete release into PackageRoot first; see docs/BUILD.md.' }
$arguments = @{ DryRun = $DryRun; ValidationOnly = $ValidationOnly; IncludeAndroid = $IncludeAndroid }
foreach ($name in @('AdvertiseHost', 'Port')) {
    if ($PSBoundParameters.ContainsKey($name)) { $arguments[$name] = $PSBoundParameters[$name] }
}
if ($DryRun -or $PSCmdlet.ShouldProcess($entry, 'Start the extracted server package')) {
    & $entry @arguments -Confirm:$false
}
