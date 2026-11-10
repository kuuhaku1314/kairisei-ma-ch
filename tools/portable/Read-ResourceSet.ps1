# Resource-set packages need only Windows PowerShell at runtime.
function Read-PortableResourceSet {
    param([string]$ManifestPath)
    $ManifestPath = [IO.Path]::GetFullPath($ManifestPath)
    $root = Split-Path -Parent ([IO.Path]::GetFullPath($ManifestPath))
    $manifest = Get-Content -LiteralPath $ManifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    function Resolve-ResourcePath {
        param([string]$Name)
        if (-not $Name -or $Name.Contains('\') -or $Name.Contains(':') -or
            $Name.StartsWith('/') -or @($Name.Split('/') | Where-Object { $_ -in @('', '.', '..') }).Count) {
            throw "Unsafe resource path: $Name"
        }
        $path = Join-Path $root $Name
        if (-not $inventory.ContainsKey($path)) { throw "Missing resource path: $Name" }
        return $path
    }
    $inventory = [Collections.Generic.Dictionary[string,object]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($item in (Get-ChildItem -LiteralPath $root -Recurse -Force)) {
        if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw "Linked resource path: $($item.FullName)" }
        $inventory.Add($item.FullName, $item)
    }
    $entrypoints = @{}
    $critical = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    $editableConfigs = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($entry in $manifest.entrypoints.PSObject.Properties) {
        $entrypoints[$entry.Name] = Resolve-ResourcePath -Name ([string]$entry.Value)
        [void]$critical.Add([string]$entry.Value)
        if ($entry.Name -ne 'cn-asset-map' -and ([string]$entry.Value).EndsWith('.json', [StringComparison]::OrdinalIgnoreCase)) {
            [void]$editableConfigs.Add([string]$entry.Value)
        }
    }
    $listed = [Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
    foreach ($row in $manifest.files) {
        $name = [string]$row.path
        $path = Resolve-ResourcePath -Name $name
        $item = $inventory[$path]
        if (-not $listed.Add($name) -or $item.PSIsContainer) { throw "Invalid resource inventory entry: $name" }
        # Editable masters are validated by their loaders; the manifest does not lock their bytes.
        if ($editableConfigs.Contains($name)) { continue }
        if ($item.Length -ne $row.bytes -or [string]$row.sha256 -notmatch '^[0-9a-f]{64}$') {
            throw "Invalid fixed-resource inventory entry: $name"
        }
        if ($critical.Contains($name) -and
            (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant() -ne $row.sha256) {
            throw "Resource entrypoint hash mismatch: $name"
        }
    }
    foreach ($name in $critical) {
        if (-not $inventory[(Join-Path $root $name)].PSIsContainer -and -not $listed.Contains($name)) {
            throw "Unregistered resource entrypoint: $name"
        }
    }
    $actual = @($inventory.Values | Where-Object { -not $_.PSIsContainer -and $_.FullName -ne $ManifestPath })
    if ($actual.Count -ne $listed.Count) { throw 'Resource inventory differs from manifest.' }
    return @{ Manifest = $manifest; Entrypoints = $entrypoints }
}
