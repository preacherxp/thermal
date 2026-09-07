# Download this release asset with curl.exe, then run it with PowerShell.
[CmdletBinding()]
param(
    [string]$Repository = $(if ($env:THERMAL_REPOSITORY) { $env:THERMAL_REPOSITORY } else { 'preacherxp/thermal' }),
    [string]$Version = $(if ($env:THERMAL_VERSION) { $env:THERMAL_VERSION } else { 'v0.5.0' }),
    [string]$InstallDir = $(if ($env:THERMAL_INSTALL_DIR) { $env:THERMAL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Thermal\bin' })
)
$ErrorActionPreference = 'Stop'
if ($env:OS -ne 'Windows_NT') { throw 'Use install.sh on Linux or macOS.' }
if ($Version -notmatch '^v[0-9][A-Za-z0-9._-]*$') { throw 'Use the installer attached to a release, or set THERMAL_VERSION to a tag such as v0.5.0.' }
if ($Repository -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') { throw 'Repository must be owner/repo.' }
$nativeArch = $env:PROCESSOR_ARCHITEW6432
if (-not $nativeArch) { $nativeArch = $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($nativeArch) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture: $nativeArch" }
}
$archive = "thermal-windows-$arch.zip"
$base = "https://github.com/$Repository/releases/download/$Version"
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$tempDir = Join-Path $tempRoot ('thermal-install-' + [guid]::NewGuid().ToString('N'))
$staged = $null
try {
    $null = New-Item -ItemType Directory -Path $tempDir
    foreach ($name in @($archive, 'checksums.txt')) {
        & curl.exe --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$base/$name" -o (Join-Path $tempDir $name)
        if ($LASTEXITCODE -ne 0) { throw "Download failed: $name" }
    }
    $entries = @(Get-Content -LiteralPath (Join-Path $tempDir 'checksums.txt') | Where-Object { $_ -match ('^[0-9a-fA-F]{64}  ' + [regex]::Escape($archive) + '$') })
    if ($entries.Count -ne 1) { throw 'Missing or duplicate archive checksum.' }
    $expected = $entries[0].Substring(0, 64)
    # Use .NET directly: Get-FileHash is not available in every PowerShell host.
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $stream = [IO.File]::OpenRead((Join-Path $tempDir $archive))
        try {
            $actual = [BitConverter]::ToString($sha256.ComputeHash($stream)).Replace('-', '')
        } finally { $stream.Dispose() }
    } finally { $sha256.Dispose() }
    if ($actual -ne $expected) { throw 'Checksum mismatch; installation cancelled.' }
    $InstallDir = [IO.Path]::GetFullPath($InstallDir)
    $null = [IO.Directory]::CreateDirectory($InstallDir)
    $staged = Join-Path $InstallDir ('.thermal-' + [guid]::NewGuid().ToString('N') + '.exe')
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead((Join-Path $tempDir $archive))
    try {
        $binary = $zip.GetEntry('thermal.exe')
        if (-not $binary) { throw 'Release contains no executable.' }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($binary, $staged)
    } finally { $zip.Dispose() }
    $destination = Join-Path $InstallDir 'thermal.exe'
    if (Test-Path -LiteralPath $destination -PathType Container) { throw 'Install target is a directory.' }
    Move-Item -LiteralPath $staged -Destination $destination -Force
    $staged = $null
    Write-Output "Installed Thermal $Version to $destination"
    if (($env:PATH -split ';') -contains $InstallDir) { Write-Output 'Run: thermal --help' }
    else { Write-Output "Add $InstallDir to your user PATH, or run: & `"$destination`" --help" }
} finally {
    if ($staged -and (Test-Path -LiteralPath $staged)) { Remove-Item -LiteralPath $staged -Force }
    $resolvedTemp = [IO.Path]::GetFullPath($tempDir)
    if ($resolvedTemp.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolvedTemp -Leaf) -match '^thermal-install-[0-9a-f]{32}$') {
        if (Test-Path -LiteralPath $resolvedTemp) { Remove-Item -LiteralPath $resolvedTemp -Recurse -Force }
    }
}
