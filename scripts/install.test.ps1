$ErrorActionPreference = 'Stop'
$installer = Join-Path (Split-Path $PSScriptRoot -Parent) 'install.ps1'
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$fixture = Join-Path $tempRoot ('thermal-test-' + [guid]::NewGuid().ToString('N'))
$savedArchitecture = $env:PROCESSOR_ARCHITECTURE
$savedNativeArchitecture = $env:PROCESSOR_ARCHITEW6432
$script:failDownload = $false
function curl.exe {
    if ($script:failDownload) { $global:LASTEXITCODE = 22; return }
    $url = @($args | Where-Object { $_ -like 'https://*' })[0]
    if ($url -notlike 'https://github.com/test-owner/thermal/releases/download/v1.2.3/*') { throw "Unexpected URL: $url" }
    $outIndex = [array]::IndexOf($args, '-o')
    Copy-Item -LiteralPath (Join-Path $fixture ($url.Split('/')[-1])) -Destination $args[$outIndex + 1]
    $global:LASTEXITCODE = 0
}
function Invoke-TestInstall {
    & $installer -Repository test-owner/thermal -Version v1.2.3 -InstallDir (Join-Path $fixture 'install with spaces')
}
function Assert-Failure {
    $failed = $false
    try { Invoke-TestInstall } catch { $failed = $true }
    if (-not $failed) { throw 'Expected installation to fail.' }
    if ([IO.File]::ReadAllText((Join-Path $fixture 'install with spaces\thermal.exe')) -ne 'previous installation') { throw 'Failed installation replaced the existing executable.' }
}
try {
    $null = New-Item -ItemType Directory -Path (Join-Path $fixture 'payload')
    [IO.File]::WriteAllText((Join-Path $fixture 'payload\thermal.exe'), 'fixture executable')
    $checksums = @()
    foreach ($arch in @('amd64', 'arm64')) {
        $name = "thermal-windows-$arch.zip"
        Compress-Archive -LiteralPath (Join-Path $fixture 'payload\thermal.exe') -DestinationPath (Join-Path $fixture $name)
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $fixture $name)).Hash.ToLowerInvariant()
        $checksums += "$hash  $name"
    }
    $checksums | Set-Content -LiteralPath (Join-Path $fixture 'checksums.txt') -Encoding ASCII
    foreach ($arch in @('AMD64', 'ARM64')) {
        $env:PROCESSOR_ARCHITECTURE = $arch
        $env:PROCESSOR_ARCHITEW6432 = ''
        Invoke-TestInstall
        if ([IO.File]::ReadAllText((Join-Path $fixture 'install with spaces\thermal.exe')) -ne 'fixture executable') { throw 'Incorrect installed executable.' }
    }
    # A 32-bit shell on ARM64 must still select the native ARM64 release.
    $env:PROCESSOR_ARCHITECTURE = 'x86'
    $env:PROCESSOR_ARCHITEW6432 = 'ARM64'
    Invoke-TestInstall
    [IO.File]::WriteAllText((Join-Path $fixture 'install with spaces\thermal.exe'), 'previous installation')
    Add-Content -LiteralPath (Join-Path $fixture 'thermal-windows-arm64.zip') -Value 'corrupt'
    Assert-Failure
    $script:failDownload = $true
    Assert-Failure
    $script:failDownload = $false
    $env:PROCESSOR_ARCHITEW6432 = ''
    Assert-Failure
    $env:PROCESSOR_ARCHITECTURE = 'AMD64'
    Set-Content -LiteralPath (Join-Path $fixture 'checksums.txt') -Value ''
    Assert-Failure
    Write-Output 'Windows installer tests passed.'
} finally {
    $env:PROCESSOR_ARCHITECTURE = $savedArchitecture
    $env:PROCESSOR_ARCHITEW6432 = $savedNativeArchitecture
    Remove-Item Function:\curl.exe
    $resolvedFixture = [IO.Path]::GetFullPath($fixture)
    if ($resolvedFixture.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and (Split-Path $resolvedFixture -Leaf) -match '^thermal-test-[0-9a-f]{32}$') {
        if (Test-Path -LiteralPath $resolvedFixture) { Remove-Item -LiteralPath $resolvedFixture -Recurse -Force }
    }
}
