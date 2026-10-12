# First-install setup for native Windows HomeOS + PostgreSQL 17.
# Run interactively from an administrator PowerShell when PostgreSQL is absent.
# Existing databases, roles, password files, and unrelated tasks are never replaced.
$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$backend = Join-Path $repo 'backend'
$frontend = Join-Path $repo 'frontend'
$launcher = Join-Path $PSScriptRoot 'start-homeos-postgres.ps1'
$secret = Join-Path $backend 'secrets\postgres-password'
$psql = 'C:\Program Files\PostgreSQL\17\bin\psql.exe'
$serviceName = 'postgresql-x64-17'
$taskName = 'HomeOS PostgreSQL'
$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
    throw 'This setup is for Windows only.'
}

$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if (-not $service) {
    if (-not (Get-Command winget.exe -ErrorAction SilentlyContinue)) {
        throw 'Install Windows Package Manager (WinGet) before running this setup.'
    }
    Write-Host 'Installing PostgreSQL 17. Complete its installer, including the postgres password prompt.'
    & winget.exe install --id PostgreSQL.PostgreSQL.17 --exact --source winget --interactive --no-upgrade --accept-package-agreements --accept-source-agreements
    if ($LASTEXITCODE -ne 0) { throw "PostgreSQL installation failed (exit $LASTEXITCODE)." }
    $service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
    if (-not $service) { throw 'PostgreSQL 17 installer completed, but its Windows service was not found.' }
}
if ($service.StartType -ne 'Automatic') {
    Set-Service -Name $serviceName -StartupType Automatic
}
if ($service.Status -ne 'Running') {
    Start-Service -Name $serviceName
}
if (-not (Test-Path -LiteralPath $psql -PathType Leaf)) {
    throw "PostgreSQL 17 command-line tools were not found: $psql"
}

function Invoke-LocalPsql([string]$user, [string]$database, [string]$password, [string]$query) {
    $previous = $env:PGPASSWORD
    try {
        $env:PGPASSWORD = $password
        $result = & $psql -w -h 127.0.0.1 -p 5432 -U $user -d $database -v ON_ERROR_STOP=1 -Atqc $query 2>$null
        if ($LASTEXITCODE -ne 0) { throw "PostgreSQL query failed for $user on $database." }
        return $result
    } finally {
        if ($null -eq $previous) { Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue }
        else { $env:PGPASSWORD = $previous }
    }
}

$appReady = $false
if (Test-Path -LiteralPath $secret -PathType Leaf) {
    $appPassword = (Get-Content -LiteralPath $secret -Raw).Trim()
    try {
        $appReady = (Invoke-LocalPsql 'homeos_app' 'homeos' $appPassword 'SELECT 1') -eq '1'
    } catch {
        # Diagnose existing/partial setups with the administrator account below.
    }
}
if (-not $appReady) {
    $secure = Read-Host 'PostgreSQL postgres password (not saved)' -AsSecureString
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try { $adminPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
    try {
        $roleExists = (Invoke-LocalPsql 'postgres' 'postgres' $adminPassword "SELECT count(*) FROM pg_roles WHERE rolname = 'homeos_app'") -eq '1'
        $dbExists = (Invoke-LocalPsql 'postgres' 'postgres' $adminPassword "SELECT count(*) FROM pg_database WHERE datname = 'homeos'") -eq '1'
        if ($roleExists -and -not (Test-Path -LiteralPath $secret -PathType Leaf)) {
            throw 'homeos_app already exists but its password file is missing; refusing to reset its password.'
        }
        if ($dbExists -and -not $roleExists) {
            throw 'The homeos database already exists without homeos_app; refusing to change its owner.'
        }
        if (-not (Test-Path -LiteralPath $secret -PathType Leaf)) {
            New-Item -ItemType Directory -Path (Split-Path $secret) -Force | Out-Null
            $appPassword = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(36))
            $stream = [IO.File]::Open($secret, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
            try {
                $bytes = [Text.Encoding]::UTF8.GetBytes($appPassword + [Environment]::NewLine)
                $stream.Write($bytes, 0, $bytes.Length)
            } finally { $stream.Dispose() }
            & icacls.exe $secret /inheritance:r /grant:r "${identity}:(R)" | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Could not restrict the PostgreSQL password file ACL.' }
        }
        if (-not $roleExists) {
            $previous = $env:PGPASSWORD
            try {
                $env:PGPASSWORD = $adminPassword
                "CREATE ROLE homeos_app LOGIN PASSWORD '$appPassword';" | & $psql -w -h 127.0.0.1 -p 5432 -U postgres -d postgres -v ON_ERROR_STOP=1 -f - | Out-Null
                if ($LASTEXITCODE -ne 0) { throw 'Could not create homeos_app.' }
            } finally {
                if ($null -eq $previous) { Remove-Item Env:PGPASSWORD -ErrorAction SilentlyContinue }
                else { $env:PGPASSWORD = $previous }
            }
        }
        if (-not $dbExists) {
            [void](Invoke-LocalPsql 'postgres' 'postgres' $adminPassword 'CREATE DATABASE homeos OWNER homeos_app')
        }
    } finally { $adminPassword = $null }
    $appPassword = (Get-Content -LiteralPath $secret -Raw).Trim()
    if ((Invoke-LocalPsql 'homeos_app' 'homeos' $appPassword 'SELECT 1') -ne '1') {
        throw 'The dedicated HomeOS PostgreSQL login could not be verified.'
    }
}

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) { throw 'Go is required to build HomeOS.' }
if (-not (Get-Command npm.cmd -ErrorAction SilentlyContinue)) { throw 'Node.js/npm is required to build HomeOS.' }
Push-Location $frontend
try {
    if (-not (Test-Path -LiteralPath 'node_modules' -PathType Container)) {
        & npm.cmd ci
        if ($LASTEXITCODE -ne 0) { throw 'npm ci failed.' }
    }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
} finally { Pop-Location }
Push-Location $backend
try {
    New-Item -ItemType Directory -Path 'bin' -Force | Out-Null
    & go.exe build -o 'bin\homeos.exe' ./cmd/homeos
    if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }
} finally { Pop-Location }

$env:HOMEOS_DATABASE_URL = 'postgres://homeos_app@127.0.0.1:5432/homeos?sslmode=disable'
$env:HOMEOS_DATABASE_PASSWORD_FILE = $secret
$env:HOMEOS_DATA_DIR = Join-Path $backend 'data'
$userTable = Invoke-LocalPsql 'homeos_app' 'homeos' $appPassword "SELECT to_regclass('public.users')"
if (-not $userTable) {
    $username = Read-Host 'New HomeOS administrator username (3-32 letters, digits, underscore)'
    if ($username -cnotmatch '^[A-Za-z0-9_]{3,32}$') { throw 'Invalid administrator username.' }
    Push-Location $backend
    try {
        & (Join-Path $backend 'bin\homeos.exe') create-admin $username
        if ($LASTEXITCODE -ne 0) { throw 'Could not create the HomeOS administrator.' }
    } finally { Pop-Location }
}

$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
$restartTask = $existing -and $existing.State -eq 'Running'
if ($existing) {
    if ($existing.Actions.Count -ne 1 -or $existing.Actions[0].Arguments -notlike "*$launcher*" -or
        ($existing.Principal.UserId -ne $identity -and $existing.Principal.UserId -ne $env:USERNAME)) {
        throw "A different scheduled task named '$taskName' already exists; it was not changed."
    }
} else {
    $powerShell = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $action = New-ScheduledTaskAction -Execute $powerShell -Argument "-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$launcher`"" -WorkingDirectory $repo
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity
    $principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited
    $settings = New-ScheduledTaskSettingsSet -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Start HomeOS with PostgreSQL 17 when this user signs in' | Out-Null
}
if ($restartTask) {
    Stop-ScheduledTask -TaskName $taskName
    $listener = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($listener) {
        $process = Get-CimInstance Win32_Process -Filter "ProcessId=$($listener.OwningProcess)"
        $expectedBinary = Join-Path $backend 'bin\homeos.exe'
        if (-not $process -or -not [string]::Equals($process.ExecutablePath, $expectedBinary, [StringComparison]::OrdinalIgnoreCase)) {
            throw 'Port 8080 belongs to an unexpected process; refusing to stop it.'
        }
        Stop-Process -Id $listener.OwningProcess -Force
    }
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        if (-not (Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 500
    }
    if (Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue) {
        throw 'The previous HomeOS instance did not release port 8080; refusing to launch a duplicate.'
    }
    Start-ScheduledTask -TaskName $taskName
} elseif (-not (Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue)) {
    Start-ScheduledTask -TaskName $taskName
}
Write-Host 'HomeOS PostgreSQL setup is ready. PostgreSQL starts with Windows; HomeOS starts when this user signs in.'
