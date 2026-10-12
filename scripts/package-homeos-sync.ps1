# Build a self-contained Windows sync bundle. Run on Windows with Go installed.
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$backend = Join-Path $repo 'backend'
$output = Join-Path $backend 'bin\HomeDrive-Sync-Windows'
$archive = Join-Path $backend 'bin\HomeDrive-Sync-Windows.zip'
if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) { throw 'Go is required to package the sync client.' }
New-Item -ItemType Directory -Path $output -Force | Out-Null
Push-Location $backend
try {
    & go.exe build -o (Join-Path $output 'homeos-sync.exe') ./cmd/homeos-sync
    if ($LASTEXITCODE -ne 0) { throw 'Sync client build failed.' }
} finally { Pop-Location }
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'homeos-sync-desktop.ps1') -Destination $output -Force
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'HomeDrive Sync.vbs') -Destination $output -Force
Copy-Item -LiteralPath (Join-Path $repo 'docs\homedrive-sync-desktop.md') -Destination (Join-Path $output 'README.md') -Force
if (Test-Path -LiteralPath $archive) { Remove-Item -LiteralPath $archive }
Compress-Archive -LiteralPath $output -DestinationPath $archive
Write-Host "HomeDrive Sync bundle: $archive"
