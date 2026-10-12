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
Push-Location $backend
try {
    & go run ./cmd/homeos
} finally {
    Pop-Location
}
