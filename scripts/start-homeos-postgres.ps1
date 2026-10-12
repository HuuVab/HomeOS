# Run HomeOS against the local PostgreSQL 17 database and existing HomeDrive files.
# The password file is intentionally ignored by Git.
$backend = (Resolve-Path (Join-Path $PSScriptRoot '..\backend')).Path
$web = (Resolve-Path (Join-Path $PSScriptRoot '..\frontend\dist')).Path
$passwordFile = Join-Path $backend 'secrets\postgres-password'
if (-not (Test-Path -LiteralPath $passwordFile -PathType Leaf)) {
    throw "PostgreSQL password file is missing: $passwordFile"
}
$env:HOMEOS_DATABASE_URL = 'postgres://homeos_app@127.0.0.1:5432/homeos?sslmode=disable'
$env:HOMEOS_DATABASE_PASSWORD_FILE = $passwordFile
$env:HOMEOS_DATA_DIR = Join-Path $backend 'data'
$env:HOMEOS_WEB_DIR = $web
$env:HOMEOS_BIND_ADDR = '127.0.0.1'
$pgReady = 'C:\Program Files\PostgreSQL\17\bin\pg_isready.exe'
if (-not (Test-Path -LiteralPath $pgReady -PathType Leaf)) {
    throw "PostgreSQL 17 is not installed at the expected location: $pgReady"
}
$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    & $pgReady -q -h 127.0.0.1 -p 5432
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Seconds 2
}
if (-not $ready) { throw 'PostgreSQL 17 did not become ready on 127.0.0.1:5432' }
$binary = Join-Path $backend 'bin\homeos.exe'
Push-Location $backend
try {
    if (Test-Path -LiteralPath $binary -PathType Leaf) {
        & $binary
    } else {
        & go run ./cmd/homeos
    }
} finally {
    Pop-Location
}
