# steam-achievements-cli installer for Windows.
#
#   irm https://raw.githubusercontent.com/tomikav/steam-achievements-cli-/main/install.ps1 | iex
#
# Downloads the latest release, verifies its SHA-256 checksum, installs
# steam.exe to %LOCALAPPDATA%\Programs\steam-achievements-cli and adds that
# folder to the user PATH. No administrator rights needed.
#
# Optional: $env:STEAM_CLI_VERSION = "v1.1.0" to pin a version.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo    = 'tomikav/steam-achievements-cli-'
$asset   = 'steam-achievements-cli_windows_amd64.zip'
$destDir = Join-Path $env:LOCALAPPDATA 'Programs\steam-achievements-cli'

if ($env:STEAM_CLI_VERSION) {
    $base = "https://github.com/$repo/releases/download/$($env:STEAM_CLI_VERSION)"
} else {
    $base = "https://github.com/$repo/releases/latest/download"
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("steam-cli-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    Write-Host "Downloading $asset"
    Invoke-WebRequest "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match ("\s" + [regex]::Escape($asset) + "$") }
    if (-not $line) { throw "checksums.txt has no entry for $asset" }
    $expected = ($line -split '\s+')[0].ToLower()
    $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { throw "Checksum mismatch for $asset. Aborting." }
    Write-Host "Checksum verified"

    Expand-Archive (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'x') -Force
    New-Item -ItemType Directory -Force -Path $destDir | Out-Null
    Copy-Item (Join-Path $tmp 'x\steam.exe') (Join-Path $destDir 'steam.exe') -Force
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$parts = @()
if ($userPath) { $parts = $userPath -split ';' | Where-Object { $_ } }
if ($parts -notcontains $destDir) {
    [Environment]::SetEnvironmentVariable('Path', (($parts + $destDir) -join ';'), 'User')
    $env:Path = "$env:Path;$destDir"
    Write-Host "Added $destDir to your user PATH"
}

Write-Host ""
& (Join-Path $destDir 'steam.exe') version
Write-Host ""
Write-Host "Next steps:"
Write-Host "  steam config set key YOUR_API_KEY     (https://steamcommunity.com/dev/apikey)"
Write-Host "  steam config set id  YOUR_STEAMID64"
Write-Host "  steam doctor"
Write-Host ""
Write-Host "Open a new terminal if 'steam' is not found."
