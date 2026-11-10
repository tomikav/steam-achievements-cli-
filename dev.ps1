# Developer shortcut: format check, vet, test, install and smoke-test in one go.
#
#   .\dev.ps1            build + test + install + doctor + list
#   .\dev.ps1 -Quick     build + install only
#
# Installs with `go install`, so `steam` works from any folder afterwards
# (Go's bin folder, usually %USERPROFILE%\go\bin, must be on PATH).

param([switch]$Quick)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

function Step($name, [scriptblock]$cmd) {
    Write-Host "==> $name" -ForegroundColor Cyan
    & $cmd
    if ($LASTEXITCODE -ne 0) { Write-Host "FAILED: $name" -ForegroundColor Red; exit $LASTEXITCODE }
}

if (-not $Quick) {
    Step 'gofmt' { $bad = gofmt -l .; if ($bad) { Write-Host $bad; $global:LASTEXITCODE = 1 } else { $global:LASTEXITCODE = 0 } }
    # -p 2 keeps the number of parallel compiler processes low, which avoids
    # out-of-memory crashes of the Go toolchain on machines with little free RAM.
    Step 'vet'   { go vet -p 2 ./... }
    Step 'test'  { go test -p 2 ./... }
}
Step 'install' { go install -p 2 ./cmd/steam }

$bin = Join-Path (go env GOPATH) 'bin'
if (($env:Path -split ';') -notcontains $bin) {
    $env:Path = "$env:Path;$bin"
    Write-Host "note: $bin is not on your PATH. Add it once with:" -ForegroundColor Yellow
    Write-Host "  [Environment]::SetEnvironmentVariable('Path', `"`$([Environment]::GetEnvironmentVariable('Path','User'));$bin`", 'User')"
}

if (-not $Quick) {
    Step 'doctor' { steam doctor }
    Step 'list'   { steam list --min 20 }
}
