# Run the Windows HomeOS server on port 8091 against Docker PostgreSQL on host port 5433.
# Redis is detected by the shared launcher on host port 6379.
& (Join-Path $PSScriptRoot 'start-homeos-postgres.ps1') -DatabasePort 5433
