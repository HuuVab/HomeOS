# Run HomeOS against PostgreSQL and existing HomeDrive files.
# The password file is intentionally ignored by Git.
param([ValidateRange(1, 65535)][int]$DatabasePort = 5432)
$backend = (Resolve-Path (Join-Path $PSScriptRoot '..\backend')).Path
$web = (Resolve-Path (Join-Path $PSScriptRoot '..\frontend\dist')).Path
$passwordFile = Join-Path $backend 'secrets\postgres-password'
if (-not (Test-Path -LiteralPath $passwordFile -PathType Leaf)) {
    throw "PostgreSQL password file is missing: $passwordFile"
}
$env:HOMEOS_DATABASE_URL = "postgres://homeos_app@127.0.0.1:$DatabasePort/homeos?sslmode=disable"
$env:HOMEOS_DATABASE_PASSWORD_FILE = $passwordFile
$env:HOMEOS_DATA_DIR = Join-Path $backend 'data'
$env:HOMEOS_WEB_DIR = $web
$env:HOMEOS_BIND_ADDR = '127.0.0.1'
$env:HOMEOS_PORT = '8091'
$ready = $false
for ($attempt = 0; $attempt -lt 30; $attempt++) {
    $socket = [Net.Sockets.TcpClient]::new()
    try {
        $connect = $socket.BeginConnect('127.0.0.1', $DatabasePort, $null, $null)
        if ($connect.AsyncWaitHandle.WaitOne(1000)) {
            try { $socket.EndConnect($connect); $ready = $true; break } catch { }
        }
    } finally {
        $socket.Dispose()
    }
    Start-Sleep -Seconds 2
}
if (-not $ready) { throw "PostgreSQL did not become reachable on 127.0.0.1:$DatabasePort" }
$redisSocket = [Net.Sockets.TcpClient]::new()
try {
    $connect = $redisSocket.BeginConnect('127.0.0.1', 6379, $null, $null)
    if ($connect.AsyncWaitHandle.WaitOne(1000)) {
        try {
            $redisSocket.EndConnect($connect)
            $env:HOMEOS_REDIS_URL = 'redis://127.0.0.1:6379/0'
            Write-Host 'HomeOS Redis acceleration enabled on 127.0.0.1:6379.'
        } catch {
            Remove-Item Env:HOMEOS_REDIS_URL -ErrorAction SilentlyContinue
            Write-Host 'Redis is unavailable; HomeOS will use PostgreSQL-only metadata reads.'
        }
    } else {
        Remove-Item Env:HOMEOS_REDIS_URL -ErrorAction SilentlyContinue
        Write-Host 'Redis is unavailable; HomeOS will use PostgreSQL-only metadata reads.'
    }
} finally {
    $redisSocket.Dispose()
}
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
